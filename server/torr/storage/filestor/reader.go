package filestor

import (
	"io"
	"os"
	"sync"
)

type Reader struct {
	torrent *Torrent
	base    int64
	length  int64

	mu     sync.Mutex
	offset int64
	closed bool

	onClose   func()
	closeOnce sync.Once
}

func (r *Reader) Read(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return 0, os.ErrClosed
	}
	if r.offset >= r.length {
		return 0, io.EOF
	}
	if int64(len(p)) > r.length-r.offset {
		p = p[:r.length-r.offset]
	}
	if len(p) == 0 {
		return 0, nil
	}
	n, err := r.torrent.readAt(p, r.base+r.offset)
	r.offset += int64(n)
	if err == io.EOF && r.offset < r.length {
		err = io.ErrUnexpectedEOF
	}
	return n, err
}

func (r *Reader) Seek(offset int64, whence int) (int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var abs int64
	switch whence {
	case io.SeekStart:
		abs = offset
	case io.SeekCurrent:
		abs = r.offset + offset
	case io.SeekEnd:
		abs = r.length + offset
	default:
		return 0, os.ErrInvalid
	}
	if abs < 0 {
		return 0, os.ErrInvalid
	}
	r.offset = abs
	return abs, nil
}

func (r *Reader) SetReadahead(int64) {}

func (r *Reader) SetResponsive() {}

func (r *Reader) Close() {
	r.mu.Lock()
	r.closed = true
	r.mu.Unlock()
	r.closeOnce.Do(func() {
		if r.onClose != nil {
			r.onClose()
		}
	})
}
