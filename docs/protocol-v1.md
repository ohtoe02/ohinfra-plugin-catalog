# Executable plugin protocol v1

This document is the wire reference for executables loaded by `ohtools` 0.2.
The protocol is language-neutral. A plugin is one executable that accepts a
protocol verb and exchanges one JSON document with the host.

For an end-to-end project, see the
[minimal Go plugin](../examples/minimal-go/README.md).

## Process contract

The host invokes these exact forms without a shell:

```text
/usr/lib/ohtools/plugins/<file> manifest --protocol=1
/usr/lib/ohtools/plugins/<file> plan --protocol=1
/usr/lib/ohtools/plugins/<file> execute --protocol=1
```

The contract for every invocation is:

- stdout contains exactly one JSON document and no human-readable logging;
- diagnostics go to stderr;
- success exits with code `0`; non-zero means the verb failed;
- `manifest` receives no stdin;
- `plan` and `execute` receive one Invocation v1 document on stdin;
- JSON is UTF-8, field names are case-sensitive, and unknown, duplicate, or
  trailing data is rejected by the host;
- the host supplies a deadline, bounds runtime and output, and may terminate the
  complete plugin process group;
- the plugin must not invoke `sudo` or assume an interactive terminal.

Current host limits are 1 MiB for a manifest, 10 MiB for a plan or result,
1 MiB for stderr, and 128 registered commands per plugin. Treat these as hard
upper bounds, not output targets.

The host applies policy, privilege checks, confirmation, `--force`,
`--dry-run`, timeout, redaction, output rendering, and audit. Plugins implement
domain-specific planning and execution; they must not attempt to bypass the
host lifecycle.

## Manifest

`manifest --protocol=1` returns the plugin identity and all commands registered
under the `ohtools` command tree.

```json
{
  "protocol_version": 1,
  "name": "example-plugin",
  "version": "1.0.0",
  "description": "Minimal reference implementation for ohtools plugin protocol v1",
  "commands": [
    {
      "path": ["example", "echo"],
      "use": "echo <text>",
      "short": "Echo text through plugin protocol v1",
      "category": "diagnostic",
      "arguments": [
        {
          "name": "text",
          "description": "Text to echo",
          "required": true,
          "variadic": false
        }
      ],
      "flags": [
        {
          "name": "uppercase",
          "type": "bool",
          "description": "Convert the echoed text to uppercase",
          "default": false
        }
      ],
      "requires_root": false,
      "requires_force": false,
      "supports_dry_run": false,
      "requires_confirmation": false
    }
  ]
}
```

### Manifest fields

| Field | Type | Rules |
| --- | --- | --- |
| `protocol_version` | integer | Must be `1`. |
| `name` | string | Must match `^[a-z][a-z0-9-]*$`. |
| `version` | string | Must be non-empty. Catalog entries additionally require stable `X.Y.Z`. |
| `description` | string | Optional concise plugin description. |
| `commands` | array | Required, 1–128 commands. Command paths must be unique. |

The filename does not define plugin identity. `name` does. Two installed
executables cannot declare the same name.

### Command fields

| Field | Type | Rules |
| --- | --- | --- |
| `path` | string array | At least two identifier segments. Registers `ohtools <segment> ...`. |
| `use` | string | Cobra-style usage for the final command, for example `echo <text>`. |
| `short` | string | One-line help text. |
| `category` | string | `diagnostic`, `operational`, or `runbook`. |
| `arguments` | array | Ordered positional argument declarations, maximum 64. |
| `flags` | array | Command-local flag declarations, maximum 64. |
| `requires_root` | boolean | Host rejects execution before planning or mutation when privileges are insufficient. |
| `requires_force` | boolean | Host requires the global `--force` control. |
| `supports_dry_run` | boolean | Must be `true` for `operational` and `runbook`. |
| `requires_confirmation` | boolean | Host requires TTY approval or global `--yes`. |

Every command path segment, argument name, and flag name must match
`^[a-z][a-z0-9-]*$`. A command cannot collide with a built-in command or a
command registered by another plugin. The entire plugin is rejected on any
collision or invalid command.

Categories define lifecycle behavior:

- `diagnostic` is read-only and the host calls `execute` directly;
- `operational` changes system state and must implement `plan`;
- `runbook` is also mutating and must implement `plan`.

An argument has `name`, optional `description`, `required`, and `variadic`.
Required arguments must precede optional arguments. A variadic argument must be
last.

A flag has `name`, `type`, optional `description`, and an optional typed
`default`. Supported types are:

- `string` with a JSON string default;
- `bool` with a JSON boolean default;
- `int` with an integral JSON number default;
- `duration` with a Go duration string such as `"30s"` or `"5m"`.

These global host flag names are reserved and cannot be declared by a plugin:

```text
config json no-color quiet verbose debug dry-run yes force timeout output
version help retry-request-id
```

## Invocation

The host sends the same Invocation shape to `plan` and `execute`:

```json
{
  "protocol_version": 1,
  "request_id": "1753358400000000000",
  "command_path": ["example", "echo"],
  "arguments": ["hello"],
  "options": {
    "uppercase": true
  },
  "deadline": "2026-07-24T12:01:00Z",
  "plan_digest": "f8c98c..."
}
```

| Field | Type | Meaning |
| --- | --- | --- |
| `protocol_version` | integer | Always `1`. |
| `request_id` | string | Opaque identifier for correlation. Do not interpret it. |
| `command_path` | string array | Exact manifest path selected by the user. |
| `arguments` | string array | Validated positional values in declaration order. |
| `options` | object | Every declared command flag with its typed value. |
| `deadline` | RFC 3339 string | Absolute host deadline when one is active. |
| `plan_digest` | lowercase hex string | Present for mutating `execute`; omitted for `plan` and diagnostic execution. |

Do not read command inputs from environment variables or raw process arguments.
Use only the Invocation. Validate the command path, argument count, option types,
and domain-specific values again before acting.

The complete fixture is
[`testdata/invocation.json`](../examples/minimal-go/testdata/invocation.json).

## Operation plan

`plan --protocol=1` is required for `operational` and `runbook` commands. It
must not change system state.

```json
{
  "command_id": "example.write",
  "summary": "Write the example message file",
  "checks": [],
  "changes": [
    {
      "object": "/tmp/ohtools-example/message.txt",
      "action": "write",
      "status": "planned",
      "details": {
        "mode": "0600"
      }
    }
  ],
  "risks": [
    "Replaces the previous example message"
  ],
  "requires_root": false,
  "requires_force": false,
  "requires_confirmation": true
}
```

`command_id` is the command path joined with dots, while Result `command` uses
spaces. For example:

```text
path:       ["example", "write"]
command_id: "example.write"
command:    "example write"
```

The host merges manifest security requirements into the plan, renders it for
dry-run or confirmation, serializes the approved plan as JSON, and computes its
SHA-256. The `execute` Invocation carries that lowercase hexadecimal digest.
Canonical digest byte fixtures are published in
[`contracts/protocol-v1/conformance/plan-digest.json`](../contracts/protocol-v1/conformance/plan-digest.json).

A plugin that verifies the digest must recreate the exact effective plan. The
safest pattern is to return the same `requires_root`, `requires_force`, and
`requires_confirmation` values from both the manifest and plan, keep plan array
ordering deterministic, and calculate the digest over the same Plan field
shape. The [Go example](../examples/minimal-go/main.go) demonstrates this.

## Result v1

`execute --protocol=1` returns the same canonical Result v1 schema as built-in
commands:

```json
{
  "schema_version": "1.0",
  "command": "example echo",
  "status": "pass",
  "timestamp": "2026-07-24T12:00:00Z",
  "duration_ms": 0,
  "host": "",
  "tool": {
    "name": "example-plugin",
    "version": "1.0.0",
    "commit": "",
    "build_date": "",
    "go_version": "",
    "architecture": ""
  },
  "checks": [],
  "data": {
    "echo": "HELLO"
  },
  "changes": [],
  "errors": []
}
```

Every top-level field is required. In particular, `checks`, `changes`, and
`errors` must be JSON arrays even when empty, and `data` must be a JSON object.
Do not emit `null` for canonical collections.

Allowed status values for the Result and each Check are:

```text
pass info warning critical skipped partial error cancelled
```

A Check contains:

- `id`: stable machine identifier;
- `status`: one allowed status;
- `summary`: concise operator-facing result;
- optional `details`: structured JSON object.

A Change contains:

- `object`: resource being changed;
- `action`: requested action;
- `status`: plugin-defined progress such as `planned`, `applied`, or `verified`;
- optional `details`: non-secret structured metadata.

A StructuredError contains:

- `kind`: `general`, `invalid_arguments`, `insufficient_privileges`,
  `missing_dependency`, `cancelled`, `timeout`, or `configuration`;
- optional stable `code`;
- human-readable `message`;
- optional `dependency`;
- `retryable` boolean;
- optional structured `details`.

Never include credentials, tokens, private keys, raw authorization headers, or
unredacted command output. The host applies recursive redaction, but the plugin
remains responsible for minimizing sensitive data before it crosses the process
boundary.

The full manifest golden is
[`testdata/manifest.json`](../examples/minimal-go/testdata/manifest.json), and
the Result-producing implementation is in
[`main.go`](../examples/minimal-go/main.go).

## Compatibility rules

Protocol v1 does not provide an exported Go SDK. Stable interfaces are the
executable argv, JSON documents in this guide, Result schema v1, and catalog
YAML. Vendor local wire structs or generate them from the published JSON
schemas in the
[`contracts/protocol-v1`](../contracts/protocol-v1/README.md) bundle; do not
import `ohtools/internal` packages. A vendored bundle must verify
`SHA256SUMS` and record its source catalog commit.

Adding an unknown field is not backward compatible with strict v1 readers.
Protocol evolution therefore requires a new negotiated protocol version.
