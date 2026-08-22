# EKA Agent Commands

The command layer of the EKA AI Skill Pack: two **custom commands** that turn the pack's skills into invocable agent workflows.

| Command | File | Purpose | Touches source code? |
|---|---|---|---|
| `/eka-discuss` | [`eka-discuss.md`](eka-discuss.md) | interactive planning discussion through the EKA flow: idea → discuss → propose → validate → approved → publish; creates knowledge drafts, delegates review through the role contract | **never** |
| `/eka-execute` | [`eka-execute.md`](eka-execute.md) | autonomous execution of approved planning (MVP scope first; selectors for container/plan/full); PM-style orchestration with the EKA state machine, team review, checkpoints, and a resume protocol; every item is implemented in its own branch + worktree created from the development branch snapshot, merged back to the development branch (PR/MR or direct merge) before the `done` transition | yes — via delegated roles |

The two commands are the two halves of one cycle: **plan in EKA, execute from EKA**. `/eka-discuss` produces approved knowledge; `/eka-execute` consumes it — the store is the handoff, no documents to reconcile.

## Why commands, not skills

Skills teach *how to behave* and are loaded on demand by the agent itself. Commands are *user-invoked workflows*: the user types `/eka-discuss` in the TUI and a complete workflow starts. The boundary from [the pack README](../README.md) holds: the commands are behavior, the CLI is the execution interface, the Runtime is the knowledge store.

## Installation

The commands are embedded in the `eka-mcp` binary (the pack-distribution vehicle per ADR-030). The official installation path is the plugin's configure surface:

```sh
eka-mcp configure --target opencode --with-commands   # install eka-discuss.md + eka-execute.md
eka-mcp configure --target opencode --with-all        # skills + commands + MCP client config
```

`configure` supports `--target opencode|claude|codex` (default `opencode`), `--dir <path>` for an explicit directory, and `--dry-run` to preview the plan. Without a `--with-*` flag nothing is copied — the pack stays reachable as MCP resources (`eka://skills/*`, `eka://templates/*`). Re-running after an upgrade refreshes the installed files.

Manual copy remains a valid fallback:

```sh
mkdir -p ~/.config/opencode/commands
cp skills/commands/eka-discuss.md skills/commands/eka-execute.md ~/.config/opencode/commands/
# or project-scoped:
mkdir -p .opencode/commands
cp skills/commands/*.md .opencode/commands/
```

Then run `/eka-discuss` or `/eka-execute` in the agent's TUI. The canonical bodies carry **description-only frontmatter**; provider-specific frontmatter is rendered at install time per target. The primary orchestrating agent runs the command; delegation happens through the role contract inside the command body.

## How commands load skills

Both command bodies reference pack skills by name. Every such reference resolves through a single load-order protocol (identical in both bodies), so file installation stays the primary delivery path while the MCP server remains a full fallback — and the commands keep working when **nothing** is installed:

1. **Installed skill directory** — read `<install-dir>/<name>/SKILL.md` from the pack installed by `eka-mcp configure --with-skills` / `eka-mcp install skills`.
2. **MCP resource** — read `eka://skills/<name>` from a connected eka-mcp server (`text/markdown`; serves the embedded SKILL.md verbatim, with each skill's frontmatter description in the resource listing).
3. **Inline hard rules** — if neither path yields the skill, proceed without it: each body carries its own Hard rules and transport-primitive table, and states explicitly which skills were unavailable (never a silent degrade).

## Provider mapping

The command **bodies are provider-agnostic**: canonical frontmatter is description-only, and delegation is expressed exclusively through the role contract (nine closed roles — architect, backend, frontend, security-review, code-review, product-review, qa, devops, documenter). Provider-specific frontmatter and the per-ecosystem role→agent mapping are applied at install time per target:

| Provider | Mechanism | Adaptation |
|---|---|---|
| **opencode** | `commands/*.md` with frontmatter (`description`, `agent`) | canonical body + rendered frontmatter at install |
| **Claude Code** | skills (`.claude/skills/<name>/SKILL.md`) or legacy `.claude/commands/<name>.md` — both create `/name` | keep the body; frontmatter: `description`, `disable-model-invocation: true` (user-invoked only), optional `context: fork` + `agent:` to run the command in a subagent context |
| **Codex** | `codex.jsonc` custom prompts → `/prompts:<name>` | keep the body as the `prompt` value; `description` in the prompts entry. For execution resume, Codex's `/goal` (persisted objective + pause/resume) complements the checkpoint protocol |

Roles are duties, not agent names: each ecosystem's install-time mapping table resolves a role to that ecosystem's agents. Both bodies carry an identical **Delegation mode** section: before any delegation attempt the primary resolves the rows from the installed `DELEGATION.txt` sidecar (else the pack's mapping table) — all-`solo` rows ⇒ `mode: solo`, any named agent ⇒ `mode: delegated` — and records the resolved mode in the session preamble and checkpoints, never silently. In `mode: solo` the primary performs every role inline (analysis roles as labeled perspectives; implementing roles under the same branch/worktree isolation rules, with review as explicit, recorded self-review); in `mode: delegated` behavior is unchanged.

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
- The commands reference skills by name (`eka-orientation`, `eka-adoption`, …) — every reference resolves through the [load-order protocol](#how-commands-load-skills): installed skill dir first, then the `eka://skills/<name>` MCP resource, else the body's inline hard rules. Installing the pack's skills alongside the commands keeps the full behavioral depth; without them the commands still work on inline rules alone.
