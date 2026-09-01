package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"strconv"

	"github.com/gin-gonic/gin"

	"server/log"
	mt "server/mimetype"
	"server/qbit"
	"server/qbitsync"
	set "server/settings"
	"server/torr"
	"server/torr/state"
)

func downloadTorrent(req torrReqJS, c *gin.Context) {
	if req.Hash == "" {
		abortWithJSONError(c, http.StatusBadRequest, errors.New("hash is empty"))
		return
	}
	if !qbitsync.Enabled() {
		abortWithJSONError(c, http.StatusConflict, errors.New("qBittorrent integration is disabled"))
		return
	}

	if err := qbitsync.Push(req.Hash); err != nil {
		log.TLogln("error push torrent to qbittorrent:", err)
		if errors.Is(err, qbit.ErrBadTorrent) {
			abortWithJSONError(c, http.StatusUnsupportedMediaType, err)
		} else {
			abortWithJSONError(c, http.StatusConflict, err)
		}
		return
	}

	tor := torr.GetTorrent(req.Hash)
	if tor == nil {
		c.Status(http.StatusNotFound)
		return
	}
	st := tor.Status()
	qbitsync.Enrich([]*state.TorrentStatus{st})
	c.JSON(200, st)
}

type qbitTestReq struct {
	URL      string `json:"url"`
	Username string `json:"username"`
	Password string `json:"password"`
}

// qbitTest godoc
//
//	@Summary		Test qBittorrent connection
//	@Description	Checks connection to the qBittorrent Web API and returns its version.
//
//	@Tags			API
//
//	@Param			request	body	qbitTestReq	true	"qBittorrent connection settings"
//
//	@Accept			json
//	@Produce		json
//	@Security		BasicAuth
//	@Success		200
//	@Router			/qbit/test [post]
func qbitTest(c *gin.Context) {
	var req qbitTestReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.AbortWithError(http.StatusBadRequest, err)
		return
	}

	version, err := qbitsync.TestConnection(req.URL, req.Username, req.Password)
	if err != nil {
		c.JSON(200, gin.H{"success": false, "error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"success": true, "version": version})
}

func qbitServeFile(c *gin.Context, tor *torr.Torrent, indexStr string) bool {
	if tor == nil || tor.LocalPath != "" || !qbitsync.Enabled() {
		return false
	}

	stats := torrentFileStats(tor)
	index := fileStatIndex(stats, indexStr)
	name := fileStatPath(stats, index)
	if name == "" {
		return false
	}

	hash := tor.Hash().HexString()
	if path, ok := qbitsync.CompleteFilePathByName(hash, name); ok {
		return serveCompletedFile(c, hash, index, name, path)
	}
	if _, ok := qbitsync.Snapshot()[hash]; ok {
		go qbitsync.PrioritizeFileByName(hash, name)
	}
	return false
}

func serveCompletedFile(c *gin.Context, hash string, index int, name, path string) bool {
	file, err := os.Open(path)
	if err != nil {
		log.TLogln("error open qbittorrent file:", err)
		return false
	}
	defer file.Close()

	stat, err := file.Stat()
	if err != nil {
		log.TLogln("error stat qbittorrent file:", err)
		return false
	}

	var timecode float64
	for _, v := range set.ListViewed(hash) {
		if v.FileIndex == index {
			timecode = v.TimeCode
			break
		}
	}
	set.SetViewed(&set.Viewed{
		Hash:      hash,
		FileIndex: index,
		TimeCode:  timecode,
	})

	c.Header("Connection", "close")
	c.Header("Server", "TorrServer (Portable SDK for UPnP devices)")
	if mime, err := mt.MimeTypeByPath(name); err == nil && mime.IsMedia() {
		c.Header("content-type", mime.String())
	}

	http.ServeContent(c.Writer, c.Request, name, stat.ModTime(), file)
	return true
}

func torrentFileStats(tor *torr.Torrent) []*state.TorrentFileStat {
	if stats := tor.Status().FileStats; len(stats) > 0 {
		return stats
	}
	var stored tsFiles
	if json.Unmarshal([]byte(tor.Data), &stored) != nil {
		return nil
	}
	return stored.TorrServer.Files
}

type tsFiles struct {
	TorrServer struct {
		Files []*state.TorrentFileStat `json:"Files"`
	} `json:"TorrServer"`
}

func fileStatIndex(stats []*state.TorrentFileStat, indexStr string) int {
	if len(stats) == 1 {
		return stats[0].Id
	}
	if index, err := strconv.Atoi(indexStr); err == nil {
		return index
	}
	return -1
}

func fileStatPath(stats []*state.TorrentFileStat, index int) string {
	for _, stat := range stats {
		if stat.Id == index {
			return stat.Path
		}
	}
	return ""
}
