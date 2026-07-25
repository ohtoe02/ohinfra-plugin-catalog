# Plugin Authoring Documentation Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add complete English plugin-authoring documentation, a copyable Go protocol-v1 example, and CI checks that keep both aligned with catalog validation.

**Architecture:** Keep prose split by reader task, make the standalone example speak the JSON protocol directly with the Go standard library, and enforce documentation/example integrity from the existing root test suite. Keep all example catalog metadata outside `plugins/` so documentation cannot alter the published snapshot.

**Tech Stack:** Markdown, Go 1.26, JSON/YAML protocol fixtures, GoReleaser v2 configuration, GitHub Actions, existing `catalogctl`.

---

### Task 1: Lock the documentation contract with a failing repository test

**Files:**
- Create: `internal/catalogtool/documentation_test.go`
- Test: `internal/catalogtool/documentation_test.go`

**Step 1: Write the failing test**

Add a test that locates the repository root, asserts that the four guides and
all important `examples/minimal-go` files exist, verifies that README links to
the authoring guide, and proves the example catalog YAML is not under
`plugins/`.

**Step 2: Run the test to verify it fails**

Run: `go test ./internal/catalogtool -run TestAuthorDocumentationLayout`

Expected: FAIL because the guides and example files do not exist.

**Step 3: Commit the test**

```bash
git add internal/catalogtool/documentation_test.go
git commit -m "test: define plugin documentation layout"
```

### Task 2: Build the standalone protocol-v1 example test-first

**Files:**
- Create: `examples/minimal-go/go.mod`
- Create: `examples/minimal-go/main_test.go`
- Create: `examples/minimal-go/main.go`
- Create: `examples/minimal-go/testdata/invocation.json`

**Step 1: Write contract tests**

Cover:

- a canonical manifest for diagnostic and operational commands;
- strict decoding of an Invocation v1 fixture;
- canonical Result v1 arrays and maps;
- a plan whose `command_id` matches the invocation path;
- SHA-256 plan digest binding on execute;
- rejection of unknown JSON fields and paths outside
  `/tmp/ohtools-example/`.

**Step 2: Run the example tests to verify they fail**

Run: `go test ./...` from `examples/minimal-go`

Expected: FAIL because the implementation is absent.

**Step 3: Implement the smallest complete plugin**

Implement `manifest`, `plan`, and `execute` dispatch, local wire structs,
strict-one-document JSON decoding, canonical normalization, path confinement,
and atomic example-file writing. Keep stdout machine-only and errors on stderr.

**Step 4: Run the example tests**

Run: `go test ./...` from `examples/minimal-go`

Expected: PASS.

**Step 5: Commit**

```bash
git add examples/minimal-go/go.mod examples/minimal-go/main.go examples/minimal-go/main_test.go examples/minimal-go/testdata/invocation.json
git commit -m "feat: add minimal protocol v1 plugin"
```

### Task 3: Add complete build and release files

**Files:**
- Create: `examples/minimal-go/Makefile`
- Create: `examples/minimal-go/.goreleaser.yaml`
- Create: `examples/minimal-go/.github/workflows/release.yml`
- Create: `examples/minimal-go/catalog/example-plugin/1.0.0.yaml`
- Create: `examples/minimal-go/README.md`

**Step 1: Add build automation**

Provide `test`, `build`, `manifest`, and `clean` targets. Build with
`CGO_ENABLED=0`, `GOOS=linux`, `GOARCH=amd64`, and `-trimpath`.

**Step 2: Add release automation**

Configure GoReleaser to publish the raw `linux/amd64` plugin binary and checksum
file from stable tags. Add a least-privilege GitHub Actions release workflow.

**Step 3: Add the catalog contribution example**

Document every catalog field and use conspicuous placeholders for the immutable
release URL, size, and SHA-256. Keep the file outside the production
`plugins/` tree.

**Step 4: Add the example README**

Explain local tests, protocol calls, release setup, checksum/size collection,
catalog copying, and which placeholders must be replaced.

**Step 5: Verify build and protocol output**

Run:

```bash
go test ./...
go build -trimpath -o example-plugin .
./example-plugin manifest --protocol=1
```

Expected: tests pass, the binary builds, and stdout is one manifest JSON
document.

**Step 6: Commit**

```bash
git add examples/minimal-go
git commit -m "docs: complete minimal plugin project example"
```

### Task 4: Document executable protocol v1

**Files:**
- Create: `docs/protocol-v1.md`
- Reference: `examples/minimal-go/main.go`
- Reference: `examples/minimal-go/testdata/invocation.json`

**Step 1: Document process rules**

Describe executable discovery, exact argv, stdin/stdout/stderr boundaries,
timeouts/output bounds, exit behavior, strict JSON, and host-applied controls.

**Step 2: Document manifest syntax**

Give a complete JSON example and field tables for manifest, command, arguments,
flags, categories, reserved flags, and mutation requirements.

**Step 3: Document plan, invocation, and Result v1**

Give complete JSON examples and explain command ID formatting, plan digest,
canonical empty collections, statuses, structured errors, and redaction.

**Step 4: Cross-check examples**

Run the example binary for each supported verb and compare the output shape with
the guide.

**Step 5: Commit**

```bash
git add docs/protocol-v1.md
git commit -m "docs: specify executable plugin protocol v1"
```

### Task 5: Document the author workflow and catalog YAML

**Files:**
- Create: `docs/plugin-authoring.md`
- Create: `docs/catalog-entry.md`

**Step 1: Write the author workflow**

Cover project layout, implementation sequence, local protocol checks, static
build, release asset publication, catalog PR preparation, versioning, yanking,
security expectations, and common mistakes.

**Step 2: Write the catalog entry reference**

Explain `plugins/<name>/<version>.yaml`, every field, stable SemVer,
compatibility, timestamps, immutable HTTPS assets, exact size/SHA-256, manifest
identity, sorting, and the no-edit-after-release rule.

**Step 3: Link to copyable files**

Every important code/config/fixture mentioned in prose must link to the exact
file under `examples/minimal-go`.

**Step 4: Commit**

```bash
git add docs/plugin-authoring.md docs/catalog-entry.md
git commit -m "docs: add plugin authoring and catalog entry guides"
```

### Task 6: Document validation and contribution review

**Files:**
- Create: `docs/validation.md`
- Modify: `CONTRIBUTING.md`
- Modify: `README.md`

**Step 1: Write the validation guide**

Explain every local command and CI stage, expected generated layout, sandbox
constraints, exact manifest comparison, common error messages, and the
publication/signing trust boundary.

**Step 2: Update repository navigation**

Add a documentation index and quick start to README. Turn CONTRIBUTING into a
linked, checkable pre-PR and review checklist while preserving the immutable
entry rule.

**Step 3: Run the documentation contract test**

Run: `go test ./internal/catalogtool -run TestAuthorDocumentationLayout`

Expected: PASS.

**Step 4: Commit**

```bash
git add README.md CONTRIBUTING.md docs/validation.md
git commit -m "docs: add validation guide and contributor checklist"
```

### Task 7: Enforce the example in CI

**Files:**
- Modify: `.github/workflows/validate.yml`

**Step 1: Add nested-module tests**

Add steps with `working-directory: examples/minimal-go` for `go test ./...`,
`go vet ./...`, and a static `linux/amd64` build.

**Step 2: Exercise manifest/catalog comparison**

Build the example binary, capture `manifest --protocol=1`, materialize the
example catalog manifest into JSON with a small documented command or fixture,
and use `catalogctl compare-manifest` so drift fails CI.

**Step 3: Run workflow-equivalent commands locally**

Run:

```bash
go test -race ./...
go vet ./...
go run ./cmd/catalogctl validate --plugins plugins
go test ./...
go vet ./...
```

Run the final two commands from `examples/minimal-go`.

Expected: all commands pass.

**Step 4: Commit**

```bash
git add .github/workflows/validate.yml
git commit -m "ci: verify plugin authoring example"
```

### Task 8: Final validation and publication

**Files:**
- Verify: all changed files

**Step 1: Format and inspect**

Run `gofmt` on new Go files, inspect `git diff --check`, and review Markdown
links and example placeholders.

**Step 2: Run the complete suite**

Run:

```bash
go test -race ./...
go vet ./...
go run ./cmd/catalogctl validate --plugins plugins
go run ./cmd/catalogctl materialize --plugins plugins --output verification
```

Also run `go test -race ./...`, `go vet ./...`, and the static build from
`examples/minimal-go`.

Expected: all checks pass and the production catalog remains empty.

**Step 3: Push and open a pull request**

Push `codex/plugin-authoring-docs` and create a ready pull request against
`main`, summarizing the guides, runnable example, and CI enforcement.

