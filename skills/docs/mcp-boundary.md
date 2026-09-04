# The MCP Boundary

How the Skill Pack relates to the future MCP integration — and why the two must never be conflated.

## The architecture today

```
AI Agent
    ↓  loads
EKA Skills          (this pack — behavior definitions)
    ↓  invokes
EKA CLI             (the current execution interface)
    ↓
EKA Runtime         (workspace + canonical store, Runtime API + Authoring API)
```

The CLI is the execution interface; the Skills define *how the agent behaves*; the Runtime is the knowledge store. The Skill Pack deliberately has **no protocol** of its own: it drives the CLI, and everything it teaches goes through existing, validated capabilities.

## The architecture tomorrow (MCP milestone — NOT implemented here)

```
AI Agent
    ↓  loads
EKA Skills          (unchanged — behavior definitions)
    ↓  invokes
EKA MCP             (future wire transport — Runtime capabilities)
    ↓
EKA Runtime         (workspace + canonical store, Runtime API + Authoring API)
```

MCP will expose **Runtime capabilities** as a transport: the same services the CLI already delegates to — `Knowledge` (retrieval), `Resolver`, `Relations`, `Timeline`, `Workspace` (state), the **Authoring API** (validate/compile/sync + the draft lifecycle), and the deterministic **Context Object** produced by the Context Engine. The wire layer is a thin adapter over contracts that already exist (ADR-014, ADR-015, ADR-021 reserve exactly this slot).

## The separation contract

| Layer | Owns | Must NOT |
|---|---|---|
| **Skills** | how an AI agent should behave: mental model, workflows, safety boundaries | become a transport; duplicate runtime logic; define wire protocols |
| **CLI** | the current execution interface | change semantics when MCP arrives (it stays a first-class consumer) |
| **MCP** | a future transport of Runtime capabilities | redefine behavior; the skills keep defining how agents work |
| **Runtime** | canonical knowledge + service contracts | grow protocol-specific surface per consumer |

Consequences for this milestone:

1. **Skills are not a substitute for MCP** — and MCP is not a substitute for skills. Skills carry the behavior; MCP will carry bytes. An agent equipped only with MCP knows *how to call*; an agent equipped with skills knows *how to behave* (when to get context, when to transition, when to review, what never to do).
2. **Skills must not duplicate Runtime logic.** Everything the skills teach is an existing CLI capability or an explicit behavioral rule. No skill invents a "query language", a "sync algorithm", or a validation rule — those live in the Runtime and the validator (P16: enforcement mechanisms may vary, the invariants do not).
3. **Skills stay provider- and transport-agnostic.** The behavior layer must survive the CLI → MCP transition unchanged. When MCP lands, the skills' commands map onto MCP tools with the same semantics (`eka get` → the retrieval tool, `eka context` → the context tool, `eka publish`/`eka transition` → the authoring tools).
4. **MCP exposes Runtime capabilities, not CLI text.** A future MCP server wraps the Runtime API services (and the Context Engine object contract) — the same contracts `eka get` and `eka context` consume today. It is not a screen-scraper of CLI output.

## What the skills already prepare

| Skill | Future MCP mapping |
|---|---|
| [eka-knowledge-retrieval](../eka-knowledge-retrieval/SKILL.md) | retrieval tools: identity/domain/containers queries, context construction (the Context Object is the deterministic prompt-ready contract — ADR-021 Decision 9) |
| [eka-knowledge-authoring](../eka-knowledge-authoring/SKILL.md) | authoring tools: draft scaffold, validate, publish (`PublishInline` is the implemented API-only single-prompt path) |
| [eka-knowledge-modification](../eka-knowledge-modification/SKILL.md) | transition tools with the same gates (gates are Runtime-side, not CLI-side) |
| [eka-knowledge-review](../eka-knowledge-review/SKILL.md) | validation/integrity tools — same reports, same verdicts |

## MCP tool naming: draft file reader vs human projection

- **MCP `draft_read`** — draft-file reader (authoring aid): returns one draft file content verbatim (the v2.0 JSON authoring document). Deterministic, no rendering. The previous name `view` is retained as a **deprecated alias** for one minor version (td:mcp-view-naming-fix) with a deprecation notice in `tools/list`; migrate to `draft_read`.
- **CLI `eka view` / `eka watch`** — human-oriented projections: Kanban, roadmap, dependency tree, cards, timeline, ticket detail — live/TTY rendering for human consumption. These remain **CLI-only** and are deliberately not exposed as MCP tools or resources (see req:eka-mcp-production §8). Agents must not conflate `draft_read` with `eka view`.

## Boundary rules for this milestone

- **Do not implement MCP** — no protocol work, no server scaffolding, no tool-definition files pretending to be MCP. The pack documents the boundary only. *(Historical: the MCP server is now implemented in `eka-mcp`; this rule scoped the original Skill Pack milestone.)*
- **Do not introduce a new Runtime API** — the skill pack adds zero runtime surface.
- **Do not extend the CLI** — the skills use existing commands; if a capability is missing, that is a CLI/Runtime milestone, not a skill concern (the skills instruct agents to inspect `eka help` when uncertain).
- **Do not design skills around MCP** — design them around behavior; transport follows.

## Runtime transport (sync)

- **MCP `sync_push`** — push the repository snapshot from the workspace store (schema `eka-sync-push-result-v1`). Same engine as CLI `eka sync push` — same snapshot compilation (`exchange.EmitSource`), same digest (`SnapshotFingerprint`), same refusal classes; crash-safe (staged in `.snapshots-tmp`, swapped atomically) so a failed push writes nothing partially. Optional `adopt` (`--adopt`, ADR-032 Option C2) re-attributes workspace-native units before pushing; `override` aligns the identity to the content namespace (ADR-020 Decision 3). Pull / `--from-docs` re-seed is deliberately **not exposed** via MCP — it re-points line references to older instances (silent regression hazard documented in `eka-knowledge-modification`) and stays an operator-supervised CLI operation (`eka sync pull`). Attempting pull arguments via `sync_push` refuses deterministically naming the CLI.

## Assignment (team-collaboration)

- **MCP `assign` / `reassign` / `unassign`** — work-item assignment as MCP write tools (schema `eka-assignment-v1`), per req:eka-mcp-production revision 3 §8.2 decision 3. Same engine as CLI `eka assign` / `eka reassign` / `eka unassign` — same target forms (`<type>:<id>` or `<ns>/<type>:<id>` for the item, `<mbr-id>` / `mbr:<id>` / `<ns>/mbr:<id>` for the member), same validation (work-item gate, namespace gate, member resolvability with repository-scoped member list, cross-repository refusal), same refusal classes (already-assigned-to-different, not-assigned, unknown identities, gate-restricted states), no partial writes. Single-assignee (ADR-029): `assign` refuses an already-assigned item (use `reassign`), `reassign` refuses an unassigned item (use `assign`), `unassign` is no-op when unassigned. Every write sets explicit agent author identity (`AuthorIdentity{Kind:"agent"}`) — never the Engineering placeholder (locked decision #7) — and returns the deterministic assignment report; `by`/`byKind` default to the agent identity `mcp-agent`.

## Feedback (meta-information outside the knowledge model)

Feedback is **meta-information about the tool** — it is not engineering knowledge, never a unit of the canonical store, and never a CKO (ADR-026). Reports live in `$EKA_HOME/feedback/` as YAML frontmatter + markdown body and are filed as GitHub issues on the fixed target repository. The MCP feedback surface mirrors the CLI subcommands exactly:

- **MCP `feedback_new`** — create a local draft under `EKA_HOME/feedback` (schema `eka-feedback-new-v1`). Same engine as `eka feedback new`: same validation (`type`/`title` required, closed value sets for `type`/`severity`/`source`), same per-type scaffold (bug: Steps/Expected/Actual, others: Description), same `fbk-YYYYMMDD-slug` id with collision suffix, same `0700` dir / `0600` file permissions. Auto-injected triage metadata: `eka_version` from the MCP build version (`pack.Version`), `os` from `GOOS/GOARCH`, `created` from today, `command` defaulting to `mcp:feedback_new` when not passed. The draft never enters the canonical store; no CKO is produced.

- **MCP `feedback_list`** — list all local feedback deterministically (schema `eka-feedback-list-v1`), id descending (newest first — ids embed `YYYYMMDD`), same as `eka feedback list`. Deterministic and honest: the first malformed file fails the whole list naming the file — a silent skip would hide a broken report. No args; empty workspace yields `feedback: []`.

- **MCP `feedback_publish`** — file a draft as a GitHub issue on the fixed target repository (schema `eka-feedback-publish-v1`), same engine as `eka feedback publish`. All constraints inherited unchanged: release-binary requirement enforced (empty bundled token refuses `issue token not bundled — use a release binary`), missing/invalid token refuses deterministically naming the remediation (`publish failed: invalid token — use a release binary` on 401/403, never a raw HTTP error), token material never appears in tool outputs/errors/logs, idempotent already-published refusal (`already published as #n <url>`), unknown draft id refuses `unknown feedback "<id>"`. The CLI's confirmation gate (`--yes` outside a terminal) is implicit in MCP — the transport is non-interactive, so no prompt is shown and no hanging is possible. On success the local file is rewritten `status: published` with `issue_number` + `issue_url` (atomic `write .tmp` + `rename`). A failed publish writes nothing partially.

Explicit statement: **feedback is meta-information outside the knowledge model** — the MCP feedback tools transport reports about the tool itself; they do not author, publish, or transition CKOs and they do not read the canonical store.

## Skill pack resources (sto:mcp-resource-agent-delivery)

Skills and command guidance are delivered via **MCP resources**, not tools, and without mandatory per-agent installation (configure defaults to MCP resources only; `eka-mcp configure --with-skills/--with-commands/--with-all` is opt-in).

### Resource set

| URI | Kind | Mime | Description |
|---|---|---|---|
| `eka://manifest` | index | `application/json` | Compact manifest (`eka-pack-manifest-v1`) — pack/plugin versions plus sorted `skills`/`templates`/`commands` lists, no bodies. |
| `eka://bootstrap` | guide | `text/markdown` | Bootstrap — lazy load order, versioned reads and fallback. |
| `eka://status` | status | `application/json` | Live workspace status (same as `status` tool). |
| `eka://skills/<name>` | guidance | `text/markdown` | One skill's `SKILL.md` (frontmatter-described, annotated). |
| `eka://templates/<type>` | guidance | `application/json` | One v2.0 draft template (annotated). |
| `eka://commands/<name>` | guidance | `text/markdown` | One command's markdown guide (frontmatter-described, annotated). |

All resources are read-only, deterministic and versioned. Every `resources/list` entry carries `annotations` (`audience: ["assistant","user"]` for guidance, `priority` 1.0 for manifest/bootstrap, 0.9 for skills, 0.8 for commands, 0.7 for templates, 0.6 for status) so clients can prioritize.

### Loading and fallback

1. **Compact index first** — read `eka://manifest` (no bodies). It is the single source of truth for the entry lists; do not synthesize guidance.
2. **Lazy fetch** — read only the needed `eka://skills/<name>`, `eka://templates/<type>` or `eka://commands/<name>` (or `eka://bootstrap` for the full guide). Bodies are fetched on demand.
3. **Versioned reads** — any pack resource supports an `@<version>` suffix, e.g. `eka://skills/eka-router@1.3.2`, `eka://manifest@1.3.2`. Unversioned means current (plugin `pack.Version` / pack `manifest.yaml` version). Unknown versions refuse `-32002 Resource not found` naming the available version and directing to retry the unversioned URI; only the current version is retained.
4. **Runtime fallback** — unknown URIs (bad name, trailing slash segment, missing skill) refuse `-32002` deterministically.
5. **Offline/install fallback** — when resources are unavailable (old server, no MCP connection), install locally: `eka-mcp install skills`, `eka-mcp install commands` or `eka-mcp configure --target <ecosystem> --with-all`, then read `<install-dir>/<name>/SKILL.md` directly. This file is the canonical fallback; resource guidance is never synthesized. The `eka://manifest` entry list remains the discovery contract.

### Separation

Guidance (skills, commands, templates, manifest, bootstrap) is always **resource content**; operations (context, get, domain, new, publish, transition, etc.) are always **tools**. Tools never return guidance directly; resources never perform writes. This keeps the transport thin and the behavior layer in the Skill Pack.

## Status

MCP integration: **implemented** in `eka-mcp` (stdio JSON-RPC 2.0, protocol `2024-11-05`) — tools `context`, `code_context`, `code_discover`, `code_get`, `get`, `domain`, `status`, `validate`, `new`, `draft_update`, `publish`, `publishBatch`, `transition`, `note`, `draft_read` (+ deprecated alias `view`), `draft_list`, `integrity_check`, `discard`, `sync_push`, `assign`, `reassign`, `unassign`, `feedback_new`, `feedback_list`, `feedback_publish`; resources `eka://status`, `eka://manifest`, `eka://bootstrap`, `eka://skills/*`, `eka://templates/*`, `eka://commands/*` (all with annotations and lazy `@<version>` reads). Human projections (`eka view`/`eka watch`) remain CLI-only. Feedback is explicitly excluded from the knowledge model (meta-information, never a CKO, never in the canonical store) per ADR-026 and req:eka-mcp-production revision 4 §8.2 decision 4. This document is the agreed boundary so that the contracts (Runtime API, Authoring API, Context Object, CKO JSON schemas) remain the interface — and the Skill Pack sits unchanged on top.
