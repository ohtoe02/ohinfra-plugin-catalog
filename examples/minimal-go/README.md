# Minimal Go plugin

This directory is a complete, standalone `ohinfra` protocol-v1 plugin project.
It intentionally uses only the Go standard library: copy the directory into a
new repository, replace `github.com/example/ohinfra-example-plugin` in
`go.mod`, and rename the plugin and commands.

The example registers:

- `ohinfra example echo <text> [--uppercase]`, a read-only diagnostic command;
- `ohinfra example write <text>`, an operational command that plans and then
  atomically writes `/tmp/ohinfra-example/message.txt`.

The write command demonstrates the protocol flow and should not be treated as a
recommended location for real plugin state.

## Project files

- `main.go` — wire types and `manifest`, `plan`, and `execute` handlers.
- `main_test.go` — protocol contract and security tests.
- `testdata/invocation.json` — a host Invocation v1 fixture.
- `testdata/manifest.json` — the manifest golden shared with catalog checks.
- `Makefile` — local test, vet, static build, and manifest targets.
- `.goreleaser.yaml` — raw `linux/amd64` binary and checksum release.
- `.github/workflows/release.yml` — copyable tag-triggered release workflow.
- `catalog/example-plugin/1.0.0.yaml` — non-production catalog entry template.

## Test and inspect the protocol

```bash
go test ./...
go vet ./...
go run . manifest --protocol=1
go run . execute --protocol=1 < testdata/invocation.json
```

The plugin writes exactly one JSON document to stdout. Diagnostics and usage
errors go to stderr.

To inspect the operational plan:

```bash
printf '%s\n' \
  '{"protocol_version":1,"request_id":"demo","command_path":["example","write"],"arguments":["hello"],"options":{}}' |
  go run . plan --protocol=1
```

An operational `execute` request must contain the SHA-256 digest of the exact
JSON plan approved by the host. Normal users should exercise that flow through
`ohinfra`, which computes and supplies the digest.

## Build the catalog asset

```bash
make test
make build VERSION=1.0.0
sha256sum dist/example-plugin_linux_amd64
stat --format=%s dist/example-plugin_linux_amd64
```

The build is static and targets only `linux/amd64`, the platform supported by
catalog protocol v1.

## Publish a release

1. Copy this example into its own public GitHub repository.
2. Replace the module path, plugin identity, command metadata, and README.
3. Copy the nested release workflow to the repository's root
   `.github/workflows/release.yml`.
4. Keep the release workflow least-privileged and enable protected tags or a
   protected release environment for the repository.
5. Tag the exact reviewed commit with a stable version such as `v1.0.0`.
6. Confirm that the release contains
   `example-plugin_linux_amd64` and `checksums.txt`.

GitHub Release assets used by the catalog are immutable inputs. Do not replace
an asset after proposing its digest. Publish a new stable version instead.

## Prepare the catalog entry

Copy `catalog/example-plugin/1.0.0.yaml` to
`plugins/example-plugin/1.0.0.yaml` in a catalog checkout, then replace:

- `YOUR-ORG` and all project metadata;
- `published_at` with the release timestamp;
- `url` with the immutable GitHub Release download URL;
- `sha256` with the lowercase 64-character digest;
- `size_bytes` with the exact byte count;
- `manifest` with the exact output of `manifest --protocol=1`.

Follow the repository [authoring guide](../../docs/plugin-authoring.md) and
[validation guide](../../docs/validation.md) before opening a pull request.
