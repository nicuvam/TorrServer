package qbitsync

import (
	"testing"

	"server/settings"
)

func TestMapPath(t *testing.T) {
	maps := []settings.QBitPathMap{
		{From: "/downloads", To: "/mnt/media"},
		{From: "/downloads/tv/", To: "/mnt/series"},
		{From: `C:\qb\data`, To: "/mnt/win"},
	}

	tests := []struct {
		name    string
		maps    []settings.QBitPathMap
		path    string
		want    string
		matched bool
	}{
		{name: "no maps", path: "/downloads/movie.mkv", want: "/downloads/movie.mkv"},
		{name: "empty path", maps: maps, path: "", want: ""},
		{name: "exact base", maps: maps, path: "/downloads", want: "/mnt/media", matched: true},
		{name: "trailing separator", maps: maps, path: "/downloads/", want: "/mnt/media", matched: true},
		{name: "child path", maps: maps, path: "/downloads/movies/a.mkv", want: "/mnt/media/movies/a.mkv", matched: true},
		{name: "longest prefix wins", maps: maps, path: "/downloads/tv/show/ep.mkv", want: "/mnt/series/show/ep.mkv", matched: true},
		{name: "partial component", maps: maps, path: "/downloads2/a.mkv", want: "/downloads2/a.mkv"},
		{name: "unmatched", maps: maps, path: "/other/a.mkv", want: "/other/a.mkv"},
		{name: "backslashes", maps: maps, path: `C:\qb\data\Show\ep.mkv`, want: "/mnt/win/Show/ep.mkv", matched: true},
		{name: "empty destination", maps: []settings.QBitPathMap{{From: "/downloads"}}, path: "/downloads/a.mkv", want: "/downloads/a.mkv"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, matched := mapPath(test.maps, test.path)
			if got != test.want || matched != test.matched {
				t.Fatalf("mapPath(%q) = (%q, %v), want (%q, %v)", test.path, got, matched, test.want, test.matched)
			}
		})
	}
}

func TestMapPathUsesSettings(t *testing.T) {
	previous := settings.BTsets
	t.Cleanup(func() { settings.BTsets = previous })

	settings.BTsets = &settings.BTSets{QBitSettings: settings.QBitConfig{
		PathMaps: []settings.QBitPathMap{{From: "/downloads", To: "/mnt/media"}},
	}}

	got, matched := MapPath("/downloads/show/ep.mkv")
	if got != "/mnt/media/show/ep.mkv" || !matched {
		t.Fatalf("MapPath = (%q, %v)", got, matched)
	}
}
