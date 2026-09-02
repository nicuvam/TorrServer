package torr

import (
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"server/torrshash"

	utils2 "server/utils"

	"github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/metainfo"

	"server/log"
	"server/settings"
	"server/torr/state"
	"server/torr/storage/filestor"
	cacheSt "server/torr/storage/state"
	"server/torr/storage/torrstor"
	"server/torr/utils"
)

type Torrent struct {
	Title     string
	Category  string
	Poster    string
	Data      string
	LocalPath string
	*torrent.TorrentSpec

	Stat      state.TorrentStat
	Timestamp int64
	Size      int64

	*torrent.Torrent
	muTorrent sync.Mutex

	bt    *BTServer
	cache *torrstor.Cache

	fstor        *filestor.Storage
	lstor        *filestor.Torrent
	localReaders atomic.Int64

	lastTimeSpeed       time.Time
	DownloadSpeed       float64
	UploadSpeed         float64
	BytesReadUsefulData int64
	BytesWrittenData    int64

	PreloadSize    int64
	PreloadedBytes int64

	DurationSeconds float64
	BitRate         string

	expiredTime time.Time

	closed <-chan struct{}

	progressTicker *time.Ticker
}

var peerSource func(metainfo.Hash) []torrent.Peer

func SetPeerSource(fn func(metainfo.Hash) []torrent.Peer) {
	peerSource = fn
}

func checkLocalPath(localPath string) error {
	if settings.BTsets == nil || settings.BTsets.TorrentsSavePath == "" {
		return nil
	}
	cachePath := filepath.Clean(settings.BTsets.TorrentsSavePath)
	sep := string(filepath.Separator)
	if localPath == cachePath ||
		strings.HasPrefix(localPath, cachePath+sep) ||
		strings.HasPrefix(cachePath, localPath+sep) {
		return fmt.Errorf("local path %s overlaps the torrents save path %s", localPath, cachePath)
	}
	return nil
}

func prepareLocalStorage(spec *torrent.TorrentSpec, localPath string) (*filestor.Storage, error) {
	if len(spec.InfoBytes) > 0 {
		mi := metainfo.MetaInfo{InfoBytes: spec.InfoBytes}
		info, err := mi.UnmarshalInfo()
		if err != nil {
			return nil, err
		}
		if _, err := filestor.PreValidate(&info, localPath); err != nil {
			return nil, err
		}
	}
	return filestor.New(localPath), nil
}

func NewTorrent(spec *torrent.TorrentSpec, bt *BTServer, localPath string) (*Torrent, error) {
	// https://github.com/anacrolix/torrent/issues/747
	if bt == nil || bt.client == nil {
		return nil, errors.New("BT client not connected")
	}

	if localPath == "" {
		if torDB := GetTorrentDB(spec.InfoHash); torDB != nil {
			localPath = torDB.LocalPath
		}
	}
	if localPath != "" {
		localPath = filepath.Clean(localPath)
		if err := checkLocalPath(localPath); err != nil {
			return nil, err
		}
	}

	loaded := bt.GetTorrent(spec.InfoHash)
	if loaded != nil && loaded.LocalPath != localPath {
		return nil, fmt.Errorf("torrent %s is already loaded with a different storage, drop it first",
			spec.InfoHash.HexString())
	}

	switch settings.BTsets.RetrackersMode {
	case 1:
		spec.Trackers = append(spec.Trackers, [][]string{utils.GetDefTrackers()}...)
	case 2:
		spec.Trackers = nil
	case 3:
		spec.Trackers = [][]string{utils.GetDefTrackers()}
	}

	trackers := utils.GetTrackerFromFile()
	if len(trackers) > 0 {
		spec.Trackers = append(spec.Trackers, [][]string{trackers}...)
	}

	var fstor *filestor.Storage
	if localPath != "" && loaded == nil {
		var err error
		fstor, err = prepareLocalStorage(spec, localPath)
		if err != nil {
			return nil, err
		}
		spec.Storage = fstor
	}

	goTorrent, _, err := bt.client.AddTorrentSpec(spec)
	if err != nil {
		if fstor != nil && goTorrent != nil {
			goTorrent.Drop()
		}
		return nil, err
	}

	bt.mu.Lock()
	defer bt.mu.Unlock()
	if tor, ok := bt.torrents[spec.InfoHash]; ok {
		return tor, nil
	}

	timeout := time.Second * time.Duration(settings.BTsets.TorrentDisconnectTimeout)
	if timeout > time.Minute {
		timeout = time.Minute
	}

	torr := new(Torrent)
	torr.Torrent = goTorrent
	torr.Stat = state.TorrentAdded
	torr.lastTimeSpeed = time.Now()
	torr.bt = bt
	torr.closed = goTorrent.Closed()
	torr.TorrentSpec = spec
	torr.LocalPath = localPath
	torr.fstor = fstor
	torr.AddExpiredTime(timeout)
	torr.Timestamp = time.Now().Unix()

	go torr.watch()

	bt.torrents[spec.InfoHash] = torr
	return torr, nil
}

func (t *Torrent) WaitInfo() bool {
	if t == nil || t.Torrent == nil {
		return false
	}

	// Close torrent if no info in 1 minute + TorrentDisconnectTimeout config option
	tm := time.NewTimer(time.Minute + time.Second*time.Duration(settings.BTsets.TorrentDisconnectTimeout))

	select {
	case <-t.Torrent.GotInfo():
		if t.isLocal() {
			t.lstor = t.fstor.Opened(t.Hash())
			return true
		}
		if t.bt != nil && t.bt.storage != nil {
			t.cache = t.bt.storage.GetCache(t.Hash())
			if t.cache != nil {
				t.cache.SetTorrent(t.Torrent)
			}
		}
		if source := peerSource; source != nil {
			tor := t.Torrent
			hash := t.Hash()
			go func() {
				if peers := source(hash); len(peers) > 0 {
					tor.AddPeers(peers)
				}
			}()
		}
		return true
	case <-t.closed:
		return false
	case <-tm.C:
		return false
	}
}

func (t *Torrent) GotInfo() bool {
	// log.TLogln("GotInfo state:", t.Stat)
	if t == nil || t.Stat == state.TorrentClosed {
		return false
	}
	// assume we have info in preload state
	// and dont override with TorrentWorking
	if t.Stat == state.TorrentPreload {
		return true
	}
	t.Stat = state.TorrentGettingInfo
	if t.WaitInfo() {
		t.Stat = state.TorrentWorking
		t.AddExpiredTime(time.Second * time.Duration(settings.BTsets.TorrentDisconnectTimeout))
		return true
	} else {
		t.Close()
		return false
	}
}

func (t *Torrent) AddExpiredTime(duration time.Duration) {
	newExpiredTime := time.Now().Add(duration)
	if t.expiredTime.Before(newExpiredTime) {
		t.expiredTime = newExpiredTime
	}
}

func (t *Torrent) watch() {
	t.progressTicker = time.NewTicker(time.Second)
	defer t.progressTicker.Stop()

	for {
		select {
		case <-t.progressTicker.C:
			go t.progressEvent()
		case <-t.closed:
			return
		}
	}
}

func (t *Torrent) progressEvent() {
	if t.expired() {
		if t.TorrentSpec != nil {
			log.TLogln("Torrent close by timeout", t.TorrentSpec.InfoHash.HexString())
		}
		t.bt.RemoveTorrent(t.Hash())
		return
	}

	t.muTorrent.Lock()
	if t.Torrent != nil && t.Torrent.Info() != nil {
		st := t.Torrent.Stats()
		deltaDlBytes := st.BytesRead.Int64() - t.BytesReadUsefulData
		deltaUpBytes := st.BytesWritten.Int64() - t.BytesWrittenData
		deltaTime := time.Since(t.lastTimeSpeed).Seconds()

		t.DownloadSpeed = float64(deltaDlBytes) / deltaTime
		t.UploadSpeed = float64(deltaUpBytes) / deltaTime

		t.BytesReadUsefulData = st.BytesRead.Int64()
		t.BytesWrittenData = st.BytesWritten.Int64()

		if t.cache != nil {
			t.PreloadedBytes = t.cache.GetState().Filled
		}
	} else {
		t.DownloadSpeed = 0
		t.UploadSpeed = 0
	}
	t.muTorrent.Unlock()

	t.lastTimeSpeed = time.Now()
	t.updateRA()
}

func (t *Torrent) updateRA() {
	// t.muTorrent.Lock()
	// defer t.muTorrent.Unlock()
	// if t.Torrent != nil && t.Torrent.Info() != nil {
	// 	pieceLen := t.Torrent.Info().PieceLength
	// 	adj := pieceLen * int64(t.Torrent.Stats().ActivePeers) / int64(1+t.cache.Readers())
	// 	switch {
	// 	case adj < pieceLen:
	// 		adj = pieceLen
	// 	case adj > pieceLen*4:
	// 		adj = pieceLen * 4
	// 	}
	// 	go t.cache.AdjustRA(adj)
	// }
	if t.cache == nil {
		return
	}
	adj := int64(16 << 20) // 16 MB fixed RA
	go t.cache.AdjustRA(adj)
}

func (t *Torrent) isLocal() bool {
	return t.fstor != nil
}

func (t *Torrent) ActiveReaders() int {
	if t.isLocal() {
		return int(t.localReaders.Load())
	}
	if t.cache == nil {
		return 0
	}
	return t.cache.Readers()
}

func (t *Torrent) expired() bool {
	if !t.isLocal() && t.cache == nil {
		return false
	}
	return t.ActiveReaders() == 0 && t.expiredTime.Before(time.Now()) && (t.Stat == state.TorrentWorking || t.Stat == state.TorrentClosed)
}

func (t *Torrent) Files() []*torrent.File {
	if t.Torrent != nil && t.Torrent.Info() != nil {
		files := t.Torrent.Files()
		return files
	}
	return nil
}

func (t *Torrent) Hash() metainfo.Hash {
	if t.Torrent != nil {
		return t.Torrent.InfoHash()
	}
	if t.TorrentSpec != nil {
		return t.TorrentSpec.InfoHash
	}
	return [20]byte{}
}

func (t *Torrent) Length() int64 {
	if t.Info() == nil {
		return 0
	}
	return t.Torrent.Length()
}

func (t *Torrent) NewReader(file *torrent.File) Reader {
	if t.Stat == state.TorrentClosed {
		return nil
	}
	if t.isLocal() {
		if t.lstor == nil {
			return nil
		}
		t.localReaders.Add(1)
		return t.lstor.NewFileReader(file.Offset(), file.Length(), func() { t.localReaders.Add(-1) })
	}
	if t.cache == nil {
		return nil
	}
	return t.cache.NewReader(file)
}

func (t *Torrent) CloseReader(reader Reader) {
	if reader == nil {
		return
	}
	if r, ok := reader.(*torrstor.Reader); ok && t.cache != nil {
		t.cache.CloseReader(r)
	} else {
		reader.Close()
	}
	t.AddExpiredTime(time.Second * time.Duration(settings.BTsets.TorrentDisconnectTimeout))
}

func (t *Torrent) GetCache() *torrstor.Cache {
	return t.cache
}

func (t *Torrent) drop() {
	t.muTorrent.Lock()
	defer t.muTorrent.Unlock()
	if t.Torrent != nil {
		t.Torrent.Drop()
		t.Torrent = nil
	}
}

func (t *Torrent) Close() bool {
	if t == nil {
		return false
	}
	if t.Stat == state.TorrentClosed {
		return true
	}
	if settings.ReadOnly {
		if t.isLocal() {
			if t.localReaders.Load() > 0 {
				return false
			}
		} else if t.cache != nil && t.cache.GetUseReaders() > 0 {
			return false
		}
	}
	t.Stat = state.TorrentClosed

	if t.bt != nil {
		t.bt.mu.Lock()
		if _, ok := t.bt.torrents[t.Hash()]; ok {
			delete(t.bt.torrents, t.Hash())
		}
		t.bt.mu.Unlock()
	}

	t.drop()
	return true
}

func (t *Torrent) Status() *state.TorrentStatus {
	t.muTorrent.Lock()
	defer t.muTorrent.Unlock()

	st := new(state.TorrentStatus)

	st.Stat = t.Stat
	st.StatString = t.Stat.String()
	st.Title = t.Title
	st.Category = t.Category
	st.Poster = t.Poster
	st.Data = t.Data
	st.LocalPath = t.LocalPath
	st.Timestamp = t.Timestamp
	st.TorrentSize = t.Size
	st.BitRate = t.BitRate
	st.DurationSeconds = t.DurationSeconds

	if t.TorrentSpec != nil {
		st.Hash = t.TorrentSpec.InfoHash.HexString()
		st.Name = t.TorrentSpec.DisplayName
	}
	if t.Torrent != nil {
		st.Name = t.Torrent.Name()
		st.Hash = t.Torrent.InfoHash().HexString()
		st.LoadedSize = t.Torrent.BytesCompleted()

		st.PreloadedBytes = t.PreloadedBytes
		st.PreloadSize = t.PreloadSize
		st.DownloadSpeed = t.DownloadSpeed
		st.UploadSpeed = t.UploadSpeed

		tst := t.Torrent.Stats()
		st.BytesWritten = tst.BytesWritten.Int64()
		st.BytesWrittenData = tst.BytesWrittenData.Int64()
		st.BytesRead = tst.BytesRead.Int64()
		st.BytesReadData = tst.BytesReadData.Int64()
		st.BytesReadUsefulData = tst.BytesReadUsefulData.Int64()
		st.ChunksWritten = tst.ChunksWritten.Int64()
		st.ChunksRead = tst.ChunksRead.Int64()
		st.ChunksReadUseful = tst.ChunksReadUseful.Int64()
		st.ChunksReadWasted = tst.ChunksReadWasted.Int64()
		st.PiecesDirtiedGood = tst.PiecesDirtiedGood.Int64()
		st.PiecesDirtiedBad = tst.PiecesDirtiedBad.Int64()
		st.TotalPeers = tst.TotalPeers
		st.PendingPeers = tst.PendingPeers
		st.ActivePeers = tst.ActivePeers
		st.ConnectedSeeders = tst.ConnectedSeeders
		st.HalfOpenPeers = tst.HalfOpenPeers

		if t.Torrent.Info() != nil {
			st.TorrentSize = t.Torrent.Length()

			files := t.Files()
			sort.Slice(files, func(i, j int) bool {
				return utils2.CompareStrings(files[i].Path(), files[j].Path())
			})
			for i, f := range files {
				st.FileStats = append(st.FileStats, &state.TorrentFileStat{
					Id:     i + 1, // in web id 0 is undefined
					Path:   f.Path(),
					Length: f.Length(),
				})
			}

			th := torrshash.New(st.Hash)
			th.AddField(torrshash.TagTitle, st.Title)
			th.AddField(torrshash.TagPoster, st.Poster)
			th.AddField(torrshash.TagCategory, st.Category)
			th.AddField(torrshash.TagSize, strconv.FormatInt(st.TorrentSize, 10))

			if t.TorrentSpec != nil {
				if len(t.TorrentSpec.Trackers) > 0 && len(t.TorrentSpec.Trackers[0]) > 0 {
					for _, tr := range t.TorrentSpec.Trackers[0] {
						th.AddField(torrshash.TagTracker, tr)
					}
				}
			}
			token, err := torrshash.Pack(th)
			if err == nil {
				st.TorrsHash = token
			}
		}
	}

	return st
}

func (t *Torrent) CacheState() *cacheSt.CacheState {
	if t.Torrent != nil && t.cache != nil {
		st := t.cache.GetState()
		st.Torrent = t.Status()
		return st
	}
	if t.isLocal() && t.Torrent != nil && t.Torrent.Info() != nil {
		info := t.Torrent.Info()
		st := new(cacheSt.CacheState)
		st.Hash = t.Hash().HexString()
		st.Capacity = t.Torrent.Length()
		st.Filled = t.Torrent.Length()
		st.PiecesLength = info.PieceLength
		st.PiecesCount = info.NumPieces()
		st.Pieces = make(map[int]cacheSt.ItemState, st.PiecesCount)
		for i := 0; i < st.PiecesCount; i++ {
			length := info.Piece(i).Length()
			st.Pieces[i] = cacheSt.ItemState{Id: i, Length: length, Size: length, Completed: true}
		}
		st.Readers = []*cacheSt.ReaderState{}
		st.Torrent = t.Status()
		return st
	}
	return nil
}
