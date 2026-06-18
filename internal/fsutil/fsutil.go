package fsutil

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
)

// ReadFileBytes reads a file. Returns the bytes.
func ReadFileBytes(path string) ([]byte, error) {
	return os.ReadFile(path)
}

// IsNotExist reports whether the error indicates a missing file.
func IsNotExist(err error) bool {
	return errors.Is(err, fs.ErrNotExist)
}

// WriteFileAtomic writes data to a temp file and renames over the target.
// On crash mid-write, the original file is left untouched.
func WriteFileAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath) // best-effort cleanup on rename failure

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}