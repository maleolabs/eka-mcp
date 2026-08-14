# EKA Agent Commands

The command layer of the EKA AI Skill Pack: two **custom commands** that turn the pack's skills into invocable agent workflows.

| Command | File | Purpose | Touches source code? |
|---|---|---|---|
| `/eka-discuss` | [`eka-discuss.md`](eka-discuss.md) | interactive planning discussion through the EKA flow: idea → discuss → propose → validate → approved → publish; creates knowledge drafts, delegates review to sub-agents | **never** |
| `/eka-execute` | [`eka-execute.md`](eka-execute.md) | autonomous execution of approved planning (MVP scope first; selectors for container/plan/full); PM-style orchestration with the EKA state machine, team review, checkpoints, and a resume protocol; every item is implemented in its own branch + worktree created from the development branch snapshot, merged back to the development branch (PR/MR or direct merge) before the `done` transition | yes — via delegated sub-agents |

The two commands are the two halves of one cycle: **plan in EKA, execute from EKA**. `/eka-discuss` produces approved knowledge; `/eka-execute` consumes it — the store is the handoff, no documents to reconcile.

## Why commands, not skills

Skills teach *how to behave* and are loaded on demand by the agent itself. Commands are *user-invoked workflows*: the user types `/eka-discuss` in the TUI and a complete workflow starts. The boundary from [the pack README](../README.md) holds: the commands are behavior, the CLI is the execution interface, the Runtime is the knowledge store.

## Installation (opencode)

The commands are embedded in the `eka` binary (ADR-023) — the official installation path:

```sh
eka install skills             # install the ten eka-* skills (the commands reference them by name)
eka install commands           # install eka-discuss.md + eka-execute.md
```

`eka install commands` auto-detects the agent configuration directory (opencode/claude); `--target opencode` forces a target, `--dir <path>` installs explicitly (project-scoped: `.opencode/commands/`), `--dry-run` previews the plan. Re-running after a CLI upgrade refreshes the installed files.

Manual copy remains a valid fallback:

```sh
mkdir -p ~/.config/opencode/commands
cp skills/commands/eka-discuss.md skills/commands/eka-execute.md ~/.config/opencode/commands/
# or project-scoped:
mkdir -p .opencode/commands
cp skills/commands/*.md .opencode/commands/
```

Then run `/eka-discuss` or `/eka-execute` in the opencode TUI. The `agent: alex` frontmatter routes the command to the primary orchestrating agent; sub-agent delegation happens through the task tool inside the command body.

## Provider mapping

The command **bodies are provider-agnostic**; the frontmatter is per-provider. The files ship with opencode frontmatter (the primary target); adapt for other agents:

| Provider | Mechanism | Adaptation |
|---|---|---|
| **opencode** | `commands/*.md` with frontmatter (`description`, `agent`) | as shipped |
| **Claude Code** | skills (`.claude/skills/<name>/SKILL.md`) or legacy `.claude/commands/<name>.md` — both create `/name` | keep the body; frontmatter: `description`, `disable-model-invocation: true` (user-invoked only), optional `context: fork` + `agent:` to run the command in a subagent context |
| **Codex** | `codex.jsonc` custom prompts → `/prompts:<name>` | keep the body as the `prompt` value; `description` in the prompts entry. For execution resume, Codex's `/goal` (persisted objective + pause/resume) complements the checkpoint protocol |

The agent-name references inside the bodies (e.g. `alex-qa`, `althea-product-specialist`) are opencode agent names — map them to the equivalent sub-agents of the target ecosystem.

## The session-context pattern (caveman reference)

The `caveman` skill is a *communication-mode* skill: it injects a persistent communication style at session start. The pattern worth borrowing is the **session-start context injection** — not the content. `/eka-discuss` applies the same pattern to EKA: preflight injects the EKA context (environment state via `eka status`/`eka sync`, the mental model via `eka-orientation`, the subject's constraints via `eka context`) before any discussion happens. The agent starts the session already grounded in the knowledge store — the context-first principle of the pack, operationalized as a command.

## The resume architecture (`/eka-execute`)

The execution command must survive interruptions (credit/context limits, network loss, machine shutdown) without losing context. The architecture, in three rules:

1. **State on disk, not in context.** Item states live in the EKA store (moved by `eka transition`), work lives in git worktrees, position lives in `.eka/execution-state.md`. Conversation memory is disposable.
2. **Resume = re-derive, never remember.** `eka get`/`eka context` are deterministic — a resume reconstructs the working context from the store + checkpoint with zero dependency on the previous transcript. This is the property that makes EKA uniquely suited for resumable execution: the context object for a subject is byte-stable, so "rebuild context" is a command, not a memory.
3. **Atomic unit = one work item.** At most one item is mid-flight; a crash loses at most that item's pending work (recoverable from the checkpoint's `current.pending` sub-state).

The checkpoint file is **operational state, not knowledge** — deliberately outside `docs/`, so it is never scanned by `eka validate` and never becomes canonical. The knowledge counterpart (what items are done, in what order, with what evidence) lives in the store itself via transitions and notes.

## Maintenance

- The command bodies are the canonical content; frontmatter changes per provider. When the pack's skills or the CLI surface evolve, update the bodies to match (the skills' own reality-check rule applies: commands only reference commands that exist — verify with `eka --help`).
- The commands reference skills by name (`eka-orientation`, `eka-adoption`, …) — install the pack's skills alongside the commands; without the skills the commands still work but lose the behavioral depth.
