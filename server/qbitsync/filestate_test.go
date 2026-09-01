package qbitsync

import (
	"os"
	"path/filepath"
	"testing"

	"server/qbit"
	"server/settings"
)

func writeFile(t *testing.T, path string, size int) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, make([]byte, size), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestLocateFileLayouts(t *testing.T) {
	t.Run("single file", func(t *testing.T) {
		save := t.TempDir()
		target := filepath.Join(save, "movie.mkv")
		writeFile(t, target, 128)

		info := qbit.TorrentInfo{ContentPath: target, SavePath: save}
		file := qbit.FileInfo{Name: "movie.mkv", Size: 128, Progress: 1}

		got, ok := locateFile(info, file, true)
		if !ok || got != target {
			t.Fatalf("locateFile = (%q, %v), want %q", got, ok, target)
		}
	})

	t.Run("subfolder", func(t *testing.T) {
		save := t.TempDir()
		content := filepath.Join(save, "Show")
		target := filepath.Join(content, "ep1.mkv")
		writeFile(t, target, 64)

		info := qbit.TorrentInfo{ContentPath: content, SavePath: save}
		file := qbit.FileInfo{Name: "Show/ep1.mkv", Size: 64, Progress: 1}

		got, ok := locateFile(info, file, false)
		if !ok || got != target {
			t.Fatalf("locateFile = (%q, %v), want %q", got, ok, target)
		}
	})

	t.Run("no subfolder", func(t *testing.T) {
		save := t.TempDir()
		target := filepath.Join(save, "ep1.mkv")
		writeFile(t, target, 64)

		info := qbit.TorrentInfo{ContentPath: save, SavePath: save}
		file := qbit.FileInfo{Name: "ep1.mkv", Size: 64, Progress: 1}

		got, ok := locateFile(info, file, false)
		if !ok || got != target {
			t.Fatalf("locateFile = (%q, %v), want %q", got, ok, target)
		}
	})

	t.Run("mapped path", func(t *testing.T) {
		previous := settings.BTsets
		t.Cleanup(func() { settings.BTsets = previous })

		save := t.TempDir()
		content := filepath.Join(save, "Show")
		target := filepath.Join(content, "ep1.mkv")
		writeFile(t, target, 64)

		settings.BTsets = &settings.BTSets{QBitSettings: settings.QBitConfig{
			PathMaps: []settings.QBitPathMap{{From: "/qb/downloads", To: save}},
		}}

		info := qbit.TorrentInfo{ContentPath: "/qb/downloads/Show", SavePath: "/qb/downloads"}
		file := qbit.FileInfo{Name: "Show/ep1.mkv", Size: 64, Progress: 1}

		got, ok := locateFile(info, file, false)
		if !ok || got != target {
			t.Fatalf("locateFile = (%q, %v), want %q", got, ok, target)
		}
	})

	t.Run("size mismatch", func(t *testing.T) {
		save := t.TempDir()
		target := filepath.Join(save, "movie.mkv")
		writeFile(t, target, 100)

		info := qbit.TorrentInfo{ContentPath: target, SavePath: save}
		file := qbit.FileInfo{Name: "movie.mkv", Size: 128, Progress: 1}

		if got, ok := locateFile(info, file, true); ok {
			t.Fatalf("locateFile = (%q, true), want no match", got)
		}
	})

	t.Run("missing file", func(t *testing.T) {
		save := t.TempDir()

		info := qbit.TorrentInfo{ContentPath: filepath.Join(save, "Show"), SavePath: save}
		file := qbit.FileInfo{Name: "Show/ep1.mkv", Size: 64, Progress: 1}

		if got, ok := locateFile(info, file, false); ok {
			t.Fatalf("locateFile = (%q, true), want no match", got)
		}
	})

	t.Run("path escape", func(t *testing.T) {
		save := t.TempDir()
		target := filepath.Join(filepath.Dir(save), "outside.mkv")
		writeFile(t, target, 64)
		t.Cleanup(func() { os.Remove(target) })

		info := qbit.TorrentInfo{ContentPath: save, SavePath: save}
		file := qbit.FileInfo{Name: "../outside.mkv", Size: 64, Progress: 1}

		if got, ok := locateFile(info, file, false); ok {
			t.Fatalf("locateFile = (%q, true), want no match", got)
		}
	})
}

func TestCompleteFilePathCachesFiles(t *testing.T) {
	env := newTestEnv(t, enabledConfig())

	save := t.TempDir()
	content := filepath.Join(save, "Show")
	target := filepath.Join(content, "ep1.mkv")
	writeFile(t, target, 64)

	if _, err := env.service.acquire(); err != nil {
		t.Fatalf("acquire: %v", err)
	}

	hash := "4444444444444444444444444444444444444444"
	info := qbit.TorrentInfo{Hash: hash, ContentPath: content, SavePath: save, Progress: 0.5}
	env.service.snapshot[hash] = info
	env.service.snapshotAt = env.clock.Now()
	env.client.files[hash] = []qbit.FileInfo{
		{Index: 0, Name: "Show/ep1.mkv", Size: 64, Progress: 1},
		{Index: 1, Name: "Show/ep2.mkv", Size: 64, Progress: 0.5},
	}

	got, ok := env.service.completeFilePath(hash, 0)
	if !ok || got != target {
		t.Fatalf("completeFilePath(0) = (%q, %v), want %q", got, ok, target)
	}

	if _, ok = env.service.completeFilePath(hash, 1); ok {
		t.Fatal("incomplete file reported as complete")
	}
	if env.client.filesCalls != 1 {
		t.Fatalf("files calls = %d, want 1", env.client.filesCalls)
	}

	env.clock.advance(filesTTL)
	env.service.snapshotAt = env.clock.Now()
	if _, ok = env.service.completeFilePath(hash, 0); !ok {
		t.Fatal("expired cache lost the completed file")
	}
	if env.client.filesCalls != 2 {
		t.Fatalf("files calls after expiry = %d, want 2", env.client.filesCalls)
	}
}

func TestPrioritizeFileRateLimited(t *testing.T) {
	env := newTestEnv(t, enabledConfig())
	hash := "5555555555555555555555555555555555555555"

	env.service.prioritizeFile(hash, 2)
	env.service.prioritizeFile(hash, 2)
	env.client.awaitPrioCalls(t, 1)

	env.clock.advance(prioInterval)
	env.service.prioritizeFile(hash, 2)
	env.client.awaitPrioCalls(t, 2)
}
