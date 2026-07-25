package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
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

func TestRunImportsReleaseSidecar(t *testing.T) {
	dir := t.TempDir()
	binary := filepath.Join(dir, "plugin")
	if err := os.WriteFile(binary, []byte("binary"), 0o700); err != nil {
		t.Fatal(err)
	}
	metadata := filepath.Join(dir, "release-metadata-v1.json")
	if err := os.WriteFile(metadata, []byte(`{}`), 0o600); err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	exit := run([]string{
		"import-release",
		"--metadata", metadata,
		"--binary", binary,
		"--plugins", filepath.Join(dir, "plugins"),
	}, func(string) string { return "" }, &bytes.Buffer{}, &stderr)
	if exit == 2 && strings.Contains(stderr.String(), "unknown command") {
		t.Fatalf("import-release command is not registered: %s", stderr.String())
	}
}
