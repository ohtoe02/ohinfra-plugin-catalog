package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestManifestDefinesDiagnosticAndOperationalCommands(t *testing.T) {
	t.Parallel()

	manifest := pluginManifest()
	if manifest.ProtocolVersion != 1 || manifest.Name != "example-plugin" || manifest.Version != version {
		t.Fatalf("unexpected identity: %#v", manifest)
	}
	if len(manifest.Commands) != 2 {
		t.Fatalf("command count = %d, want 2", len(manifest.Commands))
	}
	if got := strings.Join(manifest.Commands[0].Path, " "); got != "example echo" {
		t.Fatalf("diagnostic command = %q", got)
	}
	write := manifest.Commands[1]
	if got := strings.Join(write.Path, " "); got != "example write" {
		t.Fatalf("operational command = %q", got)
	}
	if write.Category != "operational" || !write.SupportsDryRun || !write.RequiresConfirmation {
		t.Fatalf("unsafe operational metadata: %#v", write)
	}
}

func TestExecuteDiagnosticInvocationReturnsCanonicalResult(t *testing.T) {
	t.Parallel()

	input, err := os.Open(filepath.Join("testdata", "invocation.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer input.Close()

	invocation, err := readInvocation(input)
	if err != nil {
		t.Fatal(err)
	}
	result, err := execute(invocation, t.TempDir(), fixedNow)
	if err != nil {
		t.Fatal(err)
	}
	if result.SchemaVersion != "1.0" || result.Command != "example echo" || result.Status != "pass" {
		t.Fatalf("unexpected result: %#v", result)
	}
	if result.Data["echo"] != "HELLO" {
		t.Fatalf("echo = %#v", result.Data["echo"])
	}
	if result.Checks == nil || result.Changes == nil || result.Errors == nil {
		t.Fatal("canonical result must contain empty arrays")
	}
}

func TestOperationalExecuteRequiresApprovedPlanAndWritesAtomically(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	invocation := Invocation{
		ProtocolVersion: 1,
		RequestID:       "write-1",
		CommandPath:     []string{"example", "write"},
		Arguments:       []string{"hello from ohinfra"},
		Options:         map[string]any{},
	}
	plan, err := buildPlan(invocation, root)
	if err != nil {
		t.Fatal(err)
	}
	invocation.PlanDigest, err = planDigest(plan)
	if err != nil {
		t.Fatal(err)
	}

	result, err := execute(invocation, root, fixedNow)
	if err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(filepath.Join(root, "message.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "hello from ohinfra\n" {
		t.Fatalf("content = %q", content)
	}
	if result.Status != "pass" || len(result.Changes) != 1 || result.Changes[0].Status != "applied" {
		t.Fatalf("unexpected result: %#v", result)
	}

	invocation.PlanDigest = strings.Repeat("0", 64)
	if _, err := execute(invocation, root, fixedNow); err == nil || !strings.Contains(err.Error(), "plan digest") {
		t.Fatalf("digest mismatch error = %v", err)
	}
}

func TestReadInvocationRejectsUnknownFieldsAndTrailingDocuments(t *testing.T) {
	t.Parallel()

	for _, input := range []string{
		`{"protocol_version":1,"unknown":true}`,
		`{"protocol_version":1} {}`,
	} {
		if _, err := readInvocation(strings.NewReader(input)); err == nil {
			t.Fatalf("accepted malformed invocation %q", input)
		}
	}
}

func TestRunKeepsProtocolJSONOnStdout(t *testing.T) {
	t.Parallel()

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := run(
		[]string{"example-plugin", "manifest", "--protocol=1"},
		strings.NewReader(""),
		&stdout,
		&stderr,
		t.TempDir(),
		fixedNow,
	)
	if exitCode != 0 || stderr.Len() != 0 {
		t.Fatalf("exit=%d stderr=%q", exitCode, stderr.String())
	}
	var manifest Manifest
	if err := json.Unmarshal(stdout.Bytes(), &manifest); err != nil {
		t.Fatalf("stdout is not one JSON document: %v", err)
	}
}

func fixedNow() time.Time {
	return time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)
}
