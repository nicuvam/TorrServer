package filestor

import (
	"sync"

	"github.com/anacrolix/torrent/metainfo"
	ts "github.com/anacrolix/torrent/storage"
)

type Storage struct {
	root string

	mu       sync.Mutex
	torrents map[metainfo.Hash]*Torrent
}

func New(root string) *Storage {
	return &Storage{
		root:     root,
		torrents: make(map[metainfo.Hash]*Torrent),
	}
}

func (s *Storage) OpenTorrent(info *metainfo.Info, infoHash metainfo.Hash) (ts.TorrentImpl, error) {
	layout, err := Resolve(info, s.root)
	if err != nil {
		return nil, err
	}
	tor := newTorrent(s, infoHash, layout)
	s.mu.Lock()
	s.torrents[infoHash] = tor
	s.mu.Unlock()
	return tor, nil
}

func (s *Storage) Opened(hash metainfo.Hash) *Torrent {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.torrents[hash]
}

func (s *Storage) Close() error {
	return nil
}

func (s *Storage) forget(tor *Torrent) {
	s.mu.Lock()
	if s.torrents[tor.hash] == tor {
		delete(s.torrents, tor.hash)
	}
	s.mu.Unlock()
}
