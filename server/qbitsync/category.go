package qbitsync

import "strings"

func SyncCategoryToQBit(hashHex, category string) error {
	return svc.syncCategoryToQBit(hashHex, category)
}

func (s *service) syncCategoryToQBit(hashHex, category string) error {
	name := strings.ToLower(strings.TrimSpace(category))
	if !isMirroredCategory(name) {
		return nil
	}

	hash := normalizeHash(hashHex)
	if _, present := s.snapshotData()[hash]; !present {
		return nil
	}

	client, err := s.acquire()
	if err != nil {
		return err
	}
	if err = client.SetCategory([]string{hash}, name); err != nil {
		return err
	}

	s.rememberSnapshotCategory(hash, name)
	return nil
}
