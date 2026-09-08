---
name: eka-shr-builder
description: Builds shr snapshots (L0-L2, pinned sourceHash) for EKA CKO and audited filesystem codebases — use when sharing knowledge without cloning source or when publishing L0-L2 levels via CLI/MCP.
---

# Building shr snapshots

## When to use
- Source is qualified CKO (`<ns>/<type>:<id>`) — `eka get <source>` succeeds
- Source is directory — use `--provenance audited` and see `eka-shr-non-eka` for L0 vs L1/L2

## Run exactly (low-freedom)
```bash
eka shr build <source> --level L0 --id <shr-id>
eka shr build <source> --levels L0,L1,L2
eka publish <ns>/shr:<id>
```

## Flags
| Flag | Values | Note |
|---|---|---|
| `--level` | `L0\|L1\|L2` required | `L0` meta, `L1` +summary, `L2` +snapshot (see `references/shr-levels.md`) |
| `--levels` | `L0,L1,L2` batch | 1–3 shr, suffix `-l0/-l1/-l2`, mutually exclusive with `--level` |
| `--provenance` | `extracted` (EKA), `audited` (non-EKA) | audited scans directory |
| `--project/--version` | `sourceProject`/`sourceVersion` | from `eka.yaml` (EKA) or asked (non-EKA); see `references/semver-immutability.md` |

## Scan without source (MCP parity)
```bash
eka get operations --type shr --level L0 --project my-app --version 1.2.3
eka get <ns>/shr:<id> --level L0 --project X --version Y
# MCP: tools `get`/`domain` with `level/project/version`
```

See `references/shr-filters.md` for strict vs domain filtering. For non-EKA deep audit detail, see `eka-shr-non-eka`.
