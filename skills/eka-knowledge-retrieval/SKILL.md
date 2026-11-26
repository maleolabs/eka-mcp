---
name: eka-knowledge-retrieval
description: Use when you need to retrieve Engineering Knowledge — one object, a whole Engineering Domain, a container, a subject's context, or a human projection. Teaches the explicit distinction between `eka get` (canonical machine-readable retrieval), `eka context` (constructed understanding), and `eka view` (human-oriented visualization), plus identity forms and retrieval options.
---

# Knowledge Retrieval

Retrieval is the read side of EKA. Three Runtime consumers exist, with **deliberately distinct** purposes — pick by question type:

| Command | Kind | Answers | Output |
|---|---|---|---|
| `eka get` | **retrieve** | "What is the canonical knowledge?" | canonical CKO JSON (schema `eka-cko-v2`), one Document per object, deterministic |
| `eka context` | **understand** | "What Engineering Knowledge do I need to understand this subject correctly?" | the deterministic Engineering Context Object (schema `eka-context-v1`): focus + classified neighborhood + strata + history |
| `eka view` | **visualize** | "Show me the knowledge as a human-readable projection" | per-domain visualizations: Kanban (execution), roadmap (planning), dependency tree (architecture), cards (discovery), timeline (operations), ticket detail |

All three read the canonical store through the Runtime API and never parse Markdown. The repository must be **registered once** (`eka sync` at intake/registration — registration persists; reads need no per-read sync and run directly on the shared store in any worktree). Do not use `eka view` output where you need canonical semantics; do not use `eka get` where you need a derived, constructed understanding; do not parse Markdown files for any of these answers.

## `eka get` — canonical machine-readable retrieval

```sh
eka get <target> [flags]
```

Targets:

| Target | Form | Returns |
|---|---|---|
| identity | `<ns>/<type>:<id>[:<v>]` | one Document. With `:v` = the exact instance; without = the **highest (latest) instance version** of the line (the line form). The namespace is required. |
| domain | `discovery` \| `architecture` \| `planning` \| `execution` \| `operations` | a Collection of every unit of that domain in the project, sorted by canonical form |
| containers | `containers` | every execution container (`ctr-`) of the project with plan, work items, tickets, started/ended |

Retrieval options (additive, schema-stable):

| Flag | Effect |
|---|---|
| `--no-content` | omit the `content` field (identity/state/relationships only — token-saving) |
| `--compact` | single-line JSON |
| `--upstream` / `--downstream` | identity lookups: the resolved units the target's relationships point at / the units that reference the target |
| `--timeline` | identity lookups: the line's instance history (form, instance version, revision, object hash, change log), ascending |
| `--type <token>` / `--dimension <token>` / `--phase <value>` | domain queries: exact-match filters (e.g. `--type adr`) |

Exit codes: `0` success · `1` workspace/repository-state refusal (no workspace, not an EKA repository, unregistered) · `2` usage/unknown identity/internal. stdout carries only the JSON.

## `eka context` — constructed understanding

```sh
eka context <subject> [--depth local|dependency|engineering] [--json|--compact] [--no-content]
```

Subjects: `<ns>/<type>:<id>[:<v>]` or `#<n>` (issue number, unambiguous per group). Depths:

| Depth | Reach |
|---|---|
| `local` | the focus + its instance-line history; no neighborhood |
| `dependency` (default) | the one-hop neighborhood classified into sections: upstream, downstream, dependencies, constraints, decisions, planning, review — plus the strata landscape |
| `engineering` | dependency **plus** a bounded constraint closure: higher-authority units reachable through the collected units' own relationships (max 2 hops, ≤ 64 units) |

Use `--depth engineering` when a task is bound by higher-stratum knowledge (specs, standards, decisions in force). The context is **deterministic** — no LLM, no vector search. `--json`/`--compact` emit the Context Object on stdout (JSON only).

## `eka view` — human-oriented visualization

```sh
eka view <projection> [target]
eka watch <projection> [target]   # live, TTY-only
```

Projections: `discovery`, `architecture`, `planning`, `execution` (aliases `sprint`/`wave`), `operations`, `containers` (aligned container table), `ticket <id>`, `board` (all work items across containers). Human reading aid only — never a retrieval contract.

## Decision guide

| Question | Command |
|---|---|
| "What is the exact canonical state/content/identity of object X?" | `eka get <ns>/<type>:<id>[:<v>]` |
| "Show me everything in the architecture domain" | `eka get architecture [--type adr]` |
| "What does this work item depend on / what references it?" | `eka get <id> --upstream / --downstream` |
| "How did this line evolve?" | `eka get <id> --timeline` |
| "What constrains this subject? What should I know before touching it?" | `eka context <subject> --depth engineering --json` |
| "Is this a draft or in force?" | check `stateVector.contentState` in `eka get` (draft units constrain nothing) |
| "Give me a readable overview for a human" | `eka view <domain>` |

## Retrieval etiquette

- Use `--no-content` unless the content payload is the question — keep context windows small.
- Prefer `#<n>` issue numbers when referring to work items in conversation; prefer canonical forms in scripts and in knowledge.
- The line form (`<ns>/<type>:<id>`) resolves to the **highest (latest)** instance version — the line's current revision, always. When you need a specific older revision, address it explicitly: `<ns>/<type>:<id>:<v>` (exact instance), or `--timeline` to see the line's history first.
- Ambiguity is refused, never guessed: unqualified forms, unknown identities, and ambiguous issue numbers are deterministic errors (exit `2`).

## Real example (Feather Reference Project)

```sh
# one canonical object
eka get feather/sto:publish-post:1 --no-content

# everything in the execution domain, filtered
eka get execution --type sto --no-content

# containers with their plans and items
eka get containers

# the constraint closure around an architecture decision
eka context feather/adr:content-storage --depth engineering --json

# the human board
eka view execution
```

## Next

- Understanding a whole project → [eka-project-understanding](../eka-project-understanding/SKILL.md)
- Creating or changing knowledge → [eka-knowledge-authoring](../eka-knowledge-authoring/SKILL.md) · [eka-knowledge-modification](../eka-knowledge-modification/SKILL.md)
