---
name: eka-orientation
description: Use when working in an EKA-enabled project and you need the EKA mental model first — what Engineering Knowledge is, how EKA organizes it (Engineering Domains, Knowledge Stratification, Canonical Knowledge Objects, immutable knowledge, Runtime vs Authoring). Load before acting when you are not yet EKA-literate, or when a task touches knowledge structure (identity, state, relationships, domains).
---

# EKA Orientation

Teach the mental model **before** running commands. Commands without this model produce output without understanding.

## What EKA is

EKA (Engineering Knowledge Architecture) is an **open standard** for engineering knowledge: a canonical architecture for how engineering knowledge is identified, structured, exchanged, and operated. It is not a documentation template, not a Markdown scheme, not a project management tool. The EKA repo you are working in is one implementation of the standard.

The knowledge model is a small, stable set of concepts:

| Concept | Meaning |
|---|---|
| **Identity** | `(Namespace, Type, ID, InstanceVersion)` — immutable, independent of location. References always use Identity, never file paths. |
| **State Vector** | the owned state domains of an artifact (`contentState`, `executionState`, `planningState`, `containerState`, `existenceState`, `noteState`). Every state field has exactly one owner (P6). |
| **Content** | the knowledge payload of the artifact. |
| **Relationships** | typed edges by Identity: `supersedes`, `amends`, `derives-from`, `depends-on`, `validates`, `discusses`. |
| **Classification** | dimension + Engineering Domain. |

## Engineering Domains and Knowledge Stratification

Every artifact belongs to exactly one of **five Engineering Domains** — the stratum-aligned category of knowledge it holds:

| Stratum | Domain | What it holds | Token families |
|---|---|---|---|
| 1 (highest) | **Discovery** | intent, requirements, research | `vis`, `str`, `req`, `fnd` |
| 2 | **Architecture** | architecture, decisions, specifications, standards, vocabulary | `arc`, `adr`, `dec`, `spec`, `std`, `gls` |
| 3 | **Planning** | planning | `scp`, `epc`, `plan`, `trc` |
| 4 | **Execution** | quality + operating: containers, work items, tickets, reviews, sessions | `ctr`, `sto`, `ts`, `bug`, `td`, `ch`, `spk`, `tkt`, `rvw`, `ses`, `cmt` |
| 5 (lowest) | **Operations** | operations, records | `run`, `rel` |

The domains form a strict authority chain — **Discovery → Architecture → Planning → Execution → Operations**. Two invariants:

- **Stratum Authority Invariant**: lower-stratum knowledge must not contradict higher-stratum knowledge in force, and never supersedes or amends upward (R12).
- Every non-Discovery artifact must be traceable upward to the higher-stratum knowledge it derives from (R10).

Methodology terms (Scrum, PRD, ADR, Epic, Sprint, Ticket…) are **representation aliases** onto canonical tokens — conventions, not part of the core model. Reason from the canonical model, never from methodology assumptions.

## Canonical Knowledge Objects (CKOs)

The **Canonical Knowledge Object** is the one canonical internal representation of one Engineering Knowledge Object: `unit.json` + representation-tagged content, with derived values (domain, stratum) and integrity metadata (`object_hash` = SHA-256 digest of the serialized unit). Markdown/JSON authoring files are **authoring representations** — they compile into CKOs; they are never a runtime representation. `eka get` emits CKOs as canonical JSON (schema `eka-cko-v2`); `eka context` derives the Context Object from them (schema `eka-context-v1`).

## Immutable knowledge

Engineering Knowledge is **immutable and content-addressed**:

- Objects are written once, keyed by their own content hash; there is **no update path**.
- "Changing" knowledge = creating a **new revision**: a new instance version of the same identity line, or a sanctioned state transition that publishes a new immutable payload.
- History is **derived, not maintained**: forward-only instance versions + `prev_hash` lineage + retained unreferenced payloads.
- The only mutable rows in the store are references (which payload is current for a form) and operational bookkeeping (registry, sync log).

## Runtime vs Authoring

| Side | Role | Where |
|---|---|---|
| **Authoring** | the mutable input representations: workspace **drafts** (JSON templates, `eka new`/`eka edit`/`eka publish`) and the legacy repository `docs/` tree | drafts: `<workspace>/drafts/<project>/`; docs: `<repo>/docs/` |
| **Runtime** | the canonical store of immutable CKOs + the services that read them | EKA Workspace `~/.eka/` (or `$EKA_HOME`), `eka.db` |
| **Transport** | deterministic Knowledge Snapshots between repository and store | `<repo>/exchange/snapshots/`, moved by `eka sync` |

Mental pairing: **canonical storage lives in the workspace; the repository is the transport.** The repository is registered under a project (identity from `eka.yaml`); the workspace store reconstructs the complete knowledge of a project as the union of its registered repositories.

Three runtimes share one knowledge model: **Git** (source code version control — never replaced), the **EKA Knowledge Runtime** (the local canonical store), and **Atrium** (a future unified project runtime — not implemented; it is the architectural reason the runtime exists).

## The tools in one glance

- `eka sync` — pull/push knowledge between repository and the canonical store. **Intake/migration/registration only** — one-time at project entry, never a per-read precondition (reads run directly on the shared store; the pull side re-seeds from the snapshot/docs tree and can overwrite newer store states with older instances).
- `eka get` — **retrieve**: canonical CKO JSON (machine-readable).
- `eka context` — **understand**: the deterministic Engineering Context Object around one subject.
- `eka view` — **visualize**: human projections (Kanban, roadmap, dependency tree, …).
- `eka validate` — the conformance gate (rules R0–R13) over authoring.
- `eka integrity check` — the canonical store's integrity verification.
- `eka new` / `eka edit` / `eka publish` / `eka discard` / `eka draft list` — the draft-publish authoring workflow.
- `eka transition` — sanctioned state changes (work items, plans, containers).
- `eka note` — `cmt-` note drafts (comments/review evidence) on a subject.

`eka --help` is the authoritative command list; never assume a command exists.

## Behavioral rules

- Never modify runtime storage directly, never write to SQLite, never bypass the Authoring API or validation.
- Never invent knowledge; never treat derived output (projections, contexts, summaries) as canonical.
- Never mutate immutable objects — create revisions.
- Never change higher-stratum knowledge to justify a lower-stratum implementation.

Full canonical list: [the pack's safety boundaries](../README.md#ai-safety-boundaries).

## Next

- New to the project? → [eka-project-understanding](../eka-project-understanding/SKILL.md)
- Need to fetch specific knowledge? → [eka-knowledge-retrieval](../eka-knowledge-retrieval/SKILL.md)
- Deeper background: [Engineering Operating Guide](../../skeleton/docs/workflow-guide.md), [Knowledge Runtime Architecture](../../reference/runtime-architecture.md).
