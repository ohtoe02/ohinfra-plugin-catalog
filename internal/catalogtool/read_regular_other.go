//go:build !linux

package catalogtool

import (
	"errors"
	"os"
)

// openRegularFile is a package-test compatibility seam. Production release
// import is fail-closed before reaching this implementation on non-Linux
// systems. The before/descriptor/after identity checks still prevent tests
// from silently following a stable or replaced symlink.
func openRegularFile(path string) (*os.File, error) {
	before, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if before.Mode()&os.ModeSymlink != 0 || !before.Mode().IsRegular() {
		return nil, errors.New("path must be a regular non-symlink file")
	}
	file, err := os.Open(path) // #nosec G304 -- test-only path with identity checks below.
	if err != nil {
		return nil, err
	}
	opened, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return nil, err
	}
	after, err := os.Lstat(path)
	if err != nil {
		_ = file.Close()
		return nil, err
	}
	if after.Mode()&os.ModeSymlink != 0 || !after.Mode().IsRegular() ||
		!os.SameFile(before, opened) || !os.SameFile(after, opened) {
		_ = file.Close()
		return nil, errors.New("path changed while it was being opened")
	}
	return file, nil
}
