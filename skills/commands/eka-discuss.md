---
description: EKA-guided planning discussion. Guides the flow idea → discuss → propose → validate → approved → publish, creates knowledge drafts from the discussion, delegates deep analysis and review through the role contract, and only publishes with evidence. Planning only — NEVER touches source code.
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

## Role contract

Delegation is expressed exclusively through this role contract — nine roles, a closed set ratified by the orchestrator (adding or removing a role is a breaking pack change). A role is a duty, not an agent name: each ecosystem maps roles to its own agents at install time; the semantics below are ecosystem-independent.

| Role | Kind | Input | Deliverable | Escalates to |
|---|---|---|---|---|
| architect | analysis-only | the Engineering Context Object + the proposal under discussion | architecture impact assessment: constraints in force, strata impact, dependency effects, related-knowledge landscape | primary agent |
| backend | implementing | work item identity + context object + acceptance criteria + branch/worktree conventions | implemented change on its own branch in its own worktree, quality gates green, evidence note published | primary agent |
| frontend | implementing | UI scope + acceptance criteria + branch/worktree conventions | implemented UI change on its own branch in its own worktree, quality gates green, evidence note published | primary agent |
| security-review | analysis-only | the proposal/diff + its context object | security findings with severity; blocking findings gate approval | primary agent |
| code-review | analysis-only | the proposal/diff + acceptance criteria | technical-correctness verdict with findings | primary agent |
| product-review | analysis-only | user-facing proposal/item + product context | product, UX and holistic experience verdict (this role absorbs UX-review and holistic review-specialist duties) | primary agent |
| qa | analysis-only (gate) | the draft/artifact + its evidence trail + conformance rules | QA verdict: conformance, state/change-log integrity, consistency, traceability | primary agent |
| devops | implementing | infrastructure/build/CI scope + conventions | infrastructure change on its own branch in its own worktree, quality gates green, evidence note published | primary agent |
| documenter | implementing | documentation-only scope + conventions | documentation change on its own branch in its own worktree, evidence note published | primary agent |

Contract rules:

- Sizing is decided by the item itself, not by splitting roles: `backend` covers light and heavy backend sizing, `frontend` covers standard and advanced UI.
- Analysis-only roles never implement; implementing roles never approve.
- Every role escalates to the **primary agent** — the orchestrating agent running this command, the only party that talks to the user.

## Delegation mode

Before the first delegation attempt of a session, the primary agent resolves its **delegation mode** from delegation data — never from assumptions about the environment:

1. **Resolve the rows.** Read the active delegation table: the `DELEGATION.txt` sidecar installed next to these commands when present, else the pack's mapping table for the resolved target. Each row maps one role-contract role to an agent target plus a mode: `delegate` (a named agent performs the role) or `solo` (the primary performs it inline).
2. **Determine the mode.** Every role resolving to `solo`/`primary` ⇒ the session runs as **`mode: solo`**. Any role resolving to a named agent ⇒ **`mode: delegated`**. Resolution happens once at session start, is re-checked on resume, and always precedes the first delegation attempt.
3. **Record the mode.** State the resolved mode in the session preamble (`mode: solo` or `mode: delegated`) and carry it into every checkpoint and closing summary. A missing or unreadable delegation source is stated explicitly and defaults to `mode: solo` with that assumption recorded — the degrade is never silent, in either direction.

Mode semantics:

- **`mode: delegated`** — nothing changes: every role goes to its mapped agent exactly as the role contract defines.
- **`mode: solo`** — the primary performs every role itself, inline, in role order, labeling each contribution with the role it fulfills (e.g. `[role: architect]`). Analysis-only roles become clearly labeled perspectives held to the same inputs and deliverables as delegated ones. Implementing roles are performed by the primary under exactly the rules each body sets for delegated implementation — in execution: dedicated branch + worktree per item created from the latest development branch snapshot, quality gates re-run, review sign-off before `done`. Solo never drops a role's duties and never skips review: where no second agent exists, review is an explicit self-review against the same checklist, applied in full and recorded as self-review.

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
- **Delegate deep analysis through the role contract** per domain:
  - architecture impact + deep knowledge discovery (existing related knowledge) → `architect` (input: the context object + the proposal)
  - product intent / prioritization → `product-review`
  - UX / experience → `product-review` (the role absorbs UX review)
- In `mode: solo` (Delegation mode): perform these analyses inline as clearly labeled perspectives — `[role: architect]`, `[role: product-review]` — against the same inputs and deliverables; the discussion flow and its outputs are unchanged.
- Roles never ask the user; they report to the primary agent, which discusses with the user.
- Record the discussion as evidence: the `note` primitive (`--role implementation|review`; cmt drafts discussing the subject), or a `ses` draft for session-style records.

### Gate 3 — Propose

- Create the proposal draft(s): `new <ns>/<type>:<id>` with the correct dimension (knowledge types) and relationships wired to in-force knowledge (`derives-from` / `depends-on`). Use the [authoring templates](../eka-knowledge-authoring/templates/README.md) for the family shape.
- The proposal **stays a draft** (`contentState: draft` / `proposed`) — never pre-jump to approved.
- Wire the evidence: `discusses` the discussion notes, `depends-on` the research findings.

### Gate 4 — Validate

- Run the validation surface: `validate` (docs-tree authoring) or the CKO-level self-check from `eka-knowledge-authoring` (workspace drafts — the gate runs at publish).
- **Delegate review through the role contract**:
  - `qa` — conformance, state/change-log integrity, consistency, R10/R12 traceability
  - `code-review` — technical correctness of the proposal content
  - `product-review` — holistic product/experience review (user-facing proposals)
- In `mode: solo` (Delegation mode): run each review inline as a clearly labeled perspective (`[role: qa]`, `[role: code-review]`, `[role: product-review]`) against the same checklists; findings still block approval, and verdicts are recorded noting the solo self-review.
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
3. **Membership** — for every work item, publish a `tkt-` ticket deriving from BOTH the container and the work item (see "Execution membership" in `eka-knowledge-authoring`). Without tickets the items stay `unassigned` on the board and invisible in the `view` projection.
4. Close the session only when: plan approved, container `planned`, every work item published as `planned` **and** ticketed. An approved plan with unticketed work items is an incomplete session — record the gap instead of declaring readiness for `/eka-execute`.

## Interaction model

- **Interactive by design** — unlike the execution command, this command uses the question tool. At each gate, present the state (what exists, what constrains, what is proposed) and the decision needed: proceed / refine / abandon.
- Uncertain classification or state inference → ask the user, never guess (classification error = wrong stratum = worse than no knowledge).

## Hard rules

- NEVER modify source code. Planning only.
- NEVER bypass validation; NEVER publish without evidence.
- Default to draft; never claim approved without evidence.
- Never invent identities, relationships, states, dates, or authors.
- Roles never ask the user — they escalate to the primary agent.
- Preserve EKA invariants: immutability (new revisions, never mutation), strata (no upward supersession, R12), traceability (R10).

## Output

Close with a planning summary:

- subject, family/stratum, and the existing knowledge it touches;
- the delegation mode (`mode: solo` / `mode: delegated`) and, in solo mode, which roles were performed inline as perspectives;
- draft identities created and their states (draft / approved);
- review verdicts and the evidence trail (note identities);
- approval status, and what blocks approval if not approved;
- if the planning artifacts are approved: a readiness note for `/eka-execute` — which plan/container scope the approval unlocks, the container identity (state `planned` — activation is the executor's job), and the ticketed work item count.
