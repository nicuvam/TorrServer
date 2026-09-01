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
		{"Already Nice Name", "Already Nice Name"},
		{"", ""},
	}
	for _, test := range tests {
		if got := prettyName(test.in); got != test.want {
			t.Fatalf("prettyName(%q) = %q, want %q", test.in, got, test.want)
		}
	}
}

func TestSearchName(t *testing.T) {
	tests := []struct{ in, want string }{
		{"Ted.Lasso.S04.2160p.ATVP.WEB-DL.SDR.H.265", "Ted Lasso"},
		{"Reacher.S04.2160p.AMZN.WEB-DL.DV.HDR.H.265", "Reacher"},
		{"The.Matrix.1999.1080p.BluRay.x264", "The Matrix"},
		{"NoTagsAtAll", "NoTagsAtAll"},
	}
	for _, test := range tests {
		if got := searchName(test.in); got != test.want {
			t.Fatalf("searchName(%q) = %q, want %q", test.in, got, test.want)
		}
	}
}

func TestLookupPoster(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/3/find/tt1234567":
			w.Write([]byte(`{"tv_results":[{"poster_path":"/tv.jpg"}]}`))
		case "/3/search/multi":
			if r.URL.Query().Get("query") != "Ted Lasso" {
				w.Write([]byte(`{"results":[]}`))
				return
			}
			w.Write([]byte(`{"results":[{"poster_path":"/multi.jpg"}]}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	prev := settings.BTsets
	defer func() { settings.BTsets = prev }()
	settings.BTsets = &settings.BTSets{TMDBSettings: settings.TMDBConfig{
		APIKey: "k",
		APIURL: srv.URL,
	}}

	if got := lookupPoster("tt1234567", ""); got != "https://image.tmdb.org/t/p/w300/tv.jpg" {
		t.Fatalf("find lookup = %q", got)
	}
	if got := lookupPoster("", "Ted Lasso"); got != "https://image.tmdb.org/t/p/w300/multi.jpg" {
		t.Fatalf("search lookup = %q", got)
	}
	if got := lookupPoster("", "Unknown Thing"); got != "" {
		t.Fatalf("miss lookup = %q", got)
	}
	settings.BTsets.TMDBSettings.APIKey = ""
	if got := lookupPoster("tt1234567", "Ted Lasso"); got != "" {
		t.Fatalf("no key lookup = %q", got)
	}
}
