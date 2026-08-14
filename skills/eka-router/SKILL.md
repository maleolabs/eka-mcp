---
name: eka-router
description: Use when working in an EKA-enabled project (a repository with eka.yaml, or a task that touches engineering knowledge) and you need to decide which EKA skill applies. A lightweight routing skill: read the decision tree, load the matching skill, do not load everything.
---

# EKA Router

A routing decision tree for EKA work. Load this skill when the task mentions EKA, engineering knowledge, architecture decisions, plans, tickets, work items, or when you found an `eka.yaml`/`exchange/` directory — then load **only** the skill the task matches.

## Decision tree

| If the task is… | Load | What it gives you |
|---|---|---|
| new to EKA; need the model before acting | [`eka-orientation`](../eka-orientation/SKILL.md) | the mental model: domains, strata, CKOs, immutability |
| understanding a project: what exists, what constrains | [`eka-project-understanding`](../eka-project-understanding/SKILL.md) | the context-first workflow (`eka status`/`eka get`/`eka context`; `eka sync` one-time at intake) |
| fetching specific knowledge | [`eka-knowledge-retrieval`](../eka-knowledge-retrieval/SKILL.md) | `get` vs `context` vs `view`, identity forms, options |
| creating new knowledge (req, adr, spec, plan, work item…) | [`eka-knowledge-authoring`](../eka-knowledge-authoring/SKILL.md) | draft → validate → publish, templates |
| changing existing knowledge (state, revision, correction) | [`eka-knowledge-modification`](../eka-knowledge-modification/SKILL.md) | immutability, transitions, revisions |
| reviewing knowledge or a change | [`eka-knowledge-review`](../eka-knowledge-review/SKILL.md) | EKA-native validation, constraint checks |
| bringing a non-EKA project into EKA (existing docs or none) | [`eka-adoption`](../eka-adoption/SKILL.md) | assessment, classification, migration paths, greenfield capture |
| software engineering work inside EKA (planning, tickets, delivery) | [`eka-engineering-workflow`](../eka-engineering-workflow/SKILL.md) | the canonical-domain spine and the full loop |
| a command refused, failed, or errored | [`eka-troubleshooting`](../eka-troubleshooting/SKILL.md) | refusal classes, exit codes, fixes |
| unsure what the task needs | [`eka-project-understanding`](../eka-project-understanding/SKILL.md) | orient first, then route onward |

## Routing rules

- **Load one skill, sometimes two.** E.g. an architecture change = `eka-knowledge-retrieval` (retrieve the binding knowledge) + `eka-knowledge-modification` (create the new revision). Do not load all skills.
- **Orient before changing.** Tasks that depend on project intent, architecture, planning, or constraints start with `eka-project-understanding` or `eka-context` — never with blind modification (context-first principle).
- **Troubleshooting is the fallback.** Any refusal/error → `eka-troubleshooting`; the message is deterministic and the skill maps message → fix.
- **The pack README** ([`../README.md`](../README.md)) carries the same discovery table plus the AI safety boundaries — read the boundaries once; they apply to every skill.

## After routing

Stop reading this file. Load the target skill and follow it. The router's job ends at the decision.
