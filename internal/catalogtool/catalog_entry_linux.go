//go:build linux

package catalogtool

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"
)

func writeCatalogEntryAtomic(root, packageName, fileName string, content []byte) error {
	rootFD, err := openDirectoryTree(root, 0o755)
	if err != nil {
		return fmt.Errorf("open catalog root: %w", err)
	}
	defer syscall.Close(rootFD)

	packageFD, err := openOrCreateDirectoryAt(rootFD, packageName, 0o755)
	if err != nil {
		return fmt.Errorf("open plugin catalog directory: %w", err)
	}
	defer syscall.Close(packageFD)

	stagedName, stagedFD, err := createStagedFileAt(packageFD)
	if err != nil {
		return err
	}
	staged := os.NewFile(uintptr(stagedFD), stagedName)
	if staged == nil {
		_ = syscall.Close(stagedFD)
		_ = syscall.Unlinkat(packageFD, stagedName)
		return errors.New("create staged catalog file handle")
	}
	cleanup := true
	defer func() {
		_ = staged.Close()
		if cleanup {
			_ = syscall.Unlinkat(packageFD, stagedName)
		}
	}()
	if _, err := staged.Write(content); err != nil {
		return err
	}
	if err := staged.Sync(); err != nil {
		return err
	}
	if err := staged.Close(); err != nil {
		return err
	}
	if err := linkAt(packageFD, stagedName, packageFD, fileName); err != nil {
		return err
	}
	if err := syscall.Unlinkat(packageFD, stagedName); err != nil {
		return fmt.Errorf("remove staged catalog entry: %w", err)
	}
	cleanup = false
	if err := syscall.Fsync(packageFD); err != nil {
		return fmt.Errorf("sync plugin catalog directory: %w", err)
	}
	if err := syscall.Fsync(rootFD); err != nil {
		return fmt.Errorf("sync catalog root: %w", err)
	}
	return nil
}

func linkAt(oldDirectoryFD int, oldName string, newDirectoryFD int, newName string) error {
	oldPath, err := syscall.BytePtrFromString(oldName)
	if err != nil {
		return err
	}
	newPath, err := syscall.BytePtrFromString(newName)
	if err != nil {
		return err
	}
	_, _, callErr := syscall.Syscall6(
		syscall.SYS_LINKAT,
		uintptr(oldDirectoryFD),
		uintptr(unsafe.Pointer(oldPath)),
		uintptr(newDirectoryFD),
		uintptr(unsafe.Pointer(newPath)),
		0,
		0,
	)
	if callErr != 0 {
		return callErr
	}
	return nil
}

func openDirectoryTree(path string, mode uint32) (int, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return -1, err
	}
	absolute = filepath.Clean(absolute)
	if !filepath.IsAbs(absolute) {
		return -1, errors.New("catalog root must be absolute")
	}
	currentFD, err := syscall.Open(
		string(filepath.Separator),
		syscall.O_RDONLY|syscall.O_DIRECTORY|syscall.O_CLOEXEC,
		0,
	)
	if err != nil {
		return -1, err
	}
	for _, component := range strings.Split(strings.TrimPrefix(absolute, string(filepath.Separator)), string(filepath.Separator)) {
		if component == "" {
			continue
		}
		nextFD, openErr := openOrCreateDirectoryAt(currentFD, component, mode)
		if openErr != nil {
			_ = syscall.Close(currentFD)
			return -1, openErr
		}
		_ = syscall.Close(currentFD)
		currentFD = nextFD
	}
	return currentFD, nil
}

func openOrCreateDirectoryAt(parentFD int, name string, mode uint32) (int, error) {
	if name == "" || name == "." || name == ".." || strings.ContainsRune(name, filepath.Separator) {
		return -1, errors.New("unsafe catalog directory component")
	}
	flags := syscall.O_RDONLY | syscall.O_DIRECTORY | syscall.O_NOFOLLOW | syscall.O_CLOEXEC
	fd, err := syscall.Openat(parentFD, name, flags, 0)
	if err == nil {
		return fd, nil
	}
	if !errors.Is(err, syscall.ENOENT) {
		return -1, err
	}
	mkdirErr := syscall.Mkdirat(parentFD, name, mode)
	if mkdirErr != nil && !errors.Is(mkdirErr, syscall.EEXIST) {
		return -1, mkdirErr
	}
	if mkdirErr == nil {
		if err := syscall.Fsync(parentFD); err != nil {
			return -1, err
		}
	}
	return syscall.Openat(parentFD, name, flags, 0)
}

func createStagedFileAt(directoryFD int) (string, int, error) {
	var random [16]byte
	for range 128 {
		if _, err := rand.Read(random[:]); err != nil {
			return "", -1, err
		}
		name := ".catalog-entry-" + hex.EncodeToString(random[:])
		fd, err := syscall.Openat(
			directoryFD,
			name,
			syscall.O_WRONLY|syscall.O_CREAT|syscall.O_EXCL|syscall.O_NOFOLLOW|syscall.O_CLOEXEC,
			0o600,
		)
		if err == nil {
			return name, fd, nil
		}
		if !errors.Is(err, syscall.EEXIST) {
			return "", -1, err
		}
	}
	return "", -1, errors.New("could not allocate staged catalog entry")
}
