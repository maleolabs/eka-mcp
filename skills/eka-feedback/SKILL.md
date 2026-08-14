---
name: eka-feedback
description: Use when you find a shortcoming in EKA itself — a confusing refusal, a missing command, a rough edge, an idea for improvement — and want to report it to the EKA maintainers. Teaches the feedback loop: write a local draft with `eka feedback new`, then publish it as a GitHub issue with `eka feedback publish` (ADR-026). Also load at the end of a finished job when the session revealed an EKA limitation.
---

# EKA Feedback

EKA is a tool under active development, and its best testers are its daily users — humans and agents. When EKA gets in your way (a confusing refusal, a broken expectation, a missing capability, an unclear message), report it. The channel is built into the CLI (ADR-026): **zero configuration**, works on any release binary.

Feedback is **meta-information about the tool** — it is never engineering knowledge, never a CKO, and never part of the repository or canonical store. Reports live in `$EKA_HOME/feedback/` and are filed as GitHub issues on `maleolabs/engineering-knowledge-architecture`.

## The feedback loop

```
Write a draft  →  Review it  →  Publish as a GitHub issue
eka feedback new   eka feedback list   eka feedback publish
```

### 1. Write the draft

```sh
eka feedback new --type bug --title "eka context refuses an unqualified subject" \
  --severity medium --source agent --command "eka context sto:12" \
  --content-file report.md
```

- `--type`: `bug` (something is broken), `suggestion` (a new capability), `improvement` (a change to existing behavior), `question`.
- `--severity`: `low` / `medium` / `high`. Default `low` — reserve `high` for real breakage.
- `--source`: `human` or `agent` — always declare `agent` when you are an agent; it tells maintainers who hit the issue.
- `--command`: what you actually ran (defaults to the invoking command line). Agents should pass the exact failing invocation — it is the single most useful field for reproduction.
- `--content-file`: the markdown body. Without it the scaffold is created — a `bug` gets `Steps to reproduce / Expected / Actual`, other types get `Description`.
- Triage metadata is auto-injected: `eka_version`, `os`, `created`.

The file format (YAML frontmatter + markdown body) is the record of what you reported — read it before publishing and fix anything wrong with your editor or a new `new` run.

### 2. Review before publishing

```sh
eka feedback list
```

Check the report once before it leaves your machine: is the title a real problem statement? Are the steps to reproduce concrete? Is the `command` field the exact invocation? A good report is **actionable**: what you ran, what happened, what you expected, and (for suggestions) what you imagine the fix looks like.

### 3. Publish as a GitHub issue

```sh
eka feedback publish fbk-20260812-context-subject-refusal --yes
```

- Creates the issue on the fixed target repository (the bundled build token, `issues: write` only — ADR-026) and rewrites the local file: `status: published`, `issue_number`, `issue_url`.
- **Idempotent**: publishing the same report twice refuses — never create a duplicate issue.
- On a terminal the title and target are confirmed first; in non-interactive runs (pipes, CI) `--yes` is required — a piped run never blocks on a prompt.
- After publishing, the issue link is in your local file — keep it as the record.

## Quality bar

- **One problem per report.** A report with two unrelated issues is harder to triage than two reports.
- **No known duplicates.** Before writing, ask: did this session (or an earlier one) already report this? `eka feedback list` shows your local history — the local record is the dedup surface.
- **Include the failure detail.** For a bug: the exact command, the refusal message (it is byte-pinned), the EKA version, and what you expected instead.
- **Do not report the fix only.** "Add flag X" without the underlying friction is a suggestion without a problem statement.
- **Never publish reflexively.** Feedback is a decision: a transient hiccup, an intentional design constraint, or a documented `eka help` behavior is not a shortcoming.

## Refusals you may see

| Message | Meaning | Fix |
|---|---|---|
| `issue token not bundled — use a release binary` | Dev/test/CI builds ship no token (exit 1) | Run the release binary — install via `eka update` or the install scripts |
| `publish requires --yes outside a terminal` | Non-interactive publish without `--yes` (exit 2) | Add `--yes` |
| `already published as #<n> <url>` | The report is already filed (exit 1) | Nothing to do — the issue exists |
| `unknown feedback "<id>"` | No such draft (exit 2) | Check `eka feedback list` for the exact id |

## When to report (agents)

- **At the end of a finished job**, as a conscious step: if the session revealed an EKA limitation, write + publish it before moving on (see [eka-engineering-workflow](../eka-engineering-workflow/SKILL.md) — the loop's final step).
- **In the middle of work**, when a refusal blocks you and the refusal is genuinely wrong: report it, then work around it with the documented path — never bypass the gate silently and never leave the report unwritten "to do later".
- **Do not report** when the failure was yours (a malformed command, an uninitialized repository) — the troubleshooting skill ([eka-troubleshooting](../eka-troubleshooting/SKILL.md)) is the fix, not feedback.
