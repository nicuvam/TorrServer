package qbitsync

import (
	"strings"
	"time"

	"server/qbit"
)

const snapshotTTL = 2 * time.Second

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

func (s *service) markDemand() {
	s.mu.Lock()
	s.demandAt = nowFunc()
	s.mu.Unlock()
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
			data[strings.ToLower(info.Hash)] = info
		}
		s.snapshot = data
	}
	s.mu.Unlock()

	if err != nil {
		s.logProblem(err)
	}
}
