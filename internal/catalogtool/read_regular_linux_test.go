//go:build linux

package catalogtool

import (
	"errors"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
)

func TestReadRegularFileNeverFollowsRacingFinalSymlink(t *testing.T) {
	root := t.TempDir()
	input := filepath.Join(root, "input")
	evil := filepath.Join(root, "evil")
	if err := os.WriteFile(input, []byte("trusted"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(evil, []byte("untrusted"), 0o600); err != nil {
		t.Fatal(err)
	}
	probe := filepath.Join(root, "symlink-probe")
	if err := os.Symlink(evil, probe); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if err := os.Remove(probe); err != nil {
		t.Fatal(err)
	}

	var stopped atomic.Bool
	done := make(chan struct{})
	go func() {
		defer close(done)
		for !stopped.Load() {
			staged := filepath.Join(root, "staged")
			_ = os.Remove(staged)
			if err := os.Symlink(evil, staged); err == nil {
				_ = os.Rename(staged, input)
			}
			_ = os.Remove(staged)
			if err := os.WriteFile(staged, []byte("trusted"), 0o600); err == nil {
				_ = os.Rename(staged, input)
			}
		}
	}()
	defer func() {
		stopped.Store(true)
		<-done
	}()

	successes := 0
	for range 20_000 {
		content, err := readRegularFile(input, 64)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			// ELOOP and transient replacement failures are safe rejection.
			continue
		}
		if string(content) != "trusted" {
			t.Fatalf("followed racing symlink and read %q", content)
		}
		successes++
	}
	if successes == 0 {
		t.Fatal("every regular-file read was rejected")
	}
}
