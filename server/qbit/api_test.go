package qbit

import (
	"bytes"
	"errors"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"testing"
)

func TestVersionBranchPaths(t *testing.T) {
	tests := []struct {
		version   string
		wantStop  string
		wantStart string
	}{
		{version: "2.8.19", wantStop: pausePath, wantStart: resumePath},
		{version: "2.10.4", wantStop: pausePath, wantStart: resumePath},
		{version: "2.11.0", wantStop: stopPath, wantStart: startPath},
		{version: "2.11.4", wantStop: stopPath, wantStart: startPath},
		{version: "2.12", wantStop: stopPath, wantStart: startPath},
		{version: "3.0.1", wantStop: stopPath, wantStart: startPath},
		{version: "1.99.9", wantStop: pausePath, wantStart: resumePath},
	}

	for _, test := range tests {
		t.Run(test.version, func(t *testing.T) {
			mock := newMockQB(t)
			mock.setHandler(func(w http.ResponseWriter, req capturedRequest) {
				w.Write([]byte(test.version))
			})
			client := mock.client()

			stop, err := client.StopPath()
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if stop != test.wantStop {
				t.Fatalf("got stop path %q, want %q", stop, test.wantStop)
			}

			start, err := client.StartPath()
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if start != test.wantStart {
				t.Fatalf("got start path %q, want %q", start, test.wantStart)
			}

			if mock.countPath(versionPath) != 1 {
				t.Fatalf("got %d version requests, want 1", mock.countPath(versionPath))
			}
		})
	}
}

func TestAddTorrentMultipart(t *testing.T) {
	mock := newMockQB(t)
	mock.setHandler(func(w http.ResponseWriter, req capturedRequest) {
		w.Write([]byte("Ok."))
	})
	client := mock.client()

	err := client.Add(AddOptions{
		Torrent:            []byte("d8:announcee"),
		SavePath:           "/downloads/tv",
		Category:           "tv",
		Tags:               "torrserver",
		SequentialDownload: true,
		FirstLastPiecePrio: true,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	added := mock.lastRequest(addPath)
	if added.Method != http.MethodPost {
		t.Fatalf("got add method %q, want POST", added.Method)
	}

	fields, files := parseMultipart(t, added)
	if string(files[torrentPartName]) != "d8:announcee" {
		t.Fatalf("got torrent part %q, want the raw torrent bytes", files[torrentPartName])
	}
	wantFields := map[string]string{
		"paused":             "false",
		"stopped":            "false",
		"savepath":           "/downloads/tv",
		"category":           "tv",
		"tags":               "torrserver",
		"sequentialDownload": "true",
		"firstLastPiecePrio": "true",
		"contentLayout":      originalLayout,
	}
	for name, want := range wantFields {
		if fields[name] != want {
			t.Fatalf("got field %s = %q, want %q", name, fields[name], want)
		}
	}
	if _, ok := fields["urls"]; ok {
		t.Fatal("expected no urls field when torrent bytes are sent")
	}
}

func TestAddURLsMultipart(t *testing.T) {
	mock := newMockQB(t)
	mock.setHandler(func(w http.ResponseWriter, req capturedRequest) {
		w.Write([]byte("Ok."))
	})
	client := mock.client()

	err := client.Add(AddOptions{
		URLs:   []string{"magnet:?xt=urn:btih:aabb", "magnet:?xt=urn:btih:ccdd"},
		Paused: true,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	fields, files := parseMultipart(t, mock.lastRequest(addPath))
	if len(files) != 0 {
		t.Fatalf("got %d file parts, want none", len(files))
	}
	if fields["urls"] != "magnet:?xt=urn:btih:aabb\nmagnet:?xt=urn:btih:ccdd" {
		t.Fatalf("got urls %q", fields["urls"])
	}
	if fields["paused"] != "true" || fields["stopped"] != "true" {
		t.Fatalf("got paused %q and stopped %q, want both true", fields["paused"], fields["stopped"])
	}
}

func TestAddRejectedTorrent(t *testing.T) {
	mock := newMockQB(t)
	mock.setHandler(func(w http.ResponseWriter, req capturedRequest) {
		w.WriteHeader(http.StatusUnsupportedMediaType)
	})
	client := mock.client()

	err := client.Add(AddOptions{Torrent: []byte("broken")})
	if !errors.Is(err, ErrBadTorrent) {
		t.Fatalf("got error %v, want %v", err, ErrBadTorrent)
	}

	if err = client.Add(AddOptions{}); !errors.Is(err, ErrBadTorrent) {
		t.Fatalf("got error %v, want %v", err, ErrBadTorrent)
	}
}

func TestInfoHashes(t *testing.T) {
	mock := newMockQB(t)
	mock.setHandler(func(w http.ResponseWriter, req capturedRequest) {
		w.Write([]byte(`[{"hash":"aabb","name":"show","state":"downloading","progress":0.5}]`))
	})
	client := mock.client()

	torrents, err := client.Info([]string{"aabb", "ccdd"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if hashes := mock.lastRequest(infoPath).Query.Get("hashes"); hashes != "aabb|ccdd" {
		t.Fatalf("got hashes %q, want aabb|ccdd", hashes)
	}
	if len(torrents) != 1 || torrents[0].Hash != "aabb" || torrents[0].Progress != 0.5 {
		t.Fatalf("unexpected torrents: %+v", torrents)
	}

	if _, err = client.Info(nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := mock.lastRequest(infoPath).Query["hashes"]; ok {
		t.Fatal("expected no hashes param for an empty hash list")
	}
}

func TestCompleted(t *testing.T) {
	tests := []struct {
		name    string
		torrent TorrentInfo
		want    bool
	}{
		{name: "done", torrent: TorrentInfo{Progress: 1.0, CompletionOn: 1700000000}, want: true},
		{name: "no completion time", torrent: TorrentInfo{Progress: 1.0, CompletionOn: 0}},
		{name: "negative completion time", torrent: TorrentInfo{Progress: 1.0, CompletionOn: -1}},
		{name: "almost done", torrent: TorrentInfo{Progress: 0.999, CompletionOn: 1700000000}},
		{name: "empty", torrent: TorrentInfo{}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := test.torrent.Completed(); got != test.want {
				t.Fatalf("Completed() = %v, want %v", got, test.want)
			}
		})
	}
}

func TestStateClassifiers(t *testing.T) {
	for _, state := range []string{"pausedUP", "pausedDL", "stoppedUP", "stoppedDL"} {
		if !(TorrentInfo{State: state}).Stopped() {
			t.Fatalf("state %s must be stopped", state)
		}
	}
	if (TorrentInfo{State: "downloading"}).Stopped() {
		t.Fatal("downloading must not be stopped")
	}
	if !(TorrentInfo{State: "metaDL"}).Downloading() {
		t.Fatal("metaDL must be downloading")
	}
	if !(TorrentInfo{State: "stalledUP"}).Seeding() {
		t.Fatal("stalledUP must be seeding")
	}
	if !(TorrentInfo{State: "missingFiles"}).Errored() {
		t.Fatal("missingFiles must be errored")
	}
}

func TestFilesRequest(t *testing.T) {
	mock := newMockQB(t)
	mock.setHandler(func(w http.ResponseWriter, req capturedRequest) {
		w.Write([]byte(`[{"index":0,"name":"show/e01.mkv","size":100,"progress":1,"priority":1}]`))
	})
	client := mock.client()

	files, err := client.Files("aabb")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if hash := mock.lastRequest(filesPath).Query.Get("hash"); hash != "aabb" {
		t.Fatalf("got files hash %q, want aabb", hash)
	}
	if len(files) != 1 || files[0].Name != "show/e01.mkv" || files[0].Size != 100 || files[0].Progress != 1 {
		t.Fatalf("unexpected files: %+v", files)
	}
}

func TestExportRequest(t *testing.T) {
	mock := newMockQB(t)
	mock.setHandler(func(w http.ResponseWriter, req capturedRequest) {
		w.Write([]byte("d8:announcee"))
	})
	client := mock.client()

	data, err := client.Export("aabb")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if hash := mock.lastRequest(exportPath).Query.Get("hash"); hash != "aabb" {
		t.Fatalf("got export hash %q, want aabb", hash)
	}
	if string(data) != "d8:announcee" {
		t.Fatalf("got body %q, want the raw torrent bytes", data)
	}

	for _, status := range []int{http.StatusNotFound, http.StatusConflict} {
		mock.setHandler(func(w http.ResponseWriter, req capturedRequest) {
			w.WriteHeader(status)
		})
		if _, err = client.Export("aabb"); !errors.Is(err, ErrNotFound) {
			t.Fatalf("status %d: got error %v, want %v", status, err, ErrNotFound)
		}
	}
}

func TestFilePrioRequest(t *testing.T) {
	mock := newMockQB(t)
	client := mock.client()

	if err := client.FilePrio("aabb", []int{0, 2}, PriorityMaximal); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	prio := mock.lastRequest(filePrioPath)
	if prio.Method != http.MethodPost {
		t.Fatalf("got filePrio method %q, want POST", prio.Method)
	}
	if prio.Form.Get("hash") != "aabb" || prio.Form.Get("id") != "0|2" || prio.Form.Get("priority") != "7" {
		t.Fatalf("unexpected filePrio form: %v", prio.Form)
	}
}

func parseMultipart(t *testing.T, req capturedRequest) (map[string]string, map[string][]byte) {
	t.Helper()

	mediaType, params, err := mime.ParseMediaType(req.ContentType)
	if err != nil {
		t.Fatalf("invalid content type %q: %v", req.ContentType, err)
	}
	if mediaType != "multipart/form-data" {
		t.Fatalf("got media type %q, want multipart/form-data", mediaType)
	}

	fields := make(map[string]string)
	files := make(map[string][]byte)
	reader := multipart.NewReader(bytes.NewReader(req.Body), params["boundary"])
	for {
		part, err := reader.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("read multipart: %v", err)
		}
		data, err := io.ReadAll(part)
		if err != nil {
			t.Fatalf("read part %s: %v", part.FormName(), err)
		}
		if part.FileName() != "" {
			files[part.FormName()] = data
			continue
		}
		fields[part.FormName()] = string(data)
	}
	return fields, files
}
