---
name: eka-knowledge-review
description: Use when you need to review Engineering Knowledge — check structural validity, domain correctness, stratification, relationships, consistency, traceability, and upstream constraints; or verify that a change you made is sound before finishing. Teaches the EKA-native review: use `eka validate`, `eka context`, `eka get`, and `eka integrity check` — never invent independent validation logic.
---

# Knowledge Review

Review Engineering Knowledge with EKA's own validation capabilities. The CLI is the canonical mechanical form of the conformance rules (R0–R13) and the Runtime's integrity contract — do not re-implement them from scratch, and do not substitute subjective judgment for the gates.

## The review checklist

| Check | Tool | What to look for |
|---|---|---|
| **Structural validity** | `eka validate` | conformance R0–R13: naming, structure, state values, relationships, traceability. 0 errors required; warnings must be understood |
| **Runtime integrity** | `eka integrity check` | the canonical store is internally consistent: payload hashes, decode, reference targets + index columns, attachment digests, registry — 0 violations |
| **Domain correctness** | `eka get <target>` | the unit's derived `engineeringDomain`/`stratum` match what the type and classification imply; classification is truthful |
| **Stratification** | `eka context <subject> --depth engineering --json` | the subject's `constraints` (strictly higher-authority units) — does the subject contradict anything in force? |
| **Relationships** | `eka get <subject> --upstream --downstream` | every relationship target exists and is semantically right; no dangling references (outside draft tolerance); nothing references the subject that shouldn't |
| **Consistency** | `eka get <subject>` + `--timeline` | state vector matches the change-log; content state (`draft` vs `approved`/`accepted`) matches the actual history; line form vs instance semantics |
| **Traceability** | `eka get <subject> --upstream` | every non-Discovery artifact has its upward chain: work item → container → plan → scope → requirement (R10) |
| **Upstream constraints** | `eka context <subject> --depth engineering --json` | the binding higher-stratum units: specs, standards, decisions in force, approved plans — and whether the subject honors them |

## The review loop

### 1. Gate the repository

```sh
eka validate
```

Errors → the repository is not conformant; the report lists the rule violations (R0–R13). Warnings → non-blocking findings, but each must be explainable (e.g. a draft requirement constraining nothing yet, R10 exemptions for `tkt-`/`ses-`).

### 2. Check the store

```sh
eka integrity check
```

This verifies the immutability guarantee — content-derived hashes recomputed, references verified, decode strict. Violations mean the store was tampered with or corrupted; stop and report, never "fix" the store by hand.

### 3. Review the object and its neighborhood

```sh
eka get <ns>/<type>:<id>[:<v>]              # the object itself
eka get <ns>/<type>:<id> --timeline         # its history — do state and change-log agree?
eka get <ns>/<type>:<id> --upstream         # what it derives from / depends on
eka get <ns>/<type>:<id> --downstream       # what references it
eka context <ns>/<type>:<id> --depth engineering --json   # constraints, decisions, planning in force
```

### 4. Judge against the invariants

- **Stratum Authority Invariant**: does the subject contradict higher-stratum knowledge in force? If yes, the conflict resolves **downward** — the lower-stratum object must change.
- **R12**: no `supersedes`/`amends` crossing strata upward.
- **R10**: the upward traceability chain exists for every non-Discovery artifact (exemptions: `tkt-` projections, `ses-` sessions, and knowledge artifacts with `content-state: draft` — drafts constrain nothing).
- **Draft semantics**: draft units (e.g. `req:comments-phase2` in the Reference Project) constrain nothing — do not treat drafts as in force.
- **P6 single-writer**: state is owned by exactly one domain — tickets are projections, never independently edited state.
- **P7 forward-only**: history moves forward; a regression must be a documented correction.

### 5. Record the review (when the review itself is knowledge)

Reviews are knowledge: publish them as `rvw-` review artifacts (Execution) or attach `cmt-` note drafts with `--role review` to the reviewed subject. Evidence of work goes on `cmt-` notes with `--role implementation`/`fix`. The review trail then participates in the R13 transition gates (e.g. `in-review` requires a resolved implementation note).

```sh
eka note <subject-line> --role review --domain execution
eka publish <ns>/cmt:<note-id>
```

## Reviewing AI-made changes

When you (the agent) created or changed knowledge, review it the same way before finishing:

1. `eka validate` — clean?
2. `eka get` the new object — identity, state, classification, relationships as intended?
3. `eka context --depth engineering` — does it contradict anything in force? Did you silently rely on changing higher-stratum knowledge?
4. `eka get <subject> --timeline` — is the history honest (no fabricated change-log entries)?
5. `eka integrity check` — store clean?

## AI safety rules (review-specific)

- Use EKA validation; **never invent independent validation logic** (a hand-rolled check that disagrees with `eka validate` is a bug, not a finding).
- Never "fix" findings by editing the canonical store, rewriting snapshots, or forcing publishes.
- Never treat a summary/projection as canonical truth for review conclusions — verify against the CKO.
- A review finding is not a license to change higher-stratum knowledge; it is a reason to change the lower-stratum object (or to raise the finding as a note/review artifact).

## Real example (Feather Reference Project)

```sh
# the project is conformant
eka validate reference/project

# a work item's review: object, chain, constraints
eka get feather/sto:publish-post:1 --no-content
eka get feather/sto:publish-post:1 --upstream --no-content
eka context feather/sto:publish-post:1 --depth engineering --json

# the store is sound
eka integrity check

# record the verdict as a review note
eka note feather/sto:publish-post --role review --domain execution
```

## Next

- The full loop context → [eka-engineering-workflow](../eka-engineering-workflow/SKILL.md) · [the AI Workflow](../docs/workflow.md)

## Per-reviewer trail (phase 2)

- One `cmt-` note per reviewer, grouped by `author` (kind `agent`/`worker`/`user` — never impersonate).
- Verdict is advisory (`approve`/`changes-requested`), rendered as badge with `note-state` mark (`open`/`resolved`/`dismissed`).
- Only `resolved` releases the `done` gate — `dismissed` does not (no dismiss command).
- Verdict composition: list of per-note verdicts, never single aggregate.
