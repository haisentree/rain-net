package configstore

import (
	"errors"
	"os"
	"path/filepath"
	"time"
)

var errFileNotFound = errors.New("configstore: file not found")

func readFile(p string) ([]byte, error) {
	data, err := os.ReadFile(p)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, errFileNotFound
		}
		return nil, err
	}
	return data, nil
}

// writeFile 原子写:临时文件 + rename,避免读侧看到半截内容
func writeFile(p string, val []byte) error {
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, val, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, p)
}

func modTime(p string) (time.Time, error) {
	st, err := os.Stat(p)
	if err != nil {
		return time.Time{}, err
	}
	return st.ModTime(), nil
}
