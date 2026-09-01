package filestor

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/anacrolix/torrent/metainfo"
)

type File struct {
	Path   string
	Length int64
	Pad    bool
}

type Layout struct {
	Base  string
	Files []File
}

type MissingFileError struct {
	Path string
}

func (e *MissingFileError) Error() string {
	return "file not found: " + e.Path
}

type IrregularFileError struct {
	Path string
}

func (e *IrregularFileError) Error() string {
	return "not a regular file: " + e.Path
}

type SizeMismatchError struct {
	Path string
	Want int64
	Got  int64
}

func (e *SizeMismatchError) Error() string {
	return fmt.Sprintf("size mismatch for %s: want %d bytes, got %d bytes", e.Path, e.Want, e.Got)
}

type PathEscapeError struct {
	Path string
}

func (e *PathEscapeError) Error() string {
	return "unsafe path in torrent info: " + e.Path
}

type AttemptError struct {
	Base string
	Err  error
}

type ResolveError struct {
	Root     string
	Attempts []AttemptError
}

func (e *ResolveError) Error() string {
	reasons := make([]string, 0, len(e.Attempts))
	for _, a := range e.Attempts {
		reasons = append(reasons, a.Base+": "+a.Err.Error())
	}
	return "torrent data not found in " + e.Root + " (" + strings.Join(reasons, "; ") + ")"
}

func (e *ResolveError) Unwrap() []error {
	errs := make([]error, 0, len(e.Attempts))
	for _, a := range e.Attempts {
		errs = append(errs, a.Err)
	}
	return errs
}

var (
	ErrNoInfo = errors.New("torrent info required")
	ErrNoRoot = errors.New("local path required")
)

func Resolve(info *metainfo.Info, root string) (*Layout, error) {
	if info == nil {
		return nil, ErrNoInfo
	}
	if root == "" {
		return nil, ErrNoRoot
	}
	root = filepath.Clean(root)

	var attempts []AttemptError
	if info.IsDir() {
		for _, base := range []string{root, filepath.Join(root, info.BestName())} {
			layout, err := buildDirLayout(info, base)
			if err == nil {
				return layout, nil
			}
			attempts = append(attempts, AttemptError{Base: base, Err: err})
		}
		return nil, &ResolveError{Root: root, Attempts: attempts}
	}

	named, joinErr := safeJoin(root, []string{info.BestName()})
	if joinErr != nil {
		attempts = append(attempts, AttemptError{Base: root, Err: joinErr})
	} else {
		layout, err := buildFileLayout(named, info.Length)
		if err == nil {
			return layout, nil
		}
		attempts = append(attempts, AttemptError{Base: named, Err: err})
	}

	layout, err := buildFileLayout(root, info.Length)
	if err == nil {
		return layout, nil
	}
	attempts = append(attempts, AttemptError{Base: root, Err: err})
	return nil, &ResolveError{Root: root, Attempts: attempts}
}

func PreValidate(info *metainfo.Info, root string) (*Layout, error) {
	return Resolve(info, root)
}

func buildDirLayout(info *metainfo.Info, base string) (*Layout, error) {
	files := make([]File, 0, len(info.Files))
	for _, fi := range info.UpvertedFiles() {
		parts := fi.BestPath()
		if isPadPath(parts) {
			files = append(files, File{Length: fi.Length, Pad: true})
			continue
		}
		path, err := safeJoin(base, parts)
		if err != nil {
			return nil, err
		}
		if err := checkFile(path, fi.Length); err != nil {
			return nil, err
		}
		files = append(files, File{Path: path, Length: fi.Length})
	}
	return &Layout{Base: base, Files: files}, nil
}

func buildFileLayout(path string, length int64) (*Layout, error) {
	if err := checkFile(path, length); err != nil {
		return nil, err
	}
	return &Layout{Base: filepath.Dir(path), Files: []File{{Path: path, Length: length}}}, nil
}

func isPadPath(parts []string) bool {
	return len(parts) > 1 && parts[0] == ".pad"
}

func safeJoin(base string, parts []string) (string, error) {
	display := strings.Join(parts, "/")
	if len(parts) == 0 {
		return "", &PathEscapeError{Path: display}
	}
	for _, part := range parts {
		if part == "" || part == "." || part == ".." {
			return "", &PathEscapeError{Path: display}
		}
		if strings.ContainsAny(part, `/\`) || filepath.IsAbs(part) || filepath.VolumeName(part) != "" {
			return "", &PathEscapeError{Path: display}
		}
	}
	full := filepath.Join(base, filepath.Join(parts...))
	if !strings.HasPrefix(full, base+string(filepath.Separator)) {
		return "", &PathEscapeError{Path: display}
	}
	return full, nil
}

func checkFile(path string, length int64) error {
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &MissingFileError{Path: path}
		}
		return err
	}
	if !info.Mode().IsRegular() {
		return &IrregularFileError{Path: path}
	}
	if info.Size() != length {
		return &SizeMismatchError{Path: path, Want: length, Got: info.Size()}
	}
	return nil
}
