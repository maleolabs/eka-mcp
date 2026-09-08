# Shr levels

| Level | Payload | Guard |
|---|---|---|
| L0 | title, description, sourceHash, provenance | — |
| L1 | L0 + summary (sourceType, sourceId, dimension, domain) | — |
| L2 | L1 + snapshot (full content at sourceHash) | 1 MiB |

Batch `--levels L0,L1,L2` creates 1–3 drafts with suffix `-l0/-l1/-l2`, deduped. Single `--level` required (default-deny).
