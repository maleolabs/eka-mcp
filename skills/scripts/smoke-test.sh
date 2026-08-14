#!/usr/bin/env bash
# EKA AI Skill Pack — smoke test.
#
# Executes the command surface the skills teach against the EKA Reference
# Project (reference/project) in a scratch workspace, and asserts the
# documented behaviors. Exit 0 = all assertions pass.
#
# Usage:
#   skills/scripts/smoke-test.sh            # uses the eka binary from PATH
#   EKA_PATH=/path/to/eka skills/scripts/smoke-test.sh
#   EKA_HOME_OVERRIDE=/tmp/eka-smoke skills/scripts/smoke-test.sh   # custom scratch workspace
#
# The script never touches the real ~/.eka and never modifies the
# repository (all writes go to the scratch workspace; the smoke draft
# is discarded at the end).

set -u

REPO_ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
EKA_PATH="${EKA_PATH:-eka}"
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

# ---- environment ----
if ! command -v "$EKA_PATH" >/dev/null 2>&1; then
  say "eka binary not found: $EKA_PATH (build it: go build -o /tmp/eka ./cmd/eka)"
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

# ---- 8. install command (embedded pack) ----
INSTALL_DIR="/tmp/eka-install-smoke"
rm -rf "$INSTALL_DIR"

out=$("$EKA_PATH" install skills --dir "$INSTALL_DIR/skills" --dry-run 2>&1)
assert_contains "install: dry-run plan" "$out" "Dry-run: no changes were written."
assert_contains "install: eleven skills planned" "$out" "eka-feedback"
assert_contains "install: eleven skills planned" "$out" "eka-troubleshooting"

out=$("$EKA_PATH" install skills --dir "$INSTALL_DIR/skills" 2>&1)
assert_contains "install: skills installed" "$out" "(installed)"
if [ -f "$INSTALL_DIR/skills/eka-orientation/SKILL.md" ]; then pass "install: skill folder with SKILL.md"; else fail "install: skill folder missing"; fi
if [ -f "$INSTALL_DIR/skills/eka-knowledge-authoring/templates/drafts/adr-template.json" ]; then pass "install: templates travel with the skill"; else fail "install: templates missing"; fi

out=$("$EKA_PATH" install skills --dir "$INSTALL_DIR/skills" 2>&1)
assert_contains "install: re-run refresh" "$out" "(unchanged (refresh))"

out=$("$EKA_PATH" install commands --dir "$INSTALL_DIR/commands" 2>&1)
assert_contains "install: commands installed" "$out" "(installed)"
if [ -f "$INSTALL_DIR/commands/eka-execute.md" ]; then pass "install: command file"; else fail "install: command file missing"; fi

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
out=$(cd "$PROJECT" && "$EKA_PATH" discard feather/cmt:smoke-test-implementation --force 2>&1)
if [ -f "$NOTE_DRAFT" ]; then fail "discard: note draft still present"; else pass "discard: note draft removed"; fi

say ""
say "== results: $PASSED passed, $FAILED failed =="
[ "$FAILED" -eq 0 ] && exit 0 || exit 1
