# Validate a plugin contribution

Validation has two goals: prove that catalog metadata is internally valid, and
prove that the immutable public binary has exactly the reviewed identity and
commands. Passing local checks is required before a pull request; protected CI
and maintainer review remain authoritative.

## Prerequisites

Use:

- Go 1.26.5;
- Git;
- Docker for sandboxed manifest verification;
- a public immutable `linux/amd64` GitHub Release asset.

Do not place signing keys, GitHub tokens, or private plugin assets in the
checkout. Contribution validation is intentionally public and credential-free.

## Validate the repository

Run from the catalog root:

```bash
go test -race ./...
go vet ./...
go run ./cmd/catalogctl validate --plugins plugins
```

Expected output for an empty production catalog:

```text
validated 0 catalog entries
```

With entries, the count changes. A zero exit code is the contract; do not parse
the sentence in automation.

`validate` performs:

- strict single-document YAML decoding;
- unknown and duplicate key rejection;
- stable SemVer and release timestamp checks;
- plugin/version/manifest identity checks;
- command ownership, `use`/path agreement, bounded single-line help,
  argument ordering, flag defaults, and reserved flag checks;
- mutation dry-run requirements;
- unique `linux/amd64` asset checks;
- credential-free HTTPS, SHA-256, and size validation;
- duplicate plugin/version and cross-package command ownership detection.

It does not download assets.

## Import release metadata

Use the publisher-generated sidecar and exact release binary:

```bash
go run ./cmd/catalogctl import-release \
  --metadata ./release-metadata-v1.json \
  --binary ./plugin_linux_amd64 \
  --plugins plugins
```

Import is create-only. It rejects duplicate or unknown JSON fields, symlink
inputs/output components, traversal, credential-bearing asset URLs, mismatched
identity, size, or digest, and any attempt to overwrite an existing entry.
The resulting YAML is deterministic and is validated again by normal loading.

## Materialize assets

Run:

```bash
go run ./cmd/catalogctl materialize \
  --plugins plugins \
  --output verification
```

For each catalog version, materialization:

1. creates a bounded verification location;
2. downloads the declared asset using an HTTPS-only client;
3. follows at most five allowed-host redirects;
4. ignores proxy environment variables and credentials;
5. rejects a response outside the 1–100 MiB bound;
6. verifies exact `size_bytes`;
7. verifies SHA-256 while streaming;
8. writes the expected manifest used by the sandbox comparison.

The output layout is:

```text
verification/
└── <plugin>/
    └── <version>/
        ├── plugin
        └── manifest.json
```

`verification/` is generated, untrusted input and should not be committed.

## Verify the executable manifest

Pull-request CI runs only:

```text
/plugin/plugin manifest --protocol=1
```

inside a Debian 13 container with:

- a non-root `65534:65534` user;
- no network;
- read-only filesystem;
- all Linux capabilities dropped;
- `no-new-privileges`;
- PID limit 64;
- memory limit 128 MiB;
- CPU limit 1;
- the plugin directory mounted read-only.

The resulting JSON is compared with the expected catalog manifest:

```bash
go run ./cmd/catalogctl compare-manifest \
  --expected verification/<name>/<version>/manifest.json \
  --actual verification/<name>/<version>/actual.json
```

Comparison is semantic after strict JSON decoding, but arrays remain ordered and
all fields must agree. Extra fields, missing command booleans, different flag
defaults, identity drift, malformed JSON, non-zero exit, and excess output all
fail validation.

CI does not run `plan` or `execute` from untrusted contribution binaries.
Maintainers assess those behaviors from source, tests, and release provenance.

## Validate the reference example

The reference entry contains placeholder asset data, so validate its metadata
without downloading:

```bash
go run ./cmd/catalogctl validate \
  --plugins examples/minimal-go/catalog
```

Then test the standalone module:

```bash
cd examples/minimal-go
go test -race ./...
go vet ./...
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
  go build -buildvcs=false -trimpath -o example-plugin_linux_amd64 .
```

The root test suite compares the example catalog manifest with
`examples/minimal-go/testdata/manifest.json`. The nested module tests compare
that golden with the manifest returned by its code. This creates one enforced
chain:

```text
plugin implementation → manifest golden → catalog YAML
```

The nested test suite also builds a real executable and runs
`manifest → plan → execute → plan → execute`. The second plan and result must
be an explicit successful no-op.

## GitHub Actions jobs

The pull-request workflow contains two required jobs.

### `test`

Runs:

- root `go test -race ./...`;
- root `go vet ./...`;
- strict production catalog validation;
- standalone example tests and vet;
- static `linux/amd64` example build.

### `verify-assets`

Materializes all production catalog entries, executes each manifest in the
restricted container, and compares it with signed-input metadata.

Branch protection requires both jobs. A maintainer cannot treat a local
successful download as a replacement for protected CI.

## Common failures

### `field ... not found`

The YAML contains an unknown field or wrong nesting. Compare it with
[catalog-entry.md](catalog-entry.md) and the complete
[example entry](../examples/minimal-go/catalog/example-plugin/1.0.0.yaml).

### `... is not a stable semantic version`

Use `X.Y.Z` without `-rc`, `+metadata`, or other prerelease/build components.
Update the executable manifest and filename to the same version.

### `manifest identity or protocol is invalid`

Confirm:

```text
entry name == manifest name
entry version == manifest version
manifest protocol_version == 1
```

Also confirm that the manifest has at least one command.

### `mutating commands must support dry-run`

An `operational` or `runbook` command declares
`supports_dry_run: false`. Implement a side-effect-free plan and set the field
to `true`.

### `asset platform or size is invalid`

Use exactly `linux`/`amd64` and an exact byte size from 1 through 104,857,600.
Do not enter a human-readable value such as `7 MB`.

### `asset SHA-256 is invalid`

Supply 64 hexadecimal characters for the binary asset. Quote the YAML scalar
and do not paste a checksum for an archive or checksum file.

### `asset URL must use credential-free HTTPS`

Use a public `https://` URL with no username, password, token, or temporary
authorization query. Use an immutable release download URL, not `latest`.

### `download size does not match`

The release asset changed, the URL points to an HTML error page, or
`size_bytes` was measured from a different file. Download the public URL in a
clean environment and recompute both size and digest.

### `digest does not match`

The downloaded bytes do not match the reviewed SHA-256. Do not update an
already-published entry to follow replaced bytes. Publish a new plugin version.

### `plugin manifest does not match catalog`

Run the release asset itself:

```bash
chmod +x ./plugin
./plugin manifest --protocol=1
```

Compare every field with `version.manifest`. Common causes are a stale embedded
version, omitted false booleans, reordered commands, a changed description, or
a flag default encoded with the wrong JSON type.

### Sandbox execution fails

The manifest verb must work without root, network, a writable filesystem,
ambient capabilities, shell profile, or repository files. Embed static
manifest data in the binary and reserve external dependencies for planned
execution.

## Publication trust boundary

Contribution CI validates entries and untrusted binaries but cannot sign a
catalog. Signing runs in a separate protected GitHub environment after merge
and maintainer approval. Only that job can read the Ed25519 private seed.

The signing job signs the exact bytes of `index-v1.json`. It never executes
plugin assets. It publishes the index and detached multi-signature envelope as
immutable GitHub Release assets.

Clients verify an accepted key ID, expiration, monotonic sequence, and
same-sequence byte consistency before using a snapshot. Passing a pull-request
workflow does not itself make a plugin installable.

## Pre-PR checklist

- [ ] The source and release are public.
- [ ] The version is stable and matches filename, tag, manifest, and YAML.
- [ ] The asset is a static `linux/amd64` executable.
- [ ] The URL is immutable, public HTTPS, and credential-free.
- [ ] SHA-256 and `size_bytes` were measured from that exact URL.
- [ ] The catalog manifest exactly matches the released executable.
- [ ] Operational/runbook commands plan without side effects and support dry-run.
- [ ] Privilege, force, and confirmation declarations are conservative.
- [ ] Protocol output contains no secrets or human logging on stdout.
- [ ] Root tests, vet, validation, and materialization pass.
