package qbitsync

import (
	"strings"
	"sync"

	"server/settings"
)

const noAutomationBucket = "QBitIgnored"

var (
	listNoAutomation = func() []string {
		db := settings.NewTDB()
		if db == nil {
			return nil
		}
		return db.List(noAutomationBucket)
	}
	storeNoAutomation = func(hash string) {
		if settings.ReadOnly {
			return
		}
		if db := settings.NewTDB(); db != nil {
			db.Set(noAutomationBucket, hash, []byte("1"))
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

func (i *noAutomationSet) contains(hash string) bool {
	i.mu.Lock()
	defer i.mu.Unlock()
	i.loadLocked()
	return i.hashes[hash]
}

func (i *noAutomationSet) add(hash string) {
	i.mu.Lock()
	i.loadLocked()
	i.hashes[hash] = true
	i.mu.Unlock()
	storeNoAutomation(hash)
}

func (i *noAutomationSet) remove(hash string) {
	i.mu.Lock()
	i.loadLocked()
	delete(i.hashes, hash)
	i.mu.Unlock()
	removeNoAutomation(hash)
}

func (i *noAutomationSet) loadLocked() {
	if i.loaded {
		return
	}
	i.loaded = true
	i.hashes = make(map[string]bool)
	for _, hash := range listNoAutomation() {
		i.hashes[normalizeHash(hash)] = true
	}
}
