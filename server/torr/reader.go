package torr

import "io"

type Reader interface {
	io.Reader
	io.Seeker
	SetReadahead(int64)
	SetResponsive()
	Close()
}
