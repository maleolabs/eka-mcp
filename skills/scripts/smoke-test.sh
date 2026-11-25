#!/usr/bin/env bash
# EKA AI Skill Pack — smoke test.
#
# Executes the command surface the skills teach against the EKA Reference
# Project (reference/project) in a scratch workspace, and asserts the
# documented behaviors. Exit 0 = all assertions pass.
#
# Usage:
#   skills/scripts/smoke-test.sh            # uses the eka + eka-mcp binaries from PATH
#   EKA_PATH=/path/to/eka EKA_MCP_PATH=/path/to/eka-mcp skills/scripts/smoke-test.sh
#   EKA_HOME_OVERRIDE=/tmp/eka-smoke skills/scripts/smoke-test.sh   # custom scratch workspace
#
# The script never touches the real ~/.eka and never modifies the
# repository (all writes go to the scratch workspace; the smoke draft
# is discarded at the end).

set -u

REPO_ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
EKA_PATH="${EKA_PATH:-eka}"
EKA_MCP_PATH="${EKA_MCP_PATH:-eka-mcp}"
SMOKE_HOME="${EKA_HOME_OVERRIDE:-/tmp/eka-smoke}"
PROJECT="$REPO_ROOT/reference/project"
FAILED=0
PASSED=0

say()  { printf '%s\n' "$*"; }
pass() { PASSED=$((PASSED+1)); say "  PASS: $*"; }
fail() { FAILED=$((FAILED+1)); say "  FAIL: $*"; }

assert_contains() { # <label> <haystack> <needle>
  if printf '%s' "$2" | grep -qF -- "$3"; then pass "$1"; else fail "$1 (missing: $3)"; fi
}

assert_valid_json() { # <label> <json-text>
  if printf '%s' "$2" | jq -e . >/dev/null 2>&1; then pass "$1"; else fail "$1 (output is not valid JSON)"; fi
}

jsonq() { # <json-text> <jq-filter> — raw value; empty string on parse failure
  printf '%s' "$1" | jq -r "$2" 2>/dev/null
}

# ---- environment ----
if ! command -v "$EKA_PATH" >/dev/null 2>&1; then
  say "eka binary not found: $EKA_PATH (build it: go build -o /tmp/eka ./cmd/eka)"
  exit 2
fi
if ! command -v "$EKA_MCP_PATH" >/dev/null 2>&1; then
  say "eka-mcp binary not found: $EKA_MCP_PATH (build it: go build -o /tmp/eka-mcp ./cmd/eka-mcp)"
  exit 2
fi
if ! command -v jq >/dev/null 2>&1; then
  say "jq not found (required for the eka-mcp configure --json assertions)"
  exit 2
fi
if [ ! -f "$PROJECT/eka.yaml" ]; then
  say "reference project not found: $PROJECT"
  exit 2
fi

export EKA_HOME="$SMOKE_HOME"
rm -rf "$SMOKE_HOME"
mkdir -p "$SMOKE_HOME"

say "== smoke test: EKA_HOME=$SMOKE_HOME =="

# ---- 1. setup: register + sync ----
out=$(cd "$REPO_ROOT" && "$EKA_PATH" project register "$PROJECT" 2>&1)
assert_contains "project register" "$out" "Status: registered"

out=$(cd "$REPO_ROOT" && "$EKA_PATH" sync "$PROJECT" 2>&1)
assert_contains "sync pull (snapshot mode)" "$out" "Pull: snapshot: 37 units, 0 attachments"
assert_contains "sync push" "$out" "Push: 37 units, 0 attachments"

out=$(cd "$REPO_ROOT" && "$EKA_PATH" sync "$PROJECT" 2>&1)
assert_contains "re-sync idempotent" "$out" "Status: unchanged"

# ---- 2. conformance + integrity ----
out=$(cd "$PROJECT" && "$EKA_PATH" validate . 2>&1)
assert_contains "validate: 0 errors" "$out" "Errors: 0"
assert_contains "validate: 0 warnings" "$out" "Warnings: 0"
assert_contains "validate: PASS" "$out" "Repository conforms to EKA v2.0"

out=$("$EKA_PATH" integrity check 2>&1)
assert_contains "integrity: clean" "$out" "Violations: 0"

# ---- 3. retrieval surface ----
out=$(cd "$PROJECT" && "$EKA_PATH" get feather/sto:publish-post:1 --no-content 2>&1)
assert_contains "get: schema eka-cko-v2" "$out" '"schema": "eka-cko-v2"'
assert_contains "get: canonical form" "$out" '"canonicalForm": "feather/sto:publish-post:1"'
assert_contains "get: derived domain" "$out" '"engineeringDomain": "Execution"'

out=$(cd "$PROJECT" && "$EKA_PATH" get discovery --no-content 2>&1)
assert_contains "get: domain collection count" "$out" '"count": 6'

out=$(cd "$PROJECT" && "$EKA_PATH" get containers --no-content 2>&1)
assert_contains "get: containers collection" "$out" '"collection": "containers"'

# ---- 4. context surface ----
out=$(cd "$PROJECT" && "$EKA_PATH" context feather/sto:publish-post --depth engineering --json 2>&1)
assert_contains "context: schema eka-context-v1" "$out" '"schema": "eka-context-v1"'
assert_contains "context: depth engineering" "$out" '"depth": "engineering"'
assert_contains "context: constraints section" "$out" '"constraints"'

out=$(cd "$PROJECT" && "$EKA_PATH" context '#5' --json 2>&1)
assert_contains "context: issue number #5" "$out" 'feather/sto:publish-post:1'

# ---- 5. authoring loop (scratch workspace only) ----
out=$(cd "$PROJECT" && "$EKA_PATH" new feather/sto:smoke-test 2>&1)
assert_contains "new: draft scaffolded" "$out" "Draft: sto:smoke-test"
DRAFT="$SMOKE_HOME/drafts/feather/sto-smoke-test.json"
if [ -f "$DRAFT" ]; then pass "new: draft file exists"; else fail "new: draft file missing"; fi

out=$(cd "$PROJECT" && "$EKA_PATH" publish feather/sto:smoke-test 2>&1)
assert_contains "publish: instance version 1" "$out" "Published: feather/sto:smoke-test:1"

out=$(cd "$PROJECT" && "$EKA_PATH" get feather/sto:smoke-test --timeline --no-content 2>&1)
assert_contains "get: timeline has the instance" "$out" 'feather/sto:smoke-test:1'

out=$(cd "$PROJECT" && "$EKA_PATH" note feather/sto:smoke-test --role implementation 2>&1)
assert_contains "note: cmt draft scaffolded" "$out" "Draft: cmt:smoke-test-implementation"
NOTE_DRAFT="$SMOKE_HOME/drafts/feather/cmt-smoke-test-implementation.json"
if [ -f "$NOTE_DRAFT" ]; then pass "note: draft file exists"; else fail "note: draft file missing"; fi

# ---- 6. troubleshooting surface (deterministic refusals) ----
out=$(cd /tmp && "$EKA_PATH" get discovery 2>&1)
assert_contains "refusal: not an EKA repository" "$out" "is not an EKA repository (no eka.yaml)"

out=$(cd "$PROJECT" && "$EKA_PATH" transition feather/bug:empty-title-crash in-progress 2>&1)
assert_contains "refusal: illegal transition (D1)" "$out" "is not in the D1 table"

out=$(cd "$PROJECT" && "$EKA_PATH" context '#1' 2>&1)
assert_contains "refusal: ambiguous issue number" "$out" "is ambiguous"

# ---- 7. projection ----
out=$(cd "$PROJECT" && "$EKA_PATH" view execution 2>&1)
assert_contains "view: active container" "$out" "wave-7"

# ---- 8. configure surface (embedded pack install via eka-mcp, ADR-030) ----
# The pack-distribution vehicle is the eka-mcp binary's `configure`
# subcommand (--json is mandatory); the CLI's install surface was removed
# by ADR-030. Every write below lands inside a temporary fake HOME, never
# in the real one.
CFG_HOME="$(mktemp -d "${TMPDIR:-/tmp}/eka-configure-smoke.XXXXXX")"
cfg() { # run configure anchored in the fake HOME (cwd + HOME both contained)
  (cd "$CFG_HOME" && env HOME="$CFG_HOME" "$EKA_MCP_PATH" configure "$@" 2>&1)
}

# 8.1 dry-run: plan only — exit 0, valid JSON, actions + counts, nothing written
rc=0; out=$(cfg --target opencode --with-all --dry-run --json) || rc=$?
if [ "$rc" -eq 0 ]; then pass "configure: dry-run exits 0"; else fail "configure: dry-run exit code $rc (want 0)"; fi
assert_valid_json "configure: dry-run emits valid JSON" "$out"
assert_contains "configure: dry-run flag reported" "$(jsonq "$out" '.dryRun')" "true"

CREATED="$(jsonq "$out" '.counts.created')"; CREATED="${CREATED:-0}"
OVERWRITTEN="$(jsonq "$out" '.counts.overwritten')"; OVERWRITTEN="${OVERWRITTEN:-0}"
SKIPPED="$(jsonq "$out" '.counts.skipped')"; SKIPPED="${SKIPPED:-0}"
if [ "$CREATED" -gt 0 ]; then pass "configure: dry-run plans creations ($CREATED files)"; else fail "configure: dry-run plans no creations"; fi
if [ "$OVERWRITTEN" -eq 0 ] && [ "$SKIPPED" -eq 0 ]; then pass "configure: fresh-HOME plan has nothing to overwrite/skip"; else fail "configure: fresh-HOME plan unexpected counts (overwritten=$OVERWRITTEN skipped=$SKIPPED)"; fi

BAD_ACTIONS="$(jsonq "$out" '[.changes[].action] - ["create","overwrite","skip"] | length')"
if [ "${BAD_ACTIONS:-1}" -eq 0 ]; then pass "configure: changes[] actions are create|overwrite|skip"; else fail "configure: unexpected action value in changes[]"; fi
N_CHANGES="$(jsonq "$out" '.changes | length')"; N_CHANGES="${N_CHANGES:-0}"
if [ "$N_CHANGES" -eq $((CREATED + OVERWRITTEN + SKIPPED)) ]; then pass "configure: counts sum matches changes[] length"; else fail "configure: counts ($CREATED/$OVERWRITTEN/$SKIPPED) do not sum to changes[] length ($N_CHANGES)"; fi

SIDECAR_PLANNED="$(jsonq "$out" '[.changes[].path] | map(select(endswith("/DELEGATION.txt"))) | length')"
if [ "${SIDECAR_PLANNED:-0}" -ge 1 ]; then pass "configure: DELEGATION.txt sidecar planned (non-.md by name)"; else fail "configure: DELEGATION.txt sidecar missing from plan"; fi
if [ -e "$CFG_HOME/.config" ] || [ -e "$CFG_HOME/opencode.json" ]; then fail "configure: dry-run wrote to the filesystem"; else pass "configure: dry-run writes nothing"; fi

# 8.2 real install into the fake HOME: exit 0, plan parity, artifacts on disk
rc=0; out=$(cfg --target opencode --with-all --json) || rc=$?
if [ "$rc" -eq 0 ]; then pass "configure: install exits 0"; else fail "configure: install exit code $rc (want 0)"; fi
assert_valid_json "configure: install emits valid JSON" "$out"
INSTALLED_CREATED="$(jsonq "$out" '.counts.created')"; INSTALLED_CREATED="${INSTALLED_CREATED:-0}"
if [ "$INSTALLED_CREATED" -eq "$CREATED" ]; then pass "configure: install matches the dry-run plan ($INSTALLED_CREATED created)"; else fail "configure: install created $INSTALLED_CREATED, dry-run planned $CREATED"; fi

SKILL_LIST="$(jsonq "$out" '.installed.skills | join(" ")')"
assert_contains "configure: skills installed" "$SKILL_LIST" "eka-feedback"
assert_contains "configure: skills installed" "$SKILL_LIST" "eka-troubleshooting"
COMMAND_LIST="$(jsonq "$out" '.installed.commands | join(" ")')"
assert_contains "configure: commands installed" "$COMMAND_LIST" "eka-execute.md"

if [ -f "$CFG_HOME/.config/opencode/skills/eka-orientation/SKILL.md" ]; then pass "configure: skill folder with SKILL.md"; else fail "configure: skill folder missing"; fi
if [ -f "$CFG_HOME/.config/opencode/skills/eka-knowledge-authoring/templates/drafts/adr-template.json" ]; then pass "configure: templates travel with the skill"; else fail "configure: templates missing"; fi
if [ -f "$CFG_HOME/.config/opencode/commands/eka-execute.md" ]; then pass "configure: command file"; else fail "configure: command file missing"; fi

SIDECAR="$CFG_HOME/.config/opencode/commands/DELEGATION.txt"
if [ -f "$SIDECAR" ] && [ -s "$SIDECAR" ]; then pass "configure: DELEGATION.txt sidecar present next to commands"; else fail "configure: DELEGATION.txt sidecar missing/empty"; fi
case "$(basename "$SIDECAR")" in
  *.md) fail "configure: sidecar must not be a .md file" ;;
  *) pass "configure: sidecar extension is non-.md" ;;
esac

if [ -f "$CFG_HOME/opencode.json" ] && [ "$(jsonq "$(cat "$CFG_HOME/opencode.json")" '.mcp.eka.command[0] // ""')" != "" ]; then
  pass "configure: MCP client config entry written"
else
  fail "configure: MCP client config entry missing"
fi

# 8.3 idempotency: second identical run skips everything
rc=0; out=$(cfg --target opencode --with-all --json) || rc=$?
if [ "$rc" -eq 0 ]; then pass "configure: re-run exits 0"; else fail "configure: re-run exit code $rc (want 0)"; fi
RE_CREATED="$(jsonq "$out" '.counts.created')"; RE_CREATED="${RE_CREATED:-0}"
RE_OVERWRITTEN="$(jsonq "$out" '.counts.overwritten')"; RE_OVERWRITTEN="${RE_OVERWRITTEN:-0}"
RE_SKIPPED="$(jsonq "$out" '.counts.skipped')"; RE_SKIPPED="${RE_SKIPPED:-0}"
if [ "$RE_CREATED" -eq 0 ] && [ "$RE_OVERWRITTEN" -eq 0 ] && [ "$RE_SKIPPED" -eq "$INSTALLED_CREATED" ]; then
  pass "configure: re-run reports all skips ($RE_SKIPPED)"
else
  fail "configure: re-run not idempotent (created=$RE_CREATED overwritten=$RE_OVERWRITTEN skipped=$RE_SKIPPED)"
fi
ALL_SKIPS="$(jsonq "$out" '[.changes[].action] | all(. == "skip")')"
assert_contains "configure: re-run changes[] are all skip" "$ALL_SKIPS" "true"

# 8.4 codex refusal: commands are not installable for codex — deterministic,
# non-zero exit, and zero filesystem writes.
rc=0; out=$(cfg --target codex --with-commands --json) || rc=$?
if [ "$rc" -ne 0 ]; then pass "configure: codex --with-commands refuses (exit $rc)"; else fail "configure: codex --with-commands must exit non-zero"; fi
assert_contains "configure: codex refusal is deterministic" "$out" "cannot install commands"
rc2=0; out2=$(cfg --target codex --with-commands --json) || rc2=$?
if [ "$rc" -eq "$rc2" ] && [ "$out" = "$out2" ]; then pass "configure: codex refusal deterministic across runs"; else fail "configure: codex refusal differs between runs"; fi
if [ -e "$CFG_HOME/.agents" ]; then fail "configure: codex refusal wrote files"; else pass "configure: codex refusal writes nothing"; fi

# ---- 8b. feedback surface (ADR-026: draft → publish as GitHub issue) ----
FB_TITLE="smoke feedback report"
out=$("$EKA_PATH" feedback new --type bug --title "$FB_TITLE" --severity medium --source agent --command "eka smoke" 2>&1)
assert_contains "feedback: draft created" "$out" "Status   draft"
FEEDBACK_ID=$(printf '%s' "$out" | sed -n 's/.*ID: \(fbk-[0-9]*-[a-z0-9-]*\).*/\1/p' | head -1)
if [ -z "$FEEDBACK_ID" ]; then
  FEEDBACK_ID="fbk-$(date +%Y%m%d)-smoke-feedback-report"
  say "  (id fallback: $FEEDBACK_ID)"
fi
if [ -f "$SMOKE_HOME/feedback/$FEEDBACK_ID.md" ]; then pass "feedback: draft file exists"; else fail "feedback: draft file missing"; fi

out=$("$EKA_PATH" feedback list 2>&1)
assert_contains "feedback: list shows the draft" "$out" "$FEEDBACK_ID"

out=$("$EKA_PATH" feedback publish "$FEEDBACK_ID" 2>&1)
assert_contains "feedback: non-TTY publish refuses without --yes" "$out" "requires --yes outside a terminal"

out=$("$EKA_PATH" feedback publish "$FEEDBACK_ID" --yes 2>&1)
assert_contains "feedback: dev build refuses (no bundled token)" "$out" "issue token not bundled"

# ---- 9. adoption flow (fresh repo + migrated docs) ----
ADOPT_DIR="/tmp/eka-adopt-smoke"
rm -rf "$ADOPT_DIR" && mkdir -p "$ADOPT_DIR/docs/intent" "$ADOPT_DIR/docs/requirements"
out=$(cd "$ADOPT_DIR" && "$EKA_PATH" init --project adopt --namespace adopt 2>&1)
assert_contains "adopt: identity-only init" "$out" "Validation: PASS"
if [ -f "$ADOPT_DIR/eka.yaml" ]; then pass "adopt: eka.yaml written"; else fail "adopt: eka.yaml missing"; fi

# migrate three Discovery artifacts from the reference project (namespace rewritten)
for f in vis-feather-vision str-feather-2026; do
  sed 's/"namespace": "feather"/"namespace": "adopt"/; s/feather-2026/adopt-2026/g; s/feather-vision/adopt-vision/g' \
    "$PROJECT/docs/intent/$f.json" > "$ADOPT_DIR/docs/intent/$f.json"
done
sed 's/"namespace": "feather"/"namespace": "adopt"/; s/feather-2026/adopt-2026/g; s/feather-vision/adopt-vision/g' \
  "$PROJECT/docs/requirements/req-publishing-core.json" > "$ADOPT_DIR/docs/requirements/req-publishing-core.json"

out=$(cd "$ADOPT_DIR" && "$EKA_PATH" validate . 2>&1)
assert_contains "adopt: migrated docs validate" "$out" "Errors: 0"

out=$(cd "$ADOPT_DIR" && "$EKA_PATH" sync . 2>&1)
assert_contains "adopt: docs-mode pull" "$out" "Pull: docs: 3 units, 0 attachments"

out=$(cd "$ADOPT_DIR" && "$EKA_PATH" get adopt/req:publishing-core:1 --no-content 2>&1)
assert_contains "adopt: migrated knowledge retrievable" "$out" '"canonicalForm": "adopt/req:publishing-core:1"'

# ---- cleanup ----
rm -rf "$CFG_HOME"
out=$(cd "$PROJECT" && "$EKA_PATH" discard feather/cmt:smoke-test-implementation --force 2>&1)
if [ -f "$NOTE_DRAFT" ]; then fail "discard: note draft still present"; else pass "discard: note draft removed"; fi

say ""
say "== results: $PASSED passed, $FAILED failed =="
[ "$FAILED" -eq 0 ] && exit 0 || exit 1
