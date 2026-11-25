---
name: eka-knowledge-modification
description: Use when you need to change existing Engineering Knowledge — advance a work item's state, approve a plan, activate or complete a container, revise a knowledge object, correct an error, retire mistaken knowledge, or unlink a ticket edge. Teaches the immutable/append-only discipline: never hard-delete canonical objects; changes create new revisions (`eka publish` on a new draft, `eka transition` for state changes, `eka retire`/`eka unrelate` for sanctioned removal) while preserving identity, relationships, state, stratum, traceability, and integrity.
---

# Knowledge Modification

Engineering Knowledge is **immutable and append-only**. There is no edit path for canonical objects — "modify" means *create a new revision of the line*. This is not a limitation to work around; it is what makes history derivable, synchronization idempotent, and knowledge trustworthy.

**Never mutate an existing canonical object. Never edit a published form. Never rewrite the store.**

## Sanctioned change paths

| Change kind | Path | Mechanism |
|---|---|---|
| **State change** (work item, plan, container) | `eka transition` | publishes a new immutable payload for the line with the new state + appended change-log entry; the reference moves forward |
| **Content/knowledge revision** (new content, correction, new relationship set) | new draft on the same line → `eka publish` | a new draft of `<type>:<id>` publishes as the next instance version (max + 1), a new immutable object alongside the old ones |
| **Assignment edge change** (work item → member) | `eka assign` / `eka reassign` / `eka unassign` | the single `assigned-to` edge is set, moved, or removed in place — same instance version, no churn (a published payload is re-pointed, a draft's relationships block is mutated) |
| **Soft-delete published CKO** (salah lahir/duplikat/typo) | `eka retire` | new instance highest+1 with existence-state `retired`/`archived`, content frozen — history retained, never purged |
| **Ticket edge removal** (cabut 1 edge derives-from ticket) | `eka unrelate` | same-version re-point: removes one ticket `derives-from` edge in place, no instance churn |
| Legacy docs tree | edit the authoring file → `eka validate` → `eka sync` (docs-mode re-seed) | the authoring adapter's revision path; the compiled CKO replaces the old one in the store |

Assignment edges are the narrow third path: neither a transition nor a revision — the explicit assignment commands rewrite the work item's single `assigned-to` edge (single-assignee, ADR-029) with no instance churn. Only work items (`sto-`/`ts-`/`bug-`/`td-`/`ch-`/`spk-`) are assignable, the member must be a resolvable `mbr-` line of the same repository, and refusals are deterministic (see [eka-engineering-workflow](../eka-engineering-workflow/SKILL.md)). Assignment never creates container membership — tickets remain the sole membership contract. The eka-mcp server exposes no assignment tools yet: CLI only today.

## Path 1 — State transitions (`eka transition`)

```sh
eka transition <target> <to>            # explicit destination
eka transition <target> --forward       # next sequential state
eka transition <target> --backward      # one-step pull-back (work items)
```

- Targets are **lines** in the workspace store: `<type>:<id>` (repository namespace applies) or `<ns>/<type>:<id>`. The line must exist — if it does not resolve, the repository is probably not registered yet: run `eka sync` **once** at intake, never as a per-transition routine (see the sync-pull hazard below; after registration reads and transitions run directly on the shared store).
- **Work items** (`sto`/`ts`/`bug`/`td`/`ch`/`spk`) move along the execution-state table: `planned → todo → in-progress → in-review → done`; `canceled` re-activates to `todo`; `done` is terminal.
- **Plans** (`plan-`): `draft → approved` via `--forward`; the `immutable` planning state is the container lock — it happens atomically with container activation and cannot be requested directly.
- **Containers** (`ctr-`): `planned → active → completed`. Activation is gated on the exactly-one-active rule and on the depends-on plan being approved; activation **locks the plan** atomically. Completion is gated on every registered work item being done or canceled.
- **Gates (R13) are checked, not skippable**: `in-review` requires a resolved implementation note; `done` requires every note resolved. `--force` never bypasses a transition gate — it only confirms the non-registered-in-active-container warning.
- Every transition appends the change-log entry and publishes a new immutable payload — the line's history grows; nothing is overwritten.

Transition etiquette:

- Do not fast-forward a line through states it has not earned (planned → done in one step skips the gates and the review evidence).
- A work item not registered in the current active container warns and requires confirmation (`--force` outside a terminal) — prefer registering it via its ticket/container relationship first.
- After transitions in the workspace, `eka sync push` refreshes the repository snapshot for repository-attributed knowledge.

### Transition discipline (the Status Context Protocol)

Every transition is a get → compare → align loop:

1. **Get** — read the line's ACTUAL current state first: `eka get <line>`. The store is the truth; never transition from a document, a checkpoint, or a memory of what the state "should" be.
2. **Compare** — the current state against the state the work actually requires. `in-progress` belongs at work start (never as a post-completion blip), `in-review` after a resolved implementation note (R13), `done` only after the merge landed.
3. **Align** — transition only a mismatch. A line already at the state its stage requires is left untouched (transitions are idempotent in intent — a no-op re-transition is still a history entry, so do not add it).
4. **Forward-only during execution** — while an execution is active, advance states only. Pull-back (`in-progress → todo`, `in-review → in-progress`) is D1-legal but exists for corrections (premature transition, wrong item): it is a deliberate, documented act with its own change-log entry, owned by the agent orchestrating the execution — never an automatic or routine step.
5. **Persist and verify** — after the transition, `eka sync push`, then `eka get <line>` to verify the intended state is what the store resolves.

### The sync-pull hazard (silent regression)

Transitions write the workspace store only; the repository docs tree and snapshots lag until `eka sync push` (the docs tree is legacy authoring — transitions never touch it).

A full `eka sync` (pull then push) or `eka sync pull` re-seeds the store from the snapshot — or from the docs tree when no snapshot exists (`--from-docs` always re-seeds, no digest skip) — and the pull side can re-point the line's reference to an older instance. Result: an item that was advanced to `in-progress` resolves back to `todo` with nobody intending it.

Prevention:

- During active execution, use `eka sync push` only.
- Full `eka sync` / `eka sync pull` is for intake, migration, and conscious re-seeds — always followed by verifying the affected lines with `eka get`.
- When a regression is observed (state earlier than the work context expects), re-transition the line forward to the intended state, `eka sync push`, and record the correction in the change-log — never accept the regressed state as reality.

## Path 2 — Knowledge revision (new instance version)

To revise content, add relationships, or correct an error:

```sh
eka new <ns>/<type>:<id>          # scaffold a NEW draft on the SAME line (do not touch the old object)
# ... fill content, keep identity and relationships truthful ...
eka publish <target>              # instance version = line's highest + 1
```

- **Identity is preserved**: same namespace, type, id — new instance version. References to the line keep working; references to the exact old instance keep pointing at the old immutable object (that is the point of immutability).
- **Relationships**: carry forward the relationships that still hold; change only what the revision changes. Relationship integrity is validated at publish (draft tolerance applies to draft targets only).
- **State**: the new instance starts from the line's actual current state — never regress state to a past value unless the change is a sanctioned correction (and record it in the change-log; forward-only is the rule, P7).
- **Stratum**: a revision never changes domain/stratum. Knowledge stays in its domain; moving knowledge between domains is a different artifact, not a revision.
- **Traceability**: the change-log records the transition(s) with dates and authors; `prev_hash` lineage ties the new payload to the line's history. Integrity is preserved by construction — never hand-edit hashes.

Corrections follow P7: corrections happen through new instances + relationships, never through mutation of the old object. A corrected decision is a new revision (or a new decision record `dec-`) that supersedes/amends — and supersession/amendment is **same-stratum only** (R12): higher-stratum knowledge can never be superseded by lower-stratum knowledge, and no artifact may target a higher stratum.

## Path 3 — Legacy docs tree

```sh
# edit the authoring file in place (the AUTHORING representation is mutable)
eka validate          # 0 errors required
eka sync pull --from-docs  # re-seed: compile the authoring into the canonical store
```

The compiled CKO for the line is replaced; older payloads remain in the store as history. Note the semantic difference: the docs tree is an authoring adapter — changing it changes the compiled knowledge; the runtime store itself is still never touched directly.

## Path 4 — Retire / archive (`eka retire`)

Soft-delete untuk CKO published yang salah lahir, duplikat, atau typo — bukan hard delete. Retire menerbitkan instance baru highest+1 dengan existence-state `retired` (default) atau `archived` (`--as`), content dibekukan dari instance aktif terakhir.

```sh
eka retire <target> --reason "..." [--by ...] [--force] [--dry-run] [--json] [--as retired|archived] [--cascade=cmt]
```

- **Target adalah line published**: `<type>:<id>` atau `<ns>/<type>:<id>`. Draft ditolak (pakai `eka discard` untuk draft); line unknown ditolak; line yang sudah retired bersifat idempoten (exit 0, tulis apa-apa).
- **Gates**: downstream masih aktif memblokir — instance `depends-on`/`derives-from`/`validates`/`supersedes`/`amends` yang masih active ditolak dengan sorted list yang harus di-retire/direvisi dulu; `ctr-` active dan `plan-` approved/immutable dilindungi (protected); `--reason` minimal 10 karakter; non-TTY wajib `--force` (`--force` hanya konfirmasi interaktif — tidak mem-bypass gate lain); `--dry-run` read-only menampilkan dampak tanpa menulis; `--json` emits schema `eka-retire-v1`.
- **Ticket retire**: karena R4/R7 melarang state pada `tkt-`, retire ticket menulis marker retirement di content (bukan state). Fail-closed: ticket yang di-retire tetap blocker all-done sampai di-unlink. `--dry-run` menampilkan membership + gateImpact.
- Tidak ada hard delete / purge di EKA — `eka discard` tetap draft-only.

## Path 5 — Ticket unlink (`eka unrelate`)

Cabut 1 edge `derives-from` pada ticket (`tkt-`) — same-version re-point, tanpa instance churn:

```sh
eka unrelate <tkt-line> <edge-target> [--force] [--json]
```

- **Gate R8**: sisakan minimal 1 `ctr-` resolve — unlink edge terakhir yang masih resolve ditolak; `--json` emits schema `eka-unrelate-v1`.
- **Container lock**: ticket pada container `completed` terkunci (locked); ticket pada container `active` ditolak kecuali ticket sudah retired.
- Untuk melepas ticket yang di-retire dari gate all-done (fail-closed), unlink adalah jalurnya — bukan cancel, bukan delete.

### Unlink vs retire vs supersede vs cancel

| Aksi | Apa yang terjadi | Kapan dipakai |
|---|---|---|
| `unrelate` | cabut 1 edge ticket → container; ticket tetap ada | ticket salah pasang ke container; melepas ticket retired dari gate all-done |
| `retire` | soft-delete 1 line (instance baru `retired`/`archived`, content frozen) | salah lahir / duplikat / typo pada CKO published |
| supersede/amend (revisi Path 2) | koreksi isi lewat instance baru yang menggantikan/melengkapi | isi salah tapi artifact-nya sah dan harus tetap hidup |
| `cancel` (transition) | state work item → `canceled` | pekerjaan dibatalkan, bukan knowledge-nya yang salah |

## What modification must preserve

| Invariant | How |
|---|---|
| **Identity** | same line identity; new instance version (or transition on the line) |
| **Relationships** | carried forward truthfully; validated at publish |
| **State** | current-state truthful; forward-only transitions; change-log per transition |
| **Knowledge Stratum** | domain/stratum never change through revision; supersession/amendment same-stratum only |
| **Traceability** | change-log entries with dates and authors; `prev_hash` lineage |
| **Integrity** | validation gates; object hashes content-derived; `eka integrity check` verifies |

## AI safety rules (modification-specific)

- **Never** edit a published canonical object or rewrite a snapshot by hand.
- **Never** bypass `eka transition` by directly changing a work item's state in a draft and publishing a fake history (the change-log must match the sanctioned transitions).
- **Never** transition without first reading the line's actual current state (`eka get`) — stale assumptions cause double-transitions and unintended pull-backs.
- **Never** change higher-stratum knowledge to justify a lower-stratum implementation — a conflict resolves downward (the lower stratum changes).
- **Never** regress state silently; if a correction requires regression, it is a deliberate, documented act with its own change-log entries.
- **Never** run a full `eka sync` / `eka sync pull` mid-execution — the pull side can re-point references to older instances (silent regression); use `eka sync push` during active work and verify with `eka get` after any pull.
- **Never** hard-delete knowledge: there is no purge path — the sanctioned path for salah lahir/duplikat/typo is `eka retire` (soft-delete, history retained); `eka discard` is draft-only. Superseded and retired instances stay as history (retained payloads are history, not garbage).

## Real example (Feather Reference Project)

```sh
# advance a work item: planned → todo (gates checked)
eka transition feather/td:reduce-query-count todo

# approve a draft plan (planning-state: draft → approved)
eka transition feather/plan:roadmap-v2 --forward

# complete the active container (gated on all work items done/canceled)
eka transition feather/ctr:wave-7 completed

# revise a decision: new draft on the same line, published as instance 2
eka new feather/adr:content-storage
# ... fill the revised decision ...
eka publish feather/adr:content-storage

# verify: the line's history shows both instances; the store stays clean
eka get feather/adr:content-storage --timeline --no-content
eka integrity check
```

## Next

- Creating brand-new knowledge → [eka-knowledge-authoring](../eka-knowledge-authoring/SKILL.md)
- Verifying a change is sound → [eka-knowledge-review](../eka-knowledge-review/SKILL.md)
