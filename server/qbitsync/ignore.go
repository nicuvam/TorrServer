package qbitsync

import (
	"strings"
	"sync"

	"server/settings"
)

const ignoreBucket = "QBitIgnored"

var (
	listIgnored = func() []string {
		db := settings.NewTDB()
		if db == nil {
			return nil
		}
		return db.List(ignoreBucket)
	}
	storeIgnored = func(hash string) {
		if settings.ReadOnly {
			return
		}
		if db := settings.NewTDB(); db != nil {
			db.Set(ignoreBucket, hash, []byte("1"))
		}
	}
	removeIgnored = func(hash string) {
		if settings.ReadOnly {
			return
		}
		if db := settings.NewTDB(); db != nil {
			db.Rem(ignoreBucket, hash)
		}
	}
)

type ignoreSet struct {
	mu     sync.Mutex
	loaded bool
	hashes map[string]bool
}

func Forget(hashHex string) {
	if hash := normalizeHash(hashHex); hash != "" {
		svc.ignored.add(hash)
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

func (i *ignoreSet) contains(hash string) bool {
	i.mu.Lock()
	defer i.mu.Unlock()
	i.loadLocked()
	return i.hashes[hash]
}

func (i *ignoreSet) add(hash string) {
	i.mu.Lock()
	i.loadLocked()
	i.hashes[hash] = true
	i.mu.Unlock()
	storeIgnored(hash)
}

func (i *ignoreSet) remove(hash string) {
	i.mu.Lock()
	i.loadLocked()
	delete(i.hashes, hash)
	i.mu.Unlock()
	removeIgnored(hash)
}

func (i *ignoreSet) loadLocked() {
	if i.loaded {
		return
	}
	i.loaded = true
	i.hashes = make(map[string]bool)
	for _, hash := range listIgnored() {
		i.hashes[normalizeHash(hash)] = true
	}
}
