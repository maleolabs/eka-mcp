---
description: Execute approved EKA planning autonomously — MVP scope first, selectors for container/plan/full roadmap. PM-style orchestration: implement each work item inline in its own isolated worktree (`mode: solo`), enforce the EKA state machine via transitions (gates runtime-enforced), per-item self-review, checkpoint after every item, and resume from interruptions without losing context.
---

# EKA Execution

Execute **approved** EKA planning autonomously, PM-style. The primary agent performs every step itself, inline (`mode: solo`): orchestration, implementation and review. The EKA state machine is enforced through `transition` (R13 gates are runtime-enforced), and a checkpoint is written after **every** item so execution can resume after interruptions (credit/context limits, network loss, machine shutdown) without losing context.

The user interacts exactly twice: at invocation (scope selection) and at the final report. Mid-run interruptions resume via the checkpoint protocol — no manual re-commanding of items.

## Transport primitives

Every EKA capability this command uses is a **primitive** with two transports: the `eka` CLI and the eka-mcp MCP server. The table below is the single authoritative citation — the rest of this body refers to primitives by name only.

| Primitive | CLI command | MCP tool |
|---|---|---|
| status | `eka status` | `status` |
| sync | `eka sync` / `eka sync push` | — |
| get | `eka get <form>` (single object) | `get` |
| domain | `eka get <collection>` (whole-domain listing) | `domain` |
| context | `eka context <subject>` | `context` |
| transition | `eka transition <target> <to>` | `transition` |
| note | `eka note <target> --role <role>` | `note` |
| publish | `eka publish <target>` | `publish` |
| view | `eka view <projection>` (human projection) | — |

Reality check: the MCP column cites only tools the eka-mcp binary exposes today; `—` means no MCP tool exists yet for that primitive. MCP citations are gated to the mcp-production milestone — re-verify this table against the server's tool list before relying on the MCP transport.

## Skill loading

This body references EKA skills by name (`eka-orientation`, `eka-knowledge-authoring`, …) — as imperative loads and as inline citations alike. Every such reference resolves through this load-order protocol: take the first step that yields the skill, never skip ahead, and never treat a missing skill as a blocker.

1. **Installed skill directory (primary path).** If the pack is installed on disk (`eka-mcp configure --with-skills` or `eka-mcp install skills`), read the skill's `SKILL.md` from its installed directory — `<install-dir>/<name>/SKILL.md`; detect the installation directory from the workspace, never assume one. Found → use it, stop here.
2. **MCP resource (secondary path).** Otherwise, if an eka-mcp server is connected, read the resource `eka://skills/<name>` (`text/markdown`): it serves the embedded SKILL.md verbatim, and the resource listing carries each skill's frontmatter description for discovery. Found → use it, stop here.
3. **Inline hard rules (fallback).** If neither path yields the skill, proceed without it: this body is self-sufficient — the Hard rules section below carries the non-negotiable behavior, and the transport-primitive table above carries the full CLI/MCP surface. Degrade explicitly: state once which skills were unavailable and that the run proceeds on inline rules alone — never silently, and never invent a skill's guidance.

## Execution mode

EKA is a **solo-author** knowledge system: the primary agent performs **every** step itself — orchestration, implementation and review — inline. This is the pack's single `mode: solo` execution path; agent-to-agent handoff and role mapping are not part of EKA.

- Implementation happens inline in each item's isolated worktree under the branch/worktree rules below — the isolation contract is unchanged by who executes.
- Review is an explicit **self-review** against the full checklist (conformance; technical correctness; security where security-sensitive; product/experience where user-facing), applied in full and recorded as a `cmt` note stating `self-review` — a rubber-stamp pass without the checklist is a gate violation.

## Input

$1 — scope selector:

| Selector | Scope |
|---|---|
| *(empty)* | the project's active container |
| `ctr:<id>` | one container |
| `plan:<id>` | every container under the approved plan |
| `mvp` | the MVP scope: the approved scope (`scp-`) and its containers |
| `full` | the whole approved roadmap |
| `resume` | resume an interrupted execution (checkpoint protocol, Phase 4) |

Invalid selector → stop and report.

## Phase 0 — Intake (authority from the store, not documents)

1. `sync` (precondition), then `status`.
2. Read the planning authority from the canonical store:
   - `domain` over Planning — approved plans (`planningState: approved`)
   - `domain` over Containers — container states and plan locks
   - `domain` over Execution — work items and their execution states
3. Readiness gates (blocking — stop with a report, never invent scope):
   - the plan(s) in scope are approved; a plan is locked immutable by container activation;
   - the container is active — or activate it: `transition ctr:<id> active` (gated on the exactly-one-active rule and the depends-on plan being approved; activation locks the plan atomically);
   - the work items in scope are `planned` **and ticketed to the container** — each item has a `tkt-` ticket deriving from the container and the item (the membership contract; see "Execution membership" in `eka-knowledge-authoring`). Items without tickets are a **planning gap** (not a container member: absent from the board and invisible in the `view` projection): stop, report the gap, and route back to `/eka-discuss` — never create or link tickets at intake (authority comes from the store).

## Phase 1 — Execution plan

Build the plan:

- ordered item list with dependencies (`get <item>` upstream), critical path, parallelization candidates (same-wave items with all dependencies satisfied and no overlapping files), decision-required items;
- review set per item: implementation by domain (Go/CLI/backend, UI, infrastructure, documentation-only) + QA + code review + security (when security-sensitive) + product (for user-facing items) — every step performed inline by the primary (`mode: solo`);
- present the plan; do not wait for confirmation (autonomous run).

## Branch and worktree strategy

Every item is implemented in **its own branch in a dedicated worktree** — nothing is ever committed to the development branch directly, and the primary checkout stays read-only for implementation.

1. **Development branch** — the integration branch (default `develop`; the repository convention may differ — detect it, never assume). All item branches are created from it and merged back into it.
2. **Per-item branch** — created from the **latest development branch snapshot** at item start: `git worktree add <path> -b <branch> <develop>`, branch name following the repository convention (default `feat/<type>-<id>`, e.g. `feat/sto-publish-post`). Every item gets its own branch and worktree; parallel items never share one.
3. **Worktree location** — outside the primary checkout (repository convention; default `<repo>/../worktrees/<branch>` or the configured worktree root). The worktree carries `eka.yaml` (committed), so EKA commands work inside it: identity-based resolution (ADR-017) resolves worktrees to the same registered repository, and the canonical store is machine-wide shared — `context`/`get` inside a worktree always see the authoritative knowledge, never a stale branch copy. The snapshot directory inside a worktree may lag the store (transport copy); it is harmless and refreshed on the next `sync` push.
4. **Keep current** — before merging, the item branch must be up to date with the development branch: rebase (or merge) the latest `develop` into the item branch, resolve conflicts within the item's scope, re-run build/tests/quality gates.
5. **Merge back to the development branch** — via PR/MR when the repository workflow uses one, else direct merge, following repository conventions. The merge happens **after team review sign-off and before the `done` transition** — the knowledge state must never claim `done` before the code is on the development branch.
6. **Cleanup** — after the merge, remove the worktree (`git worktree remove`) and the item branch; record both in the checkpoint.

## Phase 2 — Container priming (once per container)

When a container's execution starts (first item of the container, or a resume that finds unprimed items), transition **every** `planned` work item of the container to `todo`:

- for each item of the container with `executionState: planned`: `transition <line> todo`;
- idempotent: skip items already `todo` / `in-progress` / `done` / `canceled`.

Semantics: `todo` = **queued for this container's execution**; `planned` = not yet scheduled. After priming, no item of the active execution remains `planned`. This is the `planned → todo` step of the D1 table — batch it once at container start, never per item.

## Phase 2 — Item loop

For each work item in order (parallel batches only when files do not overlap; never more parallel items than can be safely reviewed):

1. **Idempotent gate** — `get <ns>/<type>:<id>`: verify the ACTUAL state, never checkpoint assumptions.
   - `planned` → container not primed yet (old checkpoint / interrupted before priming): run the container priming step first, then `todo` → `in-progress` below;
   - `todo` → next item: `transition <line> in-progress` **before implementing** — `in-progress` marks the item as actively worked, so it must be set at work start, never as a blip after the work is done;
   - `in-progress` → resume mid-item (Phase 4);
   - `done` / `canceled` → skip (never re-execute).
2. **Context** — `context <line>` at engineering depth: the constraints in force for the item (dependencies, decisions, planning sections).
3. **Implement** the item inline (`mode: solo`) — **mandatory: dedicated branch + worktree per item** (see Branch and worktree strategy): create the worktree from the latest development branch snapshot, work only inside it, resolve conflicts against the latest `develop`, re-run quality gates after every integration, and never touch the primary checkout. The work brief MUST include: the item identity, the context object, the acceptance criteria from the item's content, the branch name, the worktree path, and the worktree/branch conventions.
4. **Evidence** — record the work as a `cmt` note: `note <line> --role implementation`, fill `summary`/`changes`/`tests`, set `noteState: resolved`, then `publish`.
5. **Self-review** — after the implementation note is published (resolved): QA + code review minimum; product/security review per item class (same loop as the sprint-execute convention). Every review is an explicit **self-review** by the primary against the same checklist, applied in full (findings listed verbatim, including blocking ones), and recorded as `cmt` notes (`--role review`) whose summary states `self-review (mode: solo)`. A rubber-stamp pass without the checklist violates the gate; fix requests are addressed in the same worktree.
6. **Advance the state machine**:
   - `transition <line> in-review` — the item is already `in-progress` from the gate step, so `in-progress → in-review` is a D1-legal step; gated on the resolved implementation note;
   - after review sign-off: **merge the item branch into the development branch** (PR/MR or direct merge per repository convention), then `transition <line> done` — gated on every note resolved, and only after the merge landed (knowledge must never claim `done` before the code is on the development branch).
   - Never force: `--force` confirms the active-container warning only; it never bypasses a gate.
7. **Synchronize** — `sync` push (refresh the repository snapshot with the new states); for parallel batches, pull the development branch before each next batch (never start work on a stale snapshot); after every merge, `git pull` the development branch in the primary checkout and clean up the item's worktree.
8. **Checkpoint** — append to `.eka/execution-state.md` (Phase 4) after EVERY item, then dual-write the canonical snapshot: publish `<ns>/ses:execution-state` (project/namespace from the repo's `eka.yaml`) with the same scope/current/next/items/mode/decisions — this is what `eka status` renders in its `Execution` section.

## Phase 3 — Container close

When every item of the container is `done`/`canceled`: `transition ctr:<id> completed` (gated). The next container may then activate. Repeat until the scope is exhausted.

## Phase 4 — Resume protocol

### Principles

1. **State on disk, not in context.** The EKA store (item states), git (work), and the checkpoint file (position) are the truth. Conversation memory is disposable.
2. **Resume = re-derive, never remember.** `get` and `context` are deterministic (identical input → identical object, ADR-021). A resume reconstructs the working context from the store + checkpoint with zero dependency on the previous session's transcript.
3. **Atomic unit = one work item.** At most one item is ever mid-flight; everything before it is durable.

### Checkpoint file

`.eka/execution-state.md` at the repository root is the fast local checkpoint — **operational state, not knowledge**: outside `docs/`, never scanned by `validate`. It is dual-written to the canonical snapshot `<ns>/ses:execution-state` (ADR-037, project-scoped under the repo's own namespace from `eka.yaml`): after every checkpoint append, publish the ses CKO (`eka new <ns>/ses:execution-state --project <proj> --namespace <ns>`, fill scope/current/next/items/mode/decisions plus `context`/`notes`/`verification`, `eka publish`) so `eka status` and other devices see the same position via `sync push`. The markdown stays the resume source when the store is unreachable; the ses line (immutable `instanceVersion` per update) is what `eka status` renders — latest only, full history via `eka get <ns>/ses:execution-state --timeline`.

```markdown
# Execution State
scope: <selector>
updated: <date>
status: running | paused | interrupted | completed
mode: solo
items:
- <ns>/<type>:<id>: done            # every completed item (instance versions in the store)
- <ns>/<type>:<id>: in-progress     # at most one — the item actively worked
- <ns>/<type>:<id>: todo            # queued (primed at container execution start)
current:
  item: <line>
  phase: implemented | review | gate | merged
  worktree: <path> <branch>
  pending: <what remains in this item>
next: <the item after current>
decisions:
- <decision + rationale>
resume: <exact commands to resume from here>
```

### Interruption handling

| Event | Detection | Recovery |
|---|---|---|
| clean pause (user stops) | checkpoint `status: paused` | `/eka-execute resume` → re-derive → continue at `current` |
| credit/context-window kill | checkpoint exists; `current` item `in-progress` in the store | resume: verify worktree + re-run gates → finish `current.pending` → review → transition → checkpoint |
| hard crash (laptop/internet) | checkpoint `status: interrupted`, or no checkpoint but the store shows an in-progress item | reconstruct position from the store (`get` states) + git branches; verify repo integrity (`git status`, tests); decide finish/revert per item (PM decision, recorded); write a fresh checkpoint before continuing |
| no checkpoint at all | store shows only `todo`/`done` (or `planned` if the crash hit before priming) | position derivable entirely from the store — resume = fresh intake (Phase 0) plus the priming step (idempotent) plus the skip-done rules |

### Resume rules

- Never re-execute a `done` item; never double-transition — the D1 refusal (`not in the D1 table`) is confirmation, not an error.
- Items still `planned` → run the container priming step (idempotent) before continuing; items `todo` are queued and picked in order.
- Worktree gone → recreate it from the item branch (the branch strategy's per-item branch is the durable state: `git worktree add <path> <branch>`); branch gone → decide: re-implement from `current.pending` or abandon (`transition <line> canceled`) — PM decision, recorded.
- After resume, always re-run the item's gates before advancing the state machine.
- Every interrupted run must leave a checkpoint that makes the next resume deterministic.

## Phase 5 — Final report

- scope executed; items transitioned (with instance versions); gates passed; decisions made (with rationale); team review summary;
- the execution mode (`mode: solo`) and the explicit self-review records;
- repository state (snapshot digest, worktrees cleaned up, `sync` push result);
- checkpoint path and status;
- recommended next scope (`/eka-execute plan:<id>` / `ctr:<id>` / `mvp`), and any planning gaps found (deferred items, missing evidence) — never silently expand scope.

## Hard rules

- Authority comes from the store: never execute unapproved work; never invent scope.
- State changes only via `transition`; never edit published objects; never write the store by hand.
- Gates are runtime-enforced: `--force` never bypasses them.
- **Every item is implemented in its own branch + worktree, created from the latest development branch snapshot; nothing is committed to the development branch directly; the merge to the development branch (PR/MR or direct, per repository convention) happens before the `done` transition.**
- Per-item review (minimum `qa` + `code-review` sign-off) before `done`, performed as an explicit, recorded self-review (`mode: solo`).
- The primary agent is the only party that talks to the user.
- Checkpoint after EVERY item: a crash may lose at most one item's mid-flight state.
- `sync` push after transitions so snapshots carry the new states.
- The primary agent performs every implementing and review step itself, inline (`mode: solo`), under the isolation rules above; it states the mode in every preamble and checkpoint and records reviews as explicit self-reviews.
