package settings

import (
	"encoding/json"
	"testing"

	"github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/metainfo"
	"github.com/anacrolix/torrent/storage"
)

type stubClientImpl struct{}

func (stubClientImpl) OpenTorrent(info *metainfo.Info, infoHash metainfo.Hash) (storage.TorrentImpl, error) {
	return nil, nil
}

func (stubClientImpl) Close() error { return nil }

func TestTorrentDBLiveStorageBreaksUnmarshal(t *testing.T) {
	hash := metainfo.NewHashFromHex("0102030405060708090a0b0c0d0e0f1011121314")
	db := &TorrentDB{
		TorrentSpec: &torrent.TorrentSpec{
			InfoHash: hash,
			Storage:  stubClientImpl{},
		},
		Title:     "movie",
		LocalPath: "/data/movies/movie",
	}

	buf, err := json.Marshal(db)
	if err != nil {
		t.Fatal("marshal with live storage:", err)
	}

	var back *TorrentDB
	if err := json.Unmarshal(buf, &back); err == nil {
		t.Fatal("expected unmarshal of a record holding a live storage to fail")
	}
}

func TestTorrentDBSanitizedSpecRoundTrip(t *testing.T) {
	hash := metainfo.NewHashFromHex("0102030405060708090a0b0c0d0e0f1011121314")
	spec := torrent.TorrentSpec{
		InfoHash:  hash,
		InfoBytes: []byte("d4:infoe"),
		Storage:   stubClientImpl{},
	}
	spec.Storage = nil

	db := &TorrentDB{
		TorrentSpec: &spec,
		Title:       "movie",
		LocalPath:   "/data/movies/movie",
	}

	buf, err := json.Marshal(db)
	if err != nil {
		t.Fatal("marshal sanitized spec:", err)
	}

	var back *TorrentDB
	if err := json.Unmarshal(buf, &back); err != nil {
		t.Fatal("unmarshal sanitized spec:", err)
	}
	if back.InfoHash != hash {
		t.Fatalf("info hash: want %v, got %v", hash, back.InfoHash)
	}
	if string(back.InfoBytes) != string(spec.InfoBytes) {
		t.Fatalf("info bytes: want %q, got %q", spec.InfoBytes, back.InfoBytes)
	}
	if back.LocalPath != db.LocalPath {
		t.Fatalf("local path: want %q, got %q", db.LocalPath, back.LocalPath)
	}
	if back.Storage != nil {
		t.Fatal("storage must stay nil after round trip")
	}
}
