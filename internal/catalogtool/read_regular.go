package catalogtool

import (
	"errors"
	"fmt"
	"io"
	"os"
)

type regularFileOpener func(string) (*os.File, error)

func readRegularFile(path string, limit int64) ([]byte, error) {
	return readRegularFileWithOpener(path, limit, openRegularFile)
}

func readRegularFileWithOpener(
	path string,
	limit int64,
	opener regularFileOpener,
) ([]byte, error) {
	if limit < 0 {
		return nil, errors.New("file size limit must be non-negative")
	}
	file, err := opener(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("path must be a regular non-symlink file")
	}
	if info.Size() < 0 || info.Size() > limit {
		return nil, fmt.Errorf("file exceeds %d bytes", limit)
	}
	encoded, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(encoded)) > limit {
		return nil, fmt.Errorf("file exceeds %d bytes", limit)
	}
	return encoded, nil
}
