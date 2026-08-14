# EKA AI Skill Pack

> The official EKA Skill Pack: modular skill definitions that teach AI coding agents how to work with an EKA-enabled project — what Engineering Knowledge is, how EKA organizes it, how to retrieve it, how to construct context, how to modify it safely, and how to preserve its invariants.
> Status: **experimental (v0.1)**. Companion to the [Knowledge Runtime Architecture](../reference/runtime-architecture.md) and the [Context Engine](../reference/decisions/adr-021-context-engine.md). Convention zone, not an EKA artifact (no `type`/`id`).
> Anchor decision: [ADR-022 — AI Skill Pack](../reference/decisions/adr-022-ai-skill-pack.md).

## What the Skill Pack is

A set of **skills** — self-contained instruction files an AI agent loads when a task matches their description. Each skill teaches one aspect of working with EKA: the mental model, project understanding, retrieval, authoring, modification, review, and the engineering workflow. Skills run on top of the **`eka` CLI**; they define *how the agent should behave*, the CLI is the *execution interface*, and the EKA Runtime is the *knowledge store*.

The Skill Pack is **provider-agnostic**: the skill files use the widely adopted `SKILL.md` convention (name + description frontmatter, markdown body). Any agent ecosystem that supports instruction files — Claude Skills, opencode skills, custom-agent prompt packs — can consume them directly or wrap the content in its own envelope. See [Installation & Discovery](docs/installation.md).

## The core separation

| Layer | Role | Owned by |
|---|---|---|
| **Knowledge** | the canonical Engineering Knowledge model (Identity, State, Content, Relationships, Classification) | EKA standard + Runtime |
| **Workflow** | how engineering work moves through the model (draft → validate → publish, transitions, review) | EKA conventions + this Skill Pack |
| **Tools** | the current execution interface (`eka` CLI) | this repository |
| **Protocol** | future transport layers (MCP, Atrium) | future milestones |
| **Skills** | how an AI agent should behave — the behavior layer, on top of tools | **this Skill Pack** |

Skills are **not** a substitute for MCP. Skills define behavior; MCP will expose Runtime capabilities as a transport. The intended evolution is documented in [The MCP Boundary](docs/mcp-boundary.md).

## Skill discovery

Do **not** load every skill for every task. Pick the skill whose description matches the current task. The description in each skill's frontmatter is the discovery contract — agent runtimes surface it automatically.

| When the task is… | Load this skill | It teaches |
|---|---|---|
| You are unsure which skill applies | [`eka-router`](eka-router/SKILL.md) | the routing decision tree — pick the matching skill, stop after routing |
| You are new to EKA / need the mental model before acting | [`eka-orientation`](eka-orientation/SKILL.md) | what EKA is, Engineering Domains, stratification, CKOs, immutability, Runtime vs Authoring |
| Understanding a project: what exists, what constrains what | [`eka-project-understanding`](eka-project-understanding/SKILL.md) | `eka status` / `eka sync` / `eka get` / `eka context` as the context-first workflow |
| Retrieving specific knowledge (one object, a domain, a container) | [`eka-knowledge-retrieval`](eka-knowledge-retrieval/SKILL.md) | `eka get` vs `eka context` vs `eka view`, identity forms, retrieval options |
| Creating new Engineering Knowledge (requirement, ADR, spec, work item…) | [`eka-knowledge-authoring`](eka-knowledge-authoring/SKILL.md) | draft → validate → publish, the JSON draft template, `eka new` / `eka publish` |
| Changing existing knowledge (state change, revision, correction) | [`eka-knowledge-modification`](eka-knowledge-modification/SKILL.md) | immutability, new revisions, `eka transition`, never-mutate rules |
| Reviewing knowledge for correctness, consistency, traceability | [`eka-knowledge-review`](eka-knowledge-review/SKILL.md) | `eka validate`, constraint checking via `eka context`, `eka integrity check` |
| Bringing a non-EKA project into EKA (existing docs or none) | [`eka-adoption`](eka-adoption/SKILL.md) | assessment, classification, migration paths, greenfield capture |
| Relating EKA to software development work (planning, tickets, delivery) | [`eka-engineering-workflow`](eka-engineering-workflow/SKILL.md) | the canonical-domain spine, methodology independence, the full loop |
| A command refused, failed, or errored | [`eka-troubleshooting`](eka-troubleshooting/SKILL.md) | refusal classes, exit codes, deterministic fixes |
| Reporting a shortcoming of EKA itself (bug, suggestion, rough edge) | [`eka-feedback`](eka-feedback/SKILL.md) | the feedback loop: `eka feedback new` → `publish` (ADR-026), quality bar |

Example mappings:

```
Project understanding         → eka-project-understanding
Architecture change           → eka-knowledge-retrieval + eka-knowledge-modification
Implementation planning       → eka-engineering-workflow
Ticket execution              → eka-knowledge-retrieval + eka-knowledge-modification
Knowledge review              → eka-knowledge-review
Command refused               → eka-troubleshooting
Non-EKA project adoption      → eka-adoption
EKA shortcoming found         → eka-feedback
```

## Pack structure

```
skills/
├── README.md                          this index: purpose, discovery, safety boundaries
├── manifest.yaml                      pack version + CLI compatibility + skill index
├── eka-router/SKILL.md                routing: pick the right skill per task
├── eka-orientation/SKILL.md           the EKA mental model before any command
├── eka-project-understanding/SKILL.md     obtain project context (context-first)
├── eka-knowledge-retrieval/SKILL.md       eka get / context / view — the retrieval surface
├── eka-knowledge-authoring/SKILL.md       create new knowledge: draft → validate → publish
│   └── templates/                         reference drafts per token family (derived from `eka new`)
│       ├── README.md                      how to read and regenerate the templates
│       └── drafts/<type>-template.json    26 scaffolds: owned state, dimension, content keys
├── eka-knowledge-modification/SKILL.md    change knowledge: new revisions, transitions
├── eka-knowledge-review/SKILL.md          review knowledge with EKA validation, never invented logic
├── eka-adoption/SKILL.md                  bring non-EKA projects into EKA (migrate or bootstrap)
├── eka-engineering-workflow/SKILL.md      EKA and software development, methodology-independent
├── eka-troubleshooting/SKILL.md           refusal classes, exit codes, deterministic fixes
├── eka-feedback/SKILL.md                  report EKA shortcomings: write + publish feedback (ADR-026)
├── scripts/
│   └── smoke-test.sh                  executable smoke test over the Reference Project
├── commands/                          user-invoked agent workflows (opencode-ready)
│   ├── README.md                      install, provider mapping, resume architecture
│   ├── eka-discuss.md                 planning discussion: idea → … → publish (no code)
│   └── eka-execute.md                 autonomous execution of approved planning + resume
└── docs/
    ├── installation.md                install the CLI, install and discover skills
    ├── workflow.md                    the full AI workflow: Understand → Context → Reason → Change → Validate → Publish
    ├── mcp-boundary.md                current vs future MCP architecture, boundary rules
    └── reference-project-validation.md  validation evidence against the EKA Reference Project
```

## CLI dependency

The Skill Pack drives the **`eka` CLI** — it is the current execution interface. Requirements:

- `eka` installed (Go 1.24+; see [Installation](../README.md#installation) and [docs/installation.md](docs/installation.md)).
- For runtime commands (`eka get`, `eka context`, `eka view`, `eka transition`, …): an **EKA Workspace** (`~/.eka` or `$EKA_HOME`) with the target repository **registered once** (`eka sync` at intake — registration persists; reads and transitions then run directly on the shared store, no per-read sync. Full `eka sync` / `eka sync pull` re-seeds the store from the snapshot/docs tree and can overwrite newer states with older instances — use `eka sync push` during active execution).
- Since ADR-023 the pack is **embedded in the `eka` binary**: `eka install skills` / `eka install commands` is the official installation path, and the installed pack version always equals the binary's pack version (no drift on the install path; manual copies can still drift — re-running install fixes them).

**Command reality check — the skills only reference commands that exist.** The command surface evolves; when uncertain, inspect it:

```sh
eka --help              # the complete command list
eka help <command>      # usage, flags, exit codes for one command
```

Never assume a flag exists because a skill mentions a sibling flag. If the local `eka` differs from this pack's version, the CLI's own help is authoritative. The pack's **version contract** lives in [`manifest.yaml`](manifest.yaml) (`requiresEka` — the minimum CLI version the pack is written against); check it against `eka version` after upgrades.

## AI safety boundaries

The Skill Pack establishes behavioral constraints that apply to **every** skill and **every** task. An AI agent MUST NOT:

1. **Modify EKA Runtime storage directly** — no direct edits to `eka.db`, `workspace.json`, or the workspace store. All knowledge changes go through the Authoring API surface: the draft-publish commands (`eka new` / `eka publish` / `eka discard`) or the state-transition commands (`eka transition`) or, for the legacy authoring adapter, the repository `docs/` tree followed by `eka validate` and `eka sync`.
2. **Write directly to SQLite** — the canonical store is a private persistence implementation of the Runtime Kernel (ADR-014). Touching it is invalid architecture, not a shortcut.
3. **Bypass the Authoring API** — never persist knowledge by any route that skips validation (no hand-written `unit.json` in the store, no `PublishInline`-style shortcuts outside the sanctioned commands, no editing snapshot files as a substitute for authoring).
4. **Bypass validation** — the Draft → Validate → Publish sequence is mandatory. A publish that skips the CKO-level validation gate does not exist; if `eka publish` reports validation findings, fix the draft — do not force it through.
5. **Silently change higher-stratum knowledge to justify lower-stratum implementation** — lower-stratum knowledge must not contradict higher-stratum knowledge in force, and never supersedes or amends upward (Stratum Authority Invariant, R10/R12). A conflict resolves downward: the lower stratum changes.
6. **Invent Engineering Knowledge** — never fabricate identities, states, relationships, or content that the repository does not back. Knowledge must be traceable to its source (a decision, a session, a review, a requirement). If knowledge is missing, say so — do not generate it to fill a gap.
7. **Overwrite immutable knowledge** — canonical objects are immutable and content-addressed. "Changing" knowledge means creating a new revision (new instance version or a transition), never editing a published object.
8. **Treat generated summaries as canonical knowledge** — a projection (`eka view`), a context object (`eka context`), or an AI summary is a derived view, not a canonical object. Only CKOs (and their authoring representations) are canonical. Never re-author derived output as if it were source knowledge.

AI-generated knowledge remains subject to the normal EKA validation and lifecycle rules (P16: enforcement mechanisms may vary, the invariants do not — see the [AI Workflow section](../skeleton/docs/workflow-guide.md#8-ai-workflow) of the Engineering Operating Guide).

## The mental model in one line

Source code answers *"what does the software currently do?"*; Engineering Knowledge answers *"why and how is the software designed and evolved?"*. Use both. Neither replaces the other.

## Where to go next

- [AI Workflow](docs/workflow.md) — the six-step loop agents run, with a worked example.
- [Installation & Discovery](docs/installation.md) — how to install the CLI, the pack, and the skills.
- [Agent Commands](commands/README.md) — `/eka-discuss` (planning) and `/eka-execute` (execution + resume) as invocable workflows.
- [The MCP Boundary](docs/mcp-boundary.md) — the future MCP integration and what skills must not become.
- [Reference Project Validation](docs/reference-project-validation.md) — how this pack was validated against the Feather Reference Project.
- [Smoke test](scripts/smoke-test.sh) — the executable validation (`EKA_PATH=<eka> skills/scripts/smoke-test.sh`).
- EKA documentation map: [root README](../README.md), [Knowledge Runtime Architecture](../reference/runtime-architecture.md), [Runtime API](../reference/runtime-api.md), [CLI reference](../reference/cli.md), [Engineering Operating Guide](../skeleton/docs/workflow-guide.md).
