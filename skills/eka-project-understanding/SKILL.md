---
name: eka-project-understanding
description: Use when you need to understand an EKA-enabled project before acting — what project context exists, which Engineering Domains are populated, what architectural constraints bind the work, what planning and execution context exists, and how knowledge relates. The context-first workflow: prefer `eka context` and `eka get` over manually scanning every knowledge representation.
---

# Project Understanding

Obtain project context **before** modifying Engineering Knowledge or source code whenever the task depends on project intent, architecture, planning, or constraints. Do not start from blind code scanning, and do not manually walk every Markdown/JSON file — the Runtime answers these questions.

## Prerequisites

The runtime commands read the canonical store, so the repository must be **registered** once:

```sh
eka status                    # is there a workspace? what is registered?
eka sync                      # ONE-TIME: register (auto) + seed knowledge into the canonical store
```

Registration persists in the workspace; after it, `eka get` / `eka context` / `eka view` work directly on the shared store in any worktree of the repository — **no sync per read**. `eka sync` (full cycle) is an intake/migration command: the pull side re-seeds the store from the repository snapshot (or docs tree when no snapshot exists) and can overwrite newer store states with older instances — so inside an active execution use `eka sync push` only. If `eka sync` reports validation errors (docs-mode) or a corrupt snapshot, stop — resolve before continuing.

## The workflow

### 1. Orient — `eka status` and `eka project list`

```sh
eka status          # workspace path, schema, store totals, per-repository last sync
eka project list    # registered projects and repositories
```

This tells you the machine state: which projects exist, which repositories are registered, whether knowledge is synced.

### 2. Map the knowledge — domain queries

```sh
eka get discovery      # intent, requirements, research — the authority roots
eka get architecture   # architecture, decisions, specifications, standards
eka get planning       # plans, epics, scopes, traceability
eka get execution      # containers, work items, reviews, sessions (everything in motion)
eka get operations     # runbooks, releases, records
eka get containers     # execution containers with their plans and work items
```

Each returns the canonical CKO collection of the domain, sorted by canonical form. This is the project's knowledge map in one pass — use `--no-content` to keep it light, `--type <token>` to filter a family (e.g. `--type adr`).

### 3. Construct understanding — `eka context` around the subjects that matter

```sh
eka context <subject>                 # depth dependency (default): focus + one-hop neighborhood
eka context <subject> --depth engineering   # + bounded higher-authority constraint closure
eka context <subject> --json          # the Context Object as JSON (deterministic)
```

Subjects: `<ns>/<type>:<id>[:<v>]` (identity; namespace required) or `#<n>` (issue number). The context classifies the neighborhood into sections: **upstream, downstream, dependencies, constraints, decisions, planning, review**, plus the **strata landscape** and the focus's **history**. `--depth engineering` (max 2 hops, ≤ 64 units, higher-authority strata only) is the constraint check: it collects the higher-stratum knowledge that binds the subject.

### 4. Identify the constraints that bind your task

The pattern: start from the subject of your task (a work item, an ADR, a spec), then walk upward:

```sh
eka context feather/sto:<work-item> --depth engineering --json
```

- The **constraints** section = units of strictly higher authority (strata above the focus). These bind the subject — your change must not contradict them.
- The **dependencies** section = the depends-on / derives-from targets (e.g. a work item → its container → its plan).
- The **decisions / planning** sections = the ADRs, specs, scopes, and plans in force around the subject.
- The **history** section = the instance line's evolution (why it is in its current state).

For the full derivation chain of a work item, walk relationships:

```sh
eka get <ns>/<type>:<id> --upstream    # the units its relationships point at
eka get <ns>/<type>:<id> --downstream  # the units that reference it
```

### 5. Verify the understanding

- `eka validate` — the repository's conformance status (errors? warnings?).
- `eka get <subject> --timeline` — the line's revision history.
- Check the docs tree only when you need the *authoring representation* of a specific object (its exact content as authored) — the runtime answers structure and state; the authoring file answers "what was literally written".

## What you should be able to report

After this workflow you can state, for the task at hand:

- **Project context**: the project's domains and their key objects (map from step 2).
- **Relevant domains**: which strata are populated and which bind the task.
- **Architectural constraints**: the higher-authority units the task must not violate (step 4).
- **Planning context**: the plans/scopes in force (`--type plan`, `--type scp`).
- **Execution context**: the active container and the work items in motion (`eka get containers`, `eka context <ctr>`).
- **Relevant relationships**: what derives from what, what depends on what.

## Multi-repository projects

One EKA project can group several repositories (e.g. an `api`/`web`/`mobile` monorepo-style project, the Atrium model). Knowledge is partitioned by **provenance** (`source_repo`), never by namespace:

```sh
eka project register ./api      # identity (project, name, namespace) from each eka.yaml
eka project register ./web
eka project register ./mobile
eka project list                # one project, three repositories
```

- `eka sync` each repository once (registration/seed) — the workspace store reconstructs the project's **complete** knowledge as the union of all registered repositories. Repeated syncs are for deliberate re-seeds (see Prerequisites); inside an active execution use `eka sync push` only.
- `eka get <domain>` and `eka view <projection>` operate over the **project union** — one project, many repositories; results are sorted by canonical form regardless of source.
- `eka status` shows per-repository last sync — a stale repository means its snapshot is stale in the union; re-sync before trusting a query (verify with `eka get` afterward).
- Duplicate identities across repositories in one project resolve deterministic last-wins (a documented runtime limitation) — when in doubt, check which repository owns a unit via its provenance in the snapshot/manifest.

## Do NOT

- Do not read every file under `docs/` to "get context" — the runtime is the fast, canonical path. Authoring files are for authoring, not for understanding.
- Do not parse `eka.db` or any workspace storage.
- Do not trust a single object in isolation: state and constraints live in relationships and strata.
- Do not run `eka sync` as a per-read routine — registration persists; reads run on the shared store. If `eka get`/`eka context` refuse, read the refusal message: it tells you whether the repository is unregistered (one-time `eka sync` fixes it) or the snapshot is corrupt (fix the snapshot, never re-seed blindly).
- Do not start modifying knowledge or code without this context when the task depends on intent, architecture, or constraints.

## Real example (the EKA Reference Project, "Feather")

```sh
# one-time setup
EKA_HOME=$HOME/.eka eka sync reference/project

# the knowledge map
eka get discovery --no-content
eka get architecture --no-content --type adr

# understanding a work item: why does it exist, what binds it?
eka context feather/sto:publish-post --depth engineering --json

# the binding chain upward: work item → container → plan → scope → requirement
eka get feather/sto:publish-post --upstream --no-content
eka context feather/ctr:wave-7 --json
```

Expected shape of the answers: the work item's context `constraints` section lists the Planning units in force (e.g. `plan:roadmap-v1`, `scp:mvp-v1` — the approved commitments the item may not exceed); the work item's `upstream` lists its container and plan; the domain queries return the full sorted collections.

## Next

- Which command for which question? → [eka-knowledge-retrieval](../eka-knowledge-retrieval/SKILL.md)
- Creating new knowledge → [eka-knowledge-authoring](../eka-knowledge-authoring/SKILL.md)
- Full walkthrough: [the AI Workflow](../docs/workflow.md).
