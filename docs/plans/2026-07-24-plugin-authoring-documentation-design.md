# Plugin authoring documentation design

**Date:** 2026-07-24  
**Status:** Approved

## Goal

Give plugin authors one complete, reviewable path from an empty Go project to a
catalog contribution that passes the repository's local and GitHub validation.
The documentation must describe the protocol as implemented by `ohinfra`, not
introduce a separate SDK or an unofficial abstraction.

## Audience

The primary reader is an infrastructure engineer who can build a static Go
binary but has not implemented an `ohinfra` plugin before. Catalog maintainers
are the secondary audience; the validation guide should help them explain a
failed contribution without reading `catalogctl` source first.

## Information architecture

Keep the repository landing page short and route readers into four focused
guides:

- `docs/plugin-authoring.md` — end-to-end author workflow and security model.
- `docs/protocol-v1.md` — executable verbs and complete JSON wire shapes.
- `docs/catalog-entry.md` — strict YAML entry reference, release asset rules,
  and a field-by-field annotated example.
- `docs/validation.md` — local checks, CI stages, failure interpretation, and
  the maintainer review boundary.

`README.md` remains the catalog overview. `CONTRIBUTING.md` becomes a concise
submission checklist with links to the detailed guides.

## Runnable example

Add `examples/minimal-go/` as a standalone Go module so authors can copy it
without importing `ohinfra/internal` packages. The example includes:

```text
examples/minimal-go/
├── .github/workflows/release.yml
├── catalog/example-plugin/1.0.0.yaml
├── testdata/invocation.json
├── .goreleaser.yaml
├── go.mod
├── main.go
├── main_test.go
├── Makefile
└── README.md
```

The binary implements `manifest`, `plan`, and `execute` with only the Go
standard library. It exposes a diagnostic echo command and a safe operational
file command constrained to `/tmp/ohinfra-example/`. This demonstrates command
metadata, typed flags, strict input, canonical Result v1, planning, dry-run
support, and plan-digest binding without making the example depend on the host
source tree.

The catalog YAML lives below the example rather than `plugins/`, so it can be
validated and copied but can never be published as a real catalog entry by
accident.

## Validation strategy

Repository tests enforce that the documentation set and important example files
remain present and linked. The example has contract tests for all three protocol
verbs, strict invocation parsing, plan digest verification, and path
confinement. Root CI runs the example module tests and builds a static
`linux/amd64` binary.

The example's release workflow illustrates immutable GitHub Release assets and
checksum production. It is explicitly a template: copying it into a plugin
repository is required before it can run there.

Catalog contribution validation remains authoritative:

1. strict YAML load and schema/domain checks;
2. asset download with size and SHA-256 verification;
3. sandboxed `manifest --protocol=1`;
4. exact comparison between returned manifest and the catalog entry.

## Maintenance and source of truth

The executable protocol implementation in `ohinfra` and the catalog validation
code remain the source of truth. Documentation names limits and invariants that
are enforced today, and avoids promising an exported Go SDK. Any protocol or
catalog schema change must update the guides, the example, and their contract
tests in the same pull request.

