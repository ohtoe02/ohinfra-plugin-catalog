package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
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

func TestManifestMatchesCatalogGolden(t *testing.T) {
	t.Parallel()

	expected, err := os.ReadFile(filepath.Join("testdata", "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	actual, err := json.Marshal(pluginManifest())
	if err != nil {
		t.Fatal(err)
	}
	var expectedValue any
	if err := json.Unmarshal(expected, &expectedValue); err != nil {
		t.Fatal(err)
	}
	var actualValue any
	if err := json.Unmarshal(actual, &actualValue); err != nil {
		t.Fatal(err)
	}
	expectedCanonical, _ := json.Marshal(expectedValue)
	actualCanonical, _ := json.Marshal(actualValue)
	if !bytes.Equal(expectedCanonical, actualCanonical) {
		t.Fatalf("manifest does not match testdata/manifest.json\nactual: %s", actual)
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
		Arguments:       []string{"hello from ohtools"},
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
	if string(content) != "hello from ohtools\n" {
		t.Fatalf("content = %q", content)
	}
	if result.Status != "pass" || len(result.Changes) != 1 || result.Changes[0].Status != "applied" {
		t.Fatalf("unexpected result: %#v", result)
	}

	secondPlan, err := buildPlan(invocation, root)
	if err != nil {
		t.Fatal(err)
	}
	if len(secondPlan.Changes) != 0 {
		t.Fatalf("second plan changes = %#v, want no-op", secondPlan.Changes)
	}
	invocation.PlanDigest, err = planDigest(secondPlan)
	if err != nil {
		t.Fatal(err)
	}
	second, err := execute(invocation, root, fixedNow)
	if err != nil {
		t.Fatal(err)
	}
	if len(second.Changes) != 0 || second.Data["changed"] != false ||
		second.Data["reason"] != "already_desired" {
		t.Fatalf("second execution is not an explicit no-op: %#v", second)
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

func TestBuiltBinaryProtocolLifecycleIsIdempotent(t *testing.T) {
	temp := t.TempDir()
	root := filepath.Join(temp, "state")
	binary := filepath.Join(temp, "example-plugin")
	if runtime.GOOS == "windows" {
		binary += ".exe"
	}
	build := exec.Command(
		"go",
		"build",
		"-buildvcs=false",
		"-trimpath",
		"-ldflags=-X=main.defaultRoot="+filepath.ToSlash(root),
		"-o",
		binary,
		".",
	)
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build real plugin binary: %v\n%s", err, output)
	}

	manifestOutput, stderr, exit := runBinary(t, binary, "manifest", nil)
	if exit != 0 || len(stderr) != 0 {
		t.Fatalf("manifest exit=%d stderr=%q", exit, stderr)
	}
	var manifest Manifest
	if err := json.Unmarshal(manifestOutput, &manifest); err != nil {
		t.Fatalf("decode real manifest: %v", err)
	}
	if manifest.Name != "example-plugin" {
		t.Fatalf("manifest identity = %#v", manifest)
	}

	invocation := Invocation{
		ProtocolVersion: 1,
		RequestID:       "binary-lifecycle",
		CommandPath:     []string{"example", "write"},
		Arguments:       []string{"from real binary"},
		Options:         map[string]any{},
	}
	encoded, _ := json.Marshal(invocation)
	firstPlanOutput, stderr, exit := runBinary(t, binary, "plan", encoded)
	if exit != 0 || len(stderr) != 0 {
		t.Fatalf("first plan exit=%d stderr=%q", exit, stderr)
	}
	var firstPlan Plan
	if err := json.Unmarshal(firstPlanOutput, &firstPlan); err != nil {
		t.Fatal(err)
	}
	if len(firstPlan.Changes) != 1 {
		t.Fatalf("first plan changes = %#v", firstPlan.Changes)
	}
	invocation.PlanDigest, _ = planDigest(firstPlan)
	encoded, _ = json.Marshal(invocation)
	if _, stderr, exit = runBinary(t, binary, "execute", encoded); exit != 0 || len(stderr) != 0 {
		t.Fatalf("execute exit=%d stderr=%q", exit, stderr)
	}

	invocation.PlanDigest = ""
	encoded, _ = json.Marshal(invocation)
	secondPlanOutput, stderr, exit := runBinary(t, binary, "plan", encoded)
	if exit != 0 || len(stderr) != 0 {
		t.Fatalf("second plan exit=%d stderr=%q", exit, stderr)
	}
	var secondPlan Plan
	if err := json.Unmarshal(secondPlanOutput, &secondPlan); err != nil {
		t.Fatal(err)
	}
	if len(secondPlan.Changes) != 0 {
		t.Fatalf("second plan changes = %#v, want no-op", secondPlan.Changes)
	}
	invocation.PlanDigest, _ = planDigest(secondPlan)
	encoded, _ = json.Marshal(invocation)
	resultOutput, stderr, exit := runBinary(t, binary, "execute", encoded)
	if exit != 0 || len(stderr) != 0 {
		t.Fatalf("second execute exit=%d stderr=%q", exit, stderr)
	}
	var result Result
	if err := json.Unmarshal(resultOutput, &result); err != nil {
		t.Fatal(err)
	}
	if len(result.Changes) != 0 || result.Data["reason"] != "already_desired" {
		t.Fatalf("second result = %#v", result)
	}
}

func runBinary(t *testing.T, binary, verb string, input []byte) ([]byte, []byte, int) {
	t.Helper()
	command := exec.Command(binary, verb, "--protocol=1")
	command.Stdin = bytes.NewReader(input)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	err := command.Run()
	if err == nil {
		return stdout.Bytes(), stderr.Bytes(), 0
	}
	var exitError *exec.ExitError
	if errors.As(err, &exitError) {
		return stdout.Bytes(), stderr.Bytes(), exitError.ExitCode()
	}
	t.Fatalf("run %s: %v", verb, err)
	return nil, nil, -1
}

func fixedNow() time.Time {
	return time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)
}
