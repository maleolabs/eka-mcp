---
description: Builds shr sharing objects via CLI — use when creating L0-L2 snapshots from EKA CKO or audited filesystem codebase, with server-side filtering and publish flow.
---

# Shr sharing via CLI

## Run exactly
```bash
eka shr build <source> --level L0 --id <shr-id>
eka shr build <source> --levels L0,L1,L2
eka publish <ns>/shr:<id>
eka shr export <ns>/shr:<id> -o <file>.ekapkg  # type shared
eka shr import <file>.ekapkg
eka shr delete <ns>/shr:<id> --yes --force
```

## Flags
| Flag | Use |
|---|---|
| `--level L0\|L1\|L2` | single, required |
| `--levels L0,L1,L2` | batch 1–3, suffix -l0/-l1/-l2 |
| `--provenance extracted\|audited` | `audited` = directory path |
| `--project/--version` | per-project, see `references/semver-immutability.md` |

## Interactive
```bash
eka share --level L0 --project X --version Y --source <CKO|path> --title "..."
# TTY prompts missing flags; non-TTY requires flags
```

See `eka-shr-builder` for EKA, `eka-shr-non-eka` for non-EKA L0 vs L1/L2.
