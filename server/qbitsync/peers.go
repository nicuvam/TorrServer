package qbitsync

import (
	"net"
	"net/url"
	"strings"
	"time"

	"github.com/anacrolix/torrent"
)

const prefsTTL = 5 * time.Minute

func PeerSource() []torrent.Peer {
	return svc.peers()
}

func (s *service) peers() []torrent.Peer {
	client, err := s.acquire()
	if err != nil {
		return nil
	}

	port, ok := s.listenPort(client)
	if !ok {
		return nil
	}

	host := clientHost(settingsConfig().URL)
	if host == "" {
		return nil
	}
	addrs, err := net.LookupIP(host)
	if err != nil {
		return nil
	}

	peers := make([]torrent.Peer, 0, len(addrs))
	for _, addr := range addrs {
		peers = append(peers, torrent.Peer{IP: addr, Port: port})
	}
	return peers
}

func (s *service) listenPort(client clientAPI) (int, bool) {
	s.mu.Lock()
	if !s.prefsAt.IsZero() && nowFunc().Sub(s.prefsAt) < prefsTTL {
		port := s.prefs.ListenPort
		s.mu.Unlock()
		return port, port > 0
	}
	if s.prefsLoading {
		s.mu.Unlock()
		return 0, false
	}
	s.prefsLoading = true
	s.mu.Unlock()

	prefs, err := client.Preferences()

	s.mu.Lock()
	s.prefsLoading = false
	s.prefsAt = nowFunc()
	if err == nil {
		s.prefs = prefs
	}
	s.mu.Unlock()

	if err != nil {
		s.logProblem(err)
		return 0, false
	}
	return prefs.ListenPort, prefs.ListenPort > 0
}

func clientHost(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if !strings.Contains(raw, "://") {
		raw = "http://" + raw
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return parsed.Hostname()
}
