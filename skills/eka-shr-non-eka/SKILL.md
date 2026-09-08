---
name: eka-shr-non-eka
description: Non-EKA deep audit shr skill — audit filesystem codebase for non-EKA sharing, level-adjusted deep scan
---

# EKA Shr Non-EKA

Audit a non-EKA codebase (filesystem path) into shr with level-adjusted deep audit. English, eka- prefix, discoverable via MCP.

## Levels (deep audit adjusted to target level)
- `L0` shallow OK — file list only, fast, 500 cap, skip .git/node_modules/.eka/dist
- `L1/L2` deep scan — docs (.md) + codegraph (.go/.ts/.js/.py/.yaml, 100 files) + sensitivity redaction (.env/secret/.pem filtered), 1000 cap, snapshot 1MiB guard, hash pinned per level

## Usage (provenance audited)
```sh
eka shr build /path/to/codebase --provenance audited --level L0 --id share-codebase-l0
eka shr build /path/to/codebase --provenance audited --levels L0,L1 --id share-codebase
eka publish eka/shr:share-codebase-l0
eka get operations --type shr --level L0 --project my-app   # scan via MCP without source repo
```

## Implementation
- `auditNonEKAPathLevel(root, level)` — branching on level, redaction, docs/codegraph sample
- MCP `get`/`domain` with `project`/`version`/`level` filters work for audited shr too (`sourceProject` from CLI --project or eka.yaml fallback)
- Skills split: this skill for non-EKA, `eka-shr-builder` for EKA — both English

## When to use
- Source is directory path (not CKO), eka.yaml missing → ask user for project id (non-EKA), not hardcode `eka`
