package qbitsync

import (
	"strings"

	"server/torr/state"
)

func Enrich(list []*state.TorrentStatus) {
	svc.enrich(list)
}

func (s *service) enrich(list []*state.TorrentStatus) {
	if len(list) == 0 || !Enabled() {
		return
	}

	snapshot := s.snapshotView(true)
	errors := s.errorMap()
	unreachable := s.snapshotUnreachable()

	for _, status := range list {
		if status == nil {
			continue
		}
		hash := strings.ToLower(status.Hash)
		if info, ok := snapshot[hash]; ok {
			status.QBitState = info.State
			status.QBitProgress = info.Progress
			status.QBitDownloadSpeed = info.DlSpeed
			status.QBitEta = info.ETA
			if unreachable {
				status.QBitError = unreachableMessage
			}
		}
		if message, ok := errors[hash]; ok {
			status.QBitError = message
		}
	}
}
