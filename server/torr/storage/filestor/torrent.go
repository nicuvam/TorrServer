package filestor

import (
	"io"
	"log"
	"os"
	"sort"
	"sync"
	"sync/atomic"

	"github.com/anacrolix/torrent/metainfo"
	ts "github.com/anacrolix/torrent/storage"
)

type span struct {
	start int64
	end   int64
	index int
}

type Torrent struct {
	storage *Storage
	hash    metainfo.Hash
	layout  *Layout
	spans   []span
	length  int64

	mu      sync.Mutex
	handles map[int]*os.File
	closed  bool

	discarded atomic.Int64
	warnOnce  sync.Once
}

func newTorrent(storage *Storage, hash metainfo.Hash, layout *Layout) *Torrent {
	tor := &Torrent{
		storage: storage,
		hash:    hash,
		layout:  layout,
		handles: make(map[int]*os.File),
	}
	var offset int64
	for i, file := range layout.Files {
		if file.Length <= 0 {
			continue
		}
		tor.spans = append(tor.spans, span{start: offset, end: offset + file.Length, index: i})
		offset += file.Length
	}
	tor.length = offset
	return tor
}

func (t *Torrent) Piece(p metainfo.Piece) ts.PieceImpl {
	return &Piece{torrent: t, offset: p.Offset(), length: p.Length()}
}

func (t *Torrent) NewFileReader(base, length int64, onClose func()) *Reader {
	return &Reader{torrent: t, base: base, length: length, onClose: onClose}
}

func (t *Torrent) Close() error {
	t.storage.forget(t)
	t.mu.Lock()
	handles := t.handles
	t.handles = make(map[int]*os.File)
	t.closed = true
	t.mu.Unlock()
	for _, handle := range handles {
		handle.Close()
	}
	return nil
}

func (t *Torrent) readAt(b []byte, off int64) (int, error) {
	if off < 0 {
		return 0, os.ErrInvalid
	}
	read := 0
	for read < len(b) {
		if off >= t.length {
			return read, io.EOF
		}
		s := t.spanAt(off)
		want := int64(len(b) - read)
		if want > s.end-off {
			want = s.end - off
		}
		if t.layout.Files[s.index].Pad {
			clear(b[read : read+int(want)])
			read += int(want)
			off += want
			continue
		}
		handle, err := t.handle(s.index)
		if err != nil {
			return read, err
		}
		n, err := handle.ReadAt(b[read:read+int(want)], off-s.start)
		read += n
		off += int64(n)
		if err == io.EOF {
			return read, io.ErrUnexpectedEOF
		}
		if err != nil {
			return read, err
		}
		if n == 0 {
			return read, io.ErrUnexpectedEOF
		}
	}
	return read, nil
}

func (t *Torrent) spanAt(off int64) span {
	i := sort.Search(len(t.spans), func(i int) bool { return t.spans[i].end > off })
	return t.spans[i]
}

func (t *Torrent) handle(index int) (*os.File, error) {
	t.mu.Lock()
	if t.closed {
		t.mu.Unlock()
		return nil, os.ErrClosed
	}
	if handle, ok := t.handles[index]; ok {
		t.mu.Unlock()
		return handle, nil
	}
	t.mu.Unlock()

	opened, err := os.Open(t.layout.Files[index].Path)
	if err != nil {
		return nil, err
	}

	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed {
		opened.Close()
		return nil, os.ErrClosed
	}
	if handle, ok := t.handles[index]; ok {
		opened.Close()
		return handle, nil
	}
	t.handles[index] = opened
	return opened, nil
}

func (t *Torrent) discardWrite(n int) {
	t.discarded.Add(int64(n))
	t.warnOnce.Do(func() {
		log.Printf("filestor: discarding writes for torrent %s backed by %s", t.hash.HexString(), t.layout.Base)
	})
}
