package qbitsync

import (
	"strconv"
	"strings"
	"sync"
	"time"

	"server/qbit"
	"server/settings"
	"server/torr"
	"server/torr/state"
)

const categoryOther = "other"

var mirroredCategories = []string{"movie", "tv", "music", categoryOther}

type clientAPI interface {
	Add(opts qbit.AddOptions) error
	Info(hashes []string) ([]qbit.TorrentInfo, error)
	Files(hash string) ([]qbit.FileInfo, error)
	Export(hash string) ([]byte, error)
	FilePrio(hash string, indexes []int, priority int) error
	Delete(hashes []string, deleteFiles bool) error
	CreateCategory(name, savePath string) error
	Preferences() (qbit.Preferences, error)
}

type torrentRecord struct {
	Hash          string
	Category      string
	LocalPath     string
	InfoBytes     []byte
	Live          bool
	ActiveReaders int
}

var (
	nowFunc   = time.Now
	newClient = func(baseURL, username, password string) clientAPI {
		return qbit.New(baseURL, username, password)
	}
	listRecords  = torrentRecords
	engineReady  = torr.Ready
	setLocalPath = torr.SetLocalPath
	dropTorrent  = torr.DropTorrent
	importViaAdd = importTorrentViaAdd
)

type retryState struct {
	attempts int
	nextAt   time.Time
	dormant  bool
}

type filesEntry struct {
	files     []qbit.FileInfo
	updatedAt time.Time
}

type service struct {
	mu          sync.Mutex
	fingerprint string
	client      clientAPI

	snapshot        map[string]qbit.TorrentInfo
	snapshotAt      time.Time
	snapshotOkAt    time.Time
	snapshotError   string
	snapshotLoading bool
	demandAt        time.Time

	files        map[string]filesEntry
	filesLoading map[string]bool

	ignored noAutomationSet

	prefs        qbit.Preferences
	prefsAt      time.Time
	prefsLoading bool

	errors   map[string]string
	loggedAt map[string]time.Time
	retries  map[string]*retryState
	drops    map[string]bool
	prios    map[string]time.Time

	stopChan chan struct{}
	doneChan chan struct{}
	wake     chan struct{}
}

var svc = newService()

func newService() *service {
	s := new(service)
	s.wake = make(chan struct{}, 1)
	s.clearCachesLocked()
	return s
}

func settingsConfig() settings.QBitConfig {
	if settings.BTsets == nil {
		return settings.QBitConfig{}
	}
	return settings.BTsets.QBitSettings
}

func Enabled() bool {
	cfg := settingsConfig()
	return cfg.Enabled && strings.TrimSpace(cfg.URL) != ""
}

func Start() {
	svc.start()
}

func Stop() {
	svc.stop()
}

func (s *service) start() {
	s.mu.Lock()
	if s.stopChan != nil {
		s.mu.Unlock()
		return
	}
	stop, done := make(chan struct{}), make(chan struct{})
	s.stopChan, s.doneChan = stop, done
	s.mu.Unlock()
	go s.loop(stop, done)
}

func (s *service) stop() {
	s.mu.Lock()
	stop, done := s.stopChan, s.doneChan
	s.stopChan, s.doneChan = nil, nil
	s.mu.Unlock()
	if stop == nil {
		return
	}
	close(stop)
	<-done
}

func fingerprint(cfg settings.QBitConfig) string {
	maps := make([]string, 0, len(cfg.PathMaps))
	for _, m := range cfg.PathMaps {
		maps = append(maps, m.From+">"+m.To)
	}
	return strings.Join([]string{
		cfg.URL, cfg.Username, cfg.Password, strconv.FormatBool(cfg.Enabled),
		cfg.SavePath, strings.Join(maps, ","),
	}, "|")
}

func (s *service) acquire() (clientAPI, error) {
	cfg := settingsConfig()
	if !cfg.Enabled || strings.TrimSpace(cfg.URL) == "" {
		return nil, qbit.ErrNotConfigured
	}
	print := fingerprint(cfg)

	s.mu.Lock()
	defer s.mu.Unlock()
	if print != s.fingerprint {
		s.fingerprint = print
		s.client = nil
		s.clearCachesLocked()
	}
	if s.client == nil {
		s.client = newClient(cfg.URL, cfg.Username, cfg.Password)
	}
	return s.client, nil
}

func (s *service) clearCachesLocked() {
	s.snapshot = make(map[string]qbit.TorrentInfo)
	s.snapshotAt = time.Time{}
	s.snapshotOkAt = time.Time{}
	s.snapshotError = ""
	s.files = make(map[string]filesEntry)
	s.filesLoading = make(map[string]bool)
	s.prefs = qbit.Preferences{}
	s.prefsAt = time.Time{}
	s.errors = make(map[string]string)
	s.loggedAt = make(map[string]time.Time)
	s.retries = make(map[string]*retryState)
	s.drops = make(map[string]bool)
	s.prios = make(map[string]time.Time)
}

func (s *service) retryReady(key string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	entry, ok := s.retries[key]
	if !ok {
		return true
	}
	if nowFunc().Before(entry.nextAt) {
		return false
	}
	if entry.dormant {
		entry.dormant = false
		entry.attempts = 0
	}
	return true
}

func (s *service) retryFailed(key string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	entry, ok := s.retries[key]
	if !ok {
		entry = new(retryState)
		s.retries[key] = entry
	}
	entry.attempts++
	if entry.attempts >= retryAttempts {
		entry.dormant = true
		entry.nextAt = nowFunc().Add(dormantCooldown)
		return
	}
	entry.nextAt = nowFunc().Add(retryCooldown)
}

func (s *service) retrySucceeded(key string) {
	s.mu.Lock()
	delete(s.retries, key)
	s.mu.Unlock()
}

func normalizeCategory(category string) string {
	name := strings.ToLower(strings.TrimSpace(category))
	if isMirroredCategory(name) {
		return name
	}
	return categoryOther
}

func isMirroredCategory(name string) bool {
	for _, known := range mirroredCategories {
		if name == known {
			return true
		}
	}
	return false
}

func torrentRecords() []torrentRecord {
	list := torr.ListTorrent()
	records := make([]torrentRecord, 0, len(list))
	for _, tor := range list {
		if tor == nil || tor.TorrentSpec == nil {
			continue
		}
		records = append(records, torrentRecord{
			Hash:          tor.Hash().HexString(),
			Category:      tor.Category,
			LocalPath:     tor.LocalPath,
			InfoBytes:     tor.TorrentSpec.InfoBytes,
			Live:          tor.Stat != state.TorrentInDB,
			ActiveReaders: tor.ActiveReaders(),
		})
	}
	return records
}

func DeleteInQBit(hashHex string, deleteFiles bool) error {
	client, err := svc.acquire()
	if err != nil {
		return err
	}
	hash := strings.ToLower(strings.TrimSpace(hashHex))
	if err = client.Delete([]string{hash}, deleteFiles); err != nil {
		return err
	}
	svc.clearError(hash)
	svc.invalidateSnapshot()
	return nil
}

func TestConnection(baseURL, username, password string) (string, error) {
	return qbit.New(baseURL, username, password).APIVersion()
}

func EnsureCategories() error {
	client, err := svc.acquire()
	if err != nil {
		return err
	}
	var failure error
	for _, name := range mirroredCategories {
		if err = client.CreateCategory(name, ""); err != nil && failure == nil {
			failure = err
		}
	}
	return failure
}
