# The AI Workflow

The complete agent workflow for working with an EKA-enabled project. The skills teach the pieces; this document ties them into one loop.

## The loop

```
Understand  →  Context  →  Reason  →  Change  →  Validate  →  Publish
```

Six stages, one discipline: **context before change, validation before persistence**. The loop applies to knowledge work *and* to code work — the difference is which layer you change (knowledge, source code, or both).

### Stage 1 — Understand

Read the task. Map it to the canonical model:

- Which Engineering Domains does it touch? (Discovery / Architecture / Planning / Execution / Operations)
- Does it create knowledge, change knowledge, or consume knowledge?
- What is the repository state? `eka status`, `eka project list`.

Do not start changing anything yet. Load `eka-orientation` if the model is not second nature; the [project-understanding](../eka-project-understanding/SKILL.md) skill is the next step for unfamiliar projects.

### Stage 2 — Context (before any change)

The **context-first principle**: prefer obtaining project context before modifying Engineering Knowledge or source code when the task depends on project intent, architecture, planning, or constraints.

```sh
eka status                                  # registered? workspace state (no write)
eka get <domain>...                         # the knowledge map of the touched domains
eka context <subject> --depth engineering --json   # constraints in force around the subject
```

`eka sync` is **not** a per-read precondition: reads run directly on the shared store once the repository is registered (one-time `eka sync` at intake). Full `eka sync` / `eka sync pull` re-seeds the store from the repository snapshot (or docs tree) and can overwrite newer store states with older instances — use `eka sync push` during active work and verify with `eka get` after any pull.

What "having context" means:

- **Project context** — which domains are populated, what the project is committing to.
- **Relevant Engineering Domains** — the strata that bind the task.
- **Architectural constraints** — the higher-authority units in force (specs, standards, decisions, approved plans) and whether the task honors them.
- **Planning context** — plans/scopes in force, their states (draft vs approved vs immutable).
- **Execution context** — the active container, work items in motion, what depends on what.
- **Relevant relationships** — the derivation chain of the subject.

Never skip this stage because the code "looks simple". Source code answers *what the software does*; Engineering Knowledge answers *why and how it is designed and evolved*. A change that ignores the knowledge layer is a knowledge conflict waiting to happen.

### Stage 3 — Reason

With context in hand, decide:

- **What does the task require?** New knowledge (a decision, a spec, a work item), a state change (a transition), a revision (new instance version), or code only?
- **Which stratum holds the truth?** Lower-stratum changes must honor higher-stratum knowledge in force. A conflict resolves **downward** — never change higher-stratum knowledge to justify a lower-stratum implementation.
- **What are the gates?** Work item transitions carry R13 gates (in-review needs a resolved implementation note; done needs all notes resolved). Container activation locks its plan and requires the exactly-one-active rule. Plan approval is `draft → approved`; immutability is the container lock, not a direct transition.

### Stage 4 — Change

| Change kind | Path | Skill |
|---|---|---|
| New knowledge | `eka new` → fill the draft → `eka publish` (draft → validate → publish) | [eka-knowledge-authoring](../eka-knowledge-authoring/SKILL.md) |
| State change | `eka transition <line> <to>` (gates enforced) | [eka-knowledge-modification](../eka-knowledge-modification/SKILL.md) |
| Revision / correction | new draft on the same line → `eka publish` (new instance version; same stratum; R12) | [eka-knowledge-modification](../eka-knowledge-modification/SKILL.md) |
| Comment / review evidence | `eka note <line> --role implementation\|review\|fix` → `eka publish` | [eka-knowledge-authoring](../eka-knowledge-authoring/SKILL.md) |
| Legacy docs tree | edit `<repo>/docs/<dimension>/<type>-<id>.json` → `eka validate` → `eka sync` | [eka-knowledge-authoring](../eka-knowledge-authoring/SKILL.md) |
| Source code | ordinary development, kept consistent with the knowledge in force | [eka-engineering-workflow](../eka-engineering-workflow/SKILL.md) |

Immutable discipline throughout: **never mutate a canonical object, never write to the store, never bypass validation.**

### Stage 5 — Validate

```sh
eka validate           # conformance R0–R13 — 0 errors
eka integrity check    # store integrity — 0 violations
```

For the change itself, verify the object:

```sh
eka get <ns>/<type>:<id>[:<v>]            # identity, state, classification, relationships as intended?
eka get <ns>/<type>:<id> --timeline       # history honest? change-log consistent?
eka context <ns>/<type>:<id> --depth engineering --json   # contradicts anything in force?
```

Validation is not a formality — it is the invariant layer. Never ship a change with unaddressed findings.

### Stage 6 — Publish / persist

- **Drafts** → `eka publish` (workspace-native immutable CKO; instance version auto-assigned).
- **Transitions** → already published; `eka sync push` refreshes the repository snapshot for repository-attributed knowledge.
- **Docs tree** → `eka sync` (compile + seed + snapshot).
- **Evidence** → `eka note --role review` + `eka publish` so the review trail participates in the gates.

## When to skip stages — never

| Stage | Skippable? |
|---|---|
| Understand | No — every task maps to the model |
| Context | No for intent/architecture/planning-dependent tasks; retrieval-only tasks can go straight to [eka-knowledge-retrieval](../eka-knowledge-retrieval/SKILL.md) |
| Reason | No — the decision layer is what separates an agent from a text replacer |
| Change | No — nothing to validate if nothing changed (and no change is a valid outcome; say so) |
| Validate | **Never** — there is no sanctioned persistence path without the gates |
| Publish | No — unpublished knowledge does not exist in the store |

## Worked example — implement a task in the Feather Reference Project

Setup (one time):

```sh
EKA_HOME=$HOME/.eka eka sync reference/project
```

1. **Understand** — task: "advance `feather/sto:draft-autosave` and record the work". Domain: Execution.
2. **Context** —
   ```sh
   eka context feather/sto:draft-autosave --depth engineering --json
   eka get feather/ctr:wave-7 --no-content
   ```
   The context shows the constraints in force (the approved plan `plan:roadmap-v1`, the scope `scp:mvp-v1`), the container it belongs to, and the line's history (in-progress, one instance).
3. **Reason** — the item is `in-progress`; the task needs an implementation note (evidence) before it may move to `in-review` (R13 gate).
4. **Change** —
   ```sh
   eka note feather/sto:draft-autosave --role implementation --domain execution
   # fill the note draft content (summary, changes, tests), then:
   eka publish feather/cmt:draft-autosave-implementation
   eka transition feather/sto:draft-autosave in-review
   ```
5. **Validate** —
   ```sh
   eka validate
   eka integrity check
   eka get feather/sto:draft-autosave --timeline --no-content
   ```
6. **Publish/persist** — the transition is already in the store; `eka sync push` refreshes the snapshot if the repository carries it.

## Common failure modes

| Failure | Symptom | Fix |
|---|---|---|
| Context-less change | change contradicts a spec/standard in force; review finds it | always run `eka context --depth engineering` before changes |
| Gate-skipping | transition refused (note gates, container gates) | create the evidence first (`eka note`), never force |
| Ticket as owner | edited `tkt-` state instead of the work item | tickets are projections (P6) — the work item owns the state |
| Summary as canonical | re-authored a projection/context as knowledge | derived output is never canonical (safety boundary #8) |
| Invented knowledge | fabricated identities/relationships/dates | knowledge must be traceable; say "missing" instead |
| Immutability violation | tried to edit a published object | new draft on the same line → new instance version |
| Validation bypass | "it will pass later" | there is no later — fix the draft or abandon the change |
| Any refusal/error | a command refused with a deterministic message | [eka-troubleshooting](../eka-troubleshooting/SKILL.md) — read the message, follow the fix, never bypass |

## Related

- [The loop per task type](../eka-engineering-workflow/SKILL.md#typical-task-flows)
- [Safety boundaries](../README.md#ai-safety-boundaries) — the non-negotiable rules this workflow enforces.
