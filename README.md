# eka-mcp

**eka-mcp** is the AI-agent integration layer for EKA. It bundles three things
into one executable:

1. **MCP server** — a Model Context Protocol server exposing the EKA Runtime's
   retrieval capabilities as MCP tools and resources.
2. **EKA AI Skill Pack** — modular skill definitions that teach AI coding
   agents how to work with an EKA-enabled project.
3. **CLI plugin** — an `eka-cli` plugin that installs the skill pack into an
   agent configuration directory.

It is a consumer of both [`eka-core`](https://github.com/maleolabs/eka-core)
(the Runtime) and [`eka-cli`](https://github.com/maleolabs/eka-cli) (the plugin
contract).

Module path: `github.com/maleolabs/eka-mcp`

## Layering

The MCP server is layered so that transport and dispatch stay stdlib-only and
the EKA semantics stay in eka-core:

```
MCP Transport (stdio)    internal/mcp/transport.go   newline-delimited JSON-RPC
MCP Server (dispatch)    internal/mcp/server.go      JSON-RPC 2.0 request handling
EKA capability layer     internal/eka                wraps eka-core behind the
                                                     Capability interface
eka-core Runtime         github.com/maleolabs/eka-core
```

The capability layer deliberately reimplements **no** EKA domain logic — every
call delegates to eka-core (the Runtime services and the `machine/`
projections).

## The plugin

eka-mcp implements the eka-cli plugin contract (v1). It is an executable named
`eka-mcp` that answers two machine-readable subcommands:

**`eka-mcp manifest --json`** — print the plugin self-description. The
`Manifest` (`contract: "v1"`, `name: "mcp"`, `version`, `description`,
`artifacts`, plus the extended-contract `capabilities` and `source`) is
derived from the embedded skill pack, so the manifest always reflects what the
installer can actually install:

```json
{
  "contract": "v1",
  "name": "mcp",
  "version": "0.1.0",
  "description": "EKA MCP — the AI-agent integration layer: an MCP server over the EKA Runtime plus the EKA AI Skill Pack installer",
  "artifacts": [
    { "kind": "skills", "entries": ["eka-router", "eka-orientation", "…"] },
    { "kind": "commands", "entries": ["eka-discuss.md", "eka-execute.md"] }
  ],
  "capabilities": ["install", "mcp"],
  "source": "github.com/maleolabs/eka-mcp"
}
```

**`eka-mcp install <kind> --dir <dir> [--dry-run] --json`** — install one
artifact family into an agent configuration directory and print the result.
Supported kinds: `skills` (each `eka-*` directory installed as a subtree) and
`commands` (each command file installed as a single file). `--dry-run` reports
the plan without touching the filesystem.

## The MCP server

Running `eka-mcp serve` (or `eka-mcp --stdio`, or no subcommand at all) starts
the MCP server over stdio (JSON-RPC 2.0, newline-delimited). It reports MCP
protocol version `2024-11-05` and advertises the `tools` and `resources`
capabilities.

The server exposes three tools and one resource over the EKA capability layer:

| Tool / resource | Description |
|---|---|
| `get` | Fetch one Canonical Knowledge Object by identity form (canonical `"<ns>/<type>:<id>:<v>"` or qualified line `"<ns>/<type>:<id>"`), returned as a machine document (schema `eka-cko-v2`). |
| `domain` | Return every unit of one Engineering Domain of a project as a machine collection (schema `eka-cko-v2`, sorted by canonical form). |
| `status` | Return the aggregated EKA workspace status (path, schema version, projects, canonical store totals). |
| `eka://status` (resource) | The same workspace status, as a readable resource (`application/json`). |

The server opens the EKA Runtime **read-only** (`runtime.Open`), so it starts
cleanly even before a workspace exists — retrieval then reports the
uninitialized state instead of failing.

## The skill pack

The embedded [`skills/`](skills) tree is the **EKA AI Skill Pack** — see
[`skills/README.md`](skills/README.md) for the full index. It contains the
`eka-*` skills and the user-invoked commands:

| Skill | Teaches |
|---|---|
| `eka-router` | The routing decision tree — pick the matching skill. |
| `eka-orientation` | The EKA mental model before any command. |
| `eka-project-understanding` | Project context, context-first workflow. |
| `eka-knowledge-retrieval` | `eka get` / `eka context` / `eka view`. |
| `eka-knowledge-authoring` | Draft → validate → publish; the JSON draft templates. |
| `eka-knowledge-modification` | Immutability, revisions, transitions. |
| `eka-knowledge-review` | Validation and constraint checking. |
| `eka-adoption` | Bringing non-EKA projects into EKA. |
| `eka-engineering-workflow` | EKA and software development. |
| `eka-troubleshooting` | Refusal classes, exit codes, fixes. |
| `eka-feedback` | Reporting EKA shortcomings (ADR-026). |

Commands: `eka-discuss` (planning discussion) and `eka-execute` (autonomous
execution + resume). The pack also ships reference draft templates for all 26
token families and a smoke test.

## Installation

### As an eka-cli plugin (recommended)

```sh
eka plugin install mcp
```

This installs the official `eka-mcp` plugin from its GitHub release with
checksum verification, then delegates the skill-pack installation
(`eka-mcp install skills` / `eka-mcp install commands`) into your agent
configuration directory.

### Standalone binary

Build from source:

```sh
go build ./cmd/eka-mcp
```

Then run `./eka-mcp serve` as your MCP client's stdio server, or use
`./eka-mcp manifest --json` / `./eka-mcp install <kind> --dir <dir> --json`
directly.

## Versioning

- **Semantic versioning**, tag-driven. The single version constant
  (`pack.Version`, currently `0.1.0`) is reported in the plugin manifest, the
  `install` result, and the MCP `serverInfo` — one version across all three
  roles, so they never drift.

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md). Design records and ADRs live in the EKA
knowledge system, not in this repository.

## License

Apache License 2.0.
