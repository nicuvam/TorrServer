package qbit

const PriorityMaximal = 7

type TorrentInfo struct {
	Hash               string  `json:"hash"`
	Name               string  `json:"name"`
	State              string  `json:"state"`
	Category           string  `json:"category"`
	Tags               string  `json:"tags"`
	Progress           float64 `json:"progress"`
	Size               int64   `json:"size"`
	CompletedSize      int64   `json:"completed"`
	CompletionOn       int64   `json:"completion_on"`
	AddedOn            int64   `json:"added_on"`
	ContentPath        string  `json:"content_path"`
	SavePath           string  `json:"save_path"`
	DlSpeed            int64   `json:"dlspeed"`
	UpSpeed            int64   `json:"upspeed"`
	ETA                int64   `json:"eta"`
	NumSeeds           int     `json:"num_seeds"`
	NumLeechs          int     `json:"num_leechs"`
	SequentialDownload bool    `json:"seq_dl"`
	FirstLastPiecePrio bool    `json:"f_l_piece_prio"`
}

func (t TorrentInfo) Completed() bool {
	return t.Progress >= 1.0 && t.CompletionOn > 0
}

func (t TorrentInfo) Stopped() bool {
	switch t.State {
	case "pausedUP", "pausedDL", "stoppedUP", "stoppedDL":
		return true
	}
	return false
}

func (t TorrentInfo) Downloading() bool {
	switch t.State {
	case "downloading", "metaDL", "forcedMetaDL", "stalledDL", "queuedDL", "forcedDL", "allocating":
		return true
	}
	return false
}

func (t TorrentInfo) Seeding() bool {
	switch t.State {
	case "uploading", "stalledUP", "queuedUP", "forcedUP":
		return true
	}
	return false
}

func (t TorrentInfo) Errored() bool {
	switch t.State {
	case "error", "missingFiles":
		return true
	}
	return false
}

type FileInfo struct {
	Index    int     `json:"index"`
	Name     string  `json:"name"`
	Size     int64   `json:"size"`
	Progress float64 `json:"progress"`
	Priority int     `json:"priority"`
}

type Category struct {
	Name     string `json:"name"`
	SavePath string `json:"savePath"`
}

type Preferences struct {
	SavePath        string `json:"save_path"`
	TempPath        string `json:"temp_path"`
	TempPathEnabled bool   `json:"temp_path_enabled"`
	ListenPort      int    `json:"listen_port"`
	QueueingEnabled bool   `json:"queueing_enabled"`
	DHT             bool   `json:"dht"`
}

type TransferInfo struct {
	DlInfoSpeed      int64  `json:"dl_info_speed"`
	DlInfoData       int64  `json:"dl_info_data"`
	UpInfoSpeed      int64  `json:"up_info_speed"`
	UpInfoData       int64  `json:"up_info_data"`
	ConnectionStatus string `json:"connection_status"`
	DHTNodes         int64  `json:"dht_nodes"`
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
