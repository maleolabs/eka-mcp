---
name: shr-builder
description: EKA sharing object (shr) builder for agents — EKA-to-EKA and audited non-EKA spike
---

# Shr Builder Skill (EKA)

Use this skill when an agent needs to build, publish, and share EKA knowledge as `shr` sharing objects.

## When to use
- EKA-to-EKA: source is a qualified CKO (`<ns>/<type>:<id>`) in the workspace.
- Non-EKA spike: source is a filesystem codebase path to audit (provenance audited).

## Commands (via `eka` CLI — also exposed via MCP `run` when available)

### EKA-to-EKA (provenance extracted, default)
```sh
eka shr build eka/adr:sharing-object-model --level L0 --id my-share
eka shr build eka/scp:knowledge-sharing --level L2
eka shr build eka/adr:sharing-object-model --levels L0,L1,L2  # batch 3
eka publish eka/shr:my-share-l0
```

### Non-EKA audited spike (provenance audited)
```sh
eka shr build /path/to/codebase --level L0 --provenance audited --id share-codebase-l0
eka shr build /path/to/codebase --levels L0,L1 --provenance audited
eka publish eka/shr:share-codebase-l0
```
Audited mode scans the directory (skips .git, node_modules, .eka), builds a summary (file list, byte count) and pins via sha256 hash. Use for non-EKA codebases that need L0-L2 sharing.

## Levels (opt-in, default-deny)
- `L0` metadata only (title, description, sourceHash, provenance)
- `L1` L0 + safe summary/structure (sourceType, sourceId, dimension, domain)
- `L2` L1 + full snapshot (content at sourceHash, guarded 1MiB)

`--level` single, `--levels L0,L1,L2` batch (dedup, mutually exclusive). Batch uses per-level id `share-<id>-l0` etc.; `--id` with batch appends suffix.

## Server-side filtering (MCP-friendly)
Agents can scan without source repo via MCP/CLI get:
```sh
eka get operations --level L0            # domain filter, shr only
eka get operations --type shr --level L1
eka get eka/shr:my-share-l0 --level L0  # identity strict match
eka get records --level L0              # alias records -> Operations
```
MCP `get`/`domain` tools expose same `--level` filtering; `view Operations` shows 3 groups consistently.

## Hardening notes
- IDs normalized (lowercase, hyphens, ValidIdent)
- L1/L2 dedup via `buildCommonShrFields`
- Snapshot guard 1 MiB for L2
- Collision (already exists) exits 1 (fail) not 2 (usage)
- domainTokens via registry + alias `records`

## Publish flow
Drafts are workspace-native (`eka.yaml` required). After `eka shr build`, validate and publish:
```sh
eka publish eka/shr:my-share-l0
eka get eka/shr:my-share-l0
```

## MCP scan (no source repo)
```sh
eka get operations --level L0 --no-content  # title/description via content fields
```
Agent can discover `shr` via `eca get/domain` and fetch `title`/`description`/`level`/`provenance`/`sourceHash` without cloning source repo.

## References
- `cmd/shr.go` (builder)
- `cmd/get.go` (server-side --level)
