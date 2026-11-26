---
description: EKA-guided planning discussion. Guides the flow idea → discuss → propose → validate → approved → publish, creates knowledge drafts from the discussion, performs deep analysis and review inline (mode: solo), and only publishes with evidence. Planning only — NEVER touches source code.
---

# EKA Planning Discussion

Guide an interactive planning discussion through the EKA flow. The primary agent guides, the user decides. This command is **planning-only**: it creates, refines, validates, and publishes Engineering Knowledge drafts — it never modifies source code.

## Transport primitives

Every EKA capability this command uses is a **primitive** with two transports: the `eka` CLI and the eka-mcp MCP server. The table below is the single authoritative citation — the rest of this body refers to primitives by name only.

| Primitive | CLI command | MCP tool |
|---|---|---|
| status | `eka status` | `status` |
| sync | `eka sync` | — |
| context | `eka context <subject>` | `context` |
| get | `eka get <form>` | `get` |
| domain | `eka get <collection>` (whole-domain listing) | `domain` |
| new | `eka new` | `new` |
| note | `eka note <subject> --role <role>` | `note` |
| publish | `eka publish <target>` | `publish` |
| validate | `eka validate` | `validate` |
| view | `eka view <projection>` (human projection) | — |

Reality check: the MCP column cites only tools the eka-mcp binary exposes today; `—` means no MCP tool exists yet for that primitive. MCP citations are gated to the mcp-production milestone — re-verify this table against the server's tool list before relying on the MCP transport.

## Skill loading

This body references EKA skills by name (`eka-orientation`, `eka-knowledge-authoring`, …) — as imperative loads and as inline citations alike. Every such reference resolves through this load-order protocol: take the first step that yields the skill, never skip ahead, and never treat a missing skill as a blocker.

1. **Installed skill directory (primary path).** If the pack is installed on disk (`eka-mcp configure --with-skills` or `eka-mcp install skills`), read the skill's `SKILL.md` from its installed directory — `<install-dir>/<name>/SKILL.md`; detect the installation directory from the workspace, never assume one. Found → use it, stop here.
2. **MCP resource (secondary path).** Otherwise, if an eka-mcp server is connected, read the resource `eka://skills/<name>` (`text/markdown`): it serves the embedded SKILL.md verbatim, and the resource listing carries each skill's frontmatter description for discovery. Found → use it, stop here.
3. **Inline hard rules (fallback).** If neither path yields the skill, proceed without it: this body is self-sufficient — the Hard rules section below carries the non-negotiable behavior, and the transport-primitive table above carries the full CLI/MCP surface. Degrade explicitly: state once which skills were unavailable and that the run proceeds on inline rules alone — never silently, and never invent a skill's guidance.

## Execution mode

EKA is a **solo-author** knowledge system: the primary agent is the only party that talks to the user, and it performs **every** analysis, implementation and review step itself, inline. This is the pack's single `mode: solo` execution path — agent-to-agent handoff and role mapping are not part of EKA.

- Analysis and review steps are performed as clearly labeled perspectives, held to the same inputs, checklists and deliverables as any fully-performed step would be.
- Where no second agent exists, review is an explicit **self-review** against the full checklist, applied in full and recorded as self-review — a rubber-stamp pass without the checklist is a gate violation.

## Input

$ARGUMENTS — optional: a topic, a knowledge identity (`<ns>/<type>:<id>`), or an issue number (`#<n>`). Without input, propose a subject and confirm with the user.

## Preflight — session context

Start the session with EKA context (the context-first principle):

1. Verify the environment: run `status`. Not an EKA repository / no workspace / not synced → route to the `eka-adoption` skill (or `eka-troubleshooting` for refusals) via the Skill loading protocol and stop.
2. Run `sync` — knowledge must be current before discussion.
3. Load the relevant skills via the Skill loading protocol: `eka-orientation` (mental model, if not already fluent), `eka-project-understanding` (context workflow), `eka-knowledge-authoring` (draft discipline), `eka-knowledge-review` (validation).
4. Establish the subject: if the input is an identity, construct `context <subject>` at engineering depth (constraints in force); if it is a topic, map it to the knowledge space with `domain` before discussing.

## The flow — five gates

### Gate 1 — Idea

- Capture the intent precisely: what, why, for whom, and which stratum it belongs to (Discovery / Architecture / Planning / Execution / Operations).
- **Conflict/duplicate check**: `domain` over Discovery / Architecture (and `context` on the closest existing subject) — does the idea already exist? Does it contradict knowledge in force?
- Record the idea: `new` a draft of the right family (`fnd` for research/idea validation, `req`/`vis`/`str` for product intent, `dec`/`adr` for decisions, `scp`/`epc`/`plan` for planning intent) — or, when the idea is not yet knowledge, record the raw discussion first (see Gate 2).
- **Family guidance**: `req` captures product intent (Discovery); `scp` commits the MVP scope (Planning); `epc` groups the work as an optional planning unit that derives-from a scope. A requirement is not a substitute for a scope or an epic — when the user asks whether an epic is needed, compare against the in-force scope/requirements and ask which level they mean; never dismiss a family without a concrete mapping.
- Report to the user: how the idea maps (family, stratum, dimension), and what existing knowledge it touches.

### Gate 2 — Discuss

- **Constraints first**: `context <subject>` at engineering depth — read the `constraints`, `decisions`, `planning`, and `dependencies` sections. The discussion must honor the higher-authority knowledge in force.
- Structured discussion: alternatives, trade-offs, impact on lower strata, dependencies, and what evidence would be needed for approval.
- **Perform the required analysis inline** (`mode: solo`), as clearly labeled perspectives against the same inputs and deliverables:
  - architecture impact + deep knowledge discovery (existing related knowledge) — input: the context object + the proposal;
  - product intent / prioritization;
  - UX / experience.
- The primary agent is the only party that discusses with the user.
- Record the discussion as evidence: the `note` primitive (`--role implementation|review`; cmt drafts discussing the subject), or a `ses` draft for session-style records.

### Gate 3 — Propose

- Create the proposal draft(s): `new <ns>/<type>:<id>` with the correct dimension (knowledge types) and relationships wired to in-force knowledge (`derives-from` / `depends-on`). Use the [authoring templates](../eka-knowledge-authoring/templates/README.md) for the family shape.
- The proposal **stays a draft** (`contentState: draft` / `proposed`) — never pre-jump to approved.
- Wire the evidence: `discusses` the discussion notes, `depends-on` the research findings.

### Gate 4 — Validate

- Run the validation surface: `validate` (docs-tree authoring) or the CKO-level self-check from `eka-knowledge-authoring` (workspace drafts — the gate runs at publish).
- **Review the proposal inline** (`mode: solo`), each review a clearly labeled perspective against the full checklist:
  - conformance, state/change-log integrity, consistency, R10/R12 traceability;
  - technical correctness of the proposal content;
  - holistic product/experience review (user-facing proposals).
- Findings still block approval, and verdicts are recorded noting the solo self-review.
- Every finding must be addressed in the draft. Validation findings **block approval**.

### Gate 5 — Approved → Publish

- **Approval is evidence-based**: validation pass + review sign-off(s) + user confirmation. There is no approval without evidence.
- Only then: update the draft's state to `approved`/`accepted` (with change-log entries covering the transition, real dates, real author identity) and run `publish` (workspace drafts) — or, for the docs-tree path, update the authoring file, run `validate`, then `sync`.
- Not approved → keep the draft, record the open items, and summarize exactly what blocks approval.
- Record the review evidence as published `cmt` notes (`--role review`) before or at approval, so the review trail is durable.

### Gate 6 — Execution readiness (before closing the session)

A planning session is not complete until the execution state is prepared — still planning-only, still no code:

1. **Work items** — derive the work items (`sto` / `ts` / `bug` / `td` / `ch` / `spk`) from the approved plan and its scope; publish them with `execution-state: planned` (same evidence discipline as any artifact).
2. **Container** — create the execution container (`ctr-`) with `containerState: planned`. **NEVER activate it here**: activation locks the plan immutable and belongs to `/eka-execute` (Phase 0).
3. **Membership** — for every work item, publish a `tkt-` ticket deriving from BOTH the container and the work item (see "Execution membership" in `eka-knowledge-authoring`). Without tickets the items are not container members: they are absent from the board and invisible in the `view` projection.
4. Close the session only when: plan approved, container `planned`, every work item published as `planned` **and** ticketed. An approved plan with unticketed work items is an incomplete session — record the gap instead of declaring readiness for `/eka-execute`.

## Interaction model

- **Interactive by design** — unlike the execution command, this command uses the question tool. At each gate, present the state (what exists, what constrains, what is proposed) and the decision needed: proceed / refine / abandon.
- Uncertain classification or state inference → ask the user, never guess (classification error = wrong stratum = worse than no knowledge).

## Hard rules

- NEVER modify source code. Planning only.
- NEVER bypass validation; NEVER publish without evidence.
- Default to draft; never claim approved without evidence.
- Never invent identities, relationships, states, dates, or authors.
- The primary agent is the only party that talks to the user.
- Preserve EKA invariants: immutability (new revisions, never mutation), strata (no upward supersession, R12), traceability (R10).

## Output

Close with a planning summary:

- subject, family/stratum, and the existing knowledge it touches;
- the execution mode (`mode: solo`) and which analyses were performed inline as clearly labeled perspectives;
- draft identities created and their states (draft / approved);
- review verdicts and the evidence trail (note identities);
- approval status, and what blocks approval if not approved;
- if the planning artifacts are approved: a readiness note for `/eka-execute` — which plan/container scope the approval unlocks, the container identity (state `planned` — activation is the executor's job), and the ticketed work item count.
