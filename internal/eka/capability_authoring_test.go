package eka

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/maleolabs/eka-core/conformance"
	"github.com/maleolabs/eka-core/exchange"
	"github.com/maleolabs/eka-core/runtime"
	"github.com/maleolabs/eka-core/store"
	"github.com/maleolabs/eka-core/workspace"
	"github.com/maleolabs/eka-mcp/internal/mcp"
)

// authoringRuntime sets EKA_HOME to a temp dir and ensures the Runtime
// (the workspace the authoring surface writes into).
func authoringRuntime(t *testing.T) *runtime.Runtime {
	t.Helper()
	t.Setenv("EKA_HOME", t.TempDir())
	rt, err := runtime.Ensure()
	if err != nil {
		t.Fatalf("runtime.Ensure: %v", err)
	}
	t.Cleanup(func() { rt.Close() })
	return rt
}

// putUnit seeds one unit into the canonical store of the ensured
// workspace (the same store the runtime services read).
func putUnit(t *testing.T, u *exchange.Unit, project string) {
	t.Helper()
	ws, err := workspace.Ensure()
	if err != nil {
		t.Fatalf("workspace.Ensure: %v", err)
	}
	defer ws.Close()
	u.CanonicalIdentityForm = u.Identity.CanonicalForm()
	unitJSON, err := exchange.MarshalUnit(u)
	if err != nil {
		t.Fatalf("exchange.MarshalUnit: %v", err)
	}
	_, _, err = ws.Store().PutUnit(unitJSON, u.ContentPayload, store.Ref{
		Form:            u.CanonicalIdentityForm,
		ProjectID:       project,
		SourceRepo:      "seed",
		Namespace:       u.Identity.Namespace,
		Type:            u.Identity.Type,
		ID:              u.Identity.ID,
		InstanceVersion: u.Identity.InstanceVersion,
		Revision:        u.Revision,
		Dimension:       u.Classification.Dimension,
		Domain:          u.Classification.Domain,
		Phase:           u.Phase,
		UpdatedAt:       "2026-08-07T00:00:00Z",
	})
	if err != nil {
		t.Fatalf("store.PutUnit: %v", err)
	}
}

// registeredRepo creates a repository directory (basename "repo" so the
// registry name matches the eka.yaml identity) with an eka.yaml identity
// file, registers it in the workspace and returns the directory.
func registeredRepo(t *testing.T, rt *runtime.Runtime) string {
	t.Helper()
	repoDir := filepath.Join(t.TempDir(), "repo")
	writeEKAFile(t, repoDir, "proj", "repo", "test-ns")
	if _, _, _, err := rt.Workspace.RegisterRepo(repoDir, "proj"); err != nil {
		t.Fatalf("RegisterRepo: %v", err)
	}
	return repoDir
}

// --- Context -----------------------------------------------------------

// TestContextBuilds: Context builds the deterministic Context Object
// (schema eka-context-v1) around a seeded subject at the local depth.
func TestContextBuilds(t *testing.T) {
	authoringRuntime(t)
	putUnit(t, testUnit(), "feather")

	cap, err := Open()
	if err != nil {
		t.Fatal(err)
	}
	defer cap.Close()

	data, err := cap.Context("feather/adr:001-serialization:1", "feather", "local")
	if err != nil {
		t.Fatalf("Context failed: %v", err)
	}
	var obj map[string]any
	if err := json.Unmarshal(data, &obj); err != nil {
		t.Fatalf("Context must return JSON: %v\n%s", err, data)
	}
	if obj["schema"] != "eka-context-v1" {
		t.Errorf("schema = %v, want eka-context-v1", obj["schema"])
	}
	if obj["depth"] != "local" {
		t.Errorf("depth = %v, want local", obj["depth"])
	}
}

// TestContextUnknownDepth: an unknown depth token is refused
// deterministically (the ParseDepth usage-error path).
func TestContextUnknownDepth(t *testing.T) {
	authoringRuntime(t)
	putUnit(t, testUnit(), "feather")

	cap, err := Open()
	if err != nil {
		t.Fatal(err)
	}
	defer cap.Close()

	_, err = cap.Context("feather/adr:001-serialization:1", "feather", "deep")
	if err == nil || !strings.Contains(err.Error(), "unknown context depth") {
		t.Errorf("unknown depth must be refused, got %v", err)
	}
}

// --- Validate ----------------------------------------------------------

// TestValidateSkipped: a repository without a docs/ tree yields the
// deterministic skipped report — a clean pass with zero findings.
func TestValidateSkipped(t *testing.T) {
	authoringRuntime(t)
	cap, err := Open()
	if err != nil {
		t.Fatal(err)
	}
	defer cap.Close()

	root := t.TempDir() // no docs/ inside
	data, err := cap.Validate(root)
	if err != nil {
		t.Fatalf("Validate failed: %v", err)
	}
	var rep map[string]any
	if err := json.Unmarshal(data, &rep); err != nil {
		t.Fatalf("Validate must return JSON: %v\n%s", err, data)
	}
	if rep["schema"] != "eka-conformance-report-v1" {
		t.Errorf("schema = %v, want eka-conformance-report-v1", rep["schema"])
	}
	if rep["pass"] != true {
		t.Errorf("pass = %v, want true for a skipped scan", rep["pass"])
	}
	if rep["errors"] != float64(0) {
		t.Errorf("errors = %v, want 0", rep["errors"])
	}
	if results := rep["results"].([]any); len(results) != 0 {
		t.Errorf("results = %v, want empty", results)
	}
}

// TestValidateFindsViolations: a repository with a malformed docs/ tree
// yields blocking findings (errors > 0, pass false).
func TestValidateFindsViolations(t *testing.T) {
	authoringRuntime(t)
	cap, err := Open()
	if err != nil {
		t.Fatal(err)
	}
	defer cap.Close()

	root := t.TempDir()
	docs := filepath.Join(root, "docs")
	if err := os.MkdirAll(docs, 0o755); err != nil {
		t.Fatal(err)
	}
	// A knowledge artifact with a dangling reference (depends-on a
	// target that does not resolve) — Rule 5 fires as a blocking error.
	artifact := `---
namespace: eka-ref
type: adr
id: 002-dangling
instance-version: 1
revision: 1
content-state: accepted
existence-state: active
dimension: decisions
author: Engineering
created: 2026-08-05
updated: 2026-08-05
supersedes: []
derives-from: []
depends-on:
  - sto:ghost
change-log:
  - date: 2026-08-05
    domain: existence-state
    from: "-"
    to: active
    by: Engineering
  - date: 2026-08-05
    domain: content-state
    from: proposed
    to: accepted
    by: Engineering
---
# Title

## Context

ctx

## Decision

dec

## Consequences

cons

## Alternatives Considered

alt
`
	if err := os.WriteFile(filepath.Join(docs, "adr-002-dangling.md"), []byte(artifact), 0o644); err != nil {
		t.Fatal(err)
	}

	data, err := cap.Validate(root)
	if err != nil {
		t.Fatalf("Validate failed: %v", err)
	}
	var rep map[string]any
	if err := json.Unmarshal(data, &rep); err != nil {
		t.Fatal(err)
	}
	if rep["pass"] != false {
		t.Errorf("pass = %v, want false for a violating tree", rep["pass"])
	}
	if rep["errors"].(float64) < 1 {
		t.Errorf("errors = %v, want >= 1", rep["errors"])
	}
}

// --- NewDraft / Publish / View / DraftList / Discard -------------------

// TestNewDraftScaffolds: NewDraft scaffolds the deterministic draft
// (schema eka-draft-v1) with the resolved agent identity and the
// inline content merged over the template.
func TestNewDraftScaffolds(t *testing.T) {
	authoringRuntime(t)
	cap, err := Open()
	if err != nil {
		t.Fatal(err)
	}
	defer cap.Close()

	data, err := cap.NewDraft(mcp.NewDraftRequest{
		Project:   "feather",
		Namespace: "feather",
		Type:      "adr",
		ID:        "002-auth",
		By:        mcp.AuthorIdentity{Kind: "agent", Name: "mcp-agent"},
		Content:   map[string]any{"context": "auth context"},
	})
	if err != nil {
		t.Fatalf("NewDraft failed: %v", err)
	}
	var d map[string]any
	if err := json.Unmarshal(data, &d); err != nil {
		t.Fatalf("NewDraft must return JSON: %v\n%s", err, data)
	}
	if d["schema"] != "eka-draft-v1" {
		t.Errorf("schema = %v, want eka-draft-v1", d["schema"])
	}
	if d["type"] != "adr" || d["id"] != "002-auth" {
		t.Errorf("draft identity = %v", d)
	}
	path, _ := d["path"].(string)
	if path == "" {
		t.Fatal("draft result must carry the draft path")
	}
	// The draft file exists and carries the agent author + merged content.
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("draft file missing: %v", err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	if doc["author"] == nil {
		t.Error("draft must carry the agent author identity")
	}
	content := doc["content"].(map[string]any)
	if content["context"] != "auth context" {
		t.Errorf("content merge = %v, want the inline context", content["context"])
	}
}

// TestPublishDraft: the full draft -> publish cycle — the draft is
// validated, persisted as an immutable CKO (schema
// eka-publish-result-v1) and the draft file is removed (the single-use
// ticket).
func TestPublishDraft(t *testing.T) {
	authoringRuntime(t)
	cap, err := Open()
	if err != nil {
		t.Fatal(err)
	}
	defer cap.Close()

	draftData, err := cap.NewDraft(mcp.NewDraftRequest{
		Project:   "feather",
		Namespace: "feather",
		Type:      "adr",
		ID:        "003-publish",
		Dimension: "decisions",
		By:        mcp.AuthorIdentity{Kind: "agent", Name: "mcp-agent"},
	})
	if err != nil {
		t.Fatalf("NewDraft failed: %v", err)
	}
	var draftDoc map[string]any
	if err := json.Unmarshal(draftData, &draftDoc); err != nil {
		t.Fatal(err)
	}
	draftPath, _ := draftDoc["path"].(string)
	if draftPath == "" {
		t.Fatal("draft result must carry the draft path")
	}

	data, err := cap.Publish(mcp.PublishRequest{Target: "feather/adr:003-publish", Project: "feather"})
	if err != nil {
		t.Fatalf("Publish failed: %v", err)
	}
	var res map[string]any
	if err := json.Unmarshal(data, &res); err != nil {
		t.Fatalf("Publish must return JSON: %v\n%s", err, data)
	}
	if res["schema"] != "eka-publish-result-v1" {
		t.Errorf("schema = %v, want eka-publish-result-v1", res["schema"])
	}
	if res["form"] != "feather/adr:003-publish:1" {
		t.Errorf("form = %v, want feather/adr:003-publish:1", res["form"])
	}
	if res["objectHash"] == "" {
		t.Error("publish result must carry the object hash")
	}
	// The draft file is gone (single-use ticket).
	if _, err := os.Stat(draftPath); !os.IsNotExist(err) {
		t.Errorf("the draft file must be removed after publish, stat err = %v", err)
	}
	// The published object resolves.
	got, err := cap.Get("feather/adr:003-publish:1")
	if err != nil {
		t.Fatalf("published object must resolve: %v", err)
	}
	if !strings.Contains(string(got), "feather/adr:003-publish:1") {
		t.Errorf("published object = %s", got)
	}
}

// TestNoteCreatesDraft: Note creates one cmt- note draft (schema
// eka-note-result-v1) with the resolved agent identity.
func TestNoteCreatesDraft(t *testing.T) {
	rt := authoringRuntime(t)
	cap, err := Open()
	if err != nil {
		t.Fatal(err)
	}
	defer cap.Close()

	// The note subject must resolve in the workspace store and its
	// namespace must match the repository namespace.
	subject := testUnit()
	subject.Identity.Namespace = "test-ns"
	subject.CanonicalIdentityForm = "test-ns/adr:001-serialization:1"
	putUnit(t, subject, "proj")
	repoDir := registeredRepo(t, rt)

	data, err := cap.Note(mcp.NoteRequest{
		RepoPath: repoDir,
		Target:   "test-ns/adr:001-serialization",
		Role:     "implementation",
		By:       mcp.AuthorIdentity{Kind: "agent", Name: "mcp-agent"},
	})
	if err != nil {
		t.Fatalf("Note failed: %v", err)
	}
	var res map[string]any
	if err := json.Unmarshal(data, &res); err != nil {
		t.Fatalf("Note must return JSON: %v\n%s", err, data)
	}
	if res["schema"] != "eka-note-result-v1" {
		t.Errorf("schema = %v, want eka-note-result-v1", res["schema"])
	}
	if res["target"] != "test-ns/adr:001-serialization" {
		t.Errorf("target = %v", res["target"])
	}
	path, _ := res["path"].(string)
	if path == "" {
		t.Fatal("note result must carry the draft path")
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("note draft file missing: %v", err)
	}
}

// TestViewDraft: View returns the draft file content verbatim.
func TestViewDraft(t *testing.T) {
	authoringRuntime(t)
	cap, err := Open()
	if err != nil {
		t.Fatal(err)
	}
	defer cap.Close()

	if _, err := cap.NewDraft(mcp.NewDraftRequest{
		Project:   "feather",
		Namespace: "feather",
		Type:      "adr",
		ID:        "004-view",
		By:        mcp.AuthorIdentity{Kind: "agent", Name: "mcp-agent"},
	}); err != nil {
		t.Fatalf("NewDraft failed: %v", err)
	}

	data, err := cap.View("feather/adr:004-view", "feather")
	if err != nil {
		t.Fatalf("View failed: %v", err)
	}
	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("View must return the draft JSON: %v\n%s", err, data)
	}
	if doc["type"] != "adr" || doc["id"] != "004-view" {
		t.Errorf("viewed draft identity = %v", doc)
	}
}

// TestDraftReadDraft: DraftRead returns the draft file content verbatim (renamed tool, td:mcp-view-naming-fix).
func TestDraftReadDraft(t *testing.T) {
	authoringRuntime(t)
	cap, err := Open()
	if err != nil {
		t.Fatal(err)
	}
	defer cap.Close()

	if _, err := cap.NewDraft(mcp.NewDraftRequest{
		Project:   "feather",
		Namespace: "feather",
		Type:      "adr",
		ID:        "004-draft-read",
		By:        mcp.AuthorIdentity{Kind: "agent", Name: "mcp-agent"},
	}); err != nil {
		t.Fatalf("NewDraft failed: %v", err)
	}

	data, err := cap.DraftRead("feather/adr:004-draft-read", "feather")
	if err != nil {
		t.Fatalf("DraftRead failed: %v", err)
	}
	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("DraftRead must return the draft JSON: %v\n%s", err, data)
	}
	if doc["type"] != "adr" || doc["id"] != "004-draft-read" {
		t.Errorf("draft_read identity = %v", doc)
	}
}

// TestDraftReadAndViewAliasDeterministic: DraftRead and deprecated View alias return identical verbatim content.
func TestDraftReadAndViewAliasDeterministic(t *testing.T) {
	authoringRuntime(t)
	cap, err := Open()
	if err != nil {
		t.Fatal(err)
	}
	defer cap.Close()

	if _, err := cap.NewDraft(mcp.NewDraftRequest{
		Project:   "feather",
		Namespace: "feather",
		Type:      "adr",
		ID:        "004-alias",
		By:        mcp.AuthorIdentity{Kind: "agent", Name: "mcp-agent"},
	}); err != nil {
		t.Fatalf("NewDraft failed: %v", err)
	}

	a, err := cap.DraftRead("feather/adr:004-alias", "feather")
	if err != nil {
		t.Fatalf("DraftRead failed: %v", err)
	}
	b, err := cap.View("feather/adr:004-alias", "feather")
	if err != nil {
		t.Fatalf("View failed: %v", err)
	}
	if string(a) != string(b) {
		t.Errorf("DraftRead and View alias must return identical verbatim content, got %q vs %q", a, b)
	}
}

// TestDraftList: DraftList returns the draft backlog (schema
// eka-draft-list-v1) with the scaffolded draft.
func TestDraftList(t *testing.T) {
	authoringRuntime(t)
	cap, err := Open()
	if err != nil {
		t.Fatal(err)
	}
	defer cap.Close()

	if _, err := cap.NewDraft(mcp.NewDraftRequest{
		Project:   "feather",
		Namespace: "feather",
		Type:      "adr",
		ID:        "005-list",
		By:        mcp.AuthorIdentity{Kind: "agent", Name: "mcp-agent"},
	}); err != nil {
		t.Fatalf("NewDraft failed: %v", err)
	}

	data, err := cap.DraftList("feather")
	if err != nil {
		t.Fatalf("DraftList failed: %v", err)
	}
	var res map[string]any
	if err := json.Unmarshal(data, &res); err != nil {
		t.Fatalf("DraftList must return JSON: %v\n%s", err, data)
	}
	if res["schema"] != "eka-draft-list-v1" {
		t.Errorf("schema = %v, want eka-draft-list-v1", res["schema"])
	}
	if res["count"] != float64(1) {
		t.Errorf("count = %v, want 1", res["count"])
	}
	drafts := res["drafts"].([]any)
	if len(drafts) != 1 {
		t.Fatalf("drafts = %v, want 1", drafts)
	}
	first := drafts[0].(map[string]any)
	if first["id"] != "005-list" {
		t.Errorf("draft id = %v, want 005-list", first["id"])
	}
}

// TestDiscardDraft: Discard deletes the draft file without publishing
// (schema eka-discard-result-v1).
func TestDiscardDraft(t *testing.T) {
	authoringRuntime(t)
	cap, err := Open()
	if err != nil {
		t.Fatal(err)
	}
	defer cap.Close()

	if _, err := cap.NewDraft(mcp.NewDraftRequest{
		Project:   "feather",
		Namespace: "feather",
		Type:      "adr",
		ID:        "006-discard",
		By:        mcp.AuthorIdentity{Kind: "agent", Name: "mcp-agent"},
	}); err != nil {
		t.Fatalf("NewDraft failed: %v", err)
	}

	data, err := cap.Discard("feather/adr:006-discard", "feather")
	if err != nil {
		t.Fatalf("Discard failed: %v", err)
	}
	var res map[string]any
	if err := json.Unmarshal(data, &res); err != nil {
		t.Fatalf("Discard must return JSON: %v\n%s", err, data)
	}
	if res["schema"] != "eka-discard-result-v1" {
		t.Errorf("schema = %v, want eka-discard-result-v1", res["schema"])
	}
	// The draft is gone: a second discard is a deterministic refusal.
	if _, err := cap.Discard("feather/adr:006-discard", "feather"); err == nil {
		t.Error("discarding a missing draft must error")
	}
}

// --- IntegrityCheck ----------------------------------------------------

// TestIntegrityCheck: IntegrityCheck verifies the canonical store and
// returns the deterministic report (schema eka-integrity-report-v1)
// with no violations on a clean store.
func TestIntegrityCheck(t *testing.T) {
	authoringRuntime(t)
	putUnit(t, testUnit(), "feather")

	cap, err := Open()
	if err != nil {
		t.Fatal(err)
	}
	defer cap.Close()

	data, err := cap.IntegrityCheck()
	if err != nil {
		t.Fatalf("IntegrityCheck failed: %v", err)
	}
	var rep map[string]any
	if err := json.Unmarshal(data, &rep); err != nil {
		t.Fatalf("IntegrityCheck must return JSON: %v\n%s", err, data)
	}
	if rep["schema"] != "eka-integrity-report-v1" {
		t.Errorf("schema = %v, want eka-integrity-report-v1", rep["schema"])
	}
	if rep["payloadsChecked"] != float64(1) {
		t.Errorf("payloadsChecked = %v, want 1", rep["payloadsChecked"])
	}
	if violations := rep["violations"].([]any); len(violations) != 0 {
		t.Errorf("violations = %v, want none on a clean store", violations)
	}
}

// --- Transition --------------------------------------------------------

// transitionWorld seeds the canonical transition world: an active
// container ctr:wave-7 (depends-on plan:roadmap-v1), a ticket t-x
// (derives-from the container and the work item) and the work item
// sto:x with the given execution-state — all under the registered
// repository at repoDir.
func transitionWorld(t *testing.T, rt *runtime.Runtime, repoDir, project string, state string) {
	t.Helper()
	ctr := &exchange.Unit{
		Identity:       exchange.Identity{Namespace: "test-ns", Type: "ctr", ID: "wave-7", InstanceVersion: 1},
		Revision:       1,
		StateVector:    exchange.StateVector{ContainerState: "active", ExistenceState: "active"},
		Relationships:  []exchange.Relationship{{Type: "depends-on", Target: "test-ns/plan:roadmap-v1"}},
		ChangeLog:      []exchange.ChangeLogEntry{},
		Classification: exchange.Classification{},
		Content:        exchange.ContentRef{Representation: "eka/structured-text/1", File: "content"},
		ContentPayload: []byte("container wave-7"),
	}
	putUnit(t, ctr, project)
	tkt := &exchange.Unit{
		Identity:       exchange.Identity{Namespace: "test-ns", Type: "tkt", ID: "t-x", InstanceVersion: 1},
		Revision:       1,
		StateVector:    exchange.StateVector{ContentState: "draft", ExistenceState: "active"},
		Relationships:  []exchange.Relationship{{Type: "derives-from", Target: "test-ns/ctr:wave-7"}, {Type: "derives-from", Target: "test-ns/sto:x"}},
		ChangeLog:      []exchange.ChangeLogEntry{},
		Classification: exchange.Classification{},
		Content:        exchange.ContentRef{Representation: "eka/structured-text/1", File: "content"},
		ContentPayload: []byte("ticket t-x"),
	}
	putUnit(t, tkt, project)
	sto := &exchange.Unit{
		Identity:       exchange.Identity{Namespace: "test-ns", Type: "sto", ID: "x", InstanceVersion: 1},
		Revision:       1,
		StateVector:    exchange.StateVector{ExecutionState: state, ExistenceState: "active"},
		ChangeLog:      []exchange.ChangeLogEntry{{Date: "2026-08-05", Domain: "execution-state", From: "-", To: state, By: conformance.User("Eng")}},
		Classification: exchange.Classification{},
		Content:        exchange.ContentRef{Representation: "eka/structured-text/1", File: "content"},
		ContentPayload: []byte("work item x"),
	}
	putUnit(t, sto, project)
}

// TestTransitionForward: a forward transition along the D1 table
// (planned -> todo) publishes the transition in place (schema
// eka-transition-result-v1) with the agent authority.
func TestTransitionForward(t *testing.T) {
	rt := authoringRuntime(t)
	repoDir := registeredRepo(t, rt)
	transitionWorld(t, rt, repoDir, "proj", "planned")

	cap, err := Open()
	if err != nil {
		t.Fatal(err)
	}
	defer cap.Close()

	data, err := cap.Transition(mcp.TransitionRequest{
		RepoPath:  repoDir,
		Target:    "test-ns/sto:x",
		Forward:   true,
		By:        mcp.AuthorIdentity{Kind: "agent", Name: "mcp-agent"},
		Confirmed: true,
	})
	if err != nil {
		t.Fatalf("Transition failed: %v", err)
	}
	var res map[string]any
	if err := json.Unmarshal(data, &res); err != nil {
		t.Fatalf("Transition must return JSON: %v\n%s", err, data)
	}
	if res["schema"] != "eka-transition-result-v1" {
		t.Errorf("schema = %v, want eka-transition-result-v1", res["schema"])
	}
	if res["from"] != "planned" || res["to"] != "todo" {
		t.Errorf("transition = %v -> %v, want planned -> todo", res["from"], res["to"])
	}
	if res["objectHash"] == "" {
		t.Error("transition result must carry the object hash")
	}
	// The line now resolves to the new state.
	got, err := cap.Get("test-ns/sto:x:1")
	if err != nil {
		t.Fatalf("transitioned object must resolve: %v", err)
	}
	if !strings.Contains(string(got), `"executionState":"todo"`) {
		t.Errorf("transitioned object = %s, want executionState todo", got)
	}
}

// TestTransitionRefusalNoPartialWrite: an illegal transition is refused
// deterministically and the store is untouched (no partial writes).
func TestTransitionRefusalNoPartialWrite(t *testing.T) {
	rt := authoringRuntime(t)
	repoDir := registeredRepo(t, rt)
	transitionWorld(t, rt, repoDir, "proj", "planned")

	cap, err := Open()
	if err != nil {
		t.Fatal(err)
	}
	defer cap.Close()

	// planned -> done is not a D1 step: refused.
	_, err = cap.Transition(mcp.TransitionRequest{
		RepoPath:  repoDir,
		Target:    "test-ns/sto:x",
		To:        "done",
		By:        mcp.AuthorIdentity{Kind: "agent", Name: "mcp-agent"},
		Confirmed: true,
	})
	if err == nil {
		t.Fatal("an illegal transition must be refused")
	}
	// The line is unchanged: still planned.
	got, err := cap.Get("test-ns/sto:x:1")
	if err != nil {
		t.Fatalf("object must still resolve: %v", err)
	}
	if !strings.Contains(string(got), `"executionState":"planned"`) {
		t.Errorf("refused transition must leave the store untouched, got %s", got)
	}
}

// writeEKAFile writes an eka.yaml identity file into dir, creating the
// directory if needed.
func writeEKAFile(t *testing.T, dir, project, name, namespace string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	content := "version: 1\nproject: " + project + "\nname: " + name + "\nnamespace: " + namespace + "\n"
	if err := os.WriteFile(filepath.Join(dir, "eka.yaml"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
