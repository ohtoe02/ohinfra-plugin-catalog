package catalogtool

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestImportReleaseWritesDeterministicValidatedEntry(t *testing.T) {
	root := t.TempDir()
	binary, content := buildReleaseFixture(t, root)
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
			URL:       "https://github.com/ohtoe02/ohtools-plugins/releases/download/sample-v1.2.3/sample_linux_amd64",
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
	if _, err := ImportReleaseWithSandbox(
		duplicateSidecar,
		binary,
		filepath.Join(root, "duplicate-plugins"),
		trustedFixtureSandbox{},
	); err == nil || !strings.Contains(err.Error(), "duplicate JSON field") {
		t.Fatalf("duplicate field error = %v", err)
	}
	missingFieldSidecar := filepath.Join(root, "missing-field-release-metadata-v1.json")
	missingField := strings.Replace(string(encoded), `"requires_root":false,`, "", 1)
	if err := os.WriteFile(missingFieldSidecar, []byte(missingField), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ImportReleaseWithSandbox(
		missingFieldSidecar,
		binary,
		filepath.Join(root, "missing-field-plugins"),
		trustedFixtureSandbox{},
	); err == nil || !strings.Contains(err.Error(), "missing JSON field") {
		t.Fatalf("missing field error = %v", err)
	}
	plugins := filepath.Join(root, "plugins")

	output, err := ImportReleaseWithSandbox(
		sidecar,
		binary,
		plugins,
		trustedFixtureSandbox{},
	)
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
	if _, err := ImportReleaseWithSandbox(
		sidecar,
		binary,
		plugins,
		trustedFixtureSandbox{},
	); err == nil ||
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

func TestImportReleaseRejectsManifestThatDoesNotMatchBinary(t *testing.T) {
	root := t.TempDir()
	binary, content := buildReleaseFixture(t, root)
	metadata := releaseFixtureMetadata(content)
	metadata.Manifest.Commands[0].Short = "Different but otherwise valid manifest"
	sidecar := writeReleaseSidecar(t, root, metadata)

	if _, err := ImportReleaseWithSandbox(
		sidecar,
		binary,
		filepath.Join(root, "plugins"),
		trustedFixtureSandbox{},
	); err == nil || !strings.Contains(err.Error(), "manifest") {
		t.Fatalf("manifest mismatch error = %v", err)
	}
}

func TestImportReleaseFailsClosedWithoutSandbox(t *testing.T) {
	root := t.TempDir()
	binary, content := buildReleaseFixture(t, root)
	sidecar := writeReleaseSidecar(t, root, releaseFixtureMetadata(content))

	if _, err := ImportReleaseWithSandbox(
		sidecar,
		binary,
		filepath.Join(root, "plugins"),
		nil,
	); err == nil || !strings.Contains(err.Error(), "sandbox is required") {
		t.Fatalf("missing sandbox error = %v", err)
	}
}

func TestDockerManifestSandboxUsesRestrictedContainerAndExactBytes(t *testing.T) {
	root := t.TempDir()
	runtimePath := buildTestCommand(t, root, "./testdata/fakedocker", "fakedocker")
	sandbox := DockerManifestSandbox{
		RuntimePath: runtimePath,
		Image:       dockerSandboxImage,

		allowUntrustedRuntime: true,
	}

	actual, err := sandbox.Manifest(context.Background(), []byte("verified release bytes"))
	if err != nil {
		t.Fatal(err)
	}
	expected := releaseFixtureMetadata([]byte("unused")).Manifest
	if err := CompareManifest(expected, actual); err != nil {
		t.Fatal(err)
	}
}

func TestDefaultDockerManifestSandboxPinsExactImageDigest(t *testing.T) {
	t.Parallel()
	const expected = "debian@sha256:28de0877c2189802884ccd20f15ee41c203573bd87bb6b883f5f46362d24c5c2"
	if sandbox := DefaultDockerManifestSandbox(); sandbox.Image != expected {
		t.Fatalf("sandbox image=%q want=%q", sandbox.Image, expected)
	}
}

func TestDockerManifestSandboxBoundsTimeAndOutput(t *testing.T) {
	root := t.TempDir()
	runtimePath := buildTestCommand(t, root, "./testdata/fakedocker", "fakedocker")
	sandbox := DockerManifestSandbox{
		RuntimePath: runtimePath,
		Image:       dockerSandboxImage,

		allowUntrustedRuntime: true,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if _, err := sandbox.Manifest(ctx, []byte("timeout release bytes")); err == nil ||
		!strings.Contains(err.Error(), "timed out") {
		t.Fatalf("timeout error = %v", err)
	}
	outputContext, outputCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer outputCancel()
	started := time.Now()
	if _, err := sandbox.Manifest(
		outputContext,
		[]byte("oversized release bytes"),
	); err == nil || !strings.Contains(err.Error(), "stdout exceeds") {
		t.Fatalf("output limit error = %v", err)
	}
	if elapsed := time.Since(started); elapsed >= time.Second {
		t.Fatalf("output overflow cancellation took %s", elapsed)
	}
}

func TestCommitNewFileNoReplaceIsAtomicUnderConcurrency(t *testing.T) {
	root := t.TempDir()
	const contenders = 32
	start := make(chan struct{})
	results := make(chan error, contenders)
	var wait sync.WaitGroup
	for index := range contenders {
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start
			results <- writeCatalogEntryAtomic(
				root,
				"sample",
				"entry.yaml",
				[]byte(strconv.Itoa(index)),
			)
		}()
	}
	close(start)
	wait.Wait()
	close(results)

	successes := 0
	conflicts := 0
	for err := range results {
		switch {
		case err == nil:
			successes++
		case errors.Is(err, os.ErrExist):
			conflicts++
		default:
			t.Fatalf("unexpected commit error: %v", err)
		}
	}
	if successes != 1 || conflicts != contenders-1 {
		t.Fatalf("successes=%d conflicts=%d", successes, conflicts)
	}
}

func TestImportReleaseHasExactlyOneConcurrentWinner(t *testing.T) {
	root := t.TempDir()
	content := []byte("concurrent release bytes")
	binary := filepath.Join(root, "plugin")
	if err := os.WriteFile(binary, content, 0o700); err != nil {
		t.Fatal(err)
	}
	metadata := releaseFixtureMetadata(content)
	sidecar := writeReleaseSidecar(t, root, metadata)
	actual, err := json.Marshal(metadata.Manifest)
	if err != nil {
		t.Fatal(err)
	}
	plugins := filepath.Join(root, "plugins")
	const contenders = 32
	start := make(chan struct{})
	results := make(chan error, contenders)
	var wait sync.WaitGroup
	for range contenders {
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start
			_, importErr := ImportReleaseWithSandbox(
				sidecar,
				binary,
				plugins,
				staticManifestSandbox(actual),
			)
			results <- importErr
		}()
	}
	close(start)
	wait.Wait()
	close(results)

	successes := 0
	conflicts := 0
	for importErr := range results {
		switch {
		case importErr == nil:
			successes++
		case strings.Contains(importErr.Error(), "already exists"):
			conflicts++
		default:
			t.Fatalf("unexpected concurrent import error: %v", importErr)
		}
	}
	if successes != 1 || conflicts != contenders-1 {
		t.Fatalf("successes=%d conflicts=%d", successes, conflicts)
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

func TestProductionImportFailsClosedOutsideLinux(t *testing.T) {
	if runtime.GOOS == "linux" {
		t.Skip("production import is supported on Linux")
	}
	_, err := ImportRelease("missing", "missing", t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "supported only on Linux") {
		t.Fatalf("platform error = %v", err)
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

func TestImportReleaseRejectsSymlinkBinary(t *testing.T) {
	if testing.Short() {
		t.Skip("filesystem safety integration")
	}
	root := t.TempDir()
	binary, content := buildReleaseFixture(t, root)
	sidecar := writeReleaseSidecar(t, root, releaseFixtureMetadata(content))
	link := filepath.Join(root, "plugin-link")
	if runtime.GOOS == "windows" {
		link += ".exe"
	}
	if err := os.Symlink(binary, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if _, err := ImportReleaseWithSandbox(
		sidecar,
		link,
		filepath.Join(root, "plugins"),
		trustedFixtureSandbox{},
	); err == nil || !strings.Contains(err.Error(), "non-symlink") {
		t.Fatalf("symlink binary error = %v", err)
	}
}

func TestImportReleaseRejectsTraversalMetadataName(t *testing.T) {
	root := t.TempDir()
	binary, content := buildReleaseFixture(t, root)
	metadata := releaseFixtureMetadata(content)
	metadata.Name = "../escape"
	metadata.Manifest.Name = metadata.Name
	sidecar := writeReleaseSidecar(t, root, metadata)
	plugins := filepath.Join(root, "plugins")

	if _, err := ImportReleaseWithSandbox(
		sidecar,
		binary,
		plugins,
		trustedFixtureSandbox{},
	); err == nil || !strings.Contains(err.Error(), "invalid plugin name") {
		t.Fatalf("traversal metadata error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "escape")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("traversal created an outside path: %v", err)
	}
}

func TestReadRegularFileReadsFromPinnedDescriptorAfterPathReplacement(t *testing.T) {
	root := t.TempDir()
	input := filepath.Join(root, "input")
	opened := filepath.Join(root, "opened")
	if err := os.WriteFile(input, []byte("trusted"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(opened, []byte("trusted"), 0o600); err != nil {
		t.Fatal(err)
	}

	content, err := readRegularFileWithOpener(input, 64, func(path string) (*os.File, error) {
		file, openErr := os.Open(opened)
		if openErr != nil {
			return nil, openErr
		}
		if writeErr := os.WriteFile(path, []byte("replacement"), 0o600); writeErr != nil {
			_ = file.Close()
			return nil, writeErr
		}
		return file, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "trusted" {
		t.Fatalf("read replacement bytes %q", content)
	}
}

func buildReleaseFixture(t *testing.T, root string) (string, []byte) {
	t.Helper()
	binary := buildTestCommand(t, root, "./testdata/releasefixture", "releasefixture")
	content, err := os.ReadFile(binary)
	if err != nil {
		t.Fatal(err)
	}
	return binary, content
}

func buildTestCommand(t *testing.T, root, packagePath, name string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	binary := filepath.Join(root, name)
	command := exec.Command("go", "build", "-trimpath", "-o", binary, packagePath)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("build %s: %v\n%s", packagePath, err, output)
	}
	return binary
}

func releaseFixtureMetadata(content []byte) ReleaseMetadata {
	sum := sha256.Sum256(content)
	return ReleaseMetadata{
		SchemaVersion:         "1",
		Name:                  "sample",
		Description:           "sample plugin",
		Homepage:              "https://github.com/example/sample",
		Version:               "1.2.3",
		MinimumOHToolsVersion: "0.3.3",
		PublishedAt:           time.Date(2026, 7, 25, 10, 0, 0, 0, time.UTC),
		Asset: Asset{
			OS: "linux", Arch: "amd64",
			URL:       "https://github.com/ohtoe02/ohtools-plugins/releases/download/sample-v1.2.3/sample_linux_amd64",
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
}

func writeReleaseSidecar(t *testing.T, root string, metadata ReleaseMetadata) string {
	t.Helper()
	encoded, err := json.Marshal(metadata)
	if err != nil {
		t.Fatal(err)
	}
	sidecar := filepath.Join(root, "release-metadata-v1.json")
	if err := os.WriteFile(sidecar, encoded, 0o600); err != nil {
		t.Fatal(err)
	}
	return sidecar
}

type trustedFixtureSandbox struct{}

func (trustedFixtureSandbox) testOnlyManifestSandbox() {}

func (trustedFixtureSandbox) Manifest(ctx context.Context, binary []byte) ([]byte, error) {
	directory, err := os.MkdirTemp("", "ohtools-trusted-fixture-*")
	if err != nil {
		return nil, err
	}
	defer func() { _ = os.RemoveAll(directory) }()
	name := "plugin"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	path := filepath.Join(directory, name)
	if err := os.WriteFile(path, binary, 0o700); err != nil {
		return nil, err
	}
	command := exec.CommandContext(ctx, path, "manifest", "--protocol=1")
	command.Dir = directory
	command.Env = []string{}
	command.Stdin = bytes.NewReader(nil)
	return command.Output()
}

type staticManifestSandbox []byte

func (staticManifestSandbox) testOnlyManifestSandbox() {}

func (sandbox staticManifestSandbox) Manifest(context.Context, []byte) ([]byte, error) {
	return append([]byte(nil), sandbox...), nil
}
