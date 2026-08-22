# Skill Installation & Discovery

How an AI agent gets the EKA Skill Pack, how the skills are discovered, and what the environment must look like before the skills can work.

## 1. Prerequisites: the `eka` CLI and a synced workspace

The Skill Pack is a behavior layer over the **`eka` CLI** — it does not ship its own tooling and never touches the runtime directly. Install order:

1. **Install the CLI** — Linux/macOS: `curl -fsSL https://github.com/maleolabs/engineering-knowledge-architecture/releases/latest/download/install.sh | sh` (or build from source with Go 1.24+: `go build -o eka ./cmd/eka`; Windows has a PowerShell installer). Full instructions: [README Installation](../README.md#installation).
2. **Verify** — `eka version` prints the CLI build version and the EKA standard version implemented. `eka --help` is the authoritative command list; **if a skill references a command your `eka` does not have, the CLI's help wins** — inspect `eka help <command>` before substituting anything.
3. **Prepare the workspace** — runtime commands need an EKA Workspace (`~/.eka` or `$EKA_HOME`) and a **registered, synced repository**:
   ```sh
   eka project register <repo-path>   # identity comes from <repo-path>/eka.yaml
   eka sync <repo-path>               # pull knowledge into the canonical store
   eka status                         # confirm: projects, objects, last sync
   ```
   `eka sync` is the **one-time registration/seed** of the repository — registration persists, and every `eka get` / `eka context` / `eka view` / `eka transition` call the skills teach then runs directly on the shared store, with no per-read sync. Full `eka sync` / `eka sync pull` re-seeds the store from the repository snapshot (or docs tree) and can overwrite newer store states with older instances — inside an active execution use `eka sync push` only. A repository without `eka.yaml` is not an EKA repository (`eka init` creates the identity file).

## 2. Installing the Skill Pack

The pack is embedded in the **`eka-mcp` binary**; per ADR-030 the pack-distribution vehicle owns installation, so the official path is the plugin's `configure` surface (`eka install` no longer exists):

```sh
eka-mcp configure --target opencode --with-skills --json   # installs the eleven eka-* skills
eka-mcp configure --target opencode --with-all --json      # skills + commands + MCP client config
```

- Targets: `opencode` (`<base>/.config/opencode/{skills,commands}`), `claude` (`<base>/.claude/{skills,commands}`), `codex` (`<base>/.agents/skills` — skills only). Default target is `opencode`.
- `--dir <path>` anchors an explicit directory (project-scoped installs); `--dry-run` prints the plan (`create|overwrite|skip`) without writing.
- Without a `--with-*` flag nothing is copied — skills stay reachable as MCP resources (`eka://skills/*`, `eka://templates/*`).
- Re-running after an upgrade refreshes the installed files, overwriting only pack-owned files; foreign files are never touched.
- The installed pack's version equals the binary's pack version by construction — installs can never deliver a pack newer than the binary that ships it.

Manual copy remains a valid fallback (e.g. offline or non-binary builds):

| Agent ecosystem | Installation |
|---|---|
| **Claude Skills / SKILL.md-native runtimes** | copy the skill directories (`eka-orientation/`, `eka-project-understanding/`, …) into the runtime's skills directory; the `SKILL.md` frontmatter (`name`, `description`) is the discovery metadata. |
| **opencode / custom agent frameworks** | register each skill's `SKILL.md` (or its content) in the framework's skill format — the markdown body is the instruction payload. |
| **Prompt-pack / custom-instructions agents** | include the `docs/workflow.md` (the full workflow) plus the relevant skill bodies in the system prompt; keep the modular structure if the runtime supports sub-prompts. |
| **Any runtime** | at minimum, load [`README.md`](../README.md) (purpose + safety boundaries) and [`docs/workflow.md`](workflow.md) — those two files carry the non-negotiable behavior. |

Provider-agnostic contract: the **content** of the skills is the deliverable; the `SKILL.md` envelope is the widely adopted convention, not a vendor lock. Wrapping the content in another format changes nothing about the behavior defined.

The pack is versioned with the repository; update it the same way you update the repo (or copy fresh). The skills reference CLI behavior — keep CLI and pack versions aligned: [`manifest.yaml`](../manifest.yaml) declares `requiresEka` (the minimum CLI version the pack is written against); verify with `eka version` after CLI upgrades.

## 3. Discovery: how an agent finds the right skill

Each skill's frontmatter `description` is the discovery contract — agent runtimes match task descriptions against it. The pack's discovery principle: **load only the skill(s) the current task needs**; do not load all skills for all tasks.

| Task | Skill |
|---|---|
| Need the EKA mental model first | `eka-orientation` |
| Understand a project / what constrains a task | `eka-project-understanding` |
| Fetch specific knowledge | `eka-knowledge-retrieval` |
| Create new knowledge | `eka-knowledge-authoring` |
| Change existing knowledge | `eka-knowledge-modification` |
| Review knowledge or a change | `eka-knowledge-review` |
| Bring a non-EKA project into EKA (existing docs or none) | `eka-adoption` |
| Software-engineering work inside EKA | `eka-engineering-workflow` |
| A command refused, failed, or errored | `eka-troubleshooting` |
| Unsure which skill applies | `eka-router` |

Composition rules:

- `eka-router` is the fallback entry: it routes any task to the matching skill and stops.
- `eka-orientation` is the entry point for agents without EKA literacy; an already-literate agent can skip it.
- `eka-engineering-workflow` is the umbrella: tasks flow through it and delegate to the other skills.
- `eka-knowledge-modification` depends on `eka-knowledge-authoring` (revisions are new drafts); `eka-knowledge-review` is the validation companion of both; `eka-troubleshooting` is the fallback for any refusal/error.
- When in doubt: `eka-project-understanding` orients first, then the task-specific skill.

## 4. Workspace hygiene for agents

- **Use a dedicated `EKA_HOME` for experiments** — a scratch workspace (`EKA_HOME=/tmp/eka-demo`) keeps demo/validation runs away from the real store. Publishing drafts creates immutable objects in the workspace; `eka discard` removes unpublished drafts.
- **Never share the workspace store with the repository**: the store is machine-local (`~/.eka`); the repository carries only `eka.yaml`, the `docs/` tree (if used), and `exchange/snapshots/`. Commit snapshots through Git like any content — synchronization is explicit (`eka sync`), never automatic.
- **Read-only by default**: `eka get`, `eka context`, `eka view`, `eka validate`, `eka status`, `eka integrity check` never write anything. Writing paths are explicit: `eka new`/`eka publish`/`eka discard`/`eka transition`/`eka note` (workspace), `eka sync` (store + snapshot).

## 5. Installing the commands (optional)

The pack ships two user-invoked workflows in [`commands/`](../commands/README.md): `/eka-discuss` (planning discussion, no code) and `/eka-execute` (autonomous execution of approved planning with a resume protocol). They are opencode-ready; the bodies are provider-agnostic (Claude Code / Codex adaptation in the commands README).

```sh
eka-mcp configure --target opencode --with-commands --json   # official path — embedded in the eka-mcp binary (ADR-030)
# or manually:
cp skills/commands/eka-discuss.md skills/commands/eka-execute.md ~/.config/opencode/commands/
```

The commands reference the pack's skills by name — install the skills alongside them (`--with-skills` / `--with-all`).

## 6. Uninstall / updates

- **Remove the pack**: delete the copied skill directories; no runtime state is touched by the pack itself.
- **Remove the workspace**: `rm -rf ~/.eka` (or the `EKA_HOME` directory) — the repository and its snapshots remain; a fresh workspace re-syncs from the snapshot.
- **Updates**: re-copy the pack; re-run `eka sync` after CLI upgrades if the store schema changed.

## 7. Limitations

- The pack drives the CLI only — there is **no MCP and no SDK wiring** in this milestone. Agents that need a programmatic transport should wait for the [MCP boundary](mcp-boundary.md) milestone (skills then sit unchanged on top of it).
- Skills do not parse the canonical store, do not read SQLite, and do not hand-verify hashes — all verification is delegated to `eka validate` / `eka integrity check`.
- `eka edit` is interactive (TTY) — agent workflows edit draft files directly or use `--content-file`.
- Workspace-native published objects (`source_repo = "runtime"`) never enter repository snapshots; if a change must travel, author through the repository's docs tree + `eka sync`, or export/import the package.

## 8. Quick start checklist

```sh
# environment
eka version
eka project register reference/project   # example: the EKA Reference Project
eka sync reference/project
eka status

# pack sanity check against the reference project
eka validate reference/project           # 0 errors, 0 warnings
eka get feather/sto:publish-post:1 --no-content
eka context feather/adr:content-storage --depth engineering --json

# executable validation of the whole pack (scratch workspace, ~28 assertions)
EKA_PATH=eka skills/scripts/smoke-test.sh
```

All of the above are verified in [Reference Project Validation](reference-project-validation.md).
