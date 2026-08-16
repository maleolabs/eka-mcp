# Authoring Templates

Reference drafts for every authorable token family — the **exact scaffold output of `eka new`** (and `eka note` for `cmt-`), captured from the EKA Reference Project namespace (`feather`). Use them to understand a token family's owned state, dimension, content keys, and relationship wiring before authoring — no scaffold round-trip needed. These files are the resource directory of the [`eka-knowledge-authoring`](../SKILL.md) skill: when the skill is copied into an agent configuration, this directory travels with it.

## Files

| File | Family | Dimension (`--dimension`) | Owned state | Content keys |
|---|---|---|---|---|
| `vis-template.json` | `vis` — Vision | `intent` | contentState, existenceState | `content`, `purpose` |
| `str-template.json` | `str` — Strategy | `intent` | contentState, existenceState | `content`, `purpose` |
| `req-template.json` | `req` — Requirement | `requirements` | contentState, existenceState | `content`, `purpose` |
| `fnd-template.json` | `fnd` — Research Finding | `research` | contentState, existenceState | `conclusion`, `content`, `investigationSummary`, `purpose` |
| `arc-template.json` | `arc` — Architecture | `architecture` | contentState, existenceState | `content`, `purpose` |
| `adr-template.json` | `adr` — Decision Record | `decisions` | contentState, existenceState | `alternativesConsidered`, `consequences`, `context`, `decision` |
| `dec-template.json` | `dec` — Decision | `decisions` | contentState, existenceState | `alternativesConsidered`, `consequences`, `context`, `decision` |
| `spec-template.json` | `spec` — Specification | `specifications` | contentState, existenceState | `content`, `purpose` |
| `std-template.json` | `std` — Standard | `standards` | contentState, existenceState | `content`, `purpose` |
| `gls-template.json` | `gls` — Glossary | `vocabulary` | contentState, existenceState | `content`, `purpose` |
| `scp-template.json` | `scp` — Scope | `planning` (+ optional `phase`) | contentState, existenceState | `objective`, `outOfScope`, `scope` |
| `epc-template.json` | `epc` — Epic | `planning` (+ optional `phase`) | contentState, existenceState | `objective`, `outOfScope`, `scope` |
| `plan-template.json` | `plan` — Plan | `planning` | contentState, existenceState, **planningState** | `objective`, `outOfScope`, `scope` |
| `trc-template.json` | `trc` — Traceability | `planning` | contentState, existenceState | `objective`, `outOfScope`, `scope` |
| `ctr-template.json` | `ctr` — Container | — | **containerState**, existenceState; `dependsOn` wired at scaffold | `changeLog`, `objective`, `workItems` |
| `tkt-template.json` | `tkt` — Ticket (projection) | — | — (empty state vector); `derivesFrom` wired at scaffold | `commands`, `projectedStatus` |
| `sto-template.json` | `sto` — Story | — | executionState, existenceState | `acceptanceCriteria`, `description` |
| `ts-template.json` | `ts` — Task | — | executionState, existenceState | `acceptanceCriteria`, `description` |
| `bug-template.json` | `bug` — Bug | — | executionState, existenceState | `description`, `impact` |
| `td-template.json` | `td` — Tech Debt | — | executionState, existenceState | `acceptanceCriteria`, `debtRationale`, `description` |
| `ch-template.json` | `ch` — Chore | — | executionState, existenceState | `acceptanceCriteria`, `description` |
| `spk-template.json` | `spk` — Spike | — | executionState, existenceState | `conclusion`, `description`, `investigationNotes` |
| `rvw-template.json` | `rvw` — Review | `quality` | contentState, existenceState | `actionItems`, `content`, `findings`, `purpose` |
| `ses-template.json` | `ses` — Session | — | existenceState only | `context`, `notes`, `verification` |
| `mbr-template.json` | `mbr` — Member | — | contentState, existenceState | `content`, `purpose` |
| `run-template.json` | `run` — Runbook | `operations` | contentState, existenceState | `content`, `purpose` |
| `rel-template.json` | `rel` — Release Record | `records` | contentState, existenceState | `content`, `purpose` |
| `cmt-publish-post-implementation.json` | `cmt` — Note (implementation role) | — | contentState, existenceState, **noteState** | role content: `role`, `summary`, `changes[]`, `tests[]` |

## Facts encoded in these templates

- `instanceVersion` is **absent** — assigned at publish (`max + 1` for the line).
- **`dimension`** is present on knowledge types (R6 requires it at publish) — scaffolded via `--dimension <token>`; absent on work items, containers, tickets, sessions, notes.
- Initial state values are the scaffold defaults: `contentState: draft` (knowledge types), `contentState: proposed` (adr/dec), `executionState: planned` (work items), `containerState: planned` (containers), **no state at all** (tickets — empty state vector, no change-log). Keep the truthful initial state — never pre-jump states.
- `changeLog` carries one entry per owned state domain with `by: Engineering` — **replace `by` with the real author identity** (see the author-identity rule in [`../SKILL.md`](../SKILL.md)).
- `ctr-` scaffolds require `--depends-on` with a `plan-` reference (a container without a plan can never publish/activate).
- **`workItems` in a `ctr-` draft's content is prose — never parsed for membership.** Membership comes from tickets only: a `tkt-` whose `derivesFrom` carries both the container and the work item (`eka new tkt:my-ticket --derives-from ctr:wave-7,sto:my-item`). `--depends-on ctr:` on a work item does **not** make it a container member.
- `cmt-` is scaffolded by `eka note <subject-line> --role <implementation|review|fix>`, never by `eka new`; it carries the `discusses` relationship to the subject line and its author comes from `git config user.name`.
- `mbr-` (Member, ADR-029) is an operating token like `ses-`: no `--dimension`, no work-item state. Unlike `ses-` (existence-state only) it carries the full content-state + existence-state vector with the `purpose`/`content` sections — the member line expresses identity/role material, never work-item state. It is the typed target of the `assigned-to` relationship carried by work items (single-assignee, same-repository provenance).

## Allowed state values

Single source of truth: `conformance/state.go` (`DomainValues`). Values are lowercase; invalid values are blocking publish errors.

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

Transition legality is a separate rule (e.g. execution-state follows the D1 table, ADR-019 §3) — see `conformance/state.go:isLegalTransition`.

## Regeneration

These files are **derived artifacts** of the CLI — regenerate them, never hand-edit them. From an EKA repository synced into a workspace:

```sh
declare -A dim=( [vis]=intent [str]=intent [req]=requirements [fnd]=research \
  [arc]=architecture [adr]=decisions [dec]=decisions [spec]=specifications \
  [std]=standards [gls]=vocabulary [scp]=planning [epc]=planning \
  [plan]=planning [trc]=planning [rvw]=quality [run]=operations [rel]=records )
for t in "${!dim[@]}"; do
  eka new "feather/${t}:template" --dimension "${dim[$t]}"
done
for t in sto ts bug td ch spk ses mbr; do
  eka new "feather/${t}:template"
done
eka new feather/ctr:template --depends-on feather/plan:roadmap-v1
eka new feather/tkt:template
eka note feather/sto:publish-post --role implementation
# then copy <workspace>/drafts/feather/<type>-template.json here
```

Regenerate whenever `eka new` changes its template. The change-log dates reflect the regeneration day.
