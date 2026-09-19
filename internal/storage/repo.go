package storage

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"
)

var (
	ErrInvalidPath = errors.New("invalid path")
	ErrNotFound    = errors.New("not found")
	ErrExists      = errors.New("already exists")
	ErrNotEmpty    = errors.New("directory not empty")
	ErrIsDir       = errors.New("is a directory")
	ErrNotDir      = errors.New("not a directory")
)

// Entry is one file or folder in the software repository.
type Entry struct {
	Name    string    `json:"name"`
	Path    string    `json:"path"`
	IsDir   bool      `json:"is_dir"`
	Size    int64     `json:"size"`
	ModTime time.Time `json:"mod_time"`
}

// Repo is a path-jailed directory tree used as the software image store.
type Repo struct {
	root string
}

func NewRepo(root string) (*Repo, error) {
	if strings.TrimSpace(root) == "" {
		return nil, fmt.Errorf("storage root is empty")
	}
	root, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(root, 0o750); err != nil {
		return nil, fmt.Errorf("mkdir storage root: %w", err)
	}
	return &Repo{root: root}, nil
}

func (r *Repo) Root() string { return r.root }

func cleanAPIPath(relPath string) (string, error) {
	relPath = strings.ReplaceAll(relPath, "\\", "/")
	relPath = strings.TrimSpace(relPath)
	if relPath == "" {
		relPath = "/"
	}
	if strings.Contains(relPath, "\x00") {
		return "", ErrInvalidPath
	}
	cleaned := path.Clean("/" + strings.TrimPrefix(relPath, "/"))
	if cleaned == "." || !strings.HasPrefix(cleaned, "/") {
		return "", ErrInvalidPath
	}
	if hiddenSegment(cleaned) {
		return "", ErrNotFound
	}
	return cleaned, nil
}

// relPath is the API/device path ("/eos/file.bin" or "eos/file.bin").
func (r *Repo) resolve(relPath string) (abs string, apiPath string, err error) {
	cleaned, err := cleanAPIPath(relPath)
	if err != nil {
		return "", "", err
	}
	rel := strings.TrimPrefix(cleaned, "/")
	if rel == "" {
		return r.root, "/", nil
	}
	abs = filepath.Join(r.root, filepath.FromSlash(rel))
	abs = filepath.Clean(abs)
	root := filepath.Clean(r.root)
	sep := string(os.PathSeparator)
	if abs != root && !strings.HasPrefix(abs, root+sep) {
		return "", "", ErrInvalidPath
	}
	return abs, cleaned, nil
}

func hiddenSegment(apiPath string) bool {
	for _, seg := range strings.Split(strings.Trim(apiPath, "/"), "/") {
		if seg == "" {
			continue
		}
		if strings.HasPrefix(seg, ".") {
			return true
		}
	}
	return false
}

func (r *Repo) List(relPath string) ([]Entry, error) {
	abs, apiPath, err := r.resolve(relPath)
	if err != nil {
		return nil, err
	}
	st, err := os.Stat(abs)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if !st.IsDir() {
		return nil, ErrNotDir
	}
	ents, err := os.ReadDir(abs)
	if err != nil {
		return nil, err
	}
	out := make([]Entry, 0, len(ents))
	for _, e := range ents {
		if strings.HasPrefix(e.Name(), ".") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		child := apiPath
		if child == "/" {
			child = "/" + e.Name()
		} else {
			child = child + "/" + e.Name()
		}
		out = append(out, Entry{
			Name:    e.Name(),
			Path:    child,
			IsDir:   e.IsDir(),
			Size:    info.Size(),
			ModTime: info.ModTime().UTC(),
		})
	}
	return out, nil
}

func (r *Repo) Mkdir(relPath string) error {
	abs, _, err := r.resolve(relPath)
	if err != nil {
		return err
	}
	if abs == r.root {
		return nil
	}
	if _, err := os.Stat(abs); err == nil {
		return ErrExists
	} else if !os.IsNotExist(err) {
		return err
	}
	return os.MkdirAll(abs, 0o750)
}

func (r *Repo) Remove(relPath string) error {
	abs, apiPath, err := r.resolve(relPath)
	if err != nil {
		return err
	}
	if apiPath == "/" {
		return fmt.Errorf("cannot delete repository root")
	}
	st, err := os.Stat(abs)
	if err != nil {
		if os.IsNotExist(err) {
			return ErrNotFound
		}
		return err
	}
	if st.IsDir() {
		ents, err := os.ReadDir(abs)
		if err != nil {
			return err
		}
		visible := 0
		for _, e := range ents {
			if !strings.HasPrefix(e.Name(), ".") {
				visible++
			}
		}
		if visible > 0 {
			return ErrNotEmpty
		}
		return os.RemoveAll(abs)
	}
	return os.Remove(abs)
}

func (r *Repo) Move(from, to string) error {
	src, _, err := r.resolve(from)
	if err != nil {
		return err
	}
	dst, dstAPI, err := r.resolve(to)
	if err != nil {
		return err
	}
	if dstAPI == "/" {
		return fmt.Errorf("cannot replace repository root")
	}
	if _, err := os.Stat(src); err != nil {
		if os.IsNotExist(err) {
			return ErrNotFound
		}
		return err
	}
	if _, err := os.Stat(dst); err == nil {
		return ErrExists
	} else if !os.IsNotExist(err) {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o750); err != nil {
		return err
	}
	return os.Rename(src, dst)
}

func (r *Repo) Stat(relPath string) (Entry, error) {
	abs, apiPath, err := r.resolve(relPath)
	if err != nil {
		return Entry{}, err
	}
	st, err := os.Stat(abs)
	if err != nil {
		if os.IsNotExist(err) {
			return Entry{}, ErrNotFound
		}
		return Entry{}, err
	}
	name := path.Base(apiPath)
	if apiPath == "/" {
		name = ""
	}
	return Entry{
		Name:    name,
		Path:    apiPath,
		IsDir:   st.IsDir(),
		Size:    st.Size(),
		ModTime: st.ModTime().UTC(),
	}, nil
}

func (r *Repo) Open(relPath string) (*os.File, Entry, error) {
	abs, apiPath, err := r.resolve(relPath)
	if err != nil {
		return nil, Entry{}, err
	}
	st, err := os.Stat(abs)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, Entry{}, ErrNotFound
		}
		return nil, Entry{}, err
	}
	if st.IsDir() {
		return nil, Entry{}, ErrIsDir
	}
	f, err := os.Open(abs)
	if err != nil {
		return nil, Entry{}, err
	}
	name := path.Base(apiPath)
	return f, Entry{Name: name, Path: apiPath, Size: st.Size(), ModTime: st.ModTime().UTC()}, nil
}

// CreateFile opens path for writing. offset 0 truncates (or creates).
// A non-zero offset writes at that position (chunked upload).
func (r *Repo) CreateFile(relPath string, offset int64) (*os.File, error) {
	abs, apiPath, err := r.resolve(relPath)
	if err != nil {
		return nil, err
	}
	if apiPath == "/" {
		return nil, ErrIsDir
	}
	if err := os.MkdirAll(filepath.Dir(abs), 0o750); err != nil {
		return nil, err
	}
	if offset == 0 {
		return os.OpenFile(abs, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o640)
	}
	f, err := os.OpenFile(abs, os.O_WRONLY, 0o640)
	if err != nil {
		return nil, err
	}
	if _, err := f.Seek(offset, io.SeekStart); err != nil {
		f.Close()
		return nil, err
	}
	return f, nil
}

func (r *Repo) WriteAt(relPath string, offset int64, data io.Reader) (int64, error) {
	f, err := r.CreateFile(relPath, offset)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	n, err := io.Copy(f, data)
	if err != nil {
		return n, err
	}
	return n, f.Sync()
}
