package qbitsync

import (
	"path/filepath"
	"strings"

	"server/settings"
)

func MapPath(path string) (string, bool) {
	return mapPath(settingsConfig().PathMaps, path)
}

func mapPath(maps []settings.QBitPathMap, path string) (string, bool) {
	target := normalizePath(path)
	if target == "" {
		return path, false
	}

	from, to := "", ""
	for _, entry := range maps {
		source := normalizePath(entry.From)
		destination := normalizePath(entry.To)
		if source == "" || destination == "" {
			continue
		}
		if !hasPathPrefix(target, source) {
			continue
		}
		if len(source) > len(from) {
			from, to = source, destination
		}
	}
	if from == "" {
		return path, false
	}

	rest := strings.TrimPrefix(strings.TrimPrefix(target, from), "/")
	if rest == "" {
		return filepath.FromSlash(to), true
	}
	return filepath.FromSlash(to + "/" + rest), true
}

func normalizePath(path string) string {
	path = strings.ReplaceAll(strings.TrimSpace(path), `\`, "/")
	for len(path) > 1 && strings.HasSuffix(path, "/") {
		path = strings.TrimSuffix(path, "/")
	}
	return path
}

func hasPathPrefix(target, prefix string) bool {
	if target == prefix {
		return true
	}
	if prefix == "/" {
		return strings.HasPrefix(target, "/")
	}
	return strings.HasPrefix(target, prefix+"/")
}
