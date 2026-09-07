---
description: Build and publish shr sharing objects — EKA-to-EKA and audited non-EKA spike, reusable for agents. Opt-in L0/L1/L2, batch --levels, server-side filtering, and publish flow.
---

# EKA Shr Build

Build **shr sharing objects** — reusable snapshot copies pinned by `sourceHash` + `level` + `provenance` — for both EKA and non-EKA codebases. This command is a **reusable prompt** for agents: when to build, which flags to use, and how to publish so objects can be scanned via MCP without the source repo.

## When to build

- **EKA-to-EKA**: source is a qualified CKO (`eka/<type>:<id>`) present in the workspace (`eka get <source>` succeeds). Examples: ADR, SCP, REQ.
- **Non-EKA spike**: source is a filesystem path to a regular codebase (not EKA). Use `--provenance audited`.

## Primitives (CLI ↔ MCP)

| Primitive | CLI | MCP |
|---|---|---|
| get (identity/domain) | `eka get <form> [--level L0|L1|L2]` | `get` / `domain` |
| shr build | `eka shr build <source> --level L0 --provenance extracted|audited` | — (via CLI) |
| publish | `eka publish <ns>/shr:<id>` | `publish` |
| status/sync | `eka status`, `eka sync push` | `status` |

## Primary flags

- `--level L0|L1|L2` — opt-in, required for single build
- `--levels L0,L1,L2` — batch 1–3 shr at once (deduped, mutually exclusive with `--level`)
- `--provenance extracted` (default, EKA) | `audited` (non-EKA spike, source is directory path)
- `--id <shr-id>` — bare id, normalized to lowercase-hyphen (default `share-<source-id>-<level>` or `share-<basename>-<level>` for audited)
- `--title` / `--description` — default derived from source + short hash

Levels: `L0` metadata only, `L1` + safe summary/structure, `L2` + full snapshot (guarded 1 MiB).

Hardening: IDs normalized, L1/L2 dedup via `buildCommonShrFields`, snapshot guard, collision exits `1` (fail) not `2` (usage), `domainTokens` via registry + alias `records → Operations`.

## Reusable flow (copy-paste for agents)

### EKA-to-EKA
```sh
eka status && eka sync
eka get eka/adr:sharing-object-model   # verify source exists
eka shr build eka/adr:sharing-object-model --level L0 --id my-share
eka shr build eka/adr:sharing-object-model --levels L0,L1,L2
eka publish eka/shr:my-share-l0
eka get eka/shr:my-share-l0 --level L0        # strict filter
eka get operations --level L0 --type shr      # scan without source repo
```

### Non-EKA audited (spike)
```sh
eka shr build /path/to/codebase --level L0 --provenance audited --id share-codebase-l0
eka shr build /path/to/codebase --levels L0,L1 --provenance audited
eka publish eka/shr:share-codebase-l0
eka get operations --level L0                # agent scans title/description via content fields
```

## Server-side filtering (MCP-friendly)

```sh
eka get operations --level L0
eka get records --level L0          # alias records → Operations
eka get eka/shr:my-share-l0 --level L0
```

MCP `get`/`domain` expose the same `--level` filter — agents can scan `shr` without cloning the source repo (use `get operations --level L0 --no-content` to save tokens, then `get` identity for `title`/`description`/`level`/`provenance`).

## Skill to load

Load the `eka-shr-builder` skill for full guidance (level semantics, batch, guard):
- Install: `eka-mcp configure --with-skills` or `eka-mcp configure --target opencode|claude|codex --with-skills`
- MCP resource: `eka://skills/eka-shr-builder`

## Validation before publish

```sh
eka get eka/shr:<id> --level L0
eka validate  # optional, R0-R13
```

If `--level` mismatches → `eka: get: ... level "Lx" does not match filter --level "Ly"` (exit 2). Collision on `shr build` with existing id → `exit 1`.
