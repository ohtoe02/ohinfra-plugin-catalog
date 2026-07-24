# Catalog entry reference

Catalog entries are strict, reviewable YAML source files. Clients never consume
these files directly. Maintainers build them into a deterministic
`catalog-index-v1` JSON snapshot and sign the original index bytes with
Ed25519.

Use this path:

```text
plugins/<name>/<version>.yaml
```

For example:

```text
plugins/postgres-tools/1.2.0.yaml
```

The complete non-production template is
[`examples/minimal-go/catalog/example-plugin/1.0.0.yaml`](../examples/minimal-go/catalog/example-plugin/1.0.0.yaml).
It deliberately remains outside `plugins/`.

## Complete example

```yaml
name: postgres-tools
description: PostgreSQL server diagnostics and controlled maintenance
homepage: https://github.com/example/postgres-tools
version:
  version: 1.2.0
  minimum_ohinfra_version: 0.2.0
  published_at: 2026-07-24T12:00:00Z
  yanked: false
  manifest:
    protocol_version: 1
    name: postgres-tools
    version: 1.2.0
    description: PostgreSQL server diagnostics and controlled maintenance
    commands:
      - path: [postgres, connections]
        use: connections
        short: Show PostgreSQL connection usage
        category: diagnostic
        arguments: []
        flags:
          - name: database
            type: string
            description: Database to inspect
            default: postgres
        requires_root: false
        requires_force: false
        supports_dry_run: false
        requires_confirmation: false
  assets:
    - os: linux
      arch: amd64
      url: https://github.com/example/postgres-tools/releases/download/v1.2.0/postgres-tools_linux_amd64
      sha256: "4f5c3f0f2d9e8718863c56330e69c5101e2b1ef90e95cf3b037b3583dcfef357"
      size_bytes: 7340032
```

The example digest is illustrative. A contribution must use the digest and size
of its actual public release asset.

## Top-level fields

### `name`

Required plugin identifier. It must match:

```text
^[a-z][a-z0-9-]*$
```

It must equal `version.manifest.name` and remain identical across every version
of the plugin.

### `description`

Required concise catalog description. Keep it stable across version entry
files. The deterministic builder rejects versions of the same plugin whose
top-level descriptions differ.

### `homepage`

Optional public project URL. Use the source repository or maintained project
documentation, preferably credential-free HTTPS. Keep it stable across version
entries.

### `version`

Required object containing one release, its expected manifest, and platform
assets.

## Version fields

### `version`

Required stable semantic version in `X.Y.Z` form, without prerelease or build
metadata. It must match:

- the filename;
- the GitHub Release/tag semantics;
- the executable manifest version.

The catalog does not accept prerelease versions or two entries with the same
plugin/version identity.

### `minimum_ohinfra_version`

Required stable `X.Y.Z` version of the oldest compatible host. Be conservative:
set this to the first released `ohinfra` version whose protocol and host
behavior the plugin has tested.

The client excludes versions newer than the running host can support.

### `published_at`

Required RFC 3339 release timestamp:

```yaml
published_at: 2026-07-24T12:00:00Z
```

Use the public release time in UTC. Do not use the catalog PR creation time.

### `yanked`

Required boolean. Use `false` for normal releases. A yanked version remains in
catalog history but is excluded from default latest-version selection.

Yanking is the only normal reason to modify release-selection metadata for an
existing entry. Submit it as a focused, reviewed change and explain why.

### `manifest`

Required exact protocol-v1 manifest expected from the downloaded asset:

```text
<asset> manifest --protocol=1
```

Identity must satisfy:

```text
manifest.protocol_version == 1
manifest.name == entry.name
manifest.version == version.version
```

Copy every command, argument, flag, default, and security boolean. Empty
`arguments` and `flags` must be explicit arrays. Pull-request CI executes the
binary in a sandbox and compares the complete returned manifest with this
object. A description or default-value difference fails the contribution.

See [protocol-v1.md](protocol-v1.md#manifest) for every manifest field and
validation rule.

### `assets`

Required non-empty array. Protocol/catalog v1 accepts one asset per platform
and currently supports only:

```yaml
os: linux
arch: amd64
```

Duplicate platform entries are rejected.

## Asset fields

### `os` and `arch`

Required platform identifiers. Use `linux` and `amd64`.

### `url`

Required credential-free HTTPS URL to an immutable public GitHub Release asset.
Do not include user information, tokens, signed temporary query parameters, or
an HTTP URL.

Publication validation pins approved GitHub hosts and allows at most five
redirects. A redirect to another host is rejected. The fetcher does not use
publisher credentials or proxy environment variables.

Prefer the stable form:

```text
https://github.com/<owner>/<repo>/releases/download/vX.Y.Z/<asset>
```

Do not use `/releases/latest/` for plugin assets because it is mutable.

### `sha256`

Required lowercase hexadecimal SHA-256 of the exact asset bytes, 64
characters:

```bash
sha256sum example-plugin_linux_amd64
```

Do not use the checksum file's digest as the asset digest. Use the line for the
plugin binary itself.

### `size_bytes`

Required exact decimal byte count:

```bash
stat --format=%s example-plugin_linux_amd64
```

The accepted range is 1 byte through 100 MiB. Downloads are streamed and fail
on early EOF, extra bytes, an incorrect digest, or the configured bound.

## YAML syntax rules

Catalog loading is strict:

- one YAML document per file;
- no unknown fields;
- no duplicate mapping keys;
- correct scalar types (`false` is a boolean, not `"false"`);
- stable identity between path, entry, version, and manifest;
- explicit arrays for command arguments, flags, and assets.

Quote SHA-256 values. Quote other values when YAML could reinterpret them.
Timestamps should use RFC 3339 with a UTC `Z`.
Avoid anchors and aliases even when a YAML parser accepts them; repeated
security-sensitive values should remain directly reviewable.

The generated index sorts plugin names and versions deterministically; authors
should still keep commands, flags, arguments, and assets in stable logical
order because manifest comparison and plan review are order-sensitive.

## Immutability policy

After a catalog snapshot includes a release:

- do not replace its GitHub asset;
- do not change its URL, size, digest, or manifest;
- do not reuse its version for different bytes;
- do not silently widen privileges or command scope.

Publish a new stable version. For an unsafe release, separately propose
`yanked: true`.

## Validate an entry

From the catalog repository:

```bash
go run ./cmd/catalogctl validate --plugins plugins
go run ./cmd/catalogctl materialize --plugins plugins --output verification
```

The first command performs strict parsing and domain validation. The second
downloads and verifies assets. GitHub CI then runs each materialized manifest
verb in a constrained container and compares the actual JSON with the expected
manifest.

See [validation.md](validation.md) for the full workflow and error guide.
