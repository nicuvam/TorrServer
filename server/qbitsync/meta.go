package qbitsync

import (
	"encoding/json"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

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
	return title, lookupPoster(imdb, searchName(rawName))
}

func prettyName(name string) string {
	if strings.Contains(name, " ") {
		return name
	}
	cleaned := strings.NewReplacer(".", " ", "_", " ").Replace(name)
	return strings.Join(strings.Fields(cleaned), " ")
}

var releaseTagRe = regexp.MustCompile(`(?i)^((19|20)\d{2}|s\d{1,2}(e\d{1,3})?|2160p|1080p|720p|480p|4k|uhd|web|webrip|web-dl|webdl|bluray|bdrip|bdremux|remux|hdtv|dvdrip|hdr|hdr10|dv|sdr|atvp|amzn|nf|dsnp|hmax|[hx]\.?26[45]|hevc|avc|aac|ddp?\d?)$`)

func searchName(rawName string) string {
	var words []string
	for _, word := range strings.Fields(prettyName(rawName)) {
		if releaseTagRe.MatchString(word) {
			break
		}
		words = append(words, word)
	}
	return strings.Join(words, " ")
}

type tmdbResult struct {
	PosterPath string `json:"poster_path"`
}

type tmdbResponse struct {
	Results      []tmdbResult `json:"results"`
	MovieResults []tmdbResult `json:"movie_results"`
	TvResults    []tmdbResult `json:"tv_results"`
}

func lookupPoster(imdbID, query string) string {
	if settings.BTsets == nil {
		return ""
	}
	cfg := settings.BTsets.TMDBSettings
	if cfg.APIKey == "" {
		return ""
	}
	apiURL := strings.TrimRight(cfg.APIURL, "/")
	if apiURL == "" {
		apiURL = "https://api.themoviedb.org"
	}

	var reqURL string
	switch {
	case imdbID != "":
		reqURL = apiURL + "/3/find/" + url.PathEscape(imdbID) + "?api_key=" + url.QueryEscape(cfg.APIKey) + "&external_source=imdb_id"
	case query != "":
		reqURL = apiURL + "/3/search/multi?api_key=" + url.QueryEscape(cfg.APIKey) + "&query=" + url.QueryEscape(query)
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

	imageURL := strings.TrimRight(cfg.ImageURL, "/")
	if imageURL == "" {
		imageURL = "https://image.tmdb.org"
	}
	for _, group := range [][]tmdbResult{parsed.TvResults, parsed.MovieResults, parsed.Results} {
		for _, result := range group {
			if result.PosterPath != "" {
				return imageURL + "/t/p/w300" + result.PosterPath
			}
		}
	}
	return ""
}
