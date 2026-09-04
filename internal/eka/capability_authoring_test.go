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
	if filepath.IsAbs(path) || strings.Contains(path, "/tmp") {
		t.Errorf("draft path must be logical/relative, got %q", path)
	}
	raw, err := os.ReadFile(resolveDraftPath(t, cap, path))
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
	if strings.Contains(draftPath, "/tmp") || filepath.IsAbs(draftPath) {
		t.Errorf("draft path must be logical/relative, got %q", draftPath)
	}
	draftPath = resolveDraftPath(t, cap, draftPath)

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
	got, err := cap.Get("feather/adr:003-publish:1", false)
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
	if path == "" || strings.Contains(path, "/tmp") || filepath.IsAbs(path) {
		t.Errorf("note path must be logical/relative, got %q", path)
	}
	if _, err := os.Stat(resolveDraftPath(t, cap, path)); err != nil {
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

// --- LegalTransitions pinned tests (sto:mcp-transition-transparency) ---

// TestNewDraftLegalTransitions_SCP: draftResult.legalTransitions["content-state"]
// must be the exact living variant [draft review approved amended] derived
// from conformance.DomainValues — no duplicated tables. Pinned to the
// actual DomainValues order in eka-core/conformance/state.go.
func TestNewDraftLegalTransitions_SCP(t *testing.T) {
	authoringRuntime(t)
	cap, err := Open()
	if err != nil {
		t.Fatal(err)
	}
	defer cap.Close()

	data, err := cap.NewDraft(mcp.NewDraftRequest{
		Project:   "feather",
		Namespace: "feather",
		Type:      "scp",
		ID:        "001-legal-scp",
		By:        mcp.AuthorIdentity{Kind: "agent", Name: "mcp-agent"},
	})
	if err != nil {
		t.Fatalf("NewDraft scp failed: %v", err)
	}
	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("NewDraft must return JSON: %v\n%s", err, data)
	}
	lt, ok := doc["legalTransitions"].(map[string]any)
	if !ok {
		t.Fatalf("legalTransitions = %v, want map", doc["legalTransitions"])
	}
	// scp owns content-state (living) + existence-state
	wantContent := []string{"draft", "review", "approved", "amended"}
	if !equalStringSlices(toStringSlice(lt["content-state"]), wantContent) {
		t.Errorf("scp legalTransitions[content-state] = %v, want %v", lt["content-state"], wantContent)
	}
	wantExistence := []string{"active", "archived", "retired"}
	if !equalStringSlices(toStringSlice(lt["existence-state"]), wantExistence) {
		t.Errorf("scp legalTransitions[existence-state] = %v, want %v", lt["existence-state"], wantExistence)
	}
	// Must be identical to helper (single source of truth, no drift)
	wantHelper := legalTransitions("scp")
	assertLegalTransitionsMap(t, lt, wantHelper)
	if got, want := len(lt), len(wantHelper); got != want {
		t.Errorf("scp legalTransitions domains = %d, want %d", got, want)
	}
	// Verify against DomainValues directly
	if !equalStringSlices(conformance.DomainValues("content-state", "scp"), wantContent) {
		t.Errorf("conformance.DomainValues content-state scp drift: got %v want %v", conformance.DomainValues("content-state", "scp"), wantContent)
	}
}

// TestPublishLegalTransitions_Plan: publishResult for plan type has
// planning-state [draft approved immutable] — pinned to actual DomainValues.
func TestPublishLegalTransitions_Plan(t *testing.T) {
	authoringRuntime(t)
	cap, err := Open()
	if err != nil {
		t.Fatal(err)
	}
	defer cap.Close()

	if _, err := cap.NewDraft(mcp.NewDraftRequest{
		Project:   "feather",
		Namespace: "feather",
		Type:      "plan",
		ID:        "roadmap-v1",
		Dimension: "planning",
		By:        mcp.AuthorIdentity{Kind: "agent", Name: "mcp-agent"},
		Content:   map[string]any{"objective": "obj", "scope": "scope", "outOfScope": "none"},
	}); err != nil {
		t.Fatalf("NewDraft plan failed: %v", err)
	}
	data, err := cap.Publish(mcp.PublishRequest{Target: "feather/plan:roadmap-v1", Project: "feather"})
	if err != nil {
		t.Fatalf("Publish plan failed: %v", err)
	}
	var res map[string]any
	if err := json.Unmarshal(data, &res); err != nil {
		t.Fatalf("Publish must return JSON: %v\n%s", err, data)
	}
	lt, ok := res["legalTransitions"].(map[string]any)
	if !ok {
		t.Fatalf("publish legalTransitions = %v, want map", res["legalTransitions"])
	}
	wantPlanning := []string{"draft", "approved", "immutable"}
	if !equalStringSlices(toStringSlice(lt["planning-state"]), wantPlanning) {
		t.Errorf("plan legalTransitions[planning-state] = %v, want %v", lt["planning-state"], wantPlanning)
	}
	wantContent := []string{"draft", "review", "approved", "amended"}
	if !equalStringSlices(toStringSlice(lt["content-state"]), wantContent) {
		t.Errorf("plan legalTransitions[content-state] = %v, want %v", lt["content-state"], wantContent)
	}
	wantExistence := []string{"active", "archived", "retired"}
	if !equalStringSlices(toStringSlice(lt["existence-state"]), wantExistence) {
		t.Errorf("plan legalTransitions[existence-state] = %v, want %v", lt["existence-state"], wantExistence)
	}
	wantHelper := legalTransitions("plan")
	assertLegalTransitionsMap(t, lt, wantHelper)
	if !equalStringSlices(conformance.DomainValues("planning-state", "plan"), wantPlanning) {
		t.Errorf("conformance.DomainValues planning-state drift: got %v want %v", conformance.DomainValues("planning-state", "plan"), wantPlanning)
	}
}

// TestDraftReadInjectsLegalTransitions: draft_read JSON contains
// legalTransitions identical to helper and preserves original fields.
func TestDraftReadInjectsLegalTransitions(t *testing.T) {
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
		ID:        "010-draft-read-lt",
		By:        mcp.AuthorIdentity{Kind: "agent", Name: "mcp-agent"},
		Content:   map[string]any{"context": "hello"},
	}); err != nil {
		t.Fatalf("NewDraft failed: %v", err)
	}
	data, err := cap.DraftRead("feather/adr:010-draft-read-lt", "feather")
	if err != nil {
		t.Fatalf("DraftRead failed: %v", err)
	}
	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("DraftRead must return JSON: %v\n%s", err, data)
	}
	// preserves original fields
	if doc["type"] != "adr" || doc["id"] != "010-draft-read-lt" || doc["namespace"] != "feather" {
		t.Errorf("draft_read must preserve original fields, got %v", doc)
	}
	if doc["content"] == nil {
		t.Error("draft_read must preserve content")
	}
	content := doc["content"].(map[string]any)
	if content["context"] != "hello" {
		t.Errorf("draft_read content merge = %v, want hello", content["context"])
	}
	lt, ok := doc["legalTransitions"].(map[string]any)
	if !ok {
		t.Fatalf("draft_read legalTransitions = %v, want map", doc["legalTransitions"])
	}
	wantHelper := legalTransitions("adr")
	assertLegalTransitionsMap(t, lt, wantHelper)
	// adr content-state variant is proposed/accepted/superseded
	if !equalStringSlices(toStringSlice(lt["content-state"]), []string{"proposed", "accepted", "superseded"}) {
		t.Errorf("adr content-state = %v, want [proposed accepted superseded]", lt["content-state"])
	}
}

// TestViewAliasIdentical: view alias returns identical JSON to draft_read
// with same legalTransitions and preserves fields.
func TestViewAliasIdentical(t *testing.T) {
	authoringRuntime(t)
	cap, err := Open()
	if err != nil {
		t.Fatal(err)
	}
	defer cap.Close()

	if _, err := cap.NewDraft(mcp.NewDraftRequest{
		Project:   "feather",
		Namespace: "feather",
		Type:      "scp",
		ID:        "011-view-alias",
		By:        mcp.AuthorIdentity{Kind: "agent", Name: "mcp-agent"},
	}); err != nil {
		t.Fatalf("NewDraft failed: %v", err)
	}
	a, err := cap.DraftRead("feather/scp:011-view-alias", "feather")
	if err != nil {
		t.Fatalf("DraftRead failed: %v", err)
	}
	b, err := cap.View("feather/scp:011-view-alias", "feather")
	if err != nil {
		t.Fatalf("View failed: %v", err)
	}
	if string(a) != string(b) {
		t.Errorf("DraftRead and View alias must return identical JSON, got %q vs %q", a, b)
	}
	var da, db map[string]any
	if err := json.Unmarshal(a, &da); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, &db); err != nil {
		t.Fatal(err)
	}
	// both contain legalTransitions identical to helper
	la, ok := da["legalTransitions"].(map[string]any)
	if !ok {
		t.Fatalf("DraftRead legalTransitions missing: %v", da)
	}
	lb, ok := db["legalTransitions"].(map[string]any)
	if !ok {
		t.Fatalf("View legalTransitions missing: %v", db)
	}
	wantHelper := legalTransitions("scp")
	assertLegalTransitionsMap(t, la, wantHelper)
	assertLegalTransitionsMap(t, lb, wantHelper)
	if da["type"] != "scp" || da["id"] != "011-view-alias" {
		t.Errorf("view alias must preserve original fields, got %v", da)
	}
}

// TestLegalTransitionsUnknownTypeReturnsEmpty: unknown type -> {} (empty map, not nil).
func TestLegalTransitionsUnknownTypeReturnsEmpty(t *testing.T) {
	lt := legalTransitions("unknown-type-xyz")
	if lt == nil {
		t.Fatal("legalTransitions unknown type must return non-nil empty map, got nil")
	}
	if len(lt) != 0 {
		t.Errorf("legalTransitions unknown type = %v, want {}", lt)
	}
	b, _ := json.Marshal(lt)
	if string(b) != "{}" {
		t.Errorf("unknown type JSON = %s, want {}", b)
	}
	// also via inject: draft with unknown type gets {}
	data := []byte(`{"type":"unknown-type-xyz","id":"001"}`)
	out, err := injectLegalTransitions(data)
	if err != nil {
		t.Fatalf("injectLegalTransitions failed: %v", err)
	}
	var doc map[string]any
	if err := json.Unmarshal(out, &doc); err != nil {
		t.Fatal(err)
	}
	if um, ok := doc["legalTransitions"].(map[string]any); !ok || len(um) != 0 {
		t.Errorf("unknown type inject legalTransitions = %v, want {}", doc["legalTransitions"])
	}
}

// TestInjectLegalTransitionsInvalidJSONPassthrough: invalid JSON is returned unchanged.
func TestInjectLegalTransitionsInvalidJSONPassthrough(t *testing.T) {
	invalid := []byte(`{not json`)
	out, err := injectLegalTransitions(invalid)
	if err != nil {
		t.Fatalf("injectLegalTransitions invalid JSON must not error: %v", err)
	}
	if string(out) != string(invalid) {
		t.Errorf("invalid JSON passthrough = %q, want %q", out, invalid)
	}
	// missing type field also passthrough (no legalTransitions injected? original preserved)
	noType := []byte(`{"id":"001","namespace":"feather"}`)
	out2, err := injectLegalTransitions(noType)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(out2, &doc); err != nil {
		t.Fatal(err)
	}
	if _, has := doc["legalTransitions"]; has {
		t.Error("missing type must not inject legalTransitions (passthrough)")
	}
	if string(out2) != string(noType) {
		// json.Marshal reorders but content equal via map comparison — allow re-encoded equality
		var a, b map[string]any
		_ = json.Unmarshal(noType, &a)
		_ = json.Unmarshal(out2, &b)
		if len(a) != len(b) {
			t.Errorf("missing type passthrough mismatch: got %s want %s", out2, noType)
		}
	}
}

func resolveDraftPath(t *testing.T, cap *Capability, logical string) string {
	t.Helper()
	if filepath.IsAbs(logical) {
		return logical
	}
	// logical is relative to workspace (EKA_HOME)
	if home := os.Getenv("EKA_HOME"); home != "" {
		return filepath.Join(home, logical)
	}
	return filepath.Join(cap.Path(), logical)
}

func equalStringSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func toStringSlice(v any) []string {
	arr, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]string, len(arr))
	for i, e := range arr {
		out[i], _ = e.(string)
	}
	return out
}

func assertLegalTransitionsMap(t *testing.T, got map[string]any, want map[string][]string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("legalTransitions len = %d, want %d: got %v want %v", len(got), len(want), got, want)
	}
	for k, wv := range want {
		gv, ok := got[k]
		if !ok {
			t.Fatalf("legalTransitions missing domain %q", k)
		}
		if !equalStringSlices(toStringSlice(gv), wv) {
			t.Errorf("legalTransitions[%q] = %v, want %v", k, gv, wv)
		}
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
	got, err := cap.Get("test-ns/sto:x:1", false)
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
	got, err := cap.Get("test-ns/sto:x:1", false)
	if err != nil {
		t.Fatalf("object must still resolve: %v", err)
	}
	if !strings.Contains(string(got), `"executionState":"planned"`) {
		t.Errorf("refused transition must leave the store untouched, got %s", got)
	}
}

// --- DraftUpdate -------------------------------------------------------

// TestDraftUpdatePartialOverwrite: draft_update merges partial content —
// supplied keys overwrite/add, absent keys are preserved, publish still
// validates (no validation at update time).
func TestDraftUpdatePartialOverwrite(t *testing.T) {
	authoringRuntime(t)
	cap, err := Open()
	if err != nil {
		t.Fatal(err)
	}
	defer cap.Close()

	if _, err := cap.NewDraft(mcp.NewDraftRequest{
		Project:   "feather",
		Namespace: "feather",
		Type:      "sto",
		ID:        "007-update-partial",
		By:        mcp.AuthorIdentity{Kind: "agent", Name: "mcp-agent"},
		Content:   map[string]any{"description": "old desc", "acceptanceCriteria": "crit1"},
	}); err != nil {
		t.Fatalf("NewDraft failed: %v", err)
	}
	// Partial merge: only description updated.
	data, err := cap.DraftUpdate(mcp.DraftUpdateRequest{
		Target:  "feather/sto:007-update-partial",
		Project: "feather",
		Content: map[string]any{"description": "new desc"},
	})
	if err != nil {
		t.Fatalf("DraftUpdate failed: %v", err)
	}
	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("DraftUpdate must return JSON: %v\n%s", err, data)
	}
	content := doc["content"].(map[string]any)
	if content["description"] != "new desc" {
		t.Errorf("description = %v, want new desc", content["description"])
	}
	if content["acceptanceCriteria"] != "crit1" {
		t.Errorf("acceptanceCriteria = %v, want preserved crit1", content["acceptanceCriteria"])
	}
	// Verify persisted file also merged (draft_read).
	raw, err := cap.DraftRead("feather/sto:007-update-partial", "feather")
	if err != nil {
		t.Fatalf("DraftRead after update failed: %v", err)
	}
	var persisted map[string]any
	if err := json.Unmarshal(raw, &persisted); err != nil {
		t.Fatal(err)
	}
	pc := persisted["content"].(map[string]any)
	if pc["description"] != "new desc" || pc["acceptanceCriteria"] != "crit1" {
		t.Errorf("persisted content = %v, want merged", pc)
	}
	// Publish still validates — the updated draft publishes.
	if _, err := cap.Publish(mcp.PublishRequest{Target: "feather/sto:007-update-partial", Project: "feather"}); err != nil {
		t.Fatalf("Publish after DraftUpdate failed: %v", err)
	}
}

// TestDraftUpdateUnknownTarget: unknown draft refuses deterministically.
func TestDraftUpdateUnknownTarget(t *testing.T) {
	authoringRuntime(t)
	cap, err := Open()
	if err != nil {
		t.Fatal(err)
	}
	defer cap.Close()

	_, err = cap.DraftUpdate(mcp.DraftUpdateRequest{
		Target:  "feather/sto:does-not-exist",
		Project: "feather",
		Content: map[string]any{"description": "x"},
	})
	if err == nil {
		t.Fatal("unknown draft must be refused")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("unknown draft error = %q, want not found", err.Error())
	}
}

// TestDraftUpdatePostPublishRefusal: after publish the draft file is the
// single-use ticket — draft_update on the same target refuses (already
// published or discarded).
func TestDraftUpdatePostPublishRefusal(t *testing.T) {
	authoringRuntime(t)
	cap, err := Open()
	if err != nil {
		t.Fatal(err)
	}
	defer cap.Close()

	if _, err := cap.NewDraft(mcp.NewDraftRequest{
		Project:   "feather",
		Namespace: "feather",
		Type:      "sto",
		ID:        "008-update-post-publish",
		By:        mcp.AuthorIdentity{Kind: "agent", Name: "mcp-agent"},
	}); err != nil {
		t.Fatalf("NewDraft failed: %v", err)
	}
	if _, err := cap.Publish(mcp.PublishRequest{Target: "feather/sto:008-update-post-publish", Project: "feather"}); err != nil {
		t.Fatalf("Publish failed: %v", err)
	}
	_, err = cap.DraftUpdate(mcp.DraftUpdateRequest{
		Target:  "feather/sto:008-update-post-publish",
		Project: "feather",
		Content: map[string]any{"description": "x"},
	})
	if err == nil {
		t.Fatal("post-publish draft_update must be refused")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("post-publish error = %q, want not found", err.Error())
	}
}

// --- PublishBatch ------------------------------------------------------

// TestPublishBatchTopologicalOrder: --all publishes pending drafts in
// topological order (referenced first) via the same Kahn engine as CLI.
// Uses the canonical planning-unit shape (scp -> plan -> ctr -> sto -> tkt)
// that validates clean.
func TestPublishBatchTopologicalOrder(t *testing.T) {
	authoringRuntime(t)
	cap, err := Open()
	if err != nil {
		t.Fatal(err)
	}
	defer cap.Close()

	// Scaffold the planning unit via single drafts, dependent first to prove
	// order is topological not declaration. scp has no deps, plan derives-from scp.
	if _, err := cap.NewDraft(mcp.NewDraftRequest{
		Project:       "feather",
		Namespace:     "feather",
		Type:          "plan",
		ID:            "roadmap-v2",
		Dimension:     "planning",
		By:            mcp.AuthorIdentity{Kind: "agent", Name: "mcp-agent"},
		Relationships: []mcp.Relationship{{Type: "derives-from", Target: "feather/scp:product-v1"}},
	}); err != nil {
		t.Fatalf("NewDraft plan failed: %v", err)
	}
	if _, err := cap.NewDraft(mcp.NewDraftRequest{
		Project:   "feather",
		Namespace: "feather",
		Type:      "scp",
		ID:        "product-v1",
		Dimension: "planning",
		By:        mcp.AuthorIdentity{Kind: "agent", Name: "mcp-agent"},
	}); err != nil {
		t.Fatalf("NewDraft scp failed: %v", err)
	}
	data, err := cap.PublishBatch(mcp.PublishBatchRequest{Project: "feather"})
	if err != nil {
		t.Fatalf("PublishBatch failed: %v", err)
	}
	var res map[string]any
	if err := json.Unmarshal(data, &res); err != nil {
		t.Fatalf("PublishBatch must return JSON: %v\n%s", err, data)
	}
	if res["schema"] != "eka-publish-batch-v1" {
		t.Errorf("schema = %v, want eka-publish-batch-v1", res["schema"])
	}
	if res["count"] != float64(2) {
		t.Errorf("count = %v, want 2", res["count"])
	}
	published := res["published"].([]any)
	if len(published) != 2 {
		t.Fatalf("published = %v, want 2", published)
	}
	// Deterministic topological order: scp before plan (referenced first).
	first := published[0].(map[string]any)
	second := published[1].(map[string]any)
	firstForm, _ := first["form"].(string)
	secondForm, _ := second["form"].(string)
	if !strings.Contains(firstForm, "scp:product-v1") {
		t.Errorf("first published = %v, want scp:product-v1 (topological)", firstForm)
	}
	if !strings.Contains(secondForm, "plan:roadmap-v2") {
		t.Errorf("second published = %v, want plan:roadmap-v2", secondForm)
	}
	// Verify objects resolve.
	for _, f := range []string{"feather/scp:product-v1:1", "feather/plan:roadmap-v2:1"} {
		if _, err := cap.Get(f, false); err != nil {
			t.Errorf("published object %s must resolve: %v", f, err)
		}
	}
	// Draft backlog empty after batch.
	drafts, err := cap.DraftList("feather")
	if err != nil {
		t.Fatal(err)
	}
	var list map[string]any
	if err := json.Unmarshal(drafts, &list); err != nil {
		t.Fatal(err)
	}
	if list["count"] != float64(0) {
		t.Errorf("draft list count = %v, want 0 after batch publish", list["count"])
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
