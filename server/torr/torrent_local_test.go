package torr

import (
	"io"
	"testing"
	"time"

	"server/settings"
	"server/torr/state"
	"server/torr/storage/filestor"
	"server/torr/storage/torrstor"
)

type fakeReader struct {
	closed int
}

func (r *fakeReader) Read([]byte) (int, error)       { return 0, io.EOF }
func (r *fakeReader) Seek(int64, int) (int64, error) { return 0, nil }
func (r *fakeReader) SetReadahead(int64)             {}
func (r *fakeReader) SetResponsive()                 {}
func (r *fakeReader) Close()                         { r.closed++ }

func useTestBTSets(t *testing.T) {
	t.Helper()
	prev := settings.BTsets
	settings.BTsets = &settings.BTSets{TorrentDisconnectTimeout: 30}
	t.Cleanup(func() { settings.BTsets = prev })
}

func TestIsLocalFollowsStorageBinding(t *testing.T) {
	tor := &Torrent{LocalPath: "/media/movies"}
	if tor.isLocal() {
		t.Fatal("torrent without local storage must not be local")
	}
	tor.fstor = filestor.New("/media/movies")
	if !tor.isLocal() {
		t.Fatal("torrent bound to filestor must be local")
	}
}

func TestActiveReadersIgnoresLocalPathFlip(t *testing.T) {
	cached := &Torrent{cache: torrstor.NewCache(1<<20, nil), Stat: state.TorrentWorking}
	cached.LocalPath = "/media/movies"
	cached.localReaders.Store(2)
	if got := cached.ActiveReaders(); got != 0 {
		t.Fatalf("cache backed torrent must count cache readers, got %d", got)
	}

	local := &Torrent{
		LocalPath: "/media/movies",
		Stat:      state.TorrentWorking,
		fstor:     filestor.New("/media/movies"),
	}
	local.localReaders.Store(2)
	if got := local.ActiveReaders(); got != 2 {
		t.Fatalf("local torrent must count local readers, got %d", got)
	}
	local.expiredTime = time.Now().Add(-time.Minute)
	if local.expired() {
		t.Fatal("local torrent with active readers must not expire")
	}
}

func TestCloseReaderDispatchesOnReaderType(t *testing.T) {
	useTestBTSets(t)

	cached := &Torrent{cache: torrstor.NewCache(1<<20, nil)}
	cachedReader := &fakeReader{}
	cached.CloseReader(cachedReader)
	if cachedReader.closed != 1 {
		t.Fatalf("local reader must be closed on a cache backed torrent, closed=%d", cachedReader.closed)
	}

	local := &Torrent{fstor: filestor.New("/media/movies")}
	localReader := &fakeReader{}
	local.CloseReader(localReader)
	if localReader.closed != 1 {
		t.Fatalf("reader must be closed on a local torrent, closed=%d", localReader.closed)
	}
}
