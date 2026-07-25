package catalogtool

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestImportReleaseWritesDeterministicValidatedEntry(t *testing.T) {
	root := t.TempDir()
	binary := filepath.Join(root, "plugin")
	content := []byte("immutable plugin bytes")
	if err := os.WriteFile(binary, content, 0o700); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(content)
	metadata := ReleaseMetadata{
		SchemaVersion:         "1",
		Name:                  "sample",
		Description:           "sample plugin",
		Homepage:              "https://github.com/example/sample",
		Version:               "1.2.3",
		MinimumOHToolsVersion: "0.3.3",
		PublishedAt:           time.Date(2026, 7, 25, 10, 0, 0, 0, time.UTC),
		Asset: Asset{
			OS: "linux", Arch: "amd64",
			URL:       "https://github.com/example/sample/releases/download/v1.2.3/sample",
			SHA256:    hex.EncodeToString(sum[:]),
			SizeBytes: int64(len(content)),
		},
		Manifest: Manifest{
			ProtocolVersion: 1,
			Name:            "sample",
			Version:         "1.2.3",
			Description:     "sample plugin",
			Commands: []Command{{
				Path: []string{"sample", "status"}, Use: "status",
				Short: "Show sample status", Category: "diagnostic",
				Arguments: []Argument{}, Flags: []Flag{},
			}},
		},
	}
	encoded, err := json.Marshal(metadata)
	if err != nil {
		t.Fatal(err)
	}
	sidecar := filepath.Join(root, "release-metadata-v1.json")
	if err := os.WriteFile(sidecar, encoded, 0o600); err != nil {
		t.Fatal(err)
	}
	duplicateSidecar := filepath.Join(root, "duplicate-release-metadata-v1.json")
	duplicate := strings.Replace(
		string(encoded),
		`"schema_version":"1"`,
		`"schema_version":"1","schema_version":"1"`,
		1,
	)
	if err := os.WriteFile(duplicateSidecar, []byte(duplicate), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ImportRelease(
		duplicateSidecar,
		binary,
		filepath.Join(root, "duplicate-plugins"),
	); err == nil || !strings.Contains(err.Error(), "duplicate JSON field") {
		t.Fatalf("duplicate field error = %v", err)
	}
	missingFieldSidecar := filepath.Join(root, "missing-field-release-metadata-v1.json")
	missingField := strings.Replace(string(encoded), `"requires_root":false,`, "", 1)
	if err := os.WriteFile(missingFieldSidecar, []byte(missingField), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ImportRelease(
		missingFieldSidecar,
		binary,
		filepath.Join(root, "missing-field-plugins"),
	); err == nil || !strings.Contains(err.Error(), "missing JSON field") {
		t.Fatalf("missing field error = %v", err)
	}
	plugins := filepath.Join(root, "plugins")

	output, err := ImportRelease(sidecar, binary, plugins)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(plugins, "sample", "1.2.3.yaml")
	if output != want {
		t.Fatalf("output=%q want=%q", output, want)
	}
	first, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	entries, err := LoadEntries(plugins)
	if err != nil || len(entries) != 1 {
		t.Fatalf("entries=%#v error=%v", entries, err)
	}
	if _, err := ImportRelease(sidecar, binary, plugins); err == nil ||
		!strings.Contains(err.Error(), "already exists") {
		t.Fatalf("overwrite error = %v", err)
	}
	second, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	if string(first) != string(second) {
		t.Fatal("failed overwrite changed catalog entry")
	}
}

func TestImportReleaseRejectsUnsafeOrMismatchedInputs(t *testing.T) {
	if _, err := ImportRelease("missing", "missing", t.TempDir()); err == nil {
		t.Fatal("missing sidecar accepted")
	}

	root := t.TempDir()
	binary := filepath.Join(root, "plugin")
	if err := os.WriteFile(binary, []byte("actual"), 0o700); err != nil {
		t.Fatal(err)
	}
	metadata := ReleaseMetadata{
		SchemaVersion:         "1",
		Name:                  "sample",
		Description:           "sample plugin",
		Version:               "1.0.0",
		MinimumOHToolsVersion: "0.3.3",
		PublishedAt:           time.Now().UTC(),
		Asset: Asset{
			OS: "linux", Arch: "amd64",
			URL:       "https://user:secret@github.com/example/sample/releases/download/v1.0.0/sample",
			SHA256:    strings.Repeat("0", 64),
			SizeBytes: 6,
		},
		Manifest: Manifest{
			ProtocolVersion: 1, Name: "sample", Version: "1.0.0",
			Description: "sample plugin",
			Commands: []Command{{
				Path: []string{"sample", "status"}, Use: "status",
				Short: "Show sample status", Category: "diagnostic",
				Arguments: []Argument{}, Flags: []Flag{},
			}},
		},
	}
	encoded, _ := json.Marshal(metadata)
	sidecar := filepath.Join(root, "release-metadata-v1.json")
	if err := os.WriteFile(sidecar, encoded, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ImportRelease(sidecar, binary, filepath.Join(root, "plugins")); err == nil {
		t.Fatal("credential-bearing or mismatched release accepted")
	}
}

func TestImportReleaseRejectsSymlinkInput(t *testing.T) {
	if testing.Short() {
		t.Skip("filesystem safety integration")
	}
	root := t.TempDir()
	target := filepath.Join(root, "target")
	link := filepath.Join(root, "link")
	if err := os.WriteFile(target, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if _, err := ImportRelease(link, target, filepath.Join(root, "plugins")); err == nil {
		t.Fatal("symlink sidecar accepted")
	}
}
