---
description: Execute approved EKA planning autonomously — MVP scope first, selectors for container/plan/full roadmap. PM-style orchestration: delegate work items to sub-agents in isolated worktrees, enforce the EKA state machine via eka transition (gates runtime-enforced), team review after every item, checkpoint after every item, and resume from interruptions without losing context.
agent: alex
---

# EKA Execution

Execute **approved** EKA planning autonomously, PM-style. The primary agent orchestrates; it never implements engineering work itself. Every work item is delegated to sub-agents, the EKA state machine is enforced through `eka transition` (R13 gates are runtime-enforced), and a checkpoint is written after **every** item so execution can resume after interruptions (credit/context limits, network loss, machine shutdown) without losing context.

The user interacts exactly twice: at invocation (scope selection) and at the final report. Mid-run interruptions resume via the checkpoint protocol — no manual re-commanding of items.

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

1. `eka sync` (precondition), `eka status`.
2. Read the planning authority from the canonical store:
   - `eka get planning` — approved plans (`planningState: approved`)
   - `eka get containers` — container states and plan locks
   - `eka get execution` — work items and their execution states
3. Readiness gates (blocking — stop with a report, never invent scope):
   - the plan(s) in scope are approved; a plan is locked immutable by container activation;
   - the container is active — or activate it: `eka transition ctr:<id> active` (gated on the exactly-one-active rule and the depends-on plan being approved; activation locks the plan atomically);
   - the work items in scope are `planned` **and ticketed to the container** — each item has a `tkt-` ticket deriving from the container and the item (the membership contract; see "Execution membership" in `eka-knowledge-authoring`). Items without tickets are a **planning gap** (`unassigned` on the board, invisible in `eka view execution`): stop, report the gap, and route back to `/eka-discuss` — never create or link tickets at intake (authority comes from the store).

## Phase 1 — Execution plan

Build the plan:

- ordered item list with dependencies (`eka get <item> --upstream`), critical path, parallelization candidates (same-wave items with all dependencies satisfied and no overlapping files), decision-required items;
- delegation set per item: implementer by domain (`alex-backend` Go/CLI/backend, `alex-frontend` UI, `alex-devops` infrastructure, `alex-documenter` documentation-only) + QA (`alex-qa`) + code review (`alex-reviewer`) + security (`alex-security` when security-sensitive) + product (`althea-product-specialist` for user-facing items);
- present the plan; do not wait for confirmation (autonomous run).

## Branch and worktree strategy

Every item is implemented in **its own branch in a dedicated worktree** — nothing is ever committed to the development branch directly by a sub-agent, and the primary checkout stays read-only for implementation.

1. **Development branch** — the integration branch (default `develop`; the repository convention may differ — detect it, never assume). All item branches are created from it and merged back into it.
2. **Per-item branch** — created from the **latest development branch snapshot** at item start: `git worktree add <path> -b <branch> <develop>`, branch name following the repository convention (default `feat/<type>-<id>`, e.g. `feat/sto-publish-post`). Every item gets its own branch and worktree; parallel items never share one.
3. **Worktree location** — outside the primary checkout (repository convention; default `<repo>/../worktrees/<branch>` or the configured worktree root). The worktree carries `eka.yaml` (committed), so EKA commands work inside it: identity-based resolution (ADR-017) resolves worktrees to the same registered repository, and the canonical store is machine-wide shared — `eka context`/`eka get` inside a worktree always see the authoritative knowledge, never a stale branch copy. The snapshot directory inside a worktree may lag the store (transport copy); it is harmless and refreshed on the next `eka sync push`.
4. **Keep current** — before merging, the item branch must be up to date with the development branch: rebase (or merge) the latest `develop` into the item branch, resolve conflicts within the item's scope, re-run build/tests/quality gates.
5. **Merge back to the development branch** — via PR/MR when the repository workflow uses one, else direct merge, following repository conventions. The merge happens **after team review sign-off and before the `done` transition** — the knowledge state must never claim `done` before the code is on the development branch.
6. **Cleanup** — after the merge, remove the worktree (`git worktree remove`) and the item branch; record both in the checkpoint.

## Phase 2 — Container priming (once per container)

When a container's execution starts (first item of the container, or a resume that finds unprimed items), transition **every** `planned` work item of the container to `todo`:

- for each item of the container with `executionState: planned`: `eka transition <line> todo`;
- idempotent: skip items already `todo` / `in-progress` / `done` / `canceled`.

Semantics: `todo` = **queued for this container's execution**; `planned` = not yet scheduled. After priming, no item of the active execution remains `planned`. This is the `planned → todo` step of the D1 table — batch it once at container start, never per item.

## Phase 2 — Item loop

For each work item in order (parallel batches only when files do not overlap; never more parallel items than can be safely reviewed):

1. **Idempotent gate** — `eka get <ns>/<type>:<id>`: verify the ACTUAL state, never checkpoint assumptions.
   - `planned` → container not primed yet (old checkpoint / interrupted before priming): run the container priming step first, then `todo` → `in-progress` below;
   - `todo` → next item: `eka transition <line> in-progress` **before delegating** — `in-progress` marks the item as actively worked, so it must be set at work start, never as a blip after the work is done;
   - `in-progress` → resume mid-item (Phase 4);
   - `done` / `canceled` → skip (never re-execute).
2. **Context** — `eka context <line> --depth engineering --json`: the constraints in force for the item (dependencies, decisions, planning sections).
3. **Delegate** to the implementer sub-agent — **mandatory: dedicated branch + worktree per item** (see Branch and worktree strategy): the sub-agent creates the worktree from the latest development branch snapshot, works only inside it, resolves conflicts against the latest `develop`, re-runs quality gates after every integration, and never touches the primary checkout. The delegation prompt MUST include: the item identity, the context object, the acceptance criteria from the item's content, the branch name, the worktree path, and the worktree/branch conventions.
4. **Evidence** — the implementer records the work as a `cmt` note: `eka note <line> --role implementation`, fill `summary`/`changes`/`tests`, set `noteState: resolved`, `eka publish`.
5. **Team review** — after the implementation note is published (resolved): QA + code review minimum; product/UX/security review per item class (same loop as the sprint-execute convention). Review verdicts are recorded as `cmt` notes (`--role review`). Fix requests are routed back to the implementing sub-agent in the same worktree.
6. **Advance the state machine**:
   - `eka transition <line> in-review` — the item is already `in-progress` from the gate step, so `in-progress → in-review` is a D1-legal step; gated on the resolved implementation note;
   - after review sign-off: **merge the item branch into the development branch** (PR/MR or direct merge per repository convention), then `eka transition <line> done` — gated on every note resolved, and only after the merge landed (knowledge must never claim `done` before the code is on the development branch).
   - Never force: `--force` confirms the active-container warning only; it never bypasses a gate.
7. **Synchronize** — `eka sync push` (refresh the repository snapshot with the new states); for parallel batches, pull the development branch before each next batch (never start work on a stale snapshot); after every merge, `git pull` the development branch in the primary checkout and clean up the item's worktree.
8. **Checkpoint** — append to `.eka/execution-state.md` (Phase 4) after EVERY item.

## Phase 3 — Container close

When every item of the container is `done`/`canceled`: `eka transition ctr:<id> completed` (gated). The next container may then activate. Repeat until the scope is exhausted.

## Phase 4 — Resume protocol

### Principles

1. **State on disk, not in context.** The EKA store (item states), git (work), and the checkpoint file (position) are the truth. Conversation memory is disposable.
2. **Resume = re-derive, never remember.** `eka get` and `eka context` are deterministic (identical input → identical object, ADR-021). A resume reconstructs the working context from the store + checkpoint with zero dependency on the previous session's transcript.
3. **Atomic unit = one work item.** At most one item is ever mid-flight; everything before it is durable.

### Checkpoint file

`.eka/execution-state.md` at the repository root — **operational state, not knowledge**: outside `docs/`, never scanned by `eka validate`, never synced into the store.

```markdown
# Execution State
scope: <selector>
updated: <date>
status: running | paused | interrupted | completed
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
| hard crash (laptop/internet) | checkpoint `status: interrupted`, or no checkpoint but the store shows an in-progress item | reconstruct position from the store (`eka get` states) + git branches; verify repo integrity (`git status`, tests); decide finish/revert per item (PM decision, recorded); write a fresh checkpoint before continuing |
| no checkpoint at all | store shows only `todo`/`done` (or `planned` if the crash hit before priming) | position derivable entirely from the store — resume = fresh intake (Phase 0) plus the priming step (idempotent) plus the skip-done rules |

### Resume rules

- Never re-execute a `done` item; never double-transition — the D1 refusal (`not in the D1 table`) is confirmation, not an error.
- Items still `planned` → run the container priming step (idempotent) before continuing; items `todo` are queued and picked in order.
- Worktree gone → recreate it from the item branch (the branch strategy's per-item branch is the durable state: `git worktree add <path> <branch>`); branch gone → decide: re-implement from `current.pending` or abandon (`eka transition <line> canceled`) — PM decision, recorded.
- After resume, always re-run the item's gates before advancing the state machine.
- Every interrupted run must leave a checkpoint that makes the next resume deterministic.

## Phase 5 — Final report

- scope executed; items transitioned (with instance versions); gates passed; decisions made (with rationale); team review summary;
- repository state (snapshot digest, worktrees cleaned up, `eka sync push` result);
- checkpoint path and status;
- recommended next scope (`/eka-execute plan:<id>` / `ctr:<id>` / `mvp`), and any planning gaps found (deferred items, missing evidence) — never silently expand scope.

## Hard rules

- Authority comes from the store: never execute unapproved work; never invent scope.
- State changes only via `eka transition`; never edit published objects; never write the store by hand.
- Gates are runtime-enforced: `--force` never bypasses them.
- **Every item is implemented in its own branch + worktree, created from the latest development branch snapshot; nothing is committed to the development branch directly by a sub-agent; the merge to the development branch (PR/MR or direct, per repository convention) happens before the `done` transition.**
- Team review (minimum QA + code review sign-off) before `done`.
- Sub-agents never ask the user — they escalate to the primary agent (their question tool is denied).
- Checkpoint after EVERY item: a crash may lose at most one item's mid-flight state.
- `eka sync push` after transitions so snapshots carry the new states.
- The primary agent never implements engineering work itself. It orchestrates.
