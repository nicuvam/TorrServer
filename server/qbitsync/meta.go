package qbitsync

import (
	"encoding/json"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
	"unicode"

	"server/rutor"
	"server/settings"
)

var metaHTTP = &http.Client{Timeout: 5 * time.Second}

func resolveMeta(hashHex, rawName string) (string, string) {
	title := ""
	imdb := ""
	if found := rutor.FindByHash(hashHex); found != nil {
		title = found.Title
		imdb = found.IMDBID
	}
	if title == "" {
		title = prettyName(rawName)
	}
	return title, lookupPoster(imdb, posterQuery(rawName))
}

func prettyName(name string) string {
	if strings.Contains(name, " ") {
		return name
	}
	cleaned := strings.NewReplacer(".", " ", "_", " ").Replace(name)
	return strings.Join(strings.Fields(cleaned), " ")
}

var releaseTagRe = regexp.MustCompile(`(?i)^((19|20)\d{2}|s\d{1,2}(e\d{1,3})?|2160p|1080p|720p|480p|4k|uhd|web|webrip|web-dl|webdl|bluray|bdrip|bdremux|remux|hdtv|dvdrip|hdr|hdr10|dv|sdr|atvp|amzn|nf|dsnp|hmax|[hx]\.?26[45]|hevc|avc|aac|ddp?\d?)$`)

func releaseTitle(base string) string {
	var words []string
	for _, word := range strings.Fields(prettyName(base)) {
		if releaseTagRe.MatchString(word) {
			break
		}
		words = append(words, word)
	}
	return strings.Join(words, " ")
}

const (
	posterSearchMaxLen   = 50
	posterSearchMaxWords = 4
)

func posterQuery(fullTitle string) string {
	base := strings.TrimSpace(fullTitle)
	if base == "" {
		return ""
	}
	for _, sep := range []string{" [", " (", " / "} {
		if i := strings.Index(base, sep); i > 0 {
			base = strings.TrimSpace(base[:i])
		}
	}
	if parsed := releaseTitle(base); parsed != "" {
		base = parsed
	}
	words := strings.Fields(base)
	if len(words) > posterSearchMaxWords {
		words = words[:posterSearchMaxWords]
	}
	byWords := strings.Join(words, " ")
	if len(byWords) <= posterSearchMaxLen {
		return byWords
	}
	cut := byWords[:posterSearchMaxLen]
	if lastSpace := strings.LastIndex(cut, " "); lastSpace > 0 {
		cut = cut[:lastSpace]
	}
	return strings.TrimSpace(cut)
}

func queryLanguage(query string) string {
	for _, r := range query {
		if unicode.Is(unicode.Cyrillic, r) {
			return "ru"
		}
	}
	return "en"
}

type tmdbResult struct {
	PosterPath string `json:"poster_path"`
}

type tmdbResponse struct {
	Results      []tmdbResult `json:"results"`
	MovieResults []tmdbResult `json:"movie_results"`
	TvResults    []tmdbResult `json:"tv_results"`
}

func normalizeHost(raw, fallback string) string {
	trimmed := strings.TrimRight(strings.TrimSpace(raw), "/")
	if trimmed == "" {
		trimmed = fallback
	}
	if !strings.HasPrefix(trimmed, "http://") && !strings.HasPrefix(trimmed, "https://") {
		trimmed = "https://" + strings.TrimPrefix(trimmed, "//")
	}
	return trimmed
}

func tmdbAPIURL(raw string) string {
	return strings.TrimSuffix(normalizeHost(raw, "https://api.themoviedb.org"), "/3")
}

func lookupPoster(imdbID, query string) string {
	if settings.BTsets == nil {
		return ""
	}
	cfg := settings.BTsets.TMDBSettings
	if cfg.APIKey == "" {
		return ""
	}
	apiURL := tmdbAPIURL(cfg.APIURL)
	language := queryLanguage(query)

	params := url.Values{}
	params.Set("api_key", cfg.APIKey)
	params.Set("language", language)
	params.Set("include_image_language", language+",null,en")

	var reqURL string
	switch {
	case imdbID != "":
		params.Set("external_source", "imdb_id")
		reqURL = apiURL + "/3/find/" + url.PathEscape(imdbID) + "?" + params.Encode()
	case query != "":
		params.Set("query", query)
		reqURL = apiURL + "/3/search/multi?" + params.Encode()
	default:
		return ""
	}

	resp, err := metaHTTP.Get(reqURL)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return ""
	}

	var parsed tmdbResponse
	if json.NewDecoder(resp.Body).Decode(&parsed) != nil {
		return ""
	}

	imgHost := normalizeHost(cfg.ImageURL, "https://image.tmdb.org")
	if language == "ru" {
		imgHost = normalizeHost(cfg.ImageURLRu, "https://imagetmdb.com")
	}
	for _, group := range [][]tmdbResult{parsed.TvResults, parsed.MovieResults, parsed.Results} {
		for _, result := range group {
			if result.PosterPath != "" {
				return imgHost + "/t/p/w300" + result.PosterPath
			}
		}
	}
	return ""
}
