package filestor

import (
	"bytes"
	"errors"
	"io"
	"math/rand"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/anacrolix/torrent/metainfo"
	ts "github.com/anacrolix/torrent/storage"
)

var (
	_ ts.ClientImpl  = (*Storage)(nil)
	_ ts.TorrentImpl = (*Torrent)(nil)
	_ ts.PieceImpl   = (*Piece)(nil)
	_ io.ReadSeeker  = (*Reader)(nil)
)

func withPieces(info *metainfo.Info) *metainfo.Info {
	count := (info.TotalLength() + info.PieceLength - 1) / info.PieceLength
	info.Pieces = make([]byte, count*20)
	return info
}

func coreFiles() []metainfo.FileInfo {
	return []metainfo.FileInfo{
		{Path: []string{"a.bin"}, Length: 7},
		{Path: []string{"sub", "b.bin"}, Length: 5},
		{Path: []string{"c.bin"}, Length: 9},
	}
}

func coreInfo() *metainfo.Info {
	return withPieces(&metainfo.Info{
		Name:        "core",
		PieceLength: 4,
		Files:       coreFiles(),
	})
}

func testData(length int64) []byte {
	data := make([]byte, length)
	for i := range data {
		data[i] = byte(i*7 + 1)
	}
	return data
}

func writeTree(t *testing.T, base string, files []metainfo.FileInfo, data []byte) {
	t.Helper()
	var offset int64
	for _, file := range files {
		full := filepath.Join(append([]string{base}, file.Path...)...)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, data[offset:offset+file.Length], 0o644); err != nil {
			t.Fatal(err)
		}
		offset += file.Length
	}
}

func openTorrent(t *testing.T, info *metainfo.Info, root string) *Torrent {
	t.Helper()
	stor := New(root)
	var hash metainfo.Hash
	hash[0] = 1
	impl, err := stor.OpenTorrent(info, hash)
	if err != nil {
		t.Fatalf("open torrent: %v", err)
	}
	tor := impl.(*Torrent)
	t.Cleanup(func() { tor.Close() })
	return tor
}

func coreTorrent(t *testing.T) (*Torrent, *metainfo.Info, []byte, string) {
	t.Helper()
	info := coreInfo()
	data := testData(info.TotalLength())
	root := t.TempDir()
	writeTree(t, root, info.Files, data)
	return openTorrent(t, info, root), info, data, root
}

func TestPieceReadMatchesConcatenation(t *testing.T) {
	tor, info, data, _ := coreTorrent(t)

	for i := 0; i < info.NumPieces(); i++ {
		mp := info.Piece(i)
		piece := tor.Piece(mp)
		for off := int64(0); off <= mp.Length(); off++ {
			for length := 0; length <= int(mp.Length())+2; length++ {
				buf := make([]byte, length)
				n, err := piece.ReadAt(buf, off)

				if off >= mp.Length() {
					if n != 0 || err != io.EOF {
						t.Fatalf("piece %d off %d len %d: got (%d, %v), want (0, EOF)", i, off, length, n, err)
					}
					continue
				}
				want := min(int64(length), mp.Length()-off)
				if int64(n) != want || err != nil {
					t.Fatalf("piece %d off %d len %d: got (%d, %v), want (%d, nil)", i, off, length, n, err, want)
				}
				start := mp.Offset() + off
				if !bytes.Equal(buf[:n], data[start:start+want]) {
					t.Fatalf("piece %d off %d len %d: content mismatch", i, off, length)
				}
			}
		}
	}
}

func TestLastPieceIsShort(t *testing.T) {
	tor, info, data, _ := coreTorrent(t)

	last := info.Piece(info.NumPieces() - 1)
	if last.Length() != 1 {
		t.Fatalf("last piece length: got %d, want 1", last.Length())
	}
	buf := make([]byte, 4)
	n, err := tor.Piece(last).ReadAt(buf, 0)
	if n != 1 || err != nil {
		t.Fatalf("read last piece: got (%d, %v), want (1, nil)", n, err)
	}
	if buf[0] != data[len(data)-1] {
		t.Fatalf("last byte: got %d, want %d", buf[0], data[len(data)-1])
	}
}

func TestLayoutCandidates(t *testing.T) {
	t.Run("MultiFileAtRoot", func(t *testing.T) {
		info := coreInfo()
		root := t.TempDir()
		writeTree(t, root, info.Files, testData(info.TotalLength()))
		layout, err := Resolve(info, root)
		if err != nil {
			t.Fatal(err)
		}
		if layout.Base != root {
			t.Fatalf("base: got %s, want %s", layout.Base, root)
		}
	})

	t.Run("MultiFileInNamedDir", func(t *testing.T) {
		info := coreInfo()
		root := t.TempDir()
		base := filepath.Join(root, info.BestName())
		writeTree(t, base, info.Files, testData(info.TotalLength()))
		layout, err := Resolve(info, root)
		if err != nil {
			t.Fatal(err)
		}
		if layout.Base != base {
			t.Fatalf("base: got %s, want %s", layout.Base, base)
		}
	})

	t.Run("SingleFileInRoot", func(t *testing.T) {
		info := withPieces(&metainfo.Info{Name: "movie.mkv", PieceLength: 4, Length: 21})
		root := t.TempDir()
		path := filepath.Join(root, info.Name)
		if err := os.WriteFile(path, testData(21), 0o644); err != nil {
			t.Fatal(err)
		}
		layout, err := Resolve(info, root)
		if err != nil {
			t.Fatal(err)
		}
		if len(layout.Files) != 1 || layout.Files[0].Path != path {
			t.Fatalf("files: got %+v, want single %s", layout.Files, path)
		}
	})

	t.Run("SingleFileAsRoot", func(t *testing.T) {
		info := withPieces(&metainfo.Info{Name: "movie.mkv", PieceLength: 4, Length: 21})
		root := t.TempDir()
		path := filepath.Join(root, "renamed.mkv")
		if err := os.WriteFile(path, testData(21), 0o644); err != nil {
			t.Fatal(err)
		}
		layout, err := Resolve(info, path)
		if err != nil {
			t.Fatal(err)
		}
		if len(layout.Files) != 1 || layout.Files[0].Path != path {
			t.Fatalf("files: got %+v, want single %s", layout.Files, path)
		}
	})
}

func TestResolveRejectsMissingFile(t *testing.T) {
	info := coreInfo()
	root := t.TempDir()
	writeTree(t, root, info.Files, testData(info.TotalLength()))
	if err := os.Remove(filepath.Join(root, "sub", "b.bin")); err != nil {
		t.Fatal(err)
	}

	_, err := PreValidate(info, root)
	var missing *MissingFileError
	if !errors.As(err, &missing) {
		t.Fatalf("got %v, want MissingFileError", err)
	}
	if filepath.Base(missing.Path) != "b.bin" {
		t.Fatalf("path: got %s, want .../b.bin", missing.Path)
	}
}

func TestResolveRejectsSizeMismatch(t *testing.T) {
	info := coreInfo()
	root := t.TempDir()
	writeTree(t, root, info.Files, testData(info.TotalLength()))
	if err := os.WriteFile(filepath.Join(root, "sub", "b.bin"), []byte("abcd"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := PreValidate(info, root)
	var mismatch *SizeMismatchError
	if !errors.As(err, &mismatch) {
		t.Fatalf("got %v, want SizeMismatchError", err)
	}
	if mismatch.Want != 5 || mismatch.Got != 4 {
		t.Fatalf("sizes: got want=%d got=%d, expected want=5 got=4", mismatch.Want, mismatch.Got)
	}
}

func TestResolveRejectsIrregularFile(t *testing.T) {
	info := coreInfo()
	root := t.TempDir()
	writeTree(t, root, info.Files, testData(info.TotalLength()))
	if err := os.Remove(filepath.Join(root, "a.bin")); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "a.bin"), 0o755); err != nil {
		t.Fatal(err)
	}

	_, err := PreValidate(info, root)
	var irregular *IrregularFileError
	if !errors.As(err, &irregular) {
		t.Fatalf("got %v, want IrregularFileError", err)
	}
}

func TestResolveRejectsPathEscape(t *testing.T) {
	escapes := [][]string{
		{"..", "evil.bin"},
		{"sub", "..", "..", "evil.bin"},
		{string(filepath.Separator) + "etc", "passwd"},
	}
	for _, path := range escapes {
		info := withPieces(&metainfo.Info{
			Name:        "core",
			PieceLength: 4,
			Files:       []metainfo.FileInfo{{Path: path, Length: 4}},
		})
		_, err := PreValidate(info, t.TempDir())
		var escape *PathEscapeError
		if !errors.As(err, &escape) {
			t.Fatalf("path %v: got %v, want PathEscapeError", path, err)
		}
	}
}

func TestResolveRejectsEscapingTorrentName(t *testing.T) {
	outside := t.TempDir()
	root := filepath.Join(outside, "downloads")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outside, "secret.bin"), []byte("secret data"), 0o644); err != nil {
		t.Fatal(err)
	}

	for _, name := range []string{"..", "../..", "sub/..", string(filepath.Separator) + "etc"} {
		info := withPieces(&metainfo.Info{
			Name:        name,
			PieceLength: 4,
			Files:       []metainfo.FileInfo{{Path: []string{"secret.bin"}, Length: 11}},
		})
		layout, err := Resolve(info, root)
		if err == nil {
			t.Fatalf("name %q resolved outside root: %+v", name, layout)
		}
		var escape *PathEscapeError
		if !errors.As(err, &escape) {
			t.Fatalf("name %q: got %v, want PathEscapeError", name, err)
		}
	}
}

func TestTruncatedFileReturnsUnexpectedEOF(t *testing.T) {
	tor, info, _, root := coreTorrent(t)

	if err := os.Truncate(filepath.Join(root, "sub", "b.bin"), 2); err != nil {
		t.Fatal(err)
	}

	piece := tor.Piece(info.Piece(2))
	buf := make([]byte, 4)
	n, err := piece.ReadAt(buf, 0)
	if !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("got (%d, %v), want io.ErrUnexpectedEOF", n, err)
	}
}

func TestWriteAtDiscardsAndLeavesDiskUntouched(t *testing.T) {
	tor, info, data, root := coreTorrent(t)

	before := make(map[string][]byte)
	for _, file := range info.Files {
		full := filepath.Join(append([]string{root}, file.Path...)...)
		content, err := os.ReadFile(full)
		if err != nil {
			t.Fatal(err)
		}
		before[full] = content
	}

	piece := tor.Piece(info.Piece(0))
	garbage := []byte{0xDE, 0xAD, 0xBE, 0xEF}
	n, err := piece.WriteAt(garbage, 0)
	if n != len(garbage) || err != nil {
		t.Fatalf("write: got (%d, %v), want (%d, nil)", n, err, len(garbage))
	}
	if got := tor.discarded.Load(); got != int64(len(garbage)) {
		t.Fatalf("discarded: got %d, want %d", got, len(garbage))
	}

	for full, content := range before {
		current, err := os.ReadFile(full)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(current, content) {
			t.Fatalf("file %s modified by WriteAt", full)
		}
	}

	buf := make([]byte, 4)
	if _, err := piece.ReadAt(buf, 0); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(buf, data[:4]) {
		t.Fatalf("piece content changed: got %v, want %v", buf, data[:4])
	}
}

func TestConcurrentPieceReads(t *testing.T) {
	tor, info, data, _ := coreTorrent(t)

	var wg sync.WaitGroup
	for worker := 0; worker < 8; worker++ {
		wg.Add(1)
		go func(seed int64) {
			defer wg.Done()
			rnd := rand.New(rand.NewSource(seed))
			for i := 0; i < 200; i++ {
				index := rnd.Intn(info.NumPieces())
				mp := info.Piece(index)
				off := rnd.Int63n(mp.Length())
				buf := make([]byte, mp.Length()-off)
				n, err := tor.Piece(mp).ReadAt(buf, off)
				if err != nil {
					t.Errorf("piece %d off %d: %v", index, off, err)
					return
				}
				start := mp.Offset() + off
				if !bytes.Equal(buf[:n], data[start:start+int64(n)]) {
					t.Errorf("piece %d off %d: content mismatch", index, off)
					return
				}
			}
		}(int64(worker))
	}
	wg.Wait()
}

func TestReaderSeeksAcrossFileBoundary(t *testing.T) {
	tor, _, data, _ := coreTorrent(t)

	var closes int
	reader := tor.NewFileReader(0, int64(len(data)), func() { closes++ })

	pos, err := reader.Seek(6, io.SeekStart)
	if err != nil || pos != 6 {
		t.Fatalf("seek: got (%d, %v), want (6, nil)", pos, err)
	}
	buf := make([]byte, 4)
	if _, err := io.ReadFull(reader, buf); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(buf, data[6:10]) {
		t.Fatalf("cross-boundary read: got %v, want %v", buf, data[6:10])
	}

	if _, err := reader.Seek(-3, io.SeekEnd); err != nil {
		t.Fatal(err)
	}
	rest, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(rest, data[len(data)-3:]) {
		t.Fatalf("tail read: got %v, want %v", rest, data[len(data)-3:])
	}

	reader.Close()
	reader.Close()
	if closes != 1 {
		t.Fatalf("onClose calls: got %d, want 1", closes)
	}
	if _, err := reader.Read(buf); !errors.Is(err, os.ErrClosed) {
		t.Fatalf("read after close: got %v, want os.ErrClosed", err)
	}
}

func TestReaderOverSingleFileRange(t *testing.T) {
	tor, _, data, _ := coreTorrent(t)

	reader := tor.NewFileReader(7, 5, nil)
	defer reader.Close()

	content, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(content, data[7:12]) {
		t.Fatalf("range read: got %v, want %v", content, data[7:12])
	}
	if n, err := reader.Read(make([]byte, 4)); n != 0 || err != io.EOF {
		t.Fatalf("read at end: got (%d, %v), want (0, EOF)", n, err)
	}
}

func TestPadFilesReadAsZeros(t *testing.T) {
	info := withPieces(&metainfo.Info{
		Name:        "padded",
		PieceLength: 4,
		Files: []metainfo.FileInfo{
			{Path: []string{"a.bin"}, Length: 7},
			{Path: []string{".pad", "5"}, Length: 5},
			{Path: []string{"c.bin"}, Length: 9},
		},
	})
	data := testData(info.TotalLength())
	clear(data[7:12])

	root := t.TempDir()
	writeTree(t, root, []metainfo.FileInfo{info.Files[0], info.Files[2]}, append(append([]byte{}, data[:7]...), data[12:]...))

	tor := openTorrent(t, info, root)
	buf := make([]byte, info.TotalLength())
	n, err := tor.readAt(buf, 0)
	if err != nil || int64(n) != info.TotalLength() {
		t.Fatalf("read all: got (%d, %v)", n, err)
	}
	if !bytes.Equal(buf, data) {
		t.Fatalf("padded content: got %v, want %v", buf, data)
	}
}

func TestOpenedTracksTorrentLifetime(t *testing.T) {
	info := coreInfo()
	root := t.TempDir()
	writeTree(t, root, info.Files, testData(info.TotalLength()))

	stor := New(root)
	var hash metainfo.Hash
	hash[0] = 2
	impl, err := stor.OpenTorrent(info, hash)
	if err != nil {
		t.Fatal(err)
	}
	if stor.Opened(hash) != impl {
		t.Fatal("Opened did not return the opened torrent")
	}
	if err := impl.Close(); err != nil {
		t.Fatal(err)
	}
	if stor.Opened(hash) != nil {
		t.Fatal("Opened returned a closed torrent")
	}
	if err := stor.Close(); err != nil {
		t.Fatal(err)
	}
}
