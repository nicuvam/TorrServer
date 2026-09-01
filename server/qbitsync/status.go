package qbitsync

import (
	"strings"

	"server/torr/state"
)

func Enrich(list []*state.TorrentStatus) {
	if len(list) == 0 || !Enabled() {
		return
	}

	snapshot := svc.snapshotView(true)
	errors := svc.errorMap()

	for _, status := range list {
		if status == nil {
			continue
		}
		hash := strings.ToLower(status.Hash)
		if info, ok := snapshot[hash]; ok {
			status.QBitState = info.State
			status.QBitProgress = info.Progress
			status.QBitDlSpeed = info.DlSpeed
			status.QBitEta = info.ETA
			status.QBitCompletedOn = info.CompletionOn
		}
		if message, ok := errors[hash]; ok {
			status.QBitError = message
		}
	}
}
