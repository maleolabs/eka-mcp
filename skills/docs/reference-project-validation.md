# Reference Project Validation

How the EKA Skill Pack was validated against the **EKA Reference Project** (Feather, `reference/project/`), and the evidence that an agent equipped with the pack can do real work.

> Method: every command the skills teach was executed against the Reference Project with a **scratch workspace** (`EKA_HOME=/tmp/eka-skills-validation`), so the demo never touched a real store. The `eka` binary was built from this repository (`go build -o eka ./cmd/eka`). All outputs below are actual command output, abbreviated where long.

## Validation environment

```sh
EKA_HOME=/tmp/eka-skills-validation
eka project register reference/project
eka sync reference/project
```

Result (first sync, snapshot mode — the committed 37-unit snapshot seeds the store, then re-pushes byte-identically):

```
Summary:
└── Repository: project
└── Project: feather
└── Status: synced
└── Pull: snapshot: 37 units, 0 attachments
└── Push: 37 units, 0 attachments
└── Snapshot: rsf-repo-feather-2 (66a1ac034435)
```

## Success criteria → evidence

The pack's goal: an AI coding agent can (1) understand the project, (2) retrieve relevant knowledge, (3) obtain context, (4) identify architectural constraints, (5) work with knowledge, (6) validate changes. Each criterion maps to a skill and to an executed scenario.

### 1. Understand the project — `eka-project-understanding`

| Command | Result |
|---|---|
| `eka status` | Workspace `schema v4`, 1 project (`feather`), 39 objects / 40 payloads, 1 history payload, repo `project` synced (`push 66a1ac034435`) |
| `eka get discovery --no-content` | 6 units, all stratum 1: `vis:feather-vision`, `str:feather-2026`, `req:publishing-core`, `req:comments-phase2` (draft), `fnd:markdown-editor-options`, `fnd:search-approach` |
| `eka get architecture / planning / execution / operations` | the domain map: 8 architecture, 5 planning, 15 execution, 3 operations units — matching the documented project composition |
| `eka get containers` | 2 containers (`ctr:wave-6` completed, `ctr:wave-7` active) with plans and work items |

The domain map in five commands reproduces the project's Engineering Domain map — the agent can report which strata are populated and what the project commits to.

### 2. Retrieve relevant Engineering Knowledge — `eka-knowledge-retrieval`

`eka get feather/sto:publish-post:1 --no-content` returns the canonical CKO (schema `eka-cko-v2`):

```json
{
  "schema": "eka-cko-v2",
  "identity": { "namespace": "feather", "type": "sto", "id": "publish-post", "instanceVersion": 1 },
  "canonicalForm": "feather/sto:publish-post:1",
  "engineeringDomain": "Execution",
  "stratum": 4,
  "number": 5,
  "revision": 1,
  "author": "Jonas Berg",
  "created": "2026-07-29",
  "updated": "2026-08-03",
  "stateVector": { "executionState": "done", "existenceState": "active" },
  "classification": { "dimension": "requirements", "domain": "Execution" },
  "relationships": [
    { "type": "depends-on", "target": "feather/ctr:wave-7:1" },
    { "type": "depends-on", "target": "feather/plan:roadmap-v1:1" }
  ],
  "changeLog": [ { "date": "2026-07-29", "domain": "existence-state", "from": "-", "to": "active", "by": "Jonas Berg" }, "..." ]
}
```

The retrieval surface (identity lookups, domain collections, containers query, `--upstream`/`--downstream`/`--timeline`/`--no-content`) works exactly as the skill documents.

### 3. Obtain contextual information — `eka-knowledge-retrieval` (context)

`eka context feather/sto:publish-post --json` → schema `eka-context-v1`, sections: `upstream`, `downstream`, `dependencies`, `constraints`, `planning`, `review`, `history` — the classified one-hop neighborhood.

Issue-number subjects work: `eka context '#5'` resolves to `feather/sto:publish-post:1` (per-group incremental numbering, GitHub-style).

### 4. Identify architectural constraints — `eka-knowledge-retrieval` (`--depth engineering`)

`eka context feather/adr:content-storage --depth engineering --json`:

```
summary: {'focus': 1, 'units': 6, 'sections': 5, 'history': 1}
  upstream: 2 entries     (the units it depends on: the research finding `fnd:markdown-editor-options` and the requirement `req:publishing-core`)
  downstream: 2 entries   (the units that reference it)
  dependencies: 2 entries
  constraints: 4 entries  (the higher-authority Discovery units in force)
  planning: 1 entries
  history: 1 entries
```

The `constraints` section is the higher-stratum authority set — exactly what an agent must honor before changing anything the decision binds. The engineering depth stays bounded (6 units for this subject — well under the 64-unit cap).

### 5. Work with Engineering Knowledge — `eka-knowledge-authoring` + `eka-knowledge-modification`

The full authoring loop executed against the scratch workspace:

| Step | Command | Result |
|---|---|---|
| Scaffold | `eka new feather/sto:skill-demo` | JSON draft at `<workspace>/drafts/feather/sto-skill-demo.json` — template: `state {executionState: planned, existenceState: active}`, `changeLog` (2 entries), `content {acceptanceCriteria, description}` |
| Scaffold (knowledge type) | `eka new feather/adr:skill-demo` | `state {contentState: proposed, ...}`, `content {alternativesConsidered, consequences, context, decision}` |
| Scaffold (plan type) | `eka new feather/scp:skill-demo --phase mvp --derives-from feather/req:publishing-core` | `phase: "mvp"`, `relationships.derivesFrom` wired at scaffold |
| Publish v1 | `eka publish feather/sto:skill-demo` | `Published: feather/sto:skill-demo:1`, instance version 1, object hash `166a6d4192…` |
| Revise (new instance) | second `eka new` on the same line, state → `todo`, then `eka publish` | `Published: feather/sto:skill-demo:2` — a **new immutable object**, v1 untouched |
| Timeline | `eka get feather/sto:skill-demo --timeline` | both instances listed, the last execution-state change-log entry per instance (`planned` → v1, `todo` → v2) |
| State transition | `eka transition feather/td:reduce-query-count todo --force` | new immutable payload, summary `Object: feather/td:reduce-query-count` + new object hash; the non-registered warning fired as documented (`--force` confirmed outside a terminal) |
| Note draft | `eka note feather/sto:skill-demo --role implementation` | `cmt-` draft under the workspace drafts with `discusses` wired to the subject line |
| Gate refusal | `eka transition feather/sto:skill-demo:2 …` (canonical form) | refused — transitions address lines; and `eka note <canonical form>` refused: "notes address the subject line" — the skills' line-vs-instance guidance is the actual CLI behavior |

Immutable discipline verified: publishing instance 2 did not alter instance 1; the store gained a history payload (see integrity below) instead of overwriting.

### 6. Validate changes — `eka-knowledge-review`

| Command | Result |
|---|---|
| `eka validate reference/project` | `40 artifacts, 0 errors, 0 warnings` — PASS |
| `eka integrity check` (after the authoring demo) | `Payloads checked: 40, References checked: 39, Attachments: 0, History payloads: 1, Violations: 0` — the superseded demo instance counts as history, never a violation (the documented immutability semantics) |

## Human projection spot-check

`eka view execution` renders the active container `feather/ctr:wave-7` as a six-column Kanban (Planned 0 / Todo 0 / In Progress 1 / In Review 1 / Done 2 / Canceled 0), summary `Active Work: 2, Completed Work: 2, Review Queue: 1, Overall Progress: 2/4` — matching the documented projection scenarios.

## Result

All six success criteria are demonstrated against the Reference Project with the pack's own instructions — no command invented, no store touched directly, no validation bypassed. The scratch workspace exercised the full lifecycle (scaffold → publish → revise → transition → note → validate) and ended with a **clean integrity report**, which is the invariant layer the skills promise.

## Executable form

The manual walkthrough above is captured as an executable smoke test: [`../scripts/smoke-test.sh`](../scripts/smoke-test.sh) runs the whole surface against the Reference Project in a scratch workspace with ~28 assertions (setup, conformance, retrieval, context, authoring loop, deterministic refusals, projection) — exit 0 = the pack's claims hold. Run it whenever the pack or the CLI changes:

```sh
EKA_PATH=eka skills/scripts/smoke-test.sh
```

## Reproduction

```sh
go build -o /tmp/eka ./cmd/eka            # from the EKA repository root
export EKA_HOME=/tmp/eka-skills-validation # scratch workspace — never the real ~/.eka
/tmp/eka project register reference/project
/tmp/eka sync reference/project
/tmp/eka validate reference/project
/tmp/eka get feather/sto:publish-post:1 --no-content
/tmp/eka context feather/adr:content-storage --depth engineering --json
# authoring demo (scratch workspace only):
cd reference/project && /tmp/eka new feather/sto:skill-demo && /tmp/eka publish feather/sto:skill-demo
```
