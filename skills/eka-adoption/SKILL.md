---
name: eka-adoption
description: Use when a project is NOT EKA-enabled — its documentation does not follow any EKA standard, or it has no documentation at all — and you need to bring it into EKA. Teaches the adoption workflow: assessment and classification of legacy documentation into Engineering Domains and token families, state inference with honesty guardrails, relationship reconstruction, the two migration paths (repository docs tree / workspace drafts), greenfield capture strategy, and post-migration verification.
---

# EKA Adoption

Adoption = making a non-EKA project EKA-enabled. Two entry cases:

- **Existing documentation, not EKA-aligned** — Markdown/Confluence/Notion/READMEs/tickets whose structure does not follow any EKA standard. The core work is **transformation**: classify, infer, reconstruct — never invent.
- **No documentation at all** — the work is **capture strategy**: decide what knowledge to create first, from which sources, in what order.

Both cases share the same spine:

```
Assess  →  Bootstrap  →  Transform / Capture  →  Validate  →  Sync  →  Verify
```

## Stage 1 — Assess

1. **Inventory** the existing documentation: every document, its kind, its state signals, its references to other documents.
2. **Classify** each document against the canonical model:

| Document kind | Token family (domain) |
|---|---|
| vision / product brief / strategy memo | `vis`, `str` (Discovery) |
| requirement, user story backlog, spec request | `req` (Discovery) |
| research report, comparison, spike write-up | `fnd` (Discovery) |
| architecture overview, system design | `arc` (Architecture) |
| decision, RFC, ADR, design discussion | `adr`, `dec` (Architecture) |
| API/docs specification, interface contract | `spec` (Architecture) |
| standard, coding convention, DoD | `std` (Architecture) |
| glossary, terminology | `gls` (Architecture) |
| roadmap, plan, milestone, scope | `plan`, `scp`, `epc` (Planning) |
| task, story, bug report, todo list | `sto`, `ts`, `bug`, `td`, `ch`, `spk` (Execution) |
| review, retrospective, QA report | `rvw` (Execution) |
| meeting notes, work log, session transcript | `ses` (Execution) |
| runbook, playbook, ops guide, release note | `run`, `rel` (Operations) |
| comments, discussion threads on work items | `cmt` (Execution) |

3. **Decide the strategy**: migrate everything canonical-worthy / migrate a subset (the forward-looking core) / start fresh and keep legacy docs as historical reference only. Partial migration is legitimate — the [distillation obligation](../../skeleton/docs/workflow-guide.md) is a judgment, not a quota.
4. **Decide the path** (the two-target table in Stage 3) based on how the knowledge must travel.

**Misclassification is the worst adoption error**: knowledge placed in the wrong stratum claims authority it does not have (e.g. a design discussion promoted to an accepted `adr`). When a document's classification is uncertain, classify **downward** (draft, lower-authority) or flag it — never guess upward.

## Stage 2 — Bootstrap

```sh
eka init --project <id> --namespace <ns>   # identity-only: writes ONLY eka.yaml (ADR-020)
```

- The namespace is the **platform scope** of the migrated knowledge — a deliberate choice, not the directory name. Content namespace must equal the declared namespace: a mismatch refuses sync deterministically (ADR-020) — align before syncing (`eka sync --override` is the sanctioned alignment path, one-time).
- After the first sync the identity freezes — choose deliberately, edit only before that point.
- Workspace: `eka sync` creates/uses `~/.eka` (or `$EKA_HOME`).

## Stage 3 — Transform (case: existing docs)

Transform each classified document into an EKA authoring representation. Two output targets:

| Output | When | Mechanics |
|---|---|---|
| **Repository docs tree** (`docs/<dimension>/<type>-<id>.json`) | the migrated knowledge must live in the repository and travel via snapshots/export | write JSON authoring files in the dimension folders, then `eka validate` + `eka sync` (docs-mode pull) |
| **Workspace drafts** (`eka new` → fill → `eka publish`) | workspace-native knowledge only (never in snapshots); or a gradual, per-artifact migration | scaffold per artifact, fill, publish; `eka sync push` for repository-attributed knowledge |

Use the [authoring templates](../eka-knowledge-authoring/templates/README.md) (owned state, `dimension`, content keys per family) as the transformation target. Authoring JSON schema per spec-standard-v2: `namespace`, `type`, `id`, `revision`, `state`, `dimension` (knowledge types), `relationships`, `changeLog`, `content`.

**State inference — honesty guardrails:**

| Legacy signal | EKA state |
|---|---|
| "decided", "approved", "ratified", "accepted" (with a dated record) | `contentState: accepted`/`approved` |
| "RFC", "proposed", "WIP", "draft", no signal | `contentState: draft` (or `proposed` for adr/dec) |
| unknown / ambiguous | `draft` — **default to draft, never claim in-force without evidence** |
| historical/obsolete doc | do not migrate as active; migrate as history or leave out |

- **Dates**: use real dates where the document records them; otherwise the transformation date — and record that provenance in the change-log or a note. Never fabricate a past date to make knowledge look older/established.
- **Authors**: real authors where known; otherwise the migration identity — never an invented person.

**Relationship reconstruction:**

- Mine textual references ("per ADR-12", "as specified in §3", "decided in the 2026-05 session") into `derivesFrom`/`dependsOn` edges, using canonical forms (`<ns>/<type>:<id>`).
- Every non-Discovery artifact needs its upward traceability chain (R10). Missing chain = **warning + recorded gap**, never a fabricated chain to make validation pass.
- Draft targets are tolerated (draft tolerance, R5); dangling references on non-draft artifacts block validation — resolve or downgrade.

## Stage 4 — Validate and Sync

```sh
eka validate             # R0–R13: 0 errors required; warnings understood, not ignored
eka sync                 # docs-mode pull on first sync (the runtime's migration path, ADR-010)
# after further docs edits:
eka sync pull --from-docs    # forced re-seed from the docs tree
eka sync push                # refresh the repository snapshot
```

Verified behavior (adoption flow): a fresh repository with 3 transformed Discovery artifacts validates with 0 errors/0 warnings, syncs in docs mode (`Pull: docs: 3 units`), writes a snapshot, and the migrated knowledge is immediately retrievable (`eka get`/`eka context` work on it).

## Stage 5 — Capture (case: no documentation)

1. **Spine-first**: create the authority chain in order — Discovery (vision/strategy/requirements) → Architecture (decisions/specs) → Planning (scope/plan) → Execution (work items) → Operations (runbooks). Higher strata first: lower-stratum knowledge needs them for traceability (R10).
2. **Sources**: codebase (what the software does — architecture reality, not intent), team/tickets (intent, decisions, plan), conversations (meeting notes → `ses`, then distill). **Source code is evidence, not knowledge**: do not translate code structure into approved knowledge claims — record observed architecture as drafts/`fnd` until confirmed.
3. **Do not document everything**: the distillation obligation — sessions distill into requirements/decisions, findings into decisions, proven procedures into runbooks. A ticket log is not knowledge; the decisions it produced are.
4. **Minimum viable start**: one `vis`, one `str`/`req`, one `adr`, one `plan`, one `ctr` with work items — a working authority chain the project can grow.

## Stage 6 — Verify

```sh
eka validate                # repository conformant
eka integrity check         # store sound
eka get <domain>            # the knowledge map is populated, domains/strata correct
eka context <subject> --depth engineering --json   # constraints in force are the migrated authority
eka view execution          # the operating layer projects
```

Spot-check the strata landscape: every migrated artifact sits in its true domain; nothing claims authority it cannot back; the chain Discovery → Architecture → Planning → Execution → Operations is walkable.

## Adoption-specific safety rules

1. **Never fabricate**: state, relationships, dates, authors, change-log entries. Migrated knowledge is *transformed* evidence, never invention.
2. **Default to draft** — in-force claims require evidence (a dated decision record, an approved requirement).
3. **Misclassification is worse than absence** — a wrongly-classified artifact claims wrong authority; classify downward when unsure.
4. **Do not fabricate traceability** — missing chains are recorded gaps (warnings), not invented edges.
5. **Code is evidence, not knowledge** — observed code reality becomes drafts/findings, never approved knowledge without confirmation.
6. **Human confirmation for in-force claims** — state decisions (draft vs approved/accepted) that are not backed by explicit records must be confirmed, not assumed.
7. All other [pack safety boundaries](../README.md#ai-safety-boundaries) apply unchanged.

## Verified adoption walkthrough

```sh
# fresh repository, identity-only init (ADR-020)
eka init --project adopt --namespace adopt

# transform: docs/<dimension>/<type>-<id>.json (e.g. docs/intent/vis-*.json, docs/requirements/req-*.json)
# namespace in every artifact must equal the declared namespace

eka validate .              # 3 artifacts, 0 errors, 0 warnings
eka sync .                  # Pull: docs: 3 units → canonical store; Push: 3 units; snapshot written

eka get adopt/req:publishing-core:1 --no-content   # Discovery, stratum 1, approved
eka context adopt/req:publishing-core --json       # sections: upstream, dependencies, history
```

## Next

- Classification/state uncertainty → [eka-troubleshooting](../eka-troubleshooting/SKILL.md) (refusals) · [eka-knowledge-review](../eka-knowledge-review/SKILL.md) (verifying migrated knowledge)
- Authoring the transformed artifacts → [eka-knowledge-authoring](../eka-knowledge-authoring/SKILL.md)
- Running the adopted project → [eka-engineering-workflow](../eka-engineering-workflow/SKILL.md)
