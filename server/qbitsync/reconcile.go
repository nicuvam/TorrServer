package qbitsync

import (
	"bytes"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/metainfo"

	"server/qbit"
	"server/settings"
	"server/torr"
)

const (
	activeInterval = 2 * time.Second
	idleInterval   = time.Minute
	demandWindow   = 10 * time.Second
	importScope    = "import:"
)

var errMetadataNotReady = errors.New("qbittorrent: metadata not ready")

func (s *service) loop(stop, done chan struct{}) {
	defer close(done)

	for {
		s.tick(stop)

		timer := time.NewTimer(s.interval())
		select {
		case <-stop:
			timer.Stop()
			return
		case <-timer.C:
		case <-s.wake:
			timer.Stop()
		}
	}
}

func (s *service) interval() time.Duration {
	if s.demandRecent() {
		return activeInterval
	}
	return idleInterval
}

func (s *service) demandRecent() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return !s.demandAt.IsZero() && nowFunc().Sub(s.demandAt) < demandWindow
}

func stopping(stop chan struct{}) bool {
	select {
	case <-stop:
		return true
	default:
		return false
	}
}

func (s *service) tick(stop chan struct{}) {
	cfg := settingsConfig()
	if !cfg.Enabled || strings.TrimSpace(cfg.URL) == "" {
		return
	}

	records := listRecords()
	if !s.hasWork(cfg, records) {
		return
	}

	client, err := s.acquire()
	if err != nil {
		s.logProblem(err)
		return
	}

	s.refreshSnapshot()
	snapshot := s.snapshotData()

	if cfg.AutoLocal {
		s.runFlips(stop, client, records, snapshot)
	}
	if cfg.AutoImport {
		s.runImport(stop, client, records, snapshot)
	}
	s.drainDrops(stop, listRecords())
}

func (s *service) hasWork(cfg settings.QBitConfig, records []torrentRecord) bool {
	if cfg.AutoImport || s.demandRecent() || s.hasPendingDrops() {
		return true
	}
	if !cfg.AutoLocal {
		return false
	}
	for _, record := range records {
		if record.LocalPath == "" {
			return true
		}
	}
	return false
}

func (s *service) runImport(stop chan struct{}, client clientAPI, records []torrentRecord, snapshot map[string]qbit.TorrentInfo) {
	if settings.ReadOnly {
		s.logProblem(errors.New("read-only DB mode, auto import disabled"))
		return
	}
	if !engineReady() {
		return
	}

	known := make(map[string]bool, len(records))
	for _, record := range records {
		known[record.Hash] = true
	}

	for hash, info := range snapshot {
		if stopping(stop) {
			return
		}
		category := strings.ToLower(strings.TrimSpace(info.Category))
		if !isMirroredCategory(category) || known[hash] || s.ignored.contains(hash) {
			continue
		}
		if !s.retryReady(importScope + hash) {
			continue
		}
		if err := s.importTorrent(client, hash, category); err != nil {
			if errors.Is(err, errMetadataNotReady) {
				continue
			}
			s.setError(hash, err)
			s.retryFailed(importScope + hash)
			continue
		}
		s.retrySucceeded(importScope + hash)
		s.clearError(hash)
	}
}

func (s *service) importTorrent(client clientAPI, hash, category string) error {
	spec, err := importSpec(client, hash)
	if err != nil {
		return err
	}
	return importViaAdd(spec, category)
}

func importSpec(client clientAPI, hash string) (*torrent.TorrentSpec, error) {
	data, err := client.Export(hash)
	if err != nil {
		if errors.Is(err, qbit.ErrNoMetadata) {
			return nil, errMetadataNotReady
		}
		return nil, err
	}
	return specFromTorrentFile(data)
}

func importTorrentViaAdd(spec *torrent.TorrentSpec, category string) error {
	if len(spec.InfoBytes) == 0 {
		return fmt.Errorf("qbittorrent: no metadata for %s", spec.InfoHash.HexString())
	}
	if torr.HasLiveTorrent(spec.InfoHash) {
		return nil
	}
	tor, err := torr.AddTorrent(spec, "", "", "", category, "")
	if err != nil {
		return err
	}
	if !tor.GotInfo() {
		return errors.New("timeout getting torrent info")
	}
	torr.ApplyDefaultTitle(tor)
	torr.SaveTorrentToDB(tor)
	if tor.ActiveReaders() == 0 {
		torr.DropTorrent(spec.InfoHash.HexString())
	}
	return nil
}

func specFromTorrentFile(data []byte) (*torrent.TorrentSpec, error) {
	mi, err := metainfo.Load(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	meta, err := mi.UnmarshalInfo()
	if err != nil {
		return nil, err
	}

	magnet := mi.Magnet(nil, &meta)
	return &torrent.TorrentSpec{
		InfoBytes:   mi.InfoBytes,
		Trackers:    [][]string{magnet.Trackers},
		DisplayName: meta.Name,
		InfoHash:    mi.HashInfoBytes(),
	}, nil
}
