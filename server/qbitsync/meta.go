package qbitsync

import (
	"strings"

	"server/rutor"
)

func resolveTitle(hashHex, rawName string) string {
	if found := rutor.FindByHash(hashHex); found != nil && found.Title != "" {
		return found.Title
	}
	return prettyName(rawName)
}

func prettyName(name string) string {
	if strings.Contains(name, " ") {
		return name
	}
	cleaned := strings.NewReplacer(".", " ", "_", " ").Replace(name)
	return strings.Join(strings.Fields(cleaned), " ")
}
