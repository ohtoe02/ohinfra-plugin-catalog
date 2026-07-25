//go:build linux

package catalogtool

import (
	"errors"
	"os"
	"syscall"
)

func openRegularFile(path string) (*os.File, error) {
	fd, err := syscall.Open(
		path,
		syscall.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC|syscall.O_NONBLOCK,
		0,
	)
	if err != nil {
		if errors.Is(err, syscall.ELOOP) {
			return nil, errors.New("path must be a regular non-symlink file")
		}
		return nil, err
	}
	file := os.NewFile(uintptr(fd), path)
	if file == nil {
		_ = syscall.Close(fd)
		return nil, errors.New("create regular file handle")
	}
	return file, nil
}
