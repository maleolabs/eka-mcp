---
name: eka-knowledge-authoring
description: Use when you need to create new Engineering Knowledge — a requirement, research finding, architecture decision (adr/dec), specification, standard, glossary, plan, work item, container, or record. Teaches the sanctioned authoring path: Draft → Validate → Publish through the Authoring API (`eka new` / `eka edit` / `eka publish`), never by modifying Runtime storage. Also covers the legacy docs-tree adapter and `eka note` for comments.
---

# Knowledge Authoring

Create Engineering Knowledge through the **Authoring API surface** — never by writing to the Runtime store. The lifecycle is strict:

```
Draft  →  Validate  →  Publish
```

and the validation step is **mandatory** — there is no sanctioned path that skips it.

## The two authoring paths

| Path | What | When to use |
|---|---|---|
| **Workspace drafts** (primary) | `eka new` scaffolds a JSON draft in `<workspace>/drafts/<project>/<type>-<id>.json`; `eka publish` validates at CKO level and persists an immutable object | the default for agents — file-based, explicit, no repository mutation |
| **Legacy docs tree** (adapter) | authoring files in `<repo>/docs/<dimension>/<type>-<id>.json` (or legacy Markdown), then `eka validate` and `eka sync` (docs-mode) | repositories that still serialize knowledge in-repo (e.g. the EKA Reference Project) |

Both end in the same place: a **Canonical Knowledge Object** in the canonical store, produced through validation. Pick the path the repository already uses; do not mix.

## Draft templates

The exact scaffold output of `eka new`/`eka note` for every authorable token family lives in **this skill's [`templates/`](templates/README.md) directory** — owned state, `dimension`, content keys, relationship wiring per family, plus the regeneration procedure. When authoring a type you have not authored before, read the family's template file first (e.g. [`templates/drafts/adr-template.json`](templates/drafts/adr-template.json)) — it shows precisely what the draft must contain, and what `eka new` will scaffold for you.

The template files are derived from the CLI; they are reference material, never authoring inputs themselves (a real draft is scaffolded by `eka new`).

## Execution membership (work item → container)

A work item becomes a member of an execution container **only through a ticket** (`tkt-`) whose `derives-from` carries **both** references:

- `ctr:<container>` — the container reference;
- `sto:<id>` (or `ts`/`bug`/`td`/`ch`/`spk`) — the work item reference.

```sh
eka new tkt:my-ticket --derives-from ctr:wave-7,sto:my-item
```

The ticket wires the membership: `eka view execution` and `eka view ticket` derive container membership **from the ticket's relationships only** (`view/graph.go`, `machine/container.go`) — never from the container's file text.

**`--depends-on ctr:` does NOT create membership.** A work item with `--depends-on ctr:...` is **not** a member of that container — `depends-on` never contributes to container membership. (On a `ctr-` draft itself, `depends-on` is reserved for the plan reference: activating a container locks its plan atomically.)

The `workItems` content key of a `ctr-` artifact is prose — never parsed for membership.

## Assignment (work item → member)

Human ownership of work is the **assigned-to** relationship (ADR-029 / `req:team-collaboration` §4.3): a work item points at its assigned member (`mbr-` line). Assignment is relationship-only (ADR-013) and **single-assignee** — a work item carries at most one assigned-to edge. The member line is the typed target: an `mbr-` artifact (operating token, Execution domain, content-state + existence-state, `purpose`/`content` sections — see [`templates/drafts/mbr-template.json`](templates/drafts/mbr-template.json)).

Assignment changes happen **only through the explicit assignment commands** — the other authoring commands never assign (no side-effect auto-assign, and `eka relate` deliberately keeps `assigned-to` off its flag surface):

```sh
eka assign <item> --to <mbr>      # set the assignee (idempotent on the SAME member;
                                  # refuses when the item is already assigned to a
                                  # DIFFERENT member — use reassign to move)
eka reassign <item> --to <mbr>    # replace the assignment in ONE operation
                                  # (refuses when the item has no assignee)
eka unassign <item>               # remove the assigned-to edge (no-op when none)
```

- **Item** — the work item line: `<type>:<id>` (unqualified) or `<ns>/<type>:<id>` (qualified, must equal the repository's namespace). Only work items (`sto`/`ts`/`bug`/`td`/`ch`/`spk`) are assignable.
- **Member target** — `--to <mbr-id>`, `mbr:<id>`, `mbr-<id>`, or `<ns>/mbr:<id>`. The member must be a **resolvable `mbr-` line of the SAME repository** (provenance mirror of the R13 assigned-to sub-check): cross-repository assignment is refused deterministically, and an unresolvable member id refuses with the repository's known member lines listed.
- **No instance churn** — the edge is written with the SAME instance version (the relate no-churn mechanism): a published item is re-pointed to a new immutable payload, a pending draft gets its relationships block mutated in place.
- **Exit codes** — `0` the edge is in the requested state (published, draft-mutated, or unchanged); `1` deterministic refusal (non-work-item target, unresolvable/cross-repository member, already-assigned-to-a-different-member on assign, not-assigned on reassign, validation findings); `2` usage or internal error.
- **Machine output** — `--json` emits the deterministic report (schema `eka-assignment-v1`) with the pinned keys `assignee` (the canonical member identity) and `no-assignee` (the member-axis bucket flag — never `unassigned`, which belongs to the container axis).
- **R13 interplay** — the conditional assigned-to sub-check (typed target, at-most-one, provenance) is evaluated at sync time, not at publish; the R13 transition gates read note-state only.

## Allowed state values

Single source of truth: `conformance/state.go` (`DomainValues`). Values are lowercase; the initial value per domain is the scaffold default (see the family templates). An invalid value is a blocking publish error — check the table before authoring, not by trial-and-error.

| Domain | Allowed values | Owned by |
|---|---|---|
| execution-state | `planned`, `todo`, `in-progress`, `in-review`, `done`, `canceled` | work items `sto-`, `ts-`, `bug-`, `td-`, `ch-`, `spk-` |
| planning-state | `draft`, `approved`, `immutable` | `plan-` |
| container-state | `planned`, `active`, `completed` | `ctr-` |
| existence-state | `active`, `archived`, `retired` | every type that carries state |
| note-state | `open`, `resolved`, `dismissed` | `cmt-` |
| phase (context attribute) | `discovery`, `mvp`, `milestone`, `release`, `growth`, `maturity`, `sunset` | `scp-`, `plan-` only |
| content-state — living variant | `draft`, `review`, `approved`, `amended` | knowledge types `vis-`, `str-`, `req-`, `scp-`, `epc-`, `plan-`, `trc-`, `arc-`, `spec-`, `std-`, `run-`, `rel-`, `gls-`, `fnd-`, `rvw-` (and `cmt-`, `mbr-`) |
| content-state — ADR variant | `proposed`, `accepted`, `superseded` | `adr-` |
| content-state — decision variant | `draft`, `accepted`, `superseded` | `dec-` |

Transition legality is a separate rule (e.g. execution-state follows the D1 table, ADR-019 §3: `done` may only move to `canceled`, `canceled` re-activates to `todo`) — see `conformance/state.go:isLegalTransition`.

## Workflow — workspace drafts

### 1. Scaffold

```sh
eka new <ns>/<type>:<id> [--dimension <token>] [--phase <value>] \
      [--derives-from <ref>[,<ref>...]] [--depends-on <ref>[,<ref>...]] \
      [--validates <ref>] [--supersedes <ref>] [--amends <ref>] \
      [--content-file <path>] [--edit]
```

- Target: `<ns>/<type>:<id>` (namespace must equal the repository's) or `<type>:<id>` (unqualified — resolved to the repository's default namespace).
- `--dimension <token>` — **required for knowledge types** (`vis`/`str`/`req`/`fnd`/`arc`/`adr`/`dec`/`spec`/`std`/`gls`/`scp`/`epc`/`plan`/`trc`/`rvw`/`run`/`rel`): the primary knowledge dimension, validated at publish (R6 — a missing dimension is a blocking error). Valid tokens: `intent`, `requirements`, `research`, `architecture`, `decisions`, `specifications`, `standards`, `vocabulary`, `planning`, `quality`, `operations`, `records` (e.g. `adr` → `decisions`, `req` → `requirements`, `vis`/`str` → `intent`). Work items (`sto`/`ts`/`bug`/`td`/`ch`/`spk`), containers (`ctr-`), tickets (`tkt-`), sessions (`ses-`) and notes (`cmt-`) carry no dimension.
- `--phase` — phase context; `scp-`/`plan-` only.
- Relationship flags — wire the identity references at scaffold time (they also appear in the template and can be edited later).
- **`ctr-` drafts require `--depends-on` with a plan reference**: activating a container locks its plan atomically.
- `--content-file` — prepopulate draft content from a JSON object (the agent-friendly path: write the content object, then scaffold).

### 2. Fill the draft

The scaffolded JSON template (example: a work item `sto`):

```json
{
  "namespace": "feather",
  "type": "sto",
  "id": "skill-demo",
  "revision": 1,
  "state": {
    "executionState": "planned",
    "existenceState": "active"
  },
  "changeLog": [
    { "date": "2026-08-11", "domain": "executionState", "from": "-", "to": "planned", "by": "Engineering" },
    { "date": "2026-08-11", "domain": "existenceState", "from": "-", "to": "active", "by": "Engineering" }
  ],
  "content": { "acceptanceCriteria": "", "description": "" }
}
```

Discipline:

- **`state`** — only the type's **owned state domains**, camelCase keys, lowercase values. Initial values come from the template — keep the truthful initial state, do not pre-jump states you have not earned (e.g. do not scaffold a work item as `done`).
- **`changeLog`** — every state transition in the object's life, in occurrence order, `{date, domain, from, to, by}`. A state field without a matching change-log entry is invalid. Do not invent dates; use the actual day.
- **`content`** — the type's required content keys (e.g. `context`/`decision`/`consequences`/`alternativesConsidered` for `adr`; `description`/`acceptanceCriteria` for `sto`). The keys must be present (the template guarantees them); empty values publish, but a truthful artifact is filled — never publish empty knowledge and call it knowledge.
- **`relationships`** — only existing identities or, for drafts of *other* units, draft identities (draft tolerance). Never point at invented identities.
- `instanceVersion` is deliberately absent — assigned at publish.
- **Author identity (`by`)** — the scaffold defaults to `by: Engineering`; **replace it with the real author** before publishing. The `by` value must be the human/agent identity in effect — `git config user.name`, or an explicit agent identity — never a generic placeholder, never an invented person. (`eka transition` has the same discipline via `--by` with `--by-kind user|agent|worker`; `eka note` takes the author from `git config user.name`.)

Use `eka edit <target>` only interactively (TTY); for agent workflows edit the draft file directly (it is a mutable workspace-local file) or use `--content-file`.

### 3. Validate

There is no standalone "validate draft" command — validation happens **at publish** (CKO level: identity, state values, owned-set, change-log consistency, relationship resolution with draft tolerance, phase, classification validity — the dimension for knowledge types (R6), required content keys). Before publishing, two pre-flight signals exist:

- `eka draft list` — shows an `invalid — N errors` marker on drafts that fail the single-file structural classification;
- `eka edit <target>` (TTY) — closes with the draft's full re-validation report.

`eka validate` validates the **repository docs tree only** (`docs/`) — it never validates workspace drafts. Location-shaped rules (filename/folder conventions) do not apply to workspace drafts.

Self-check before publish (mirrors the CKO gate): state values valid? every owned domain in the change-log? knowledge type carries its `dimension`? relationships resolvable (or draft-tolerant)? required content keys present? classification consistent with the type? `by` replaced with the real author?

### 4. Publish

```sh
eka publish <target> [--instance-version V]
```

- Validates, then persists the **immutable CKO** in the workspace store, then removes the draft — all-or-nothing. A failed validation leaves the draft untouched (fix and retry).
- **Instance version auto-assigned**: the line's highest + 1 (1 for a new line). Explicit `--instance-version` must exceed the line's highest (forward-only, P7).
- Published objects are **workspace-native** (`source_repo = "runtime"`): part of the project union, visible to `eka get`/`eka context`/`eka view`, but **never enter a repository snapshot** — `eka sync` remains the explicit transport for repository-attributed knowledge.
- The draft file is a single-use ticket: a second publish of the same draft fails.
- `eka discard <target> [--force]` — drop a draft without publishing. `eka draft list` — see the backlog.

### 5. Verify

```sh
eka get <ns>/<type>:<id>:<v>    # the new immutable object
eka get <ns>/<type>:<id> --timeline   # its place in the line's history
```

## Workflow — legacy docs tree (repository adapter)

```sh
# write/update <repo>/docs/<dimension>/<type>-<id>.json  (or legacy .md)
eka validate        # conformance gate R0–R13 — must pass with 0 errors
eka sync            # docs-mode pull (first sync) compiles the authoring into CKOs
```

Rules: the file location must match the type's dimension (R6), the filename must match the identity (R2), and every non-Discovery artifact needs its upward traceability chain (R10). Validation failures block the pull.

## Notes (`cmt-`) — the comment/evidence layer

```sh
eka note <subject-line> --role implementation|review|fix [--domain <domain>] [--content-file <path>]
```

- Creates a `cmt-` note draft in the workspace with a `discusses` relationship to the subject **line** (not a specific instance).
- Roles: `implementation` (evidence of work), `review` (review verdict), `fix` (fix addressing notes).
- Notes are drafts until published (`eka publish`); a draft with `noteState: resolved` already gate-satisfies the R13 transition gates.
- Use notes to attach evidence/review to work items instead of inventing ad-hoc knowledge.

## AI safety rules (authoring-specific)

- Never write to the canonical store, `eka.db`, or any workspace storage directly.
- Never hand-craft a `unit.json` in the store; the store's content is produced by the sanctioned publish path only.
- Never bypass validation ("it will pass later", "validation is a formality" — it is not).
- Never invent identities, states, relationships, authors, dates, or content.
- AI-generated knowledge is subject to the same validation and lifecycle as human-authored knowledge (P16).

## Real example (Feather Reference Project)

```sh
# 1. scaffold a work item draft, content from a prepared JSON object
eka new feather/sto:export-csv --content-file /tmp/export-csv-content.json

# 2. (agent) edit the draft file directly, then publish
eka publish feather/sto:export-csv

# 3. verify the immutable object
eka get feather/sto:export-csv:1 --no-content
```

```sh
# a review note on a completed work item
eka note feather/sto:publish-post --role review --domain execution
eka publish feather/cmt:publish-post-review
```

## Next

- Changing existing knowledge (revision, transition) → [eka-knowledge-modification](../eka-knowledge-modification/SKILL.md)
- Reviewing what exists → [eka-knowledge-review](../eka-knowledge-review/SKILL.md)
