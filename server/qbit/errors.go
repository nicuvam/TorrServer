package qbit

import (
	"errors"
	"fmt"
)

var (
	ErrNotConfigured = errors.New("qbittorrent: not configured")
	ErrAuth          = errors.New("qbittorrent: authentication failed")
	ErrBanned        = errors.New("qbittorrent: login banned")
	ErrUnreachable   = errors.New("qbittorrent: unreachable")
	ErrBadTorrent    = errors.New("qbittorrent: torrent rejected")
	ErrNotFound      = errors.New("qbittorrent: not found")
)

type APIError struct {
	Status int
	Body   string
}

func (e *APIError) Error() string {
	if e.Body == "" {
		return fmt.Sprintf("qbittorrent: unexpected status %d", e.Status)
	}
	return fmt.Sprintf("qbittorrent: unexpected status %d: %s", e.Status, e.Body)
}
