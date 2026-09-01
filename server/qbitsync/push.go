package qbitsync

import (
	"bytes"
	"fmt"
	"net/url"
	"strings"

	"github.com/anacrolix/torrent/metainfo"

	"server/qbit"
	"server/torr"
)

func Push(hashHex string) error {
	return svc.push(hashHex)
}

func (s *service) push(hashHex string) error {
	client, err := s.acquire()
	if err != nil {
		return err
	}

	hash := strings.ToLower(strings.TrimSpace(hashHex))
	tor := torr.GetTorrent(hash)
	if tor == nil {
		return fmt.Errorf("qbittorrent: torrent not found: %s", hash)
	}

	cfg := settingsConfig()
	category := normalizeCategory(tor.Category)
	_ = client.CreateCategory(category, "")

	opts := qbit.AddOptions{
		SavePath:           cfg.SavePath,
		Category:           category,
		Tags:               cfg.Tags,
		SequentialDownload: cfg.SequentialDownload,
		FirstLastPiecePrio: cfg.FirstLastPiecePrio,
	}
	if data := torrentFile(tor); len(data) > 0 {
		opts.Torrent = data
	} else {
		opts.URLs = []string{magnetLink(tor)}
	}

	if err = client.Add(opts); err != nil {
		s.setError(hash, err)
		return err
	}

	s.clearError(hash)
	s.invalidateSnapshot()
	return nil
}

func torrentFile(tor *torr.Torrent) []byte {
	infoBytes := torrentInfoBytes(tor)
	if len(infoBytes) == 0 {
		return nil
	}

	mi := metainfo.MetaInfo{InfoBytes: infoBytes}
	if trackers := torrentTrackers(tor); len(trackers) > 0 {
		mi.Announce = trackers[0]
		mi.AnnounceList = metainfo.AnnounceList{trackers}
	}

	buf := new(bytes.Buffer)
	if err := mi.Write(buf); err != nil {
		return nil
	}
	return buf.Bytes()
}

func magnetLink(tor *torr.Torrent) string {
	link := "magnet:?xt=urn:btih:" + tor.Hash().HexString()
	for _, tracker := range torrentTrackers(tor) {
		link += "&tr=" + url.QueryEscape(tracker)
	}
	return link
}

func torrentInfoBytes(tor *torr.Torrent) []byte {
	if tor.Torrent != nil && tor.Torrent.Info() != nil {
		if data := tor.Torrent.Metainfo().InfoBytes; len(data) > 0 {
			return data
		}
	}
	if tor.TorrentSpec != nil {
		return tor.TorrentSpec.InfoBytes
	}
	return nil
}

func torrentTrackers(tor *torr.Torrent) []string {
	if tor.TorrentSpec == nil {
		return nil
	}
	var trackers []string
	for _, tier := range tor.TorrentSpec.Trackers {
		for _, tracker := range tier {
			if tracker != "" {
				trackers = append(trackers, tracker)
			}
		}
	}
	return trackers
}
