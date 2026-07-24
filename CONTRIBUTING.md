# Contributing a plugin

Read [Author a plugin](docs/plugin-authoring.md), the
[protocol-v1 reference](docs/protocol-v1.md), and the
[catalog entry reference](docs/catalog-entry.md) before submitting metadata.
The [minimal Go project](examples/minimal-go/README.md) contains copyable
examples of all important plugin files.

## Contributor checklist

- [ ] The plugin source repository and release asset are public.
- [ ] The release is a static `linux/amd64` executable with stable `X.Y.Z`
      versioning.
- [ ] `plugins/<name>/<version>.yaml` contains no placeholders.
- [ ] Name and version match the path, release, and protocol manifest.
- [ ] The YAML manifest exactly matches
      `plugin manifest --protocol=1`.
- [ ] The asset URL is immutable credential-free HTTPS.
- [ ] `size_bytes` and SHA-256 describe that exact public binary.
- [ ] Mutating commands implement side-effect-free plans and dry-run support.
- [ ] Root/force/confirmation requirements are conservative.
- [ ] Root tests, vet, strict validation, and materialization pass as described
      in [Validation](docs/validation.md).

Open a focused pull request with the release URL, tag/commit, validation output,
and a short explanation of privileges and state changes. A maintainer must
review and merge it before a signed catalog release can be published.

## Maintainer checklist

- [ ] Review source provenance and the immutable release commit/tag.
- [ ] Check command scope, input validation, external execution, and redaction.
- [ ] Confirm manifest security declarations match implementation behavior.
- [ ] Require both protected checks: `test` and `verify-assets`.
- [ ] Publish from the protected signing environment; never run plugin binaries
      in the signing job.

Changing an existing released entry is prohibited. Publish a new plugin version
or mark an existing version as yanked in a separately reviewed change.
