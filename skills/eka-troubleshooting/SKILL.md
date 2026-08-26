---
name: eka-troubleshooting
description: Use when an EKA command refuses, fails, or errors — refused messages, unexpected exit codes, validation failures, transition refusals, ambiguous identities, snapshot or integrity problems. Teaches the deterministic refusal classes of the CLI and the correct fix for each. Also load when a workflow stalls and you are not sure why.
---

# EKA Troubleshooting

EKA refuses **deterministically**: every refusal is a designed message with a designed exit code. Diagnose from the message — never guess, never work around the gate.

## The general protocol

1. **Read the refusal message** — it is byte-pinned and actionable (the CLI contract).
2. **Check the exit code**: `0` success (warnings allowed) · `1` blocking violation or workspace/repository-state refusal · `2` usage/internal error.
3. **Diagnose with**: `eka status` (workspace state), `eka project list` (registrations), `eka validate` (conformance), `eka integrity check` (store).
4. **Inspect before assuming**: `eka help <command>` — if the local CLI differs from what you expect, the CLI's help wins.
5. **Never bypass** a gate: `--force` never skips validation or transition gates; `--override` has one narrow purpose (namespace alignment); the store is never hand-editable.

## MCP tool refusals carry the full report

When you consume EKA through eka-mcp, publish/assignment and transition refusals are self-contained — **no CLI fallback is needed for diagnosis**:

- **Validation refusals** (`publish refused: draft <line> failed CKO-level validation with N blocking error(s); the draft was kept`) embed the FULL conformance report inline as a second text content block, in the established `eka-conformance-report-v1` shape: per-finding rule id, severity and message, warnings included. Read the findings straight from the refusal; do not re-run `eka validate` to reconstruct them.
- **Transition membership-gate refusals** (`<line> is not registered in the current active container`) surface the deterministic warning plus an explicit retry affordance: `retry with confirmed:true to proceed anyway (asserts the work item may leave the current active container)`. The refusal is retryable — repeat the same `transition` call with `"confirmed": true`; nothing was written by the refused run.
- The first text content block remains the byte-stable sanitized headline (existing refusal-class matching keeps working), and every finding passes the same path-redaction policy as the headline — store paths appear as `<path>`, identity forms survive.

## Refusal classes and fixes

### A. Not an EKA repository

```
eka: get refused: /path is not an EKA repository (no eka.yaml); run 'eka init' first
```

Every repo-scoped command requires a directory tree carrying `eka.yaml`. Fix: run the command **inside** the repository, or create the identity file (`eka init`, or the adoption path for docs-marked repositories). `eka init`/`eka validate`/`eka status`/`eka project list`/`eka draft list`/`eka integrity check` are ungated — they work outside a repository by design.

### B. Workspace / registration refusals

```
eka: get refused: repository /path is not registered in the EKA workspace; run 'eka sync' (auto-registers) or 'eka project register' first
```

The repository exists but is not registered. Fix: `eka sync` (auto-registers from `eka.yaml`) or `eka project register .`. Runtime commands also refuse when there is **no workspace at all** (missing `~/.eka`/`$EKA_HOME`) — `eka sync` creates the workspace; read-only commands never do (by design).

### C. Namespace mismatch (ADR-020)

```
eka: sync refused: the repository content namespace <contentNS> differs from the registered repository namespace <repoNS>; run 'eka sync --override' to align the repository identity to <contentNS>
```

Content spanning multiple namespaces refuses without an override (`a repository is one platform`). Fix: for the single-distinct-namespace case, `eka sync --override` aligns the identity to the content (one-time; identity freezes again). For multi-namespace content: consolidate the content — never force it.

### D. Identity / subject errors

| Message | Cause | Fix |
|---|---|---|
| `cannot parse "<form>" ... unknown type token` | unknown type token in the target | check the token — knowledge: `vis` `str` `req` `fnd` `arc` `adr` `dec` `spec` `std` `gls` `scp` `epc` `plan` `trc` `rvw` `run` `rel`; execution: `ctr` `sto` `ts` `bug` `td` `ch` `spk` `ses` `cmt`; `tkt` is authorable via `eka new tkt:` (state vector is empty — it is a pure projection, refreshed on read) |
| `the namespace is required` / unqualified-form refusal | `eka get`/`eka context` resolve globally — `<type>:<id>` is ambiguous | use `<ns>/<type>:<id>[:<v>]` |
| unknown identity (exit 2) | the form does not resolve | find it: `eka get <domain> [--type <token>]`, then address the resolved form |
| `issue number #<n> is ambiguous: <a>, <b>; narrow it with a projection or type (e.g. 'eka view ticket #1')` | the number exists in several per-group counters (work items, tickets, notes count independently) | narrow: `eka view ticket #1`, or use the canonical form |
| line vs instance confusion | `<ns>/<type>:<id>` resolves to the **highest (latest)** instance version; transitions/notes address **lines** | for a specific (older) instance use the versioned canonical form `<ns>/<type>:<id>:<v>` or `--timeline`; for transitions/notes use the line form |

### E. Validation failures

```
Verdict: FAIL
eka: publish refused: draft feather/adr:<id> failed CKO-level validation with N blocking error(s); the draft was kept
```

The report lists findings by rule (R0–R13). Over MCP the full report is embedded in the refusal itself (see "MCP tool refusals carry the full report"). Common blockers:

- **R6 `missing dimension on knowledge artifact type "<t>"`** — knowledge types (`vis`/`str`/`req`/`fnd`/`arc`/`adr`/`dec`/`spec`/`std`/`gls`/`scp`/`epc`/`plan`/`trc`/`rvw`/`run`/`rel`) require `--dimension <token>` at scaffold. Work items and containers do not.
- State values not in the type's owned set, or change-log entries missing/out of order.
- Relationship targets that do not resolve (draft tolerance covers only draft targets).
- R10 warnings (missing upward traceability) do **not** block — but resolve them before relying on the object.

Fix the draft (it was kept) and publish again. `eka validate` failures: the repository must pass with 0 errors before export/sync operate.

### F. Transition refusals

| Message | Cause | Fix |
|---|---|---|
| `transition <from> -> <to> is not in the D1 table; legal transitions from "<from>": <list>` | illegal state jump | follow the D1 table (`planned → todo → in-progress → in-review → done`; `canceled` re-activates to `todo`; `done` exits only to `canceled`) |
| gate refusal (in-review needs a resolved implementation note; done needs every note resolved — R13) | missing note evidence | `eka note <line> --role implementation`, set `noteState: resolved`, publish |
| `not registered in the current active container` | the work item has no ticket deriving from the active container | confirm in a terminal or pass `--force` (never bypasses gates); over MCP the refusal says it: retry with `confirmed: true`; preferably fix the container registration |
| container activation/completion refusals | exactly-one-active rule, depends-on plan not approved, or items not done/canceled | complete the active container first; approve the plan; finish the items |
| plan cannot go `immutable` | immutability is the container lock, not a direct transition | activate the container that locks the plan |

### G. Note refusals

```
eka: note: note: feather/sto:<id>:2 is a canonical published form; notes address the subject line
```

Notes (`cmt-`) discuss a subject **line**, never a specific instance. Fix: `eka note feather/sto:<id> --role <role>` (line form).

### H. Draft lifecycle refusals

- `draft not found` / second publish fails — the draft file is a **single-use ticket**; publish removes it. Scaffold a new draft.
- scaffold collision (`<project>/<type>-<id>` exists) — use a different id or discard the existing draft.
- `eka edit` refused outside a terminal / on published forms — edit the draft JSON file directly (it is mutable workspace-local content) or use `--content-file`.

### I. Snapshot / sync failures

- `snapshot package refused` (exit 1) — the snapshot is corrupt; it is **never** silently skipped. Restore the committed snapshot from Git (`git checkout <repo>/exchange/snapshots`) and re-sync.
- docs-mode pull refused (exit 1, full report) — the docs tree fails conformance; fix the authoring, then re-run.
- `sync refused: ... not an EKA repository` — see class A.
- deletions not applied — by design (additive transport); superseded units stay as history.

### J. Integrity violations

```
• payload-hash <hash>: recomputed SHA-256(unit.json || content) is <got>
```

`eka integrity check` detected store inconsistency (manual modification or corruption). **Never "fix" the store by hand** — the canonical store is a private persistence implementation. Re-sync from the trusted snapshot, or rebuild the workspace. History payloads (unreferenced) are expected, never violations.

### K. Status regression (state moved backwards without intent)

Symptom: an item that was `in-progress` (or `in-review`) resolves as `todo` again; no deliberate pull-back was run. This is a **silent regression**, not a checkpoint error.

Causes:

- a full `eka sync` (or `eka sync pull`, or docs-mode re-seed) ran while the snapshot/docs tree was stale — the pull side re-points the line's reference to the pulled (older) instance. Transitions never touch the docs tree, so docs/snapshot lag until `eka sync push`;
- an explicit `eka transition <line> todo` or `--backward` ran as a routine step — D1 makes one-step pull-back (`in-progress → todo`) legal, so it succeeds without a warning;
- two agents racing: one transitions forward while another pulls a stale snapshot.

Fix:

```sh
eka get <ns>/<type>:<id>          # confirm the actual (regressed) state
eka transition <line> in-progress # re-advance forward (D1-legal)
eka sync push                     # refresh the snapshot
eka get <ns>/<type>:<id>          # verify the intended state resolves
```

Record the regression (item, observed state, likely cause) — in the checkpoint for execution runs, in the change-log for corrections.

Prevent:

- during active execution use `eka sync push` only; full `eka sync` / `eka sync pull` only at intake/resume, always followed by `eka get` verification of the affected lines;
- pull-back only as a deliberate, recorded correction — never an automatic or routine step;
- read the actual state before any transition (get → compare → align; see [eka-knowledge-modification](../eka-knowledge-modification/SKILL.md)).

## Quick diagnostic ladder

```sh
eka status              # workspace exists? what is registered? last sync?
eka project list        # which repositories are bound to which projects?
eka validate            # authoring conformance — rule findings
eka integrity check     # store integrity — hash/reference/decode findings
eka help <command>      # the command's contract, flags, exit codes
```

## Do NOT

- Do not bypass a refusal with a workaround (editing the store, hand-writing snapshots, faking change-log entries).
- Do not "fix" findings by mutating canonical objects — create revisions (see [eka-knowledge-modification](../eka-knowledge-modification/SKILL.md)).
- Do not ignore warnings — they are non-blocking by design (R10, draft tolerance), but each must be understood.
- Do not assume a message is a bug — the refusal strings are byte-pinned CLI contract; if the behavior still looks wrong, check `eka version` and the CLI docs before anything else.
