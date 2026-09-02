package qbitsync

import (
	"sync"

	"server/settings"
)

const importedBucket = "QBitImported"

var (
	listImported = func() []string {
		db := settings.NewTDB()
		if db == nil {
			return nil
		}
		return db.List(importedBucket)
	}
	storeImported = func(hash string) {
		if settings.ReadOnly {
			return
		}
		if db := settings.NewTDB(); db != nil {
			db.Set(importedBucket, hash, []byte("1"))
		}
	}
	removeImported = func(hash string) {
		if settings.ReadOnly {
			return
		}
		if db := settings.NewTDB(); db != nil {
			db.Rem(importedBucket, hash)
		}
	}
)

type importedSet struct {
	mu     sync.Mutex
	loaded bool
	hashes map[string]bool
}

func (i *importedSet) contains(hash string) bool {
	i.mu.Lock()
	defer i.mu.Unlock()
	i.loadLocked()
	return i.hashes[hash]
}

func (i *importedSet) add(hash string) {
	i.mu.Lock()
	i.loadLocked()
	i.hashes[hash] = true
	i.mu.Unlock()
	storeImported(hash)
}

func (i *importedSet) remove(hash string) {
	i.mu.Lock()
	i.loadLocked()
	delete(i.hashes, hash)
	i.mu.Unlock()
	removeImported(hash)
}

func (i *importedSet) loadLocked() {
	if i.loaded {
		return
	}
	i.loaded = true
	i.hashes = make(map[string]bool)
	for _, hash := range listImported() {
		i.hashes[normalizeHash(hash)] = true
	}
}
