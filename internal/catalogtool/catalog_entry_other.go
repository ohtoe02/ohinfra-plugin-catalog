//go:build !linux

package catalogtool

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// writeCatalogEntryAtomic exists on non-Linux systems only so package-local
// tests can exercise import conversion. Production import is fail-closed
// before reaching this function.
func writeCatalogEntryAtomic(root, packageName, fileName string, content []byte) error {
	directory := filepath.Join(root, packageName)
	if err := rejectExistingSymlinksForTest(directory); err != nil {
		return err
	}
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return err
	}
	if err := rejectExistingSymlinksForTest(directory); err != nil {
		return err
	}
	temp, err := os.CreateTemp(directory, ".catalog-entry-*")
	if err != nil {
		return err
	}
	tempName := temp.Name()
	defer func() { _ = os.Remove(tempName) }()
	if err := temp.Chmod(0o600); err != nil {
		_ = temp.Close()
		return err
	}
	if _, err := temp.Write(content); err != nil {
		_ = temp.Close()
		return err
	}
	if err := temp.Sync(); err != nil {
		_ = temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	if err := os.Link(tempName, filepath.Join(directory, fileName)); err != nil {
		return err
	}
	return nil
}

func rejectExistingSymlinksForTest(path string) error {
	current := filepath.Clean(path)
	for {
		info, err := os.Lstat(current)
		if err == nil && info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("output path contains symlink %s", current)
		}
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		parent := filepath.Dir(current)
		if parent == current {
			return nil
		}
		current = parent
	}
}
