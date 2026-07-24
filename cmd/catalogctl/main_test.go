package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestRunBuildsEmptyCatalog(t *testing.T) {
	dir := t.TempDir()
	plugins := filepath.Join(dir, "plugins")
	if err := os.MkdirAll(plugins, 0o700); err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(dir, "index.json")
	var stderr bytes.Buffer
	exit := run([]string{
		"build",
		"--plugins", plugins,
		"--output", output,
		"--sequence", "1",
		"--generated-at", "2026-07-24T00:00:00Z",
		"--expires-at", "2026-08-07T00:00:00Z",
	}, func(string) string { return "" }, &bytes.Buffer{}, &stderr)
	if exit != 0 {
		t.Fatalf("exit=%d stderr=%s", exit, stderr.String())
	}
	if _, err := os.Stat(output); err != nil {
		t.Fatal(err)
	}
}
