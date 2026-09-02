package api

import (
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strconv"

	"github.com/anacrolix/dms/dlna"
	"github.com/anacrolix/missinggo/v2/httptoo"
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

// qbitCategories godoc
//
//	@Summary		Create mirrored qBittorrent categories
//	@Description	Creates the movie, tv, music and other categories in qBittorrent using the saved connection settings.
//
//	@Tags			API
//
//	@Accept			json
//	@Produce		json
//	@Security		BasicAuth
//	@Success		200
//	@Router			/qbit/categories [post]
func qbitCategories(c *gin.Context) {
	if err := qbitsync.EnsureCategories(); err != nil {
		c.JSON(200, gin.H{"success": false, "error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"success": true})
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
	stat, err := os.Stat(path)
	if err != nil {
		log.TLogln("error stat qbittorrent file:", err)
		return false
	}

	if set.MaxSize > 0 && stat.Size() > set.MaxSize {
		log.TLogln(fmt.Sprintf("File %s size (%d) exceeded max allowed %d bytes", name, stat.Size(), set.MaxSize))
		http.Error(c.Writer, fmt.Sprintf("file size exceeded max allowed %d bytes", set.MaxSize), http.StatusForbidden)
		return true
	}

	file, err := os.Open(path)
	if err != nil {
		log.TLogln("error open qbittorrent file:", err)
		return false
	}
	defer file.Close()

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
	if streamTimeout := set.BTsets.TorrentDisconnectTimeout; streamTimeout > 0 {
		c.Header("X-Stream-Timeout", fmt.Sprintf("%d", streamTimeout))
	}
	etag := hex.EncodeToString([]byte(fmt.Sprintf("%s/%s", hash, name)))
	c.Header("ETag", httptoo.EncodeQuotedString(etag))
	c.Header("transferMode.dlna.org", "Streaming")
	if mime, err := mt.MimeTypeByPath(name); err == nil && mime.IsMedia() {
		c.Header("content-type", mime.String())
	}
	if c.GetHeader("getContentFeatures.dlna.org") != "" {
		c.Header("contentFeatures.dlna.org", dlna.ContentFeatures{
			SupportRange:    true,
			SupportTimeSeek: true,
		}.String())
	}
	if c.GetHeader("Range") != "" {
		c.Header("Accept-Ranges", "bytes")
	}

	http.ServeContent(c.Writer, c.Request, name, stat.ModTime(), file)
	return true
}

func torrentFileStats(tor *torr.Torrent) []*state.TorrentFileStat {
	if stats := tor.Status().FileStats; len(stats) > 0 {
		return stats
	}
	return torr.FileStatsFromData(tor.Data)
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
