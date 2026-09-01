package qbitsync

import (
	"server/rutor"
)

func resolveTitle(hashHex, rawName string) string {
	if found := rutor.FindByHash(hashHex); found != nil && found.Title != "" {
		return found.Title
	}
	return rawName
}
