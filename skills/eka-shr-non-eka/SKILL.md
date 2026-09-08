---
name: eka-shr-non-eka
description: Audits non-EKA filesystem codebases into shr via level-adjusted deep scan — use when source is directory and eka.yaml missing, L0 shallow vs L1/L2 docs+codegraph+redaction.
---

# Auditing non-EKA codebases

## When to use
Source is directory path (not CKO). `eka.yaml` missing → asks `project` id (not hardcode `eka`).

## Run exactly
```bash
eka shr build /path/to/code --provenance audited --level L0 --id share-<base>-l0
eka shr build /path/to/code --provenance audited --levels L0,L1
eka publish <ns>/shr:<id>
```

## Levels
| Level | Scan | Cap | Files |
|---|---|---|---|
| L0 | file list only | 500 | skip .git/node_modules/.eka/dist |
| L1/L2 | +docs(.md)+codegraph(.go/.ts/.js/.py/.yaml) + redaction(.env/secret/.pem) | 1000, 1 MiB guard | hash pinned per level |

See `references/audit-levels.md` for sample truncation.

## Scan (MCP parity)
```bash
eka get operations --type shr --level L0 --project my-app
```

For EKA CKO, see `eka-shr-builder`.
