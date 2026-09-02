package qbitsync

import (
	"strings"
	"time"

	"server/qbit"
)

const (
	snapshotTTL        = 2 * time.Second
	snapshotStaleAfter = time.Minute
	unreachableMessage = "qBittorrent unreachable"
)

func Snapshot() map[string]qbit.TorrentInfo {
	return svc.snapshotView(true)
}

func (s *service) snapshotView(demand bool) map[string]qbit.TorrentInfo {
	if demand {
		s.markDemand()
	}

	s.mu.Lock()
	data := s.snapshot
	fresh := nowFunc().Sub(s.snapshotAt) < snapshotTTL
	s.mu.Unlock()

	if !fresh && Enabled() {
		go s.refreshSnapshot()
	}
	return data
}

func (s *service) snapshotData() map[string]qbit.TorrentInfo {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.snapshot
}

func (s *service) vanishedRemovalsAllowed() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.snapshotError == "" && s.snapshotOkRuns > 1
}

func (s *service) seenInQBit(hash string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.seen[hash]
}

func (s *service) forgetSeen(hash string) {
	s.mu.Lock()
	delete(s.seen, hash)
	s.mu.Unlock()
}

func (s *service) rememberSnapshotCategory(hash, category string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	info, ok := s.snapshot[hash]
	if !ok || strings.EqualFold(info.Category, category) {
		return
	}
	info.Category = category
	updated := make(map[string]qbit.TorrentInfo, len(s.snapshot))
	for key, value := range s.snapshot {
		updated[key] = value
	}
	updated[hash] = info
	s.snapshot = updated
}

func (s *service) snapshotUnreachable() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.snapshotError != "" && nowFunc().Sub(s.snapshotOkAt) > snapshotStaleAfter
}

func (s *service) markDemand() {
	s.mu.Lock()
	idle := s.demandAt.IsZero() || nowFunc().Sub(s.demandAt) >= demandWindow
	s.demandAt = nowFunc()
	s.mu.Unlock()
	if idle {
		select {
		case s.wake <- struct{}{}:
		default:
		}
	}
}

func (s *service) invalidateSnapshot() {
	s.mu.Lock()
	s.snapshotAt = time.Time{}
	s.mu.Unlock()
}

func (s *service) refreshSnapshot() {
	client, err := s.acquire()
	if err != nil {
		return
	}

	s.mu.Lock()
	if s.snapshotLoading {
		s.mu.Unlock()
		return
	}
	s.snapshotLoading = true
	s.mu.Unlock()

	list, err := client.Info(nil)

	s.mu.Lock()
	s.snapshotLoading = false
	s.snapshotAt = nowFunc()
	if err == nil {
		data := make(map[string]qbit.TorrentInfo, len(list))
		for _, info := range list {
			hash := strings.ToLower(info.Hash)
			data[hash] = info
			s.seen[hash] = true
		}
		s.snapshot = data
		s.snapshotOkAt = s.snapshotAt
		s.snapshotError = ""
		s.snapshotOkRuns++
	} else {
		s.snapshotError = err.Error()
	}
	s.mu.Unlock()

	if err != nil {
		s.logProblem(err)
	}
}
