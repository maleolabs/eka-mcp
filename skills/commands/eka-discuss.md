---
description: EKA-guided planning discussion. Guides the flow idea → discuss → propose → validate → approved → publish, creates knowledge drafts from the discussion, delegates review/validation to sub-agents, and only publishes with evidence. Planning only — NEVER touches source code.
agent: alex
---

# EKA Planning Discussion

Guide an interactive planning discussion through the EKA flow. The agent guides, the user decides. This command is **planning-only**: it creates, refines, validates, and publishes Engineering Knowledge drafts — it never modifies source code.

## Input

$ARGUMENTS — optional: a topic, a knowledge identity (`<ns>/<type>:<id>`), or an issue number (`#<n>`). Without input, propose a subject and confirm with the user.

## Preflight — session context

Start the session with EKA context (the context-first principle):

1. Verify the environment: run `eka status`. Not an EKA repository / no workspace / not synced → route to the `eka-adoption` skill (or `eka-troubleshooting` for refusals) and stop.
2. Run `eka sync` — knowledge must be current before discussion.
3. Load the relevant skills: `eka-orientation` (mental model, if not already fluent), `eka-project-understanding` (context workflow), `eka-knowledge-authoring` (draft discipline), `eka-knowledge-review` (validation).
4. Establish the subject: if the input is an identity, construct `eka context <subject> --depth engineering --json` (constraints in force); if it is a topic, map it to the knowledge space with `eka get <domain>` before discussing.

## The flow — five gates

### Gate 1 — Idea

- Capture the intent precisely: what, why, for whom, and which stratum it belongs to (Discovery / Architecture / Planning / Execution / Operations).
- **Conflict/duplicate check**: `eka get discovery` / `eka get architecture` (and `eka context` on the closest existing subject) — does the idea already exist? Does it contradict knowledge in force?
- Record the idea: `eka new` a draft of the right family (`fnd` for research/idea validation, `req`/`vis`/`str` for product intent, `dec`/`adr` for decisions, `scp`/`epc`/`plan` for planning intent) — or, when the idea is not yet knowledge, record the raw discussion first (see Gate 2).
- **Family guidance**: `req` captures product intent (Discovery); `scp` commits the MVP scope (Planning); `epc` groups the work as an optional planning unit that derives-from a scope. A requirement is not a substitute for a scope or an epic — when the user asks whether an epic is needed, compare against the in-force scope/requirements and ask which level they mean; never dismiss a family without a concrete mapping.
- Report to the user: how the idea maps (family, stratum, dimension), and what existing knowledge it touches.

### Gate 2 — Discuss

- **Constraints first**: `eka context <subject> --depth engineering --json` — read the `constraints`, `decisions`, `planning`, and `dependencies` sections. The discussion must honor the higher-authority knowledge in force.
- Structured discussion: alternatives, trade-offs, impact on lower strata, dependencies, and what evidence would be needed for approval.
- **Delegate deep analysis to sub-agents** per domain:
  - architecture impact → `alex-architect` (input: the context object + the proposal)
  - product intent / prioritization → `althea-product-specialist`
  - UX / experience → `althea-ux-specialist`
  - knowledge discovery (existing related knowledge) → `explore`
- Sub-agents never ask the user; they report to the primary agent, which discusses with the user.
- Record the discussion as evidence: `eka note <subject-line> --role <implementation|review>` (cmt drafts discussing the subject), or a `ses` draft for session-style records.

### Gate 3 — Propose

- Create the proposal draft(s): `eka new <ns>/<type>:<id>` with the correct `--dimension` (knowledge types) and relationships wired to in-force knowledge (`--derives-from` / `--depends-on`). Use the [authoring templates](../eka-knowledge-authoring/templates/README.md) for the family shape.
- The proposal **stays a draft** (`contentState: draft` / `proposed`) — never pre-jump to approved.
- Wire the evidence: `discusses` the discussion notes, `depends-on` the research findings.

### Gate 4 — Validate

- Run the validation surface: `eka validate` (docs-tree authoring) or the CKO-level self-check from `eka-knowledge-authoring` (workspace drafts — the gate runs at publish).
- **Delegate review to sub-agents**:
  - `alex-qa` — conformance, state/change-log integrity, consistency, R10/R12 traceability
  - `alex-reviewer` — technical correctness of the proposal content
  - `althea-review-specialist` — holistic product/experience review (user-facing proposals)
- Every finding must be addressed in the draft. Validation findings **block approval**.

### Gate 5 — Approved → Publish

- **Approval is evidence-based**: validation pass + review sign-off(s) + user confirmation. There is no approval without evidence.
- Only then: update the draft's state to `approved`/`accepted` (with change-log entries covering the transition, real dates, real author identity) and `eka publish` (workspace drafts) — or, for the docs-tree path, update the authoring file, `eka validate`, `eka sync`.
- Not approved → keep the draft, record the open items, and summarize exactly what blocks approval.
- Record the review evidence as published `cmt` notes (`--role review`) before or at approval, so the review trail is durable.

### Gate 6 — Execution readiness (before closing the session)

A planning session is not complete until the execution state is prepared — still planning-only, still no code:

1. **Work items** — derive the work items (`sto` / `ts` / `bug` / `td` / `ch` / `spk`) from the approved plan and its scope; publish them with `execution-state: planned` (same evidence discipline as any artifact).
2. **Container** — create the execution container (`ctr-`) with `containerState: planned`. **NEVER activate it here**: activation locks the plan immutable and belongs to `/eka-execute` (Phase 0).
3. **Membership** — for every work item, publish a `tkt-` ticket deriving from BOTH the container and the work item (see "Execution membership" in `eka-knowledge-authoring`). Without tickets the items stay `unassigned` on the board and invisible in `eka view execution`.
4. Close the session only when: plan approved, container `planned`, every work item published as `planned` **and** ticketed. An approved plan with unticketed work items is an incomplete session — record the gap instead of declaring readiness for `/eka-execute`.

## Interaction model

- **Interactive by design** — unlike the execution command, this command uses the question tool. At each gate, present the state (what exists, what constrains, what is proposed) and the decision needed: proceed / refine / abandon.
- Uncertain classification or state inference → ask the user, never guess (classification error = wrong stratum = worse than no knowledge).

## Hard rules

- NEVER modify source code. Planning only.
- NEVER bypass validation; NEVER publish without evidence.
- Default to draft; never claim approved without evidence.
- Never invent identities, relationships, states, dates, or authors.
- Sub-agents never ask the user — they escalate to the primary agent.
- Preserve EKA invariants: immutability (new revisions, never mutation), strata (no upward supersession, R12), traceability (R10).

## Output

Close with a planning summary:

- subject, family/stratum, and the existing knowledge it touches;
- draft identities created and their states (draft / approved);
- review verdicts and the evidence trail (note identities);
- approval status, and what blocks approval if not approved;
- if the planning artifacts are approved: a readiness note for `/eka-execute` — which plan/container scope the approval unlocks, the container identity (state `planned` — activation is the executor's job), and the ticketed work item count.
