package qbitsync

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/bencode"
	"github.com/anacrolix/torrent/metainfo"

	"server/qbit"
	"server/settings"
	"server/torr/state"
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
	infoErr   error
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
		if c.infoErr != nil {
			return nil, c.infoErr
		}
		list := make([]qbit.TorrentInfo, 0, len(c.torrents))
		for _, info := range c.torrents {
			list = append(list, info)
		}
		return list, nil
	}

	c.hashCalls++
	if c.infoErr != nil {
		return nil, c.infoErr
	}
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

func (c *fakeClient) setInfoErr(err error) {
	c.mu.Lock()
	c.infoErr = err
	c.mu.Unlock()
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

func (c *fakeClient) awaitFilesCalls(t *testing.T, want int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		c.mu.Lock()
		got := c.filesCalls
		c.mu.Unlock()
		if got == want {
			return
		}
		if got > want || time.Now().After(deadline) {
			t.Fatalf("files calls = %d, want %d", got, want)
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
	imported []importedAdd
	ignored  map[string]bool
	running  chan struct{}
}

func (e *testEnv) tick() {
	e.service.tick(e.running)
}

func (e *testEnv) enrichOne(hash string) *state.TorrentStatus {
	status := &state.TorrentStatus{Hash: hash}
	e.service.enrich([]*state.TorrentStatus{status})
	return status
}

type importedAdd struct {
	spec     *torrent.TorrentSpec
	category string
}

func newTestEnv(t *testing.T, cfg settings.QBitConfig) *testEnv {
	t.Helper()

	previousSets := settings.BTsets
	previousNow := nowFunc
	previousNew := newClient
	previousList := listRecords
	previousLocal := setLocalPath
	previousDrop := dropTorrent
	previousImport := importViaAdd
	previousReady := engineReady
	previousIgnoreList := listNoAutomation
	previousIgnoreStore := storeNoAutomation
	previousIgnoreRemove := removeNoAutomation
	t.Cleanup(func() {
		settings.BTsets = previousSets
		nowFunc = previousNow
		newClient = previousNew
		listRecords = previousList
		setLocalPath = previousLocal
		dropTorrent = previousDrop
		importViaAdd = previousImport
		engineReady = previousReady
		listNoAutomation = previousIgnoreList
		storeNoAutomation = previousIgnoreStore
		removeNoAutomation = previousIgnoreRemove
	})

	env := &testEnv{
		client:  newFakeClient(),
		clock:   &testClock{current: time.Unix(1700000000, 0)},
		local:   make(map[string]string),
		ignored: make(map[string]bool),
		running: make(chan struct{}),
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
	engineReady = func() bool { return true }
	importViaAdd = func(spec *torrent.TorrentSpec, category string) error {
		env.imported = append(env.imported, importedAdd{spec: spec, category: category})
		return nil
	}
	listNoAutomation = func() []string {
		hashes := make([]string, 0, len(env.ignored))
		for hash := range env.ignored {
			hashes = append(hashes, hash)
		}
		return hashes
	}
	storeNoAutomation = func(hash string) { env.ignored[hash] = true }
	removeNoAutomation = func(hash string) { delete(env.ignored, hash) }

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

	env.tick()

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

func TestFlipRetriesThenSleepsUntilDormantCooldown(t *testing.T) {
	env := newTestEnv(t, enabledConfig())

	info := testInfo("release", []metainfo.FileInfo{{Path: []string{"ep.mkv"}, Length: 32}})
	hash := metainfo.HashBytes(testInfoBytes(t, info)).HexString()
	env.records = []torrentRecord{{Hash: hash, InfoBytes: testInfoBytes(t, info)}}
	env.client.torrents[hash] = completedInfo(hash, "/missing/release", "/missing", "tv")

	env.tick()
	if env.client.hashCalls != 1 {
		t.Fatalf("first tick hash calls = %d, want 1", env.client.hashCalls)
	}
	if _, ok := env.service.errorMap()[hash]; !ok {
		t.Fatal("failed flip did not record an error")
	}

	env.tick()
	if env.client.hashCalls != 1 {
		t.Fatalf("cooldown tick hash calls = %d, want 1", env.client.hashCalls)
	}

	env.clock.advance(retryCooldown)
	env.tick()
	if env.client.hashCalls != 2 {
		t.Fatalf("second attempt hash calls = %d, want 2", env.client.hashCalls)
	}

	env.clock.advance(retryCooldown)
	env.tick()
	if env.client.hashCalls != 3 {
		t.Fatalf("third attempt hash calls = %d, want 3", env.client.hashCalls)
	}

	env.clock.advance(retryCooldown)
	env.tick()
	if env.client.hashCalls != 3 {
		t.Fatalf("dormant tick hash calls = %d, want 3", env.client.hashCalls)
	}

	env.clock.advance(dormantCooldown)
	env.tick()
	if env.client.hashCalls != 4 {
		t.Fatalf("hash calls after dormant cooldown = %d, want 4", env.client.hashCalls)
	}

	env.clock.advance(retryCooldown)
	env.tick()
	if env.client.hashCalls != 5 {
		t.Fatalf("hash calls after woken retry = %d, want 5", env.client.hashCalls)
	}
	if len(env.local) != 0 {
		t.Fatalf("local path set for failed flip: %v", env.local)
	}
}

func TestFlipSkipsHashWithoutAutomation(t *testing.T) {
	env := newTestEnv(t, enabledConfig())

	files := []metainfo.FileInfo{{Path: []string{"ep.mkv"}, Length: 32}}
	info := testInfo("release", files)
	root := t.TempDir()
	writeFiles(t, root, files)

	hash := metainfo.HashBytes(testInfoBytes(t, info)).HexString()
	env.records = []torrentRecord{{Hash: hash, InfoBytes: testInfoBytes(t, info)}}
	env.client.torrents[hash] = completedInfo(hash, root, filepath.Dir(root), "tv")
	env.ignored[hash] = true

	env.tick()

	if len(env.local) != 0 {
		t.Fatalf("flipped a hash excluded from automation: %v", env.local)
	}
	if env.client.hashCalls != 0 {
		t.Fatalf("hash calls = %d, want 0", env.client.hashCalls)
	}
}

func TestEnrichReportsUnreachableWhileSnapshotIsStale(t *testing.T) {
	env := newTestEnv(t, enabledConfig())

	hash := "5555555555555555555555555555555555555555"
	env.client.torrents[hash] = completedInfo(hash, "/data/release", "/data", "tv")
	env.service.refreshSnapshot()

	if status := env.enrichOne(hash); status.QBitError != "" {
		t.Fatalf("fresh snapshot reported %q", status.QBitError)
	}

	env.client.setInfoErr(errors.New("connection refused"))
	env.clock.advance(snapshotTTL)
	env.service.refreshSnapshot()

	if status := env.enrichOne(hash); status.QBitError != "" {
		t.Fatalf("first failed refresh reported %q", status.QBitError)
	}

	env.clock.advance(snapshotStaleAfter)
	env.service.refreshSnapshot()

	status := env.enrichOne(hash)
	if status.QBitError != unreachableMessage {
		t.Fatalf("stale snapshot error = %q, want %q", status.QBitError, unreachableMessage)
	}
	if status.QBitProgress != 1 || status.QBitState != "stalledUP" {
		t.Fatalf("stale snapshot values dropped: progress=%v state=%q", status.QBitProgress, status.QBitState)
	}

	env.client.setInfoErr(nil)
	env.clock.advance(snapshotTTL)
	env.service.refreshSnapshot()

	if status := env.enrichOne(hash); status.QBitError != "" {
		t.Fatalf("successful refresh left error %q", status.QBitError)
	}
}

func TestEnrichKeepsPerHashErrorWhileUnreachable(t *testing.T) {
	env := newTestEnv(t, enabledConfig())

	hash := "6666666666666666666666666666666666666666"
	env.client.torrents[hash] = completedInfo(hash, "/data/release", "/data", "tv")
	env.service.refreshSnapshot()
	env.service.setError(hash, errors.New("local files not found at /data/release"))

	env.client.setInfoErr(errors.New("connection refused"))
	env.clock.advance(snapshotStaleAfter + snapshotTTL)
	env.service.refreshSnapshot()

	if status := env.enrichOne(hash); status.QBitError != "local files not found at /data/release" {
		t.Fatalf("error = %q, want the per-hash message", status.QBitError)
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

	env.tick()

	if env.local[hash] != root {
		t.Fatalf("local path = %q, want %q", env.local[hash], root)
	}
	if len(env.dropped) != 0 {
		t.Fatalf("torrent dropped while readers are active: %v", env.dropped)
	}

	env.records = []torrentRecord{{Hash: hash, LocalPath: root, Live: true}}
	env.tick()

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

	env.tick()

	if len(env.imported) != 1 {
		t.Fatalf("imported %d torrents, want 1", len(env.imported))
	}
	added := env.imported[0]
	if added.spec.InfoHash.HexString() != newHash {
		t.Fatalf("imported hash = %s, want %s", added.spec.InfoHash.HexString(), newHash)
	}
	if added.category != "tv" {
		t.Fatalf("imported category = %q, want tv", added.category)
	}
	if len(added.spec.InfoBytes) == 0 || added.spec.DisplayName != "imported" {
		t.Fatalf("imported spec = infoBytes:%d dn:%q", len(added.spec.InfoBytes), added.spec.DisplayName)
	}

	env.records = append(env.records, torrentRecord{Hash: newHash})
	env.tick()
	if len(env.imported) != 1 {
		t.Fatalf("imported %d torrents after second tick, want 1", len(env.imported))
	}
}

func importConfig() settings.QBitConfig {
	cfg := enabledConfig()
	cfg.AutoLocal = false
	cfg.AutoImport = true
	return cfg
}

func TestAutoImportSkipsForgottenHash(t *testing.T) {
	env := newTestEnv(t, importConfig())

	files := []metainfo.FileInfo{{Path: []string{"ep.mkv"}, Length: 32}}
	info := testInfo("forgotten", files)
	hash := metainfo.HashBytes(testInfoBytes(t, info)).HexString()
	env.client.torrents[hash] = completedInfo(hash, "/data/forgotten", "/data", "tv")
	env.client.exports[hash] = testTorrentFile(t, info)
	env.ignored[hash] = true

	env.tick()

	if len(env.imported) != 0 {
		t.Fatalf("imported %d forgotten torrents, want 0", len(env.imported))
	}
	if env.client.exportCalls != 0 {
		t.Fatalf("export calls for forgotten torrent = %d, want 0", env.client.exportCalls)
	}

	env.service.ignored.remove(hash)
	if env.ignored[hash] {
		t.Fatal("unforget did not clear the persisted hash")
	}

	env.tick()
	if len(env.imported) != 1 {
		t.Fatalf("imported %d torrents after unforget, want 1", len(env.imported))
	}
}

func TestAutoImportWaitsForEngine(t *testing.T) {
	env := newTestEnv(t, importConfig())

	files := []metainfo.FileInfo{{Path: []string{"ep.mkv"}, Length: 32}}
	info := testInfo("pending", files)
	hash := metainfo.HashBytes(testInfoBytes(t, info)).HexString()
	env.client.torrents[hash] = completedInfo(hash, "/data/pending", "/data", "tv")
	env.client.exports[hash] = testTorrentFile(t, info)

	ready := false
	engineReady = func() bool { return ready }

	env.tick()
	if len(env.imported) != 0 || env.client.exportCalls != 0 {
		t.Fatalf("import attempted while engine not ready: imported=%d exports=%d", len(env.imported), env.client.exportCalls)
	}
	if _, blocked := env.service.errorMap()[hash]; blocked {
		t.Fatal("engine not ready was recorded as a torrent error")
	}

	ready = true
	env.tick()
	if len(env.imported) != 1 {
		t.Fatalf("imported %d torrents once engine ready, want 1", len(env.imported))
	}
}

func TestAutoImportSkipsMetadatalessWithoutRetry(t *testing.T) {
	env := newTestEnv(t, importConfig())

	hash := "8888888888888888888888888888888888888888"
	env.client.torrents[hash] = completedInfo(hash, "/data/pending", "/data", "movie")
	env.client.exportErr = qbit.ErrNoMetadata

	for i := 0; i < retryAttempts+1; i++ {
		env.tick()
	}

	if len(env.imported) != 0 {
		t.Fatalf("imported %d metadata-less torrents, want 0", len(env.imported))
	}
	if _, ok := env.service.errorMap()[hash]; ok {
		t.Fatal("metadata-less torrent recorded an error")
	}
	if env.client.exportCalls != retryAttempts+1 {
		t.Fatalf("export calls = %d, want %d", env.client.exportCalls, retryAttempts+1)
	}

	env.client.exportErr = nil
	info := testInfo("pending", []metainfo.FileInfo{{Path: []string{"ep.mkv"}, Length: 32}})
	ready := metainfo.HashBytes(testInfoBytes(t, info)).HexString()
	delete(env.client.torrents, hash)
	env.client.torrents[ready] = completedInfo(ready, "/data/pending", "/data", "movie")
	env.client.exports[ready] = testTorrentFile(t, info)

	env.tick()
	if len(env.imported) != 1 {
		t.Fatalf("imported %d torrents once metadata arrived, want 1", len(env.imported))
	}
}
