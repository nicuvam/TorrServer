package qbitsync

import (
	"strings"
	"sync"

	"server/qbit"
	"server/settings"
)

const noAutomationBucket = "QBitIgnored"

var (
	listNoAutomation = func() map[string]string {
		db := settings.NewTDB()
		if db == nil {
			return nil
		}
		entries := make(map[string]string)
		for _, hash := range db.List(noAutomationBucket) {
			entries[hash] = decodeRememberedCategory(db.Get(noAutomationBucket, hash))
		}
		return entries
	}
	storeNoAutomation = func(hash, category string) {
		if settings.ReadOnly {
			return
		}
		if db := settings.NewTDB(); db != nil {
			db.Set(noAutomationBucket, hash, []byte(category))
		}
	}
	removeNoAutomation = func(hash string) {
		if settings.ReadOnly {
			return
		}
		if db := settings.NewTDB(); db != nil {
			db.Rem(noAutomationBucket, hash)
		}
	}
)

type noAutomationSet struct {
	mu         sync.Mutex
	loaded     bool
	categories map[string]string
}

func Forget(hashHex string) {
	if hash := normalizeHash(hashHex); hash != "" {
		svc.ignored.add(hash, mirroredCategory(svc.snapshotData()[hash]))
	}
}

func Unforget(hashHex string) {
	if hash := normalizeHash(hashHex); hash != "" {
		svc.ignored.remove(hash)
	}
}

func normalizeHash(hashHex string) string {
	return strings.ToLower(strings.TrimSpace(hashHex))
}

func mirroredCategory(info qbit.TorrentInfo) string {
	name := strings.ToLower(strings.TrimSpace(info.Category))
	if isMirroredCategory(name) {
		return name
	}
	return ""
}

func decodeRememberedCategory(value []byte) string {
	category := strings.ToLower(strings.TrimSpace(string(value)))
	if category == "1" {
		return ""
	}
	return category
}

func (i *noAutomationSet) contains(hash string) bool {
	i.mu.Lock()
	defer i.mu.Unlock()
	i.loadLocked()
	_, ok := i.categories[hash]
	return ok
}

func (i *noAutomationSet) rememberedCategory(hash string) (string, bool) {
	i.mu.Lock()
	defer i.mu.Unlock()
	i.loadLocked()
	category, ok := i.categories[hash]
	return category, ok
}

func (i *noAutomationSet) add(hash, category string) {
	i.mu.Lock()
	i.loadLocked()
	i.categories[hash] = category
	i.mu.Unlock()
	storeNoAutomation(hash, category)
}

func (i *noAutomationSet) remove(hash string) {
	i.mu.Lock()
	i.loadLocked()
	delete(i.categories, hash)
	i.mu.Unlock()
	removeNoAutomation(hash)
}

func (i *noAutomationSet) loadLocked() {
	if i.loaded {
		return
	}
	i.loaded = true
	i.categories = make(map[string]string)
	for hash, category := range listNoAutomation() {
		i.categories[normalizeHash(hash)] = category
	}
}
