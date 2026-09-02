package qbitsync

import (
	"fmt"
	"time"

	"github.com/anacrolix/torrent/metainfo"

	"server/log"
	"server/qbit"
	"server/torr/storage/filestor"
)

const (
	retryAttempts   = 3
	retryCooldown   = 5 * time.Minute
	dormantCooldown = time.Hour
	flipScope       = "flip:"
)

func (s *service) runFlips(stop chan struct{}, client clientAPI, records []torrentRecord, snapshot map[string]qbit.TorrentInfo) {
	for _, record := range records {
		if stopping(stop) {
			return
		}
		if record.LocalPath != "" || s.ignored.contains(record.Hash) {
			continue
		}
		cached, ok := snapshot[record.Hash]
		if !ok || !cached.Completed() {
			continue
		}
		if !s.retryReady(flipScope + record.Hash) {
			continue
		}

		info, err := s.freshInfo(client, record.Hash)
		if err == nil {
			if !info.Completed() {
				continue
			}
			err = s.flip(record, info)
		}
		if err != nil {
			s.setError(record.Hash, err)
			s.retryFailed(flipScope + record.Hash)
			continue
		}

		s.retrySucceeded(flipScope + record.Hash)
		s.clearError(record.Hash)
		s.queueDrop(record)
	}
}

func (s *service) freshInfo(client clientAPI, hash string) (qbit.TorrentInfo, error) {
	list, err := client.Info([]string{hash})
	if err != nil {
		return qbit.TorrentInfo{}, err
	}
	for _, info := range list {
		return info, nil
	}
	return qbit.TorrentInfo{}, fmt.Errorf("%w: %s", qbit.ErrNotFound, hash)
}

func (s *service) flip(record torrentRecord, info qbit.TorrentInfo) error {
	if len(record.InfoBytes) == 0 {
		return fmt.Errorf("qbittorrent: no torrent info bytes for %s", record.Hash)
	}

	mi := metainfo.MetaInfo{InfoBytes: record.InfoBytes}
	meta, err := mi.UnmarshalInfo()
	if err != nil {
		return err
	}

	local, _ := MapPath(info.ContentPath)
	if local == "" {
		return fmt.Errorf("qbittorrent: no content path for %s", record.Hash)
	}
	if _, err = filestor.PreValidate(&meta, local); err != nil {
		log.TLogln("qbittorrent flip validation:", record.Hash, err)
		return fmt.Errorf("local files not found at %s", local)
	}
	return setLocalPath(record.Hash, local)
}

func (s *service) queueDrop(record torrentRecord) {
	if !record.Live {
		return
	}
	s.mu.Lock()
	s.drops[record.Hash] = true
	s.mu.Unlock()
}

func (s *service) drainDrops(stop chan struct{}, records []torrentRecord) {
	s.mu.Lock()
	pending := len(s.drops)
	s.mu.Unlock()
	if pending == 0 {
		return
	}

	for _, record := range records {
		if stopping(stop) {
			return
		}
		s.mu.Lock()
		queued := s.drops[record.Hash]
		s.mu.Unlock()
		if !queued {
			continue
		}
		if record.Live && record.ActiveReaders > 0 {
			continue
		}
		if record.Live {
			dropTorrent(record.Hash)
		}
		s.mu.Lock()
		delete(s.drops, record.Hash)
		s.mu.Unlock()
	}
}

func (s *service) hasPendingDrops() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.drops) > 0
}
