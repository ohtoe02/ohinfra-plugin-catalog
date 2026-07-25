# ohtools plugin catalog

This public repository is the curated source for plugins installable through
`ohtools plugin`. It stores reviewable version entries; clients consume only
Ed25519-signed snapshots published as immutable GitHub Release assets.

The initial catalog is intentionally empty. Plugin publishers add one strict
YAML entry per version under `plugins/<name>/<version>.yaml`.

## Documentation

- [Author a plugin](docs/plugin-authoring.md) — project structure, implementation,
  security, release, and submission workflow.
- [Executable protocol v1](docs/protocol-v1.md) — complete manifest, invocation,
  plan, and Result JSON syntax.
- [Protocol v1 contract bundle](contracts/protocol-v1/README.md) — canonical
  JSON Schemas, conformance vectors, and checksums for vendored validators.
- [Catalog entry reference](docs/catalog-entry.md) — strict YAML fields,
  immutable asset rules, and a complete entry.
- [Release metadata v1](docs/release-metadata-v1.md) — strict sidecar consumed
  by deterministic catalog import.
- [Validation guide](docs/validation.md) — local checks, sandboxed CI, common
  failures, and the signing boundary.
- [Minimal Go plugin](examples/minimal-go/README.md) — a standalone, tested
  project with all important build, release, fixture, and catalog files.

Start by copying the minimal project, then follow the authoring guide. The
example catalog entry remains outside `plugins/` and is never published as a
production entry.

## Validate a contribution

```bash
go test ./...
go run ./cmd/catalogctl validate --plugins plugins
go run ./cmd/catalogctl materialize --plugins plugins --output verification
```

Pull-request CI downloads each declared `linux/amd64` asset, verifies its size
and SHA-256, then executes only its `manifest` verb in a non-root, read-only,
networkless container. Publishing and signing happen in a separate protected
workflow.

See the [validation guide](docs/validation.md) before opening a pull request.

## Published files

- `index-v1.json` — deterministic catalog snapshot.
- `index-v1.json.sig` — one or more detached Ed25519 signatures.

Catalog clients reject expired snapshots, sequence rollback, unknown keys,
malformed metadata and assets whose bytes differ from the signed digest.
