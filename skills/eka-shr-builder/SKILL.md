---
name: eka-shr-builder
description: EKA sharing object (shr) builder for agents — EKA-to-EKA and audited non-EKA spike
---

# EKA Shr Builder Skill

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

## Clarified (knowledge-sharing-clarified, per follow-up)

### Per-project identifier
- `sourceProject` / `sourceVersion` captured from `eka.yaml` (EKA, derived via resolveNewScope, not hardcode `eka`) or asked for non-EKA (CLI asks, MCP via --project)
- `eka get operations --type shr --level L0 --project my-app --version 1.2.3` — server-side filters `level` + `project` + `version` (also identity `eka/shr:<id> --level/--project/--version` strict match)
- `sourceNamespace` derived from target project, not source repo
- Semver immutability: 3-part `X.Y.Z` major immutable, 2-part `X.Y` minor immutable — enforced on `shr build`/`delete` (collision exit 1, `--force` override for correction)
- EKA vs non-EKA detection: check `eka.yaml` exists; EKA uses `eka.yaml` project/namespace, non-EKA asks user for project id

### Dedicated shr export/import (ekapkg RSF, type distinction)
- `eka shr export eka/shr:<id> -o <file>.ekapkg` — deterministic RSF package `type=shared` (not reuse `eka export`)
- `eka shr import <file>.ekapkg` — restores share in other workspace with correct level/provenance, type `shared` vs `live KMS` preserved

### Delete shared knowledge
- `eka shr delete eka/shr:<id> --yes/--force` or `eka shr delete --project <name> --version <v> --level L0 --yes`
- Requires confirmation (`--yes`/`--force`), respects immutability but allows `--force` correction, does not disturb other references

### Deep audit (non-EKA) level-adjusted
- `L0` shallow fast (file list, 200 cap, skip .git/node_modules/.eka)
- `L1/L2` deep scan docs (.md) + codegraph (.go/.ts/.js/.py/.yaml) + sensitivity redaction (.env/secret/.pem filtered), 1MiB snapshot guard
- `auditNonEKAPathLevel(root, level)` — hash pinned per level

### Skills split & interactive share
- `eka-shr-builder` (EKA) vs `eka-shr-non-eka` (non-EKA deep audit) — both English, `eka-` prefix, discoverable via `eka://skills/eka-shr-builder` & `eka://skills/eka-shr-non-eka` (MCP) and `eka get operations --type shr`
- General command `eka share` (interactive Q&A: level, identifier project+version, provenance, title, export choice) — `isTerminal` prompt, not conflicting with skill name

### MCP parity
- `get`/`domain` tools now expose `level`, `project`, `version` (parity CLI `--level/--project/--version`)
- Agents can scan `shr` without cloning source repo via `domain` + `get` with shr filters

