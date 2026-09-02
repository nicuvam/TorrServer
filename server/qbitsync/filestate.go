package qbitsync

import (
	"errors"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"server/qbit"
)

const (
	filesTTL     = 5 * time.Second
	prioInterval = time.Minute
)

var errFilesNotReady = errors.New("qbittorrent: file list not cached yet")

func CompleteFilePathByName(hashHex, name string) (string, bool) {
	index, ok := svc.fileIndexByName(hashHex, name)
	if !ok {
		return "", false
	}
	return svc.completeFilePath(hashHex, index)
}

func PrioritizeFileByName(hashHex, name string) {
	index, ok := svc.fileIndexByName(hashHex, name)
	if !ok {
		return
	}
	svc.prioritizeFile(hashHex, index)
}

func (s *service) fileIndexByName(hashHex, name string) (int, bool) {
	client, err := s.acquire()
	if err != nil {
		return 0, false
	}

	hash := strings.ToLower(strings.TrimSpace(hashHex))
	if _, known := s.torrentInfo(hash); !known {
		return 0, false
	}

	files, err := s.torrentFiles(client, hash)
	if err != nil {
		return 0, false
	}

	file, ok := matchFileName(files, name)
	if !ok {
		return 0, false
	}
	return file.Index, true
}

func (s *service) completeFilePath(hashHex string, fileIndex int) (string, bool) {
	client, err := s.acquire()
	if err != nil {
		return "", false
	}

	hash := strings.ToLower(strings.TrimSpace(hashHex))
	info, ok := s.torrentInfo(hash)
	if !ok {
		return "", false
	}

	files, err := s.torrentFiles(client, hash)
	if err != nil {
		return "", false
	}

	file, ok := findFile(files, fileIndex)
	if !ok || file.Progress < 1.0 {
		return "", false
	}
	return locateFile(info, file, len(files) == 1)
}

func (s *service) prioritizeFile(hashHex string, fileIndex int) {
	client, err := s.acquire()
	if err != nil {
		return
	}

	hash := strings.ToLower(strings.TrimSpace(hashHex))
	key := hash + ":" + strconv.Itoa(fileIndex)

	s.mu.Lock()
	if last, ok := s.prios[key]; ok && nowFunc().Sub(last) < prioInterval {
		s.mu.Unlock()
		return
	}
	s.prios[key] = nowFunc()
	s.mu.Unlock()

	go func() {
		if err := client.FilePrio(hash, []int{fileIndex}, qbit.PriorityMaximal); err != nil {
			s.logProblem(err)
		}
	}()
}

func (s *service) torrentInfo(hash string) (qbit.TorrentInfo, bool) {
	info, ok := s.snapshotView(true)[hash]
	return info, ok
}

func (s *service) torrentFiles(client clientAPI, hash string) ([]qbit.FileInfo, error) {
	s.mu.Lock()
	entry, cached := s.files[hash]
	fresh := cached && nowFunc().Sub(entry.updatedAt) < filesTTL
	refresh := !fresh && !s.filesLoading[hash]
	if refresh {
		s.filesLoading[hash] = true
	}
	s.mu.Unlock()

	if refresh {
		go s.refreshFiles(client, hash)
	}
	if cached {
		return entry.files, nil
	}
	return nil, errFilesNotReady
}

func (s *service) refreshFiles(client clientAPI, hash string) {
	files, err := client.Files(hash)

	s.mu.Lock()
	delete(s.filesLoading, hash)
	if err == nil {
		s.files[hash] = filesEntry{files: files, updatedAt: nowFunc()}
	}
	s.mu.Unlock()

	if err != nil {
		s.logProblem(err)
	}
}

func findFile(files []qbit.FileInfo, index int) (qbit.FileInfo, bool) {
	for _, file := range files {
		if file.Index == index {
			return file, true
		}
	}
	if index >= 0 && index < len(files) {
		return files[index], true
	}
	return qbit.FileInfo{}, false
}

func matchFileName(files []qbit.FileInfo, name string) (qbit.FileInfo, bool) {
	target := strings.Trim(normalizePath(name), "/")
	if target == "" {
		return qbit.FileInfo{}, false
	}

	var suffixed qbit.FileInfo
	found := false
	for _, file := range files {
		candidate := strings.Trim(normalizePath(file.Name), "/")
		if candidate == target {
			return file, true
		}
		if !found && (hasPathSuffix(candidate, target) || hasPathSuffix(target, candidate)) {
			suffixed, found = file, true
		}
	}
	return suffixed, found
}

func hasPathSuffix(name, suffix string) bool {
	return suffix != "" && strings.HasSuffix(name, "/"+suffix)
}

func locateFile(info qbit.TorrentInfo, file qbit.FileInfo, single bool) (string, bool) {
	for _, candidate := range filePathCandidates(info, file, single) {
		if isCompleteFile(candidate, file.Size) {
			return candidate, true
		}
	}
	return "", false
}

func filePathCandidates(info qbit.TorrentInfo, file qbit.FileInfo, single bool) []string {
	name := normalizePath(file.Name)
	if name == "" || !isRelativeName(name) {
		return nil
	}

	content, _ := MapPath(info.ContentPath)
	save, _ := MapPath(info.SavePath)
	relative := filepath.FromSlash(name)

	var candidates []string
	if single && content != "" && path.Base(name) == path.Base(normalizePath(info.ContentPath)) {
		candidates = append(candidates, content)
	}
	if save != "" {
		candidates = append(candidates, filepath.Join(save, relative))
	}
	if content != "" {
		candidates = append(candidates, filepath.Join(content, relative))
	}
	return candidates
}

func isRelativeName(name string) bool {
	if strings.HasPrefix(name, "/") {
		return false
	}
	for _, part := range strings.Split(name, "/") {
		if part == ".." {
			return false
		}
	}
	return true
}

func isCompleteFile(path string, size int64) bool {
	stat, err := os.Stat(path)
	if err != nil {
		return false
	}
	return stat.Mode().IsRegular() && stat.Size() == size
}
