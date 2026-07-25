# Release metadata v1

`release-metadata-v1.json` is an unsigned build output transported alongside an
immutable plugin binary. It removes hand-copying from the catalog contribution
workflow. Catalog signing still covers only the existing catalog index v1.

The document is strict JSON with exactly these fields:

```json
{
  "schema_version": "1",
  "name": "example-plugin",
  "description": "Minimal reference implementation",
  "homepage": "https://github.com/example/example-plugin",
  "version": "1.0.0",
  "minimum_ohtools_version": "0.3.3",
  "published_at": "2026-07-25T10:00:00Z",
  "asset": {
    "os": "linux",
    "arch": "amd64",
    "url": "https://github.com/example/example-plugin/releases/download/v1.0.0/example-plugin_linux_amd64",
    "sha256": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
    "size_bytes": 1234567
  },
  "manifest": {
    "protocol_version": 1,
    "name": "example-plugin",
    "version": "1.0.0",
    "description": "Minimal reference implementation",
    "commands": [
      {
        "path": ["example", "status"],
        "use": "status",
        "short": "Show example status",
        "category": "diagnostic",
        "arguments": [],
        "flags": [],
        "requires_root": false,
        "requires_force": false,
        "supports_dry_run": false,
        "requires_confirmation": false
      }
    ]
  }
}
```

`description` and `homepage` are optional. All other fields are required.
The manifest must be the exact object returned by the release binary. Name,
version, and description must agree across sidecar, manifest, and catalog.

`asset` uses existing catalog asset field names. Version 1 accepts only
`linux/amd64`, a credential-free immutable HTTPS URL, lowercase SHA-256, and an
exact byte size from 1 through 100 MiB.

Import with:

```bash
go run ./cmd/catalogctl import-release \
  --metadata release-metadata-v1.json \
  --binary example-plugin_linux_amd64 \
  --plugins plugins
```

The importer reads regular non-symlink files, validates the exact binary, and
creates `plugins/<name>/<version>.yaml`. It never replaces an existing entry.
Review, materialization, sandboxed manifest comparison, and protected signing
remain required.
