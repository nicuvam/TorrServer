package qbitsync

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"server/settings"
)

func TestPrettyName(t *testing.T) {
	tests := []struct{ in, want string }{
		{"Ted.Lasso.S04.2160p.ATVP.WEB-DL.SDR.H.265", "Ted Lasso S04 2160p ATVP WEB-DL SDR H 265"},
		{"Some_Show_S01_1080p", "Some Show S01 1080p"},
		{"Малой (Копы) / Сезон: 1 / Серии: 1-16 из 16", "Малой (Копы) / Сезон: 1 / Серии: 1-16 из 16"},
		{"", ""},
	}
	for _, test := range tests {
		if got := prettyName(test.in); got != test.want {
			t.Fatalf("prettyName(%q) = %q, want %q", test.in, got, test.want)
		}
	}
}

func TestPosterQuery(t *testing.T) {
	tests := []struct{ in, want string }{
		{"Малой (Копы) / Сезон: 1 / Серии: 1-16 из 16 (Иван Макаревич) [2026, комедия, WEB-DL]", "Малой"},
		{"Ted.Lasso.S04.2160p.ATVP.WEB-DL.SDR.H.265", "Ted Lasso"},
		{"Reacher.S04.2160p.AMZN.WEB-DL.DV.HDR.H.265", "Reacher"},
		{"The Wire / Прослушка [Сезоны 1-5]", "The Wire"},
		{"One Two Three Four Five Six", "One Two Three Four"},
		{"", ""},
	}
	for _, test := range tests {
		if got := posterQuery(test.in); got != test.want {
			t.Fatalf("posterQuery(%q) = %q, want %q", test.in, got, test.want)
		}
	}
}

func TestQueryLanguage(t *testing.T) {
	if got := queryLanguage("Малой"); got != "ru" {
		t.Fatalf("cyrillic language = %q", got)
	}
	if got := queryLanguage("Ted Lasso"); got != "en" {
		t.Fatalf("latin language = %q", got)
	}
}

func TestTmdbAPIURL(t *testing.T) {
	tests := []struct{ in, want string }{
		{"", "https://api.themoviedb.org"},
		{"https://api.themoviedb.org", "https://api.themoviedb.org"},
		{"https://api.themoviedb.org/3", "https://api.themoviedb.org"},
		{"proxy.example.com/3/", "https://proxy.example.com"},
	}
	for _, test := range tests {
		if got := tmdbAPIURL(test.in); got != test.want {
			t.Fatalf("tmdbAPIURL(%q) = %q, want %q", test.in, got, test.want)
		}
	}
}

func TestLookupPoster(t *testing.T) {
	var lastQuery, lastLanguage string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		lastLanguage = r.URL.Query().Get("language")
		switch r.URL.Path {
		case "/3/find/tt1234567":
			w.Write([]byte(`{"tv_results":[{"poster_path":"/tv.jpg"}]}`))
		case "/3/search/multi":
			lastQuery = r.URL.Query().Get("query")
			if lastQuery == "Малой" || lastQuery == "Ted Lasso" {
				w.Write([]byte(`{"results":[{"poster_path":"/multi.jpg"}]}`))
				return
			}
			w.Write([]byte(`{"results":[]}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	prev := settings.BTsets
	defer func() { settings.BTsets = prev }()
	settings.BTsets = &settings.BTSets{TMDBSettings: settings.TMDBConfig{
		APIKey:     "k",
		APIURL:     srv.URL,
		ImageURL:   "https://image.tmdb.org",
		ImageURLRu: "https://imagetmdb.com",
	}}

	if got := lookupPoster("tt1234567", ""); got != "https://image.tmdb.org/t/p/w300/tv.jpg" {
		t.Fatalf("find lookup = %q", got)
	}
	if got := lookupPoster("", "Ted Lasso"); got != "https://image.tmdb.org/t/p/w300/multi.jpg" || lastLanguage != "en" {
		t.Fatalf("en lookup = %q lang %q", got, lastLanguage)
	}
	if got := lookupPoster("", "Малой"); got != "https://imagetmdb.com/t/p/w300/multi.jpg" || lastLanguage != "ru" {
		t.Fatalf("ru lookup = %q lang %q", got, lastLanguage)
	}
	if got := lookupPoster("", "Unknown Thing"); got != "" {
		t.Fatalf("miss lookup = %q", got)
	}
	settings.BTsets.TMDBSettings.APIKey = ""
	if got := lookupPoster("tt1234567", "Ted Lasso"); got != "" {
		t.Fatalf("no key lookup = %q", got)
	}
}
