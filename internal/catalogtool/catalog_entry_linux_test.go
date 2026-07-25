//go:build linux

package catalogtool

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestWriteCatalogEntryAtomicDoesNotFollowPackageSymlink(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, "sample")); err != nil {
		t.Fatal(err)
	}

	err := writeCatalogEntryAtomic(root, "sample", "1.0.0.yaml", []byte("entry"))
	if err == nil {
		t.Fatal("package-directory symlink was followed")
	}
	if _, statErr := os.Stat(filepath.Join(outside, "1.0.0.yaml")); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("outside entry stat error = %v", statErr)
	}
}

func TestWriteCatalogEntryAtomicCreatesWithoutReplacing(t *testing.T) {
	root := filepath.Join(t.TempDir(), "plugins")
	if err := writeCatalogEntryAtomic(root, "sample", "1.0.0.yaml", []byte("first")); err != nil {
		t.Fatal(err)
	}
	if err := writeCatalogEntryAtomic(root, "sample", "1.0.0.yaml", []byte("second")); !errors.Is(err, os.ErrExist) {
		t.Fatalf("replacement error = %v", err)
	}
	content, err := os.ReadFile(filepath.Join(root, "sample", "1.0.0.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "first" {
		t.Fatalf("entry content = %q", content)
	}
}
