package qbit

const PriorityMaximal = 7

type TorrentInfo struct {
	Hash         string  `json:"hash"`
	State        string  `json:"state"`
	Category     string  `json:"category"`
	Progress     float64 `json:"progress"`
	CompletionOn int64   `json:"completion_on"`
	ContentPath  string  `json:"content_path"`
	SavePath     string  `json:"save_path"`
	DlSpeed      int64   `json:"dlspeed"`
	ETA          int64   `json:"eta"`
}

func (t TorrentInfo) Completed() bool {
	return t.Progress >= 1.0 && t.CompletionOn > 0
}

type FileInfo struct {
	Index    int     `json:"index"`
	Name     string  `json:"name"`
	Size     int64   `json:"size"`
	Progress float64 `json:"progress"`
}

type Preferences struct {
	ListenPort int `json:"listen_port"`
}

type AddOptions struct {
	URLs               []string
	Torrent            []byte
	SavePath           string
	Category           string
	Tags               string
	Paused             bool
	SequentialDownload bool
	FirstLastPiecePrio bool
}
