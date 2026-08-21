---
name: eka-engineering-workflow
description: Use when a task is software-engineering work inside an EKA-enabled project — implementation planning, ticket execution, architecture changes, reviews, operations. Teaches how EKA relates to software development, how Engineering Knowledge evolves through the canonical domains, methodology independence (no Scrum/PRD/ADR assumptions), and the full Understand → Context → Reason → Change → Validate → Publish loop.
---

# Engineering Workflow

EKA is the knowledge spine of software development: it records **why and how** the software is designed and evolved. Source code answers *"what does the software currently do?"*; Engineering Knowledge answers *"why and how is it designed and evolved?"*. Use both — neither replaces the other.

## The canonical spine

Engineering Knowledge evolves through the five Engineering Domains, top-down authority:

```
Discovery  (intent, requirements, research)      — why the product exists
   ↓
Architecture  (architecture, decisions, specs)   — how it is designed
   ↓
Planning  (scopes, epics, plans)                 — what is committed and when
   ↓
Execution  (containers, work items, reviews)     — the work in motion
   ↓
Operations  (runbooks, releases, records)        — how it runs and what happened
```

Work flows down the chain as authority; evidence flows up as traceability (R10: every non-Discovery artifact derives from the stratum above).

## Methodology independence

Do **not** assume Scrum, PRD, ADR, Epic, Sprint, Ticket, or any methodology. Those are **representation aliases** onto canonical tokens (`scp-`, `adr-`, `epc-`, `tkt-`, …) — conventions that vary by team. Reason from the canonical model:

- A **requirement** is `req-` in Discovery — not "a PRD".
- A **decision record** is `adr-`/`dec-` in Architecture — not "an ADR document".
- A **work item** is `sto-`/`ts-`/`bug-`/`td-`/`ch-`/`spk-` in Execution — not "a ticket" (tickets `tkt-` are pure projections of work items, never independently edited state).
- A **plan** is `plan-`/`scp-`/`epc-` in Planning — its states are `draft → approved → immutable`, not "sprint status".

When a team uses its own vocabulary, map it to canonical tokens before reasoning — never reason from the vocabulary alone.

## The agent loop

```
Understand  →  Context  →  Reason  →  Change  →  Validate  →  Publish
```

### Understand

Read the task. Identify which Engineering Domains it touches. Check the repository state: `eka status`, `eka project list`.

### Context — before any change

Prefer obtaining project context before modifying knowledge or code when the task depends on project intent, architecture, planning, or constraints:

```sh
eka status                                # registered? workspace state (no write)
eka get <domain>...                       # the knowledge map of the touched domains
eka context <subject> --depth engineering --json   # constraints in force around the subject
```

Reads (`eka get`/`eka context`/`eka view`) run directly on the machine-wide canonical store — no sync needed once the repository is registered. `eka sync` is an **intake/migration/registration command, not a per-read precondition**: full `eka sync` / `eka sync pull` re-seeds the store from the repository snapshot (or docs tree) and can overwrite newer store states with older instances (see the sync-pull hazard in [eka-knowledge-modification](../eka-knowledge-modification/SKILL.md)). When a task needs knowledge the store does not yet have, sync once — never as a routine step inside an active execution.

Do not modify based solely on source code; do not modify knowledge based solely on a local file. Context first. (Full workflow: [eka-project-understanding](../eka-project-understanding/SKILL.md).)

### Reason

- Which stratum holds the truth for this task? (A code change must honor Architecture in force; a plan change must honor approved Discovery; an execution change must honor the locked plan.)
- Does the task need new knowledge (a decision, a spec, a work item), a state change (a transition), or a revision?
- What are the binding constraints? (`eka context --depth engineering` — the `constraints`/`decisions`/`planning` sections.)

### Change

- **New knowledge** → [eka-knowledge-authoring](../eka-knowledge-authoring/SKILL.md): draft → validate → publish.
- **State changes** → [eka-knowledge-modification](../eka-knowledge-modification/SKILL.md): `eka transition` (gates apply), never direct mutation. Before ANY transition: `eka get <line>` — read the line's actual current state, compare it with the state the work stage requires, and transition only the mismatch (Status Context Protocol: get → compare → align). Forward-only while an execution is active; pull-back is a deliberate, documented correction, never an automatic step. After the transition: `eka sync push` (never a full `eka sync` mid-execution — the pull side can re-point references to older instances and silently regress states), then verify with `eka get`.
- **Revisions/corrections** → new instance version, same line, same stratum (R12).
- **Source code** → ordinary development; knowledge and code stay consistent — a code change that contradicts Architecture in force is a knowledge conflict to resolve (downward), not a silent code-only change.

### Validate

```sh
eka validate           # conformance: 0 errors
eka integrity check    # store integrity: 0 violations
```

Never finish with validation findings unaddressed.

### Publish / persist

- Knowledge you created: `eka publish` (workspace) or `eka sync` (docs tree / snapshot transport).
- State changes: already published by `eka transition`; run `eka sync push` to refresh repository-attributed snapshots.
- Review evidence: `eka note --role review` + `eka publish`.

### Feedback — a conscious end-of-work step

When the job is done and the session revealed a shortcoming of **EKA itself** — a confusing refusal, a missing command, a rough edge — report it before moving on (ADR-026). This is a **conscious decision, not an automatic side effect**: only report when the friction was real and the report would be actionable.

```sh
eka feedback new --type bug --title "<problem>" --source agent --command "<failing invocation>"
eka feedback publish <id> --yes        # file it as a GitHub issue
```

Full guidance — quality bar, refusals, when not to report: [eka-feedback](../eka-feedback/SKILL.md).

## Typical task flows

| Task | Flow |
|---|---|
| **Implementation planning** | Context: `eka context <plan|scope>` + `eka get planning` → Reason: what execution work does the plan imply → Change: new work item drafts → Validate → Publish |
| **Ticket execution** | Context: `eka context <work-item>` (constraints, dependencies) → verify the actual state: `eka get <work-item>` (never assume from a board or memory) → Change: `eka transition <work-item> in-progress` **before coding** (`in-progress` marks active work, never a post-completion blip) — transition only when the current state differs from the required stage; forward-only during execution → code → evidence via `eka note --role implementation` → `in-review` (gated on resolved implementation note) → `done` (gated on all notes resolved) → after each transition `eka sync push`; never a full `eka sync` mid-execution (the pull side can re-point references to older instances and silently regress states) |
| **Architecture change** | Context: `eka context <adr|arc>` `--depth engineering` (what binds the change) → Change: new `adr-`/`dec-` draft (or revision) deriving from the higher stratum → Validate → Publish |
| **Knowledge review** | [eka-knowledge-review](../eka-knowledge-review/SKILL.md): validate → context → integrity → record the verdict as a note/review |
| **Operations work** | Context: `eka get operations` + the runbooks (`run-`) → execute → record (`rel-`, `run-` updates as revisions) |

## Common workflow mistakes

- Starting a change without context ("the code says so" is not the authority chain).
- Treating tickets as state owners (tickets are projections — the work item owns the state, P6).
- Skipping gates: a work item cannot be `in-review` without a resolved implementation note, cannot be `done` with unresolved notes (R13).
- Advancing `in-progress` only after the work is done — `in-progress` means work has **started**, so the transition must land before coding, not as a visible skip on the board right before `in-review`.
- Transitioning without reading the current state (`eka get` first) — double-transitions and unintended pull-backs come from stale assumptions; compare the observed state with the required stage and align only the mismatch.
- Running a full `eka sync` mid-execution — the pull side re-seeds the store from a stale snapshot/docs tree and can silently regress states; use `eka sync push` during active work and verify with `eka get` after any pull.
- Bypassing the loop: publishing knowledge that was never validated, or editing canonical objects "just this once".
- Using methodology vocabulary as if it were the model (Scrum ≠ EKA Planning; PRD ≠ Discovery).
- Confusing derived output with canonical knowledge: a board, a context object, or a summary is a view — not an object to re-author.

## Real example (Feather Reference Project)

```sh
# planning a new capability: understand what the active wave commits
eka context feather/ctr:wave-7 --depth engineering --json

# executing: advance the work item as the code lands, with evidence
eka transition feather/sto:draft-autosave in-progress
eka note feather/sto:draft-autosave --role implementation --domain execution
eka publish feather/cmt:draft-autosave-implementation
eka transition feather/sto:draft-autosave in-review

# validating the repository state
eka validate
eka integrity check
```

## Next

- [The AI Workflow](../docs/workflow.md) — the complete walkthrough with the six-step loop in depth.
- [The MCP Boundary](../docs/mcp-boundary.md) — how this behavior layer will sit on future transports.

## Review trail convention (phase 2)

Per-reviewer `cmt-` trail: one note per reviewer, agent identity via `--by --by-kind agent|worker`, verdict `approve` is advisory, `resolve` only releases `done` (see `eka-knowledge-review`).
