# eka-mcp

**eka-mcp** is the AI-agent integration layer for EKA. It bundles three things
into one executable:

1. **MCP server** — a Model Context Protocol server exposing the EKA Runtime's
   retrieval capabilities as MCP tools and resources.
2. **EKA AI Skill Pack** — modular skill definitions that teach AI coding
   agents how to work with an EKA-enabled project.
3. **CLI plugin** — an `eka-cli` plugin that installs the skill pack into an
   agent configuration directory.

It is a consumer of [`eka-core`](https://github.com/maleolabs/eka-core) (the
Runtime and the plugin contract) and an `eka-cli` plugin.

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
  "version": "1.0.0",
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

**`eka-mcp configure [--target opencode|claude|codex] [--dir <dir>] [--with-skills] [--with-commands] [--with-all] [--dry-run] --json`** —
per-agent configuration UX (outside the fixed manifest/install contract): writes
the MCP client config entry for the target ecosystem (absolute binary path +
`EKA_HOME` when set). By default it **only** writes the MCP client config — skills
and commands are accessible via MCP resources `eka://skills/*` and
`eka://templates/*` and are **not** installed unless opted in via `--with-skills`,
`--with-commands`, or `--with-all` (`--with-all` = both). `--dry-run` prints the
plan without writing (and reflects only the opted-in installs). Unsupported
`--target` fails deterministically listing the supported targets. The write merges
without overwriting other servers' entries and is idempotent. Default `--target` is
`opencode`; `--dir` defaults to the current working directory (workspace root).

Opted-in installs land in the target's conventional directories under an anchor
(`<base>` = `--dir`, else the user home for opencode/claude and the working
directory for codex): `opencode` → `<base>/.config/opencode/{skills,commands}`,
`claude` → `<base>/.claude/{skills,commands}`, `codex` → `<base>/.agents/skills`
(skills only — codex-cli removed the prompts directory in 0.117.0, so
`--with-commands`/`--with-all` refuse deterministically). Canonical command
bodies stay description-only in the pack; frontmatter is rendered per target at
install time, and the active role→agent delegation table travels alongside as a
non-.md `DELEGATION.txt` sidecar (next to the installed commands for
opencode/claude, inside the skills subtree for codex) so legacy command dirs
never see a phantom `.md` command. Re-installing overwrites only pack-owned
files; foreign files are never touched; symlinked targets refuse.

## The MCP server

Running `eka-mcp serve` (or `eka-mcp --stdio`, or no subcommand at all) starts
the MCP server over stdio (JSON-RPC 2.0, newline-delimited). It reports MCP
protocol version `2024-11-05` and advertises the `tools` and `resources`
capabilities.

The server exposes 13 tools and three resource families over the EKA capability
layer:

| Tool / resource | Description |
|---|---|
| `context` | Build the deterministic Context Object around one subject (schema `eka-context-v1`). |
| `get` | Fetch one Canonical Knowledge Object by identity form (canonical `"<ns>/<type>:<id>:<v>"` or qualified line `"<ns>/<type>:<id>"`), returned as a machine document (schema `eka-cko-v2`). |
| `domain` | Return every unit of one Engineering Domain of a project as a machine collection (schema `eka-cko-v2`, sorted by canonical form). |
| `status` | Return the aggregated EKA workspace status (path, schema version, projects, canonical store totals). When no workspace is present it returns `{"initialized":false,…}` deterministically. |
| `validate` | Run the authoring conformance gate over a repository. |
| `new` | Scaffold one draft (schema `eka-draft-v1`). |
| `publish` | Publish one draft (schema `eka-publish-result-v1`). |
| `transition` | Move a work item along the transition table (schema `eka-transition-result-v1`). |
| `note` | Create one `cmt-` note draft (schema `eka-note-result-v1`). |
| `view` | Return one draft file content verbatim. |
| `draft_list` | List the draft backlog. |
| `integrity_check` | Verify the canonical store. |
| `discard` | Delete one draft without publishing. |
| `eka://status` (resource) | The same workspace status, as a readable resource (`application/json`). |
| `eka://skills/<name>` (resource) | The `SKILL.md` of one embedded skill (read-only). |
| `eka://templates/<type>` (resource) | The v2.0 JSON draft template of one type (read-only). |

The server opens the EKA Runtime **read-only** (`runtime.Open`), so it starts
cleanly even before a workspace exists — `initialize` and `tools/list` always
answer, and retrieval tools report the uninitialized state deterministically
(`status` returns `initialized:false`; `get`/`domain`/`context` return
`workspace not initialized`) instead of crashing or leaking stack traces.

### Hardening contract

The server is hardened to production standard, with a deterministic
conformance suite (`internal/mcp/conformance_test.go`) and fuzz harness
(`internal/mcp/fuzz_test.go`) asserting the contract:

- **Protocol**: `initialize` echoes the client's announced protocol version
  (falling back to the `2024-11-05` baseline when none is announced);
  capabilities advertise exactly `tools` + `resources`.
- **Batch rejection**: a JSON-RPC batch array is refused deterministically
  (`-32600`, fixed message) — the server never processes batches.
- **Bounded read line**: one stdio message line is capped at 64 MiB; an
  oversized line is refused deterministically and the stream resynchronizes
  at the next newline.
- **Error message policy**: every error path returns a fixed, client-safe
  refusal-class message — no Go internals, no file paths, no store details,
  no stack traces.
- **Framing**: newline-delimited responses, flushed after every reply.

### Integration test (real client)

`scripts/integration-opencode.sh` drives a real opencode session against the
server: it builds the binary, registers it as a stdio MCP server in a
throwaway opencode project, and asserts that the session calls the `status`
tool and receives its JSON — proving the handshake, framing and tool dispatch
work end-to-end with a real client. It uses the credential-free
`opencode/deepseek-v4-flash-free` model by default:

```sh
scripts/integration-opencode.sh
```

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

Alongside the pack, the embedded [`mappings/`](mappings) directory ships the
pre-rendered role→agent delegation tables per ecosystem (`opencode.toml` — the
default reference mapping, `claude.toml`, `codex.toml`) of
req:agent-agnostic-skill-pack R4: one closed 9-role vocabulary, declarative
per-ecosystem resolution (`delegate` to a named agent or explicit `solo`
degrade), rendered to plain text for the future DELEGATION.txt sidecar.

## Installation

### As an eka-cli plugin (recommended)

```sh
eka plugin install mcp
eka-mcp configure --target opencode --dir . --json                          # only writes MCP client config (skills/commands via MCP resources)
eka-mcp configure --target opencode --dir . --with-all --json               # also installs skills + commands (.config/opencode/{skills,commands} under <dir>) + DELEGATION.txt
eka-mcp configure --target opencode --dir . --with-skills --json            # only skills, --with-commands for commands only
```

This installs the official `eka-mcp` plugin from its GitHub release with
checksum verification. The `configure` subcommand is the per-agent setup:
it writes the MCP client config entry (absolute `eka-mcp` binary path +
`EKA_HOME` when set) and, only when opted in, delegates the skill/command
install (`--with-skills` / `--with-commands` / `--with-all`). Without those
flags no files are copied — skills and templates remain available via MCP
resources `eka://skills/*` and `eka://templates/*`. Use `--dry-run` to
preview (paths + create|overwrite|skip, nothing written), `--target
opencode|claude|codex` to select the ecosystem (default `opencode`).

### Manual MCP client configuration (without the subcommand)

If you skip `eka-mcp configure`, add the entry for `eka` to your agent's
MCP config by hand. Replace `/absolute/path/to/eka-mcp` with the absolute
path to the built binary (`which eka-mcp` or `go build -o /path/eka-mcp ./cmd/eka-mcp`),
and set `EKA_HOME` only when your workspace lives outside `~/.eka`.

**opencode** — `opencode.json` (`mcp` section, workspace-local `opencode.json` or
`~/.config/opencode/opencode.json`):

```json
{
  "mcp": {
    "eka": {
      "type": "local",
      "command": ["/absolute/path/to/eka-mcp"],
      "enabled": true,
      "environment": {
        "EKA_HOME": "/absolute/path/to/workspace-home"
      }
    }
  }
}
```

Omit `environment.EKA_HOME` when `EKA_HOME` is unset (the server then uses
`~/.eka`).

**Claude Code** — `~/.claude.json` (`mcpServers` section):

```json
{
  "mcpServers": {
    "eka": {
      "command": "/absolute/path/to/eka-mcp",
      "args": [],
      "env": {
        "EKA_HOME": "/absolute/path/to/workspace-home"
      }
    }
  }
}
```

**Codex** — `~/.codex` (or `~/.codex/config.json` when `~/.codex` is a directory,
`mcpServers` section):

```json
{
  "mcpServers": {
    "eka": {
      "command": "/absolute/path/to/eka-mcp",
      "args": [],
      "env": {
        "EKA_HOME": "/absolute/path/to/workspace-home"
      }
    }
  }
}
```

All three merges preserve other servers' entries; re-running `eka-mcp
configure` is idempotent. Verify with `--dry-run --json`.

### Standalone binary

Build from source:

```sh
go build ./cmd/eka-mcp
```

Then run `./eka-mcp serve` as your MCP client's stdio server, or use
`./eka-mcp manifest --json` / `./eka-mcp install <kind> --dir <dir> --json`
/ `./eka-mcp configure --target <target> --dir <dir> --json` directly.

## Versioning

- **Semantic versioning**, tag-driven. The single version constant
  (`pack.Version`, currently `1.0.0`) is reported in the plugin manifest, the
  `install` result, and the MCP `serverInfo` — one version across all three
  roles, so they never drift.

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md). Design records and ADRs live in the EKA
knowledge system, not in this repository.

## License

Apache License 2.0.
