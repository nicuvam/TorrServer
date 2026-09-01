package qbitsync

import (
	"bytes"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/anacrolix/torrent/bencode"
	"github.com/anacrolix/torrent/metainfo"

	"server/qbit"
	"server/settings"
)

type fakeClient struct {
	mu sync.Mutex

	torrents map[string]qbit.TorrentInfo
	files    map[string][]qbit.FileInfo
	exports  map[string][]byte

	listCalls   int
	hashCalls   int
	filesCalls  int
	exportCalls int
	addCalls    int
	prioCalls   int
	deleteCalls int
	prefsCalls  int

	exportErr error
}

func newFakeClient() *fakeClient {
	return &fakeClient{
		torrents: make(map[string]qbit.TorrentInfo),
		files:    make(map[string][]qbit.FileInfo),
		exports:  make(map[string][]byte),
	}
}

func (c *fakeClient) Add(opts qbit.AddOptions) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.addCalls++
	return nil
}

func (c *fakeClient) Info(hashes []string) ([]qbit.TorrentInfo, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(hashes) == 0 {
		c.listCalls++
		list := make([]qbit.TorrentInfo, 0, len(c.torrents))
		for _, info := range c.torrents {
			list = append(list, info)
		}
		return list, nil
	}

	c.hashCalls++
	var list []qbit.TorrentInfo
	for _, hash := range hashes {
		if info, ok := c.torrents[hash]; ok {
			list = append(list, info)
		}
	}
	return list, nil
}

func (c *fakeClient) Files(hash string) ([]qbit.FileInfo, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.filesCalls++
	return c.files[hash], nil
}

func (c *fakeClient) Export(hash string) ([]byte, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.exportCalls++
	if c.exportErr != nil {
		return nil, c.exportErr
	}
	return c.exports[hash], nil
}

func (c *fakeClient) FilePrio(hash string, indexes []int, priority int) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.prioCalls++
	return nil
}

func (c *fakeClient) Delete(hashes []string, deleteFiles bool) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.deleteCalls++
	return nil
}

func (c *fakeClient) CreateCategory(name, savePath string) error {
	return nil
}

func (c *fakeClient) Preferences() (qbit.Preferences, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.prefsCalls++
	return qbit.Preferences{ListenPort: 6881}, nil
}

func (c *fakeClient) awaitPrioCalls(t *testing.T, want int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		c.mu.Lock()
		got := c.prioCalls
		c.mu.Unlock()
		if got == want {
			return
		}
		if got > want || time.Now().After(deadline) {
			t.Fatalf("prio calls = %d, want %d", got, want)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func (c *fakeClient) totalCalls() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.listCalls + c.hashCalls + c.filesCalls + c.exportCalls + c.addCalls + c.prioCalls + c.deleteCalls + c.prefsCalls
}

type testClock struct {
	mu      sync.Mutex
	current time.Time
}

func (c *testClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.current
}

func (c *testClock) advance(d time.Duration) {
	c.mu.Lock()
	c.current = c.current.Add(d)
	c.mu.Unlock()
}

type testEnv struct {
	service  *service
	client   *fakeClient
	clock    *testClock
	records  []torrentRecord
	local    map[string]string
	dropped  []string
	imported []*settings.TorrentDB
}

func newTestEnv(t *testing.T, cfg settings.QBitConfig) *testEnv {
	t.Helper()

	previousSets := settings.BTsets
	previousNow := nowFunc
	previousNew := newClient
	previousList := listRecords
	previousLocal := setLocalPath
	previousDrop := dropTorrent
	previousSave := saveTorrentDB
	t.Cleanup(func() {
		settings.BTsets = previousSets
		nowFunc = previousNow
		newClient = previousNew
		listRecords = previousList
		setLocalPath = previousLocal
		dropTorrent = previousDrop
		saveTorrentDB = previousSave
	})

	env := &testEnv{
		client: newFakeClient(),
		clock:  &testClock{current: time.Unix(1700000000, 0)},
		local:  make(map[string]string),
	}
	env.service = newService()

	settings.BTsets = &settings.BTSets{QBitSettings: cfg}
	nowFunc = env.clock.Now
	newClient = func(baseURL, username, password string) clientAPI { return env.client }
	listRecords = func() []torrentRecord { return env.records }
	setLocalPath = func(hash, path string) error {
		env.local[hash] = path
		return nil
	}
	dropTorrent = func(hash string) { env.dropped = append(env.dropped, hash) }
	saveTorrentDB = func(record *settings.TorrentDB) { env.imported = append(env.imported, record) }

	return env
}

func enabledConfig() settings.QBitConfig {
	return settings.QBitConfig{Enabled: true, URL: "http://qb:8080", AutoLocal: true}
}

func testInfo(name string, files []metainfo.FileInfo) *metainfo.Info {
	info := &metainfo.Info{Name: name, PieceLength: 16384, Files: files}
	count := (info.TotalLength() + info.PieceLength - 1) / info.PieceLength
	info.Pieces = make([]byte, count*20)
	return info
}

func testInfoBytes(t *testing.T, info *metainfo.Info) []byte {
	t.Helper()
	data, err := bencode.Marshal(info)
	if err != nil {
		t.Fatalf("marshal info: %v", err)
	}
	return data
}

func testTorrentFile(t *testing.T, info *metainfo.Info) []byte {
	t.Helper()
	mi := metainfo.MetaInfo{InfoBytes: testInfoBytes(t, info)}
	buf := new(bytes.Buffer)
	if err := mi.Write(buf); err != nil {
		t.Fatalf("write metainfo: %v", err)
	}
	return buf.Bytes()
}

func writeFiles(t *testing.T, base string, files []metainfo.FileInfo) {
	t.Helper()
	for _, file := range files {
		full := filepath.Join(append([]string{base}, file.Path...)...)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, make([]byte, file.Length), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func completedInfo(hash, contentPath, savePath, category string) qbit.TorrentInfo {
	return qbit.TorrentInfo{
		Hash:         hash,
		Name:         "release",
		State:        "stalledUP",
		Category:     category,
		Progress:     1,
		CompletionOn: 1700000000,
		ContentPath:  contentPath,
		SavePath:     savePath,
	}
}

func TestTickIdleMakesNoRequests(t *testing.T) {
	env := newTestEnv(t, enabledConfig())

	env.service.tick()

	if calls := env.client.totalCalls(); calls != 0 {
		t.Fatalf("idle tick made %d qBittorrent calls", calls)
	}
	if got := env.service.interval(); got != idleInterval {
		t.Fatalf("idle interval = %v, want %v", got, idleInterval)
	}

	env.service.markDemand()
	if got := env.service.interval(); got != activeInterval {
		t.Fatalf("demand interval = %v, want %v", got, activeInterval)
	}
}

func TestFlipRetriesThenGoesDormant(t *testing.T) {
	env := newTestEnv(t, enabledConfig())

	info := testInfo("release", []metainfo.FileInfo{{Path: []string{"ep.mkv"}, Length: 32}})
	hash := metainfo.HashBytes(testInfoBytes(t, info)).HexString()
	env.records = []torrentRecord{{Hash: hash, InfoBytes: testInfoBytes(t, info)}}
	env.client.torrents[hash] = completedInfo(hash, "/missing/release", "/missing", "tv")

	env.service.tick()
	if env.client.hashCalls != 1 {
		t.Fatalf("first tick hash calls = %d, want 1", env.client.hashCalls)
	}
	if _, ok := env.service.errorMap()[hash]; !ok {
		t.Fatal("failed flip did not record an error")
	}

	env.service.tick()
	if env.client.hashCalls != 1 {
		t.Fatalf("cooldown tick hash calls = %d, want 1", env.client.hashCalls)
	}

	env.clock.advance(retryCooldown)
	env.service.tick()
	if env.client.hashCalls != 2 {
		t.Fatalf("second attempt hash calls = %d, want 2", env.client.hashCalls)
	}

	env.clock.advance(retryCooldown)
	env.service.tick()
	if env.client.hashCalls != 3 {
		t.Fatalf("third attempt hash calls = %d, want 3", env.client.hashCalls)
	}

	env.clock.advance(24 * time.Hour)
	env.service.tick()
	if env.client.hashCalls != 3 {
		t.Fatalf("dormant tick hash calls = %d, want 3", env.client.hashCalls)
	}
	if len(env.local) != 0 {
		t.Fatalf("local path set for failed flip: %v", env.local)
	}
}

func TestFlipWaitsForActiveReaders(t *testing.T) {
	env := newTestEnv(t, enabledConfig())

	files := []metainfo.FileInfo{
		{Path: []string{"ep1.mkv"}, Length: 32},
		{Path: []string{"ep2.mkv"}, Length: 48},
	}
	info := testInfo("release", files)
	root := t.TempDir()
	writeFiles(t, root, files)

	hash := metainfo.HashBytes(testInfoBytes(t, info)).HexString()
	env.records = []torrentRecord{{Hash: hash, InfoBytes: testInfoBytes(t, info), Live: true, ActiveReaders: 1}}
	env.client.torrents[hash] = completedInfo(hash, root, filepath.Dir(root), "tv")

	env.service.tick()

	if env.local[hash] != root {
		t.Fatalf("local path = %q, want %q", env.local[hash], root)
	}
	if len(env.dropped) != 0 {
		t.Fatalf("torrent dropped while readers are active: %v", env.dropped)
	}

	env.records = []torrentRecord{{Hash: hash, LocalPath: root, Live: true}}
	env.service.tick()

	if len(env.dropped) != 1 || env.dropped[0] != hash {
		t.Fatalf("dropped = %v, want [%s]", env.dropped, hash)
	}
}

func TestAutoImportSkipsKnownAndUncategorized(t *testing.T) {
	cfg := enabledConfig()
	cfg.AutoLocal = false
	cfg.AutoImport = true
	env := newTestEnv(t, cfg)

	files := []metainfo.FileInfo{{Path: []string{"ep.mkv"}, Length: 32}}
	info := testInfo("imported", files)
	infoBytes := testInfoBytes(t, info)
	newHash := metainfo.HashBytes(infoBytes).HexString()

	knownHash := "1111111111111111111111111111111111111111"
	uncategorized := "2222222222222222222222222222222222222222"
	foreign := "3333333333333333333333333333333333333333"

	env.records = []torrentRecord{{Hash: knownHash}}
	env.client.torrents[newHash] = completedInfo(newHash, "/data/imported", "/data", "tv")
	env.client.torrents[knownHash] = completedInfo(knownHash, "/data/known", "/data", "movie")
	env.client.torrents[uncategorized] = completedInfo(uncategorized, "/data/other", "/data", "")
	env.client.torrents[foreign] = completedInfo(foreign, "/data/soft", "/data", "software")
	env.client.exports[newHash] = testTorrentFile(t, info)

	env.service.tick()

	if len(env.imported) != 1 {
		t.Fatalf("imported %d torrents, want 1", len(env.imported))
	}
	record := env.imported[0]
	if record.InfoHash.HexString() != newHash {
		t.Fatalf("imported hash = %s, want %s", record.InfoHash.HexString(), newHash)
	}
	if record.Category != "tv" || record.Title != "imported" {
		t.Fatalf("imported record = %+v", record)
	}
	if len(record.InfoBytes) == 0 || record.Storage != nil {
		t.Fatalf("imported spec not sanitized: infoBytes=%d storage=%v", len(record.InfoBytes), record.Storage)
	}
	if record.Size != info.TotalLength() || record.Timestamp != env.clock.Now().Unix() {
		t.Fatalf("imported size/timestamp = %d/%d", record.Size, record.Timestamp)
	}

	env.records = append(env.records, torrentRecord{Hash: newHash})
	env.service.tick()
	if len(env.imported) != 1 {
		t.Fatalf("imported %d torrents after second tick, want 1", len(env.imported))
	}
}
