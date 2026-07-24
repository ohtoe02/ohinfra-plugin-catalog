# ohinfra plugin catalog

This public repository is the curated source for plugins installable through
`ohinfra plugin`. It stores reviewable version entries; clients consume only
Ed25519-signed snapshots published as immutable GitHub Release assets.

The initial catalog is intentionally empty. Plugin publishers add one strict
YAML entry per version under `plugins/<name>/<version>.yaml`.

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

## Published files

- `index-v1.json` — deterministic catalog snapshot.
- `index-v1.json.sig` — one or more detached Ed25519 signatures.

Catalog clients reject expired snapshots, sequence rollback, unknown keys,
malformed metadata and assets whose bytes differ from the signed digest.
