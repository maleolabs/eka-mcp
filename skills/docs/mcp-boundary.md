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

## Boundary rules for this milestone

- **Do not implement MCP** — no protocol work, no server scaffolding, no tool-definition files pretending to be MCP. The pack documents the boundary only.
- **Do not introduce a new Runtime API** — the skill pack adds zero runtime surface.
- **Do not extend the CLI** — the skills use existing commands; if a capability is missing, that is a CLI/Runtime milestone, not a skill concern (the skills instruct agents to inspect `eka help` when uncertain).
- **Do not design skills around MCP** — design them around behavior; transport follows.

## Status

MCP integration: **not started** — intentionally out of scope for the AI Skill Pack milestone. This document is the agreed boundary so that when the MCP milestone begins, the contracts (Runtime API, Authoring API, Context Object, CKO JSON schemas) are already the interface — and the Skill Pack sits unchanged on top.
