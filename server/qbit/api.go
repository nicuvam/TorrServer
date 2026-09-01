package qbit

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

const (
	addPath            = "api/v2/torrents/add"
	infoPath           = "api/v2/torrents/info"
	filesPath          = "api/v2/torrents/files"
	exportPath         = "api/v2/torrents/export"
	filePrioPath       = "api/v2/torrents/filePrio"
	deletePath         = "api/v2/torrents/delete"
	recheckPath        = "api/v2/torrents/recheck"
	categoriesPath     = "api/v2/torrents/categories"
	createCategoryPath = "api/v2/torrents/createCategory"
	setCategoryPath    = "api/v2/torrents/setCategory"
	preferencesPath    = "api/v2/app/preferences"
	transferInfoPath   = "api/v2/transfer/info"

	formContentType = "application/x-www-form-urlencoded"
	torrentPartName = "torrents"
	torrentPartFile = "torrent.torrent"
	originalLayout  = "Original"
)

func (c *Client) Add(opts AddOptions) error {
	body := &bytes.Buffer{}
	form := multipart.NewWriter(body)

	switch {
	case len(opts.Torrent) > 0:
		part, err := form.CreateFormFile(torrentPartName, torrentPartFile)
		if err != nil {
			return err
		}
		if _, err = part.Write(opts.Torrent); err != nil {
			return err
		}
	case len(opts.URLs) > 0:
		if err := form.WriteField("urls", strings.Join(opts.URLs, "\n")); err != nil {
			return err
		}
	default:
		return ErrBadTorrent
	}

	paused := strconv.FormatBool(opts.Paused)
	fields := [][2]string{
		{"paused", paused},
		{"stopped", paused},
		{"sequentialDownload", strconv.FormatBool(opts.SequentialDownload)},
		{"firstLastPiecePrio", strconv.FormatBool(opts.FirstLastPiecePrio)},
		{"contentLayout", originalLayout},
	}
	if opts.SavePath != "" {
		fields = append(fields, [2]string{"savepath", opts.SavePath})
	}
	if opts.Category != "" {
		fields = append(fields, [2]string{"category", opts.Category})
	}
	if opts.Tags != "" {
		fields = append(fields, [2]string{"tags", opts.Tags})
	}

	for _, field := range fields {
		if err := form.WriteField(field[0], field[1]); err != nil {
			return err
		}
	}
	if err := form.Close(); err != nil {
		return err
	}

	_, err := c.do(http.MethodPost, addPath, form.FormDataContentType(), body.Bytes())
	return err
}

func (c *Client) Info(hashes []string) ([]TorrentInfo, error) {
	params := url.Values{}
	if len(hashes) > 0 {
		params.Set("hashes", strings.Join(hashes, "|"))
	}

	data, err := c.do(http.MethodGet, withQuery(infoPath, params), "", nil)
	if err != nil {
		return nil, err
	}

	var torrents []TorrentInfo
	if err = json.Unmarshal(data, &torrents); err != nil {
		return nil, fmt.Errorf("qbittorrent: invalid torrents response: %v", err)
	}
	return torrents, nil
}

func (c *Client) Files(hash string) ([]FileInfo, error) {
	data, err := c.do(http.MethodGet, withQuery(filesPath, url.Values{"hash": {hash}}), "", nil)
	if err != nil {
		return nil, err
	}

	var files []FileInfo
	if err = json.Unmarshal(data, &files); err != nil {
		return nil, fmt.Errorf("qbittorrent: invalid files response: %v", err)
	}
	return files, nil
}

func (c *Client) Export(hash string) ([]byte, error) {
	data, err := c.do(http.MethodGet, withQuery(exportPath, url.Values{"hash": {hash}}), "", nil)
	if err != nil {
		var apiErr *APIError
		if errors.As(err, &apiErr) && apiErr.Status == http.StatusConflict {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return data, nil
}

func (c *Client) FilePrio(hash string, indexes []int, priority int) error {
	ids := make([]string, 0, len(indexes))
	for _, index := range indexes {
		ids = append(ids, strconv.Itoa(index))
	}

	form := url.Values{}
	form.Set("hash", hash)
	form.Set("id", strings.Join(ids, "|"))
	form.Set("priority", strconv.Itoa(priority))
	return c.postForm(filePrioPath, form)
}

func (c *Client) Delete(hashes []string, deleteFiles bool) error {
	form := url.Values{}
	form.Set("hashes", strings.Join(hashes, "|"))
	form.Set("deleteFiles", strconv.FormatBool(deleteFiles))
	return c.postForm(deletePath, form)
}

func (c *Client) Recheck(hashes []string) error {
	form := url.Values{}
	form.Set("hashes", strings.Join(hashes, "|"))
	return c.postForm(recheckPath, form)
}

func (c *Client) Categories() (map[string]Category, error) {
	data, err := c.do(http.MethodGet, categoriesPath, "", nil)
	if err != nil {
		return nil, err
	}

	categories := make(map[string]Category)
	if err = json.Unmarshal(data, &categories); err != nil {
		return nil, fmt.Errorf("qbittorrent: invalid categories response: %v", err)
	}
	return categories, nil
}

func (c *Client) CreateCategory(name, savePath string) error {
	form := url.Values{}
	form.Set("category", name)
	if savePath != "" {
		form.Set("savePath", savePath)
	}
	return c.postForm(createCategoryPath, form)
}

func (c *Client) SetCategory(hashes []string, category string) error {
	form := url.Values{}
	form.Set("hashes", strings.Join(hashes, "|"))
	form.Set("category", category)
	return c.postForm(setCategoryPath, form)
}

func (c *Client) Preferences() (Preferences, error) {
	var prefs Preferences

	data, err := c.do(http.MethodGet, preferencesPath, "", nil)
	if err != nil {
		return prefs, err
	}
	if err = json.Unmarshal(data, &prefs); err != nil {
		return prefs, fmt.Errorf("qbittorrent: invalid preferences response: %v", err)
	}
	return prefs, nil
}

func (c *Client) TransferInfo() (TransferInfo, error) {
	var transfer TransferInfo

	data, err := c.do(http.MethodGet, transferInfoPath, "", nil)
	if err != nil {
		return transfer, err
	}
	if err = json.Unmarshal(data, &transfer); err != nil {
		return transfer, fmt.Errorf("qbittorrent: invalid transfer response: %v", err)
	}
	return transfer, nil
}

func (c *Client) postForm(path string, form url.Values) error {
	_, err := c.do(http.MethodPost, path, formContentType, []byte(form.Encode()))
	return err
}
