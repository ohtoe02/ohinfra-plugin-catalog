# Author an ohtools plugin

An `ohtools` plugin is a public, static `linux/amd64` executable that exposes
first-class CLI commands through the
[executable protocol v1](protocol-v1.md). The catalog does not build plugins
for publishers: it reviews metadata, verifies an immutable release asset, and
publishes that metadata in a signed snapshot.

This guide takes a plugin from a new repository to a catalog pull request.

## Start from the reference project

Copy [`examples/minimal-go`](../examples/minimal-go/README.md) into a new public
repository. It contains every important project file:

```text
.
├── .github/workflows/release.yml
├── catalog/example-plugin/1.0.0.yaml
├── testdata/invocation.json
├── testdata/manifest.json
├── .goreleaser.yaml
├── go.mod
├── main.go
├── main_test.go
├── Makefile
└── README.md
```

Replace at least:

1. the module path in `go.mod`;
2. the plugin name, version, description, and commands in `main.go`;
3. contract expectations in `main_test.go` and `testdata`;
4. binary/project names in the Makefile and GoReleaser configuration;
5. repository metadata in README and the catalog entry template.

Keep the plugin independent of `ohtools/internal` packages. They are not a
public SDK and may change without compatibility guarantees.

## Design command paths

Each path has at least a module and command:

```json
["postgres", "connections"]
["postgres", "vacuum"]
["network", "dns", "check"]
```

This registers:

```text
ohtools postgres connections
ohtools postgres vacuum
ohtools network dns check
```

Use lowercase identifiers with letters, numbers, and hyphens. Choose a
plugin-specific module name to reduce collisions. Do not copy built-in paths
such as `system info`, `disk usage`, or `service status`.

Classify each command before implementation:

- `diagnostic`: observes state and never changes it;
- `operational`: performs one controlled change;
- `runbook`: performs a multi-step operational procedure.

Operational and runbook commands must support dry-run. Declare root, force, and
confirmation requirements in the manifest so the host can enforce them before
execution.

## Implement the three verbs

Dispatch only these forms:

```text
plugin manifest --protocol=1
plugin plan --protocol=1
plugin execute --protocol=1
```

`manifest` describes all commands. Diagnostic commands are handled directly by
`execute`. Mutating commands first receive `plan`; the host may stop after
rendering that plan for `--dry-run`. After approval, the host sends `execute`
with the digest of the approved plan.

Keep stdout reserved for one JSON document. Use stderr for diagnostics and a
non-zero process exit for malformed input or an inability to produce a valid
protocol response.

Read the full [protocol reference](protocol-v1.md) before adding fields or
wire behavior.

## Validate all input again

The host validates manifest-declared syntax, but a plugin is a separate security
boundary. Revalidate:

- protocol version and selected command path;
- exact positional argument count;
- every option type;
- allowed identifiers and enumerations;
- filesystem paths after cleaning and resolving;
- domain-specific bounds before allocating or executing.

Never pass untrusted text through `sh -c`, `bash -c`, or a constructed command
line. If an external program is unavoidable, use an absolute executable path
and an argv array, restrict the environment, and bound output and runtime.

The example's mutating command derives a fixed target below a dedicated root and
uses a staged file plus atomic rename. Real plugins should use an equally narrow
resource model.

## Return canonical results

Return Result schema `1.0` for successful protocol execution, including empty
arrays and an empty data object where appropriate. Choose statuses according to
operator impact, not process implementation details.

Useful conventions:

- `pass`: requested work or check completed;
- `info`: observation with no threshold failure;
- `warning`: degraded state that does not require immediate intervention;
- `critical`: severe state or failed rollback;
- `partial`: a composite operation completed only in part;
- `error`: execution could not produce the requested outcome;
- `cancelled`: context cancellation or operator cancellation;
- `skipped`: a check was intentionally not run.

Do not put secrets in errors, checks, changes, data, stderr, or debug output.
Assume all protocol data may be rendered or audited after host redaction.

## Test locally

From the plugin repository:

```bash
go test -race ./...
go vet ./...
go run . manifest --protocol=1
go run . execute --protocol=1 < testdata/invocation.json
```

Tests should cover:

- exact manifest identity and command metadata;
- one JSON document on stdout and clean stderr on success;
- unknown fields, wrong types, and trailing JSON rejection;
- every command path and argument layout;
- plan determinism and digest mismatch rejection;
- dry-run through the host lifecycle;
- path traversal, option smuggling, and shell metacharacters;
- atomicity, verification, and rollback for changes;
- canonical empty Result collections;
- cancellation, deadline, and bounded output behavior;
- secret-like values never reaching output.

The reference tests are in
[`main_test.go`](../examples/minimal-go/main_test.go).

## Build a static asset

The first catalog version supports Debian 12/13 on `linux/amd64` only. Build
with:

```bash
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
  go build -buildvcs=false -trimpath \
  -ldflags="-s -w -X main.version=1.0.0" \
  -o dist/example-plugin_linux_amd64 .
```

Verify the artifact, not merely the local source:

```bash
file dist/example-plugin_linux_amd64
ldd dist/example-plugin_linux_amd64
sha256sum dist/example-plugin_linux_amd64
stat --format=%s dist/example-plugin_linux_amd64
```

For a static binary, `ldd` should report that it is not dynamically linked.
The exact checksum and byte count become signed catalog metadata.

The copyable configurations are:

- [`Makefile`](../examples/minimal-go/Makefile);
- [`.goreleaser.yaml`](../examples/minimal-go/.goreleaser.yaml);
- [release workflow](../examples/minimal-go/.github/workflows/release.yml).

## Publish an immutable GitHub Release

Release from the exact reviewed commit:

1. update the version returned by `manifest`;
2. run the complete test suite;
3. create a stable `vX.Y.Z` tag;
4. let the protected release workflow publish the raw binary and checksum;
5. verify the public download without authentication;
6. record its exact URL, SHA-256, byte size, and publication time.

Do not replace an asset after its catalog contribution is opened. A changed
binary with the same version invalidates reviewed metadata and will fail
materialization. Publish a new version instead.

The catalog accepts public, credential-free HTTPS URLs on approved GitHub asset
hosts. Catalog and client downloads intentionally ignore publisher credentials
and proxy environment variables.

## Prepare the catalog contribution

The release workflow should emit `release-metadata-v1.json` next to the exact
binary. Download both files without credentials, then import them:

```bash
go run ./cmd/catalogctl import-release \
  --metadata ./release-metadata-v1.json \
  --binary ./plugin_linux_amd64 \
  --sandbox-runtime /usr/bin/docker \
  --plugins plugins
```

The command strictly decodes the sidecar, rejects symlinks and credential URLs,
checks the binary size and SHA-256, verifies the exact manifest in the same
restricted Docker sandbox used by CI, and creates
`plugins/<plugin-name>/<version>.yaml` with atomic create-if-absent semantics.
It fails closed if the trusted Docker CLI or preloaded sandbox image is
unavailable.
See the [release metadata reference](release-metadata-v1.md).

Read the complete [catalog entry reference](catalog-entry.md), then run:

```bash
go test -race ./...
go vet ./...
go run ./cmd/catalogctl validate --plugins plugins
go run ./cmd/catalogctl materialize --plugins plugins --output verification
```

`materialize` downloads every declared asset, verifies size and SHA-256, and
prepares it for sandboxed manifest execution. Pull-request CI repeats these
checks and compares the actual manifest with the YAML metadata.

## Open the pull request

The pull request should include:

- one new immutable version entry, or a separately reviewed yanking change;
- a link to the plugin source repository and release;
- the release tag and commit;
- local validation output;
- a short explanation of privileges, changes, and external executables;
- tests for input validation and operational failure modes.

Maintainers review code provenance, command scope, privilege declarations,
manifest accuracy, and asset identity. Merge does not immediately sign a
snapshot: catalog publication is a separate protected job.

## Release subsequent versions

Use stable `X.Y.Z` SemVer and add a new YAML file for each release. Never edit
the asset URL, digest, size, or manifest of an already published catalog
version.

If a release must stop being selected, propose `yanked: true` in a focused pull
request with the reason. Yanking prevents new default selection; it does not
silently replace installed binaries.

Prereleases, downgrade, arm64, user plugin directories, multiple catalogs, and
self-service signing keys are outside protocol/catalog v1.
