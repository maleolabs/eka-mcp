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

## Status

MCP integration: **implemented** in `eka-mcp` (stdio JSON-RPC 2.0, protocol `2024-11-05`) — tools `context`, `get`, `domain`, `status`, `validate`, `new`, `publish`, `transition`, `note`, `draft_read` (+ deprecated alias `view`), `draft_list`, `integrity_check`, `discard`, `sync_push`, `assign`, `reassign`, `unassign`; resources `eka://status`, `eka://skills/*`, `eka://templates/*`. Human projections (`eka view`/`eka watch`) remain CLI-only. This document is the agreed boundary so that the contracts (Runtime API, Authoring API, Context Object, CKO JSON schemas) remain the interface — and the Skill Pack sits unchanged on top.
