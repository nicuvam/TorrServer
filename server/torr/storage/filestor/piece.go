package filestor

import (
	"io"
	"os"

	ts "github.com/anacrolix/torrent/storage"
)

type Piece struct {
	torrent *Torrent
	offset  int64
	length  int64
}

func (p *Piece) ReadAt(b []byte, off int64) (int, error) {
	if off < 0 {
		return 0, os.ErrInvalid
	}
	if off >= p.length {
		return 0, io.EOF
	}
	if int64(len(b)) > p.length-off {
		b = b[:p.length-off]
	}
	if len(b) == 0 {
		return 0, nil
	}
	return p.torrent.readAt(b, p.offset+off)
}

func (p *Piece) WriteAt(b []byte, off int64) (int, error) {
	p.torrent.discardWrite(len(b))
	return len(b), nil
}

func (p *Piece) MarkComplete() error {
	return nil
}

func (p *Piece) MarkNotComplete() error {
	return nil
}

func (p *Piece) Completion() ts.Completion {
	return ts.Completion{Complete: true, Ok: true}
}
