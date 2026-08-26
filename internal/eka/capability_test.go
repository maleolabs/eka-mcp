package eka

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/maleolabs/eka-core/conformance"
	"github.com/maleolabs/eka-core/exchange"
	"github.com/maleolabs/eka-core/metadata"
	"github.com/maleolabs/eka-core/runtime"
	"github.com/maleolabs/eka-core/store"
	"github.com/maleolabs/eka-core/workspace"
	"github.com/maleolabs/eka-mcp/internal/mcp"
)

// TestOpenDetached: opening the capability on a machine without an EKA
// workspace must succeed (the server initialization criterion) — the
// runtime is in the detached state, and the retrieval surface reports
// the uninitialized workspace instead of crashing.
func TestOpenDetached(t *testing.T) {
	home := t.TempDir() // no workspace.json inside
	t.Setenv("EKA_HOME", home)

	cap, err := Open()
	if err != nil {
		t.Fatalf("Open on a workspace-less machine must succeed: %v", err)
	}
	defer cap.Close()

	if cap.Exists() {
		t.Error("Exists must be false without a workspace")
	}

	_, err = cap.Get("feather/adr:001-serialization:1", false)
	if err == nil {
		t.Fatal("Get without a workspace must error")
	}
	if !strings.Contains(err.Error(), "not initialized") {
		t.Errorf("detached Get error must report the uninitialized workspace, got %v", err)
	}

	data, err := cap.Status()
	if err != nil {
		t.Fatalf("Status without a workspace must return uninitialized shape, got error %v", err)
	}
	var st map[string]any
	if err := json.Unmarshal(data, &st); err != nil {
		t.Fatalf("detached Status must be JSON: %v", err)
	}
	if st["initialized"] != false {
		t.Errorf("detached Status initialized = %v, want false", st["initialized"])
	}
	if st["schema"] != "eka-status-v1" {
		t.Errorf("detached Status schema = %v, want eka-status-v1", st["schema"])
	}
}

// seedUnit writes one canonical knowledge object directly into the
// canonical store of a freshly ensured workspace (the same store the
// runtime services read — the seed path a repo pull would take). The
// capability layer is then exercised against real knowledge.
func seedUnit(t *testing.T, u *exchange.Unit) (projectID string) {
	t.Helper()
	ws, err := workspace.Ensure()
	if err != nil {
		t.Fatalf("workspace.Ensure: %v", err)
	}
	defer ws.Close()

	unitJSON, err := exchange.MarshalUnit(u)
	if err != nil {
		t.Fatalf("exchange.MarshalUnit: %v", err)
	}
	_, _, err = ws.Store().PutUnit(unitJSON, u.ContentPayload, store.Ref{
		Form:            u.Identity.CanonicalForm(),
		ProjectID:       "feather",
		SourceRepo:      "seed",
		Namespace:       u.Identity.Namespace,
		Type:            u.Identity.Type,
		ID:              u.Identity.ID,
		InstanceVersion: u.Identity.InstanceVersion,
		Revision:        u.Revision,
		Dimension:       u.Classification.Dimension,
		Domain:          u.Classification.Domain,
		Phase:           u.Phase,
		UpdatedAt:       "2026-08-14T00:00:00Z",
	})
	if err != nil {
		t.Fatalf("store.PutUnit: %v", err)
	}
	return "feather"
}

func testUnit() *exchange.Unit {
	return &exchange.Unit{
		Identity: exchange.Identity{
			Namespace: "feather", Type: "adr", ID: "001-serialization", InstanceVersion: 1,
		},
		CanonicalIdentityForm: "feather/adr:001-serialization:1",
		Revision:              1,
		Classification: exchange.Classification{
			Dimension: "decisions",
			Domain:    "Architecture",
		},
		Content:        exchange.ContentRef{Representation: exchange.ContentRepresentation},
		ContentPayload: []byte("# ADR-001 — Login serialization\n\n## Context\n\nContext body.\n"),
	}
}

// TestGetResolvesObject: Get resolves a seeded canonical identity form
// to its machine document through eka-core (Resolver.Resolve +
// machine.NewDocument) — the capability layer delegates, it does not
// reimplement.
func TestGetResolvesObject(t *testing.T) {
	home := t.TempDir()
	t.Setenv("EKA_HOME", home)
	seedUnit(t, testUnit())

	cap, err := Open()
	if err != nil {
		t.Fatal(err)
	}
	defer cap.Close()

	data, err := cap.Get("feather/adr:001-serialization:1", false)
	if err != nil {
		t.Fatalf("Get on a seeded object failed: %v", err)
	}
	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("Get must return the machine document JSON: %v\n%s", err, data)
	}
	if doc["schema"] != "eka-cko-v2" {
		t.Errorf("schema = %v, want eka-cko-v2", doc["schema"])
	}
	if doc["canonicalForm"] != "feather/adr:001-serialization:1" {
		t.Errorf("canonicalForm = %v", doc["canonicalForm"])
	}
	if doc["engineeringDomain"] != "Architecture" {
		t.Errorf("engineeringDomain = %v, want Architecture", doc["engineeringDomain"])
	}
}

// TestGetLineForm: the qualified line form ("<ns>/<type>:<id>")
// resolves to the latest instance of the line.
func TestGetLineForm(t *testing.T) {
	home := t.TempDir()
	t.Setenv("EKA_HOME", home)
	seedUnit(t, testUnit())

	cap, err := Open()
	if err != nil {
		t.Fatal(err)
	}
	defer cap.Close()

	data, err := cap.Get("feather/adr:001-serialization", false)
	if err != nil {
		t.Fatalf("Get on the line form failed: %v", err)
	}
	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	if doc["canonicalForm"] != "feather/adr:001-serialization:1" {
		t.Errorf("line form must resolve to instance 1, got %v", doc["canonicalForm"])
	}
}

// TestGetUnresolved: an unknown identity reports a resolution miss
// (not a panic, not a fabricated document).
func TestGetUnresolved(t *testing.T) {
	home := t.TempDir()
	t.Setenv("EKA_HOME", home)
	seedUnit(t, testUnit())

	cap, err := Open()
	if err != nil {
		t.Fatal(err)
	}
	defer cap.Close()

	_, err = cap.Get("feather/adr:999-nope:1", false)
	if err == nil {
		t.Fatal("Get of an unknown identity must error")
	}
	if !strings.Contains(err.Error(), "no object resolves") {
		t.Errorf("unresolved Get error = %v", err)
	}
}

// TestDomainCollection: Domain returns the seeded unit as a machine
// collection (sorted by canonical form) through eka-core's
// Knowledge.Search + machine.NewCollection.
func TestDomainCollection(t *testing.T) {
	home := t.TempDir()
	t.Setenv("EKA_HOME", home)
	seedUnit(t, testUnit())

	cap, err := Open()
	if err != nil {
		t.Fatal(err)
	}
	defer cap.Close()

	data, err := cap.Domain("feather", "Architecture", false)
	if err != nil {
		t.Fatalf("Domain failed: %v", err)
	}
	var col map[string]any
	if err := json.Unmarshal(data, &col); err != nil {
		t.Fatalf("Domain must return the machine collection JSON: %v\n%s", err, data)
	}
	if col["schema"] != "eka-cko-v2" || col["collection"] != "domain" {
		t.Errorf("collection shape = %v", col)
	}
	if col["domain"] != "Architecture" {
		t.Errorf("domain = %v, want Architecture", col["domain"])
	}
	if col["count"] != float64(1) {
		t.Errorf("count = %v, want 1", col["count"])
	}
	units := col["units"].([]any)
	if len(units) != 1 {
		t.Fatalf("units = %d, want 1", len(units))
	}
	first := units[0].(map[string]any)
	if first["canonicalForm"] != "feather/adr:001-serialization:1" {
		t.Errorf("collection unit = %v", first["canonicalForm"])
	}
}

// TestDomainEmpty: a domain with no knowledge yields an empty
// collection (count 0, empty unit list — the machine contract).
func TestDomainEmpty(t *testing.T) {
	home := t.TempDir()
	t.Setenv("EKA_HOME", home)
	seedUnit(t, testUnit())

	cap, err := Open()
	if err != nil {
		t.Fatal(err)
	}
	defer cap.Close()

	data, err := cap.Domain("feather", "Operations", false)
	if err != nil {
		t.Fatal(err)
	}
	var col map[string]any
	if err := json.Unmarshal(data, &col); err != nil {
		t.Fatal(err)
	}
	if col["count"] != float64(0) {
		t.Errorf("count = %v, want 0", col["count"])
	}
	if units := col["units"].([]any); len(units) != 0 {
		t.Errorf("units = %v, want empty", units)
	}
}

// TestGetNoContentStripsContent: noContent:true strips the content payload
// via machine.Document.StripContent at parity with CLI --no-content —
// content absent, identity/stateVector/relationships intact. Payload
// measurement: stripped is substantially smaller than full.
func TestGetNoContentStripsContent(t *testing.T) {
	home := t.TempDir()
	t.Setenv("EKA_HOME", home)
	// Seed with a relationship so we can assert relationships intact.
	u := testUnit()
	u.Relationships = []exchange.Relationship{{Type: "depends-on", Target: "feather/adr:002"}}
	u.StateVector.ContentState = "draft"
	seedUnit(t, u)

	cap, err := Open()
	if err != nil {
		t.Fatal(err)
	}
	defer cap.Close()

	full, err := cap.Get("feather/adr:001-serialization:1", false)
	if err != nil {
		t.Fatalf("Get full failed: %v", err)
	}
	stripped, err := cap.Get("feather/adr:001-serialization:1", true)
	if err != nil {
		t.Fatalf("Get noContent failed: %v", err)
	}
	var fullDoc, stripDoc map[string]any
	if err := json.Unmarshal(full, &fullDoc); err != nil {
		t.Fatalf("full Get must be JSON: %v", err)
	}
	if err := json.Unmarshal(stripped, &stripDoc); err != nil {
		t.Fatalf("stripped Get must be JSON: %v", err)
	}
	if _, has := fullDoc["content"]; !has {
		t.Error("full Get must carry content")
	}
	if _, has := stripDoc["content"]; has {
		t.Error("stripped Get must NOT carry content (StripContent)")
	}
	// Identity/stateVector/relationships intact.
	for _, key := range []string{"identity", "stateVector", "relationships", "canonicalForm"} {
		if _, has := stripDoc[key]; !has {
			t.Errorf("stripped Get must retain %q, got keys %v", key, stripDoc)
		}
	}
	if stripDoc["canonicalForm"] != "feather/adr:001-serialization:1" {
		t.Errorf("canonicalForm = %v", stripDoc["canonicalForm"])
	}
	// Default unchanged: omitted noContent (false) is full payloads — already verified.
	// Payload measurement: stripped is smaller (content carries the large body).
	if len(stripped) >= len(full) {
		t.Errorf("stripped payload len %d must be < full len %d (payload economy)", len(stripped), len(full))
	}
	t.Logf("get payload: full %d bytes, stripped %d bytes (saved %d bytes, %.1f%%)", len(full), len(stripped), len(full)-len(stripped), 100*float64(len(full)-len(stripped))/float64(len(full)))
}

// TestDomainNoContentStripsContent: domain noContent:true strips each unit's
// content payload via StripContent at parity with CLI --no-content —
// content absent per unit, identity/stateVector/relationships intact.
// Measures collection payload economy.
func TestDomainNoContentStripsContent(t *testing.T) {
	home := t.TempDir()
	t.Setenv("EKA_HOME", home)
	u := testUnit()
	u.StateVector.ContentState = "draft"
	u.Relationships = []exchange.Relationship{{Type: "depends-on", Target: "feather/adr:002"}}
	seedUnit(t, u)

	cap, err := Open()
	if err != nil {
		t.Fatal(err)
	}
	defer cap.Close()

	full, err := cap.Domain("feather", "Architecture", false)
	if err != nil {
		t.Fatalf("Domain full failed: %v", err)
	}
	stripped, err := cap.Domain("feather", "Architecture", true)
	if err != nil {
		t.Fatalf("Domain noContent failed: %v", err)
	}
	var fullCol, stripCol map[string]any
	if err := json.Unmarshal(full, &fullCol); err != nil {
		t.Fatalf("full Domain must be JSON: %v", err)
	}
	if err := json.Unmarshal(stripped, &stripCol); err != nil {
		t.Fatalf("stripped Domain must be JSON: %v", err)
	}
	if fullCol["count"] != float64(1) || stripCol["count"] != float64(1) {
		t.Errorf("count full %v stripped %v, want 1", fullCol["count"], stripCol["count"])
	}
	fUnits := fullCol["units"].([]any)
	sUnits := stripCol["units"].([]any)
	if len(fUnits) != 1 || len(sUnits) != 1 {
		t.Fatalf("units len full %d stripped %d, want 1", len(fUnits), len(sUnits))
	}
	fU := fUnits[0].(map[string]any)
	sU := sUnits[0].(map[string]any)
	if _, has := fU["content"]; !has {
		t.Error("full domain unit must carry content")
	}
	if _, has := sU["content"]; has {
		t.Error("stripped domain unit must NOT carry content (StripContent per unit)")
	}
	for _, key := range []string{"identity", "stateVector", "relationships", "canonicalForm"} {
		if _, has := sU[key]; !has {
			t.Errorf("stripped domain unit must retain %q", key)
		}
	}
	if sU["canonicalForm"] != "feather/adr:001-serialization:1" {
		t.Errorf("stripped canonicalForm = %v", sU["canonicalForm"])
	}
	if len(stripped) >= len(full) {
		t.Errorf("stripped domain len %d must be < full len %d", len(stripped), len(full))
	}
	t.Logf("domain payload: full %d bytes, stripped %d bytes (saved %d bytes, %.1f%%)", len(full), len(stripped), len(full)-len(stripped), 100*float64(len(full)-len(stripped))/float64(len(full)))
}

// TestStatusJSON: Status returns the eka-core workspace status
// aggregation as JSON.
func TestStatusJSON(t *testing.T) {
	home := t.TempDir()
	t.Setenv("EKA_HOME", home)
	seedUnit(t, testUnit())

	cap, err := Open()
	if err != nil {
		t.Fatal(err)
	}
	defer cap.Close()

	data, err := cap.Status()
	if err != nil {
		t.Fatalf("Status failed: %v", err)
	}
	var st map[string]any
	if err := json.Unmarshal(data, &st); err != nil {
		t.Fatal(err)
	}
	if st["Path"] == "" {
		t.Errorf("status must carry the workspace path, got %v", st["Path"])
	}
	if got := st["Objects"]; got != float64(1) {
		t.Errorf("status Objects = %v, want 1", got)
	}
	if got := st["SchemaVersion"]; got == nil {
		t.Error("status must carry the schema version")
	}
}

// TestOpenAndCloseIdempotent: Open/Close round-trips are safe, and the
// capability is reusable after the runtime is opened.
func TestOpenAndCloseIdempotent(t *testing.T) {
	home := t.TempDir()
	t.Setenv("EKA_HOME", home)
	rt, err := runtime.Ensure()
	if err != nil {
		t.Fatal(err)
	}
	defer rt.Close()

	cap, err := Open()
	if err != nil {
		t.Fatal(err)
	}
	if !cap.Exists() {
		t.Error("Exists must be true on an ensured runtime")
	}
	if err := cap.Close(); err != nil {
		t.Errorf("Close failed: %v", err)
	}
}

// TestSyncPushNoop: a repository with no stored units is a no-op push
// — no snapshot written, deterministic empty result (AC #2 no partial
// writes, AC #1 byte-deterministic).
func TestSyncPushNoop(t *testing.T) {
	home := t.TempDir()
	t.Setenv("EKA_HOME", home)
	if _, err := workspace.Ensure(); err != nil {
		t.Fatalf("workspace.Ensure: %v", err)
	}
	base := t.TempDir()
	repo := filepath.Join(base, "myrepo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	m := metadata.Metadata{Version: metadata.SchemaVersion, Project: "feather", Name: "myrepo", Namespace: "feather"}
	if err := os.WriteFile(filepath.Join(repo, "eka.yaml"), m.Marshal(), 0o644); err != nil {
		t.Fatal(err)
	}
	cap, err := Open()
	if err != nil {
		t.Fatal(err)
	}
	defer cap.Close()

	data, err := cap.SyncPush(repo, false, false)
	if err != nil {
		t.Fatalf("SyncPush noop failed: %v", err)
	}
	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("SyncPush must be JSON: %v\n%s", err, data)
	}
	if doc["schema"] != "eka-sync-push-result-v1" {
		t.Errorf("schema = %v, want eka-sync-push-result-v1", doc["schema"])
	}
	if doc["project"] != "feather" || doc["repo"] != "myrepo" {
		t.Errorf("project/repo = %v/%v, want feather/myrepo", doc["project"], doc["repo"])
	}
	if doc["pushedUnits"] != float64(0) {
		t.Errorf("pushedUnits = %v, want 0 (no stored objects)", doc["pushedUnits"])
	}
	// Byte-deterministic for identical store state: after the first
	// push registers the repo (newRepo:true), the next two pushes share
	// the identical state (newRepo:false, no snapshot).
	data2, err := cap.SyncPush(repo, false, false)
	if err != nil {
		t.Fatal(err)
	}
	data3, err := cap.SyncPush(repo, false, false)
	if err != nil {
		t.Fatal(err)
	}
	if string(data2) != string(data3) {
		t.Errorf("SyncPush must be byte-deterministic for identical store state, got %q vs %q", data2, data3)
	}
	// No snapshot directory created for no-op.
	if _, err := os.Stat(filepath.Join(repo, "exchange", "snapshots")); !os.IsNotExist(err) {
		t.Errorf("no-op push must not create snapshots dir, stat err = %v", err)
	}
}

// TestSyncPushWithUnits: seeding one unit then pushing must emit a
// deterministic snapshot (header.json + units/) with the digest in the
// result; a failed push (non-repo path) writes nothing partially and
// sanitizes the path in the error (AC #2, #4).
func TestSyncPushWithUnits(t *testing.T) {
	home := t.TempDir()
	t.Setenv("EKA_HOME", home)
	ws, err := workspace.Ensure()
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	base := t.TempDir()
	repo := filepath.Join(base, "rpush")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	m := metadata.Metadata{Version: metadata.SchemaVersion, Project: "beather", Name: "rpush", Namespace: "beather"}
	if err := os.WriteFile(filepath.Join(repo, "eka.yaml"), m.Marshal(), 0o644); err != nil {
		t.Fatal(err)
	}
	// Seed one canonical unit attributed to this repo (the path a pull
	// would have seeded).
	u := &exchange.Unit{
		Identity:              exchange.Identity{Namespace: "beather", Type: "adr", ID: "001-x", InstanceVersion: 1},
		CanonicalIdentityForm: "beather/adr:001-x:1",
		Revision:              1,
		Classification:        exchange.Classification{Dimension: "decisions", Domain: "Architecture"},
		Content:               exchange.ContentRef{Representation: exchange.ContentRepresentation},
		ContentPayload:        []byte("# ADR 001\n\nbody\n"),
	}
	unitJSON, err := exchange.MarshalUnit(u)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := ws.Store().PutUnit(unitJSON, u.ContentPayload, store.Ref{
		Form: "beather/adr:001-x:1", ProjectID: "beather", SourceRepo: "rpush",
		Namespace: "beather", Type: "adr", ID: "001-x", InstanceVersion: 1,
		Revision: 1, Dimension: "decisions", Domain: "Architecture", UpdatedAt: "2026-08-14T00:00:00Z",
	}); err != nil {
		t.Fatal(err)
	}
	if err := ws.Store().RecordSync(store.SyncEntry{ProjectID: "beather", Repo: "rpush", Direction: "pull", SnapshotDigest: "seed", Units: 1, At: "2026-08-14T00:00:00Z"}); err != nil {
		t.Fatal(err)
	}

	cap, err := Open()
	if err != nil {
		t.Fatal(err)
	}
	defer cap.Close()

	data, err := cap.SyncPush(repo, false, false)
	if err != nil {
		t.Fatalf("SyncPush with units failed: %v", err)
	}
	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	if doc["pushedUnits"] != float64(1) {
		t.Errorf("pushedUnits = %v, want 1", doc["pushedUnits"])
	}
	if doc["snapshotDigest"] == "" {
		t.Error("snapshotDigest must be set after pushing units")
	}
	if _, err := os.Stat(filepath.Join(repo, "exchange", "snapshots", "header.json")); err != nil {
		t.Errorf("snapshot header.json must exist after push: %v", err)
	}
	// Determinism: second push of identical state yields same digest bytes.
	data2, err := cap.SyncPush(repo, false, false)
	if err != nil {
		t.Fatal(err)
	}
	var doc2 map[string]any
	if err := json.Unmarshal(data2, &doc2); err != nil {
		t.Fatal(err)
	}
	if doc["snapshotDigest"] != doc2["snapshotDigest"] {
		t.Errorf("second push digest = %v, want %v (deterministic)", doc2["snapshotDigest"], doc["snapshotDigest"])
	}
}

// TestSyncPushRefusalNoEkaYaml: SyncPush on a non-repository path must
// refuse deterministically and not leak the absolute path (AC #4).
func TestSyncPushRefusalNoEkaYaml(t *testing.T) {
	home := t.TempDir()
	t.Setenv("EKA_HOME", home)
	notRepo := t.TempDir()
	// Ensure workspace exists so the refusal is the repo gate, not detached.
	if _, err := workspace.Ensure(); err != nil {
		t.Fatal(err)
	}
	cap, err := Open()
	if err != nil {
		t.Fatal(err)
	}
	defer cap.Close()

	_, err = cap.SyncPush(notRepo, false, false)
	if err == nil {
		t.Fatal("SyncPush on a non-repo must error")
	}
	if !strings.Contains(err.Error(), "is not an EKA repository") {
		t.Errorf("refusal must name repository gate, got %v", err)
	}
}

// assignmentEnv seeds a repo with members and a work item for assignment tests.
// It uses the Authoring API (NewDraft+Publish) to create valid CKOs, mirroring
// the CLI's assignmentEnv helper.
func assignmentEnv(t *testing.T) (repo string, cap *Capability) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("EKA_HOME", home)
	ws, err := workspace.Ensure()
	if err != nil {
		t.Fatalf("workspace.Ensure: %v", err)
	}
	t.Cleanup(func() { ws.Close() })
	base := t.TempDir()
	repo = filepath.Join(base, "rassign")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	m := metadata.Metadata{Version: metadata.SchemaVersion, Project: "acme", Name: "rassign", Namespace: "acme"}
	if err := os.WriteFile(filepath.Join(repo, "eka.yaml"), m.Marshal(), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := ws.RegisterRepoMetadata(repo, m); err != nil {
		t.Fatalf("RegisterRepoMetadata: %v", err)
	}
	// Ensure repo is cwd for Authoring.NewDraft namespace resolution.
	orig, _ := os.Getwd()
	if err := os.Chdir(repo); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(orig) })

	rt, err := runtime.Ensure()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { rt.Close() })

	by := mcp.AuthorIdentity{Kind: "agent", Name: "mcp-agent"}
	toBy := func(a mcp.AuthorIdentity) conformance.AuthorIdentity {
		return conformance.AuthorIdentity{Kind: a.Kind, Name: a.Name}
	}
	// Helper to create and publish one item via Authoring API.
	publishItem := func(project, ns, typ, id string, content map[string]any) {
		t.Helper()
		// Scaffold draft.
		_, err := runtime.Authoring.NewDraft(rt, runtime.NewDraftRequest{
			Project:   project,
			Namespace: ns,
			Type:      typ,
			ID:        id,
			By:        toBy(by),
			ContentFile: func() string {
				if content == nil {
					return ""
				}
				f, _ := os.CreateTemp("", "content-*.json")
				_ = json.NewEncoder(f).Encode(content)
				_ = f.Close()
				path := f.Name()
				t.Cleanup(func() { os.Remove(path) })
				return path
			}(),
		})
		if err != nil {
			t.Fatalf("NewDraft %s:%s: %v", typ, id, err)
		}
		if _, err := runtime.Authoring.Publish(rt, typ+":"+id, runtime.PublishOptions{Project: project}); err != nil {
			t.Fatalf("Publish %s:%s: %v", typ, id, err)
		}
	}

	publishItem("acme", "acme", "mbr", "alice", map[string]any{"purpose": "p", "content": "c"})
	publishItem("acme", "acme", "mbr", "bob", map[string]any{"purpose": "p", "content": "c"})
	publishItem("acme", "acme", "sto", "item-a", map[string]any{"description": "d", "acceptanceCriteria": "ac"})

	cap2, err := Open()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cap2.Close() })
	return repo, cap2
}

func parseAssignmentResult(t *testing.T, data []byte) map[string]any {
	t.Helper()
	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("assignment result must be JSON: %v\n%s", err, data)
	}
	return doc
}

// TestAssignAddsEdgeWithoutInstanceChurn: assign sets the edge, no instance churn, deterministic.
func TestAssignAddsEdgeWithoutInstanceChurn(t *testing.T) {
	repo, cap := assignmentEnv(t)
	data, err := cap.Assign(newAssignReq(repo, "sto:item-a", "mbr:alice"))
	if err != nil {
		t.Fatalf("Assign failed: %v", err)
	}
	doc := parseAssignmentResult(t, data)
	if doc["schema"] != "eka-assignment-v1" || doc["ok"] != true {
		t.Errorf("assign result = %v, want ok true with schema", doc)
	}
	if doc["action"] != "assign" || doc["assignee"] != "acme/mbr:alice" {
		t.Errorf("assign doc = %v, want acme/mbr:alice", doc)
	}
	if doc["state"] != "published" {
		t.Errorf("state = %v, want published", doc["state"])
	}
	if doc["by"] != "mcp-agent" {
		t.Errorf("by = %v, want mcp-agent default agent identity", doc["by"])
	}
	// Verify the store edge was added.
	ws, _ := workspace.Ensure()
	units, _ := ws.Store().UnitsByLine("acme", "sto", "item-a")
	if len(units) == 0 {
		t.Fatal("work item must exist after assign")
	}
	found := false
	for _, rel := range units[0].Relationships {
		if rel.Type == "assigned-to" {
			found = true
		}
	}
	if !found {
		t.Error("stored unit must carry assigned-to edge after assign")
	}
	// Deterministic by: explicit agent identity.
	data2, err := cap.Assign(newAssignReqWithBy(repo, "sto:item-a", "mbr:alice", "alice", "agent"))
	// Already assigned to same member → unchanged, still ok true.
	if err != nil {
		t.Fatalf("second assign (idempotent) failed: %v", err)
	}
	doc2 := parseAssignmentResult(t, data2)
	if doc2["state"] != "unchanged" {
		t.Errorf("idempotent assign state = %v, want unchanged", doc2["state"])
	}
	if doc2["by"] != "alice" {
		t.Errorf("by = %v, want alice", doc2["by"])
	}
}

func newAssignReq(repo, target, to string) mcp.AssignmentRequest {
	return mcp.AssignmentRequest{RepoPath: repo, Target: target, To: to, By: defaultBy()}
}

func newAssignReqWithBy(repo, target, to, name, kind string) mcp.AssignmentRequest {
	return mcp.AssignmentRequest{RepoPath: repo, Target: target, To: to, By: byWithKind(name, kind)}
}

func defaultBy() mcp.AuthorIdentity { return mcp.AuthorIdentity{Kind: "agent", Name: "mcp-agent"} }
func byWithKind(name, kind string) mcp.AuthorIdentity {
	return mcp.AuthorIdentity{Kind: kind, Name: name}
}

// TestAssignDifferentTargetRefused: assigning already-assigned to different member is refused, no partial write.
func TestAssignDifferentTargetRefused(t *testing.T) {
	repo, cap := assignmentEnv(t)
	if _, err := cap.Assign(newAssignReq(repo, "sto:item-a", "mbr:alice")); err != nil {
		t.Fatalf("first assign: %v", err)
	}
	_, err := cap.Assign(newAssignReq(repo, "sto:item-a", "mbr:bob"))
	if err == nil {
		t.Fatal("conflicting assign must be refused")
	}
	if !strings.Contains(err.Error(), "already assigned") || !strings.Contains(err.Error(), "reassign") {
		t.Errorf("refusal = %q, want already assigned + reassign hint", err.Error())
	}
}

// TestReassignMovesEdgeSingleOperation: reassign moves the edge in one operation, no instance churn.
func TestReassignMovesEdgeSingleOperation(t *testing.T) {
	repo, cap := assignmentEnv(t)
	if _, err := cap.Assign(newAssignReq(repo, "sto:item-a", "mbr:alice")); err != nil {
		t.Fatal(err)
	}
	data, err := cap.Reassign(newAssignReq(repo, "sto:item-a", "mbr:bob"))
	if err != nil {
		t.Fatalf("Reassign failed: %v", err)
	}
	doc := parseAssignmentResult(t, data)
	if doc["action"] != "reassign" || doc["assignee"] != "acme/mbr:bob" {
		t.Errorf("reassign doc = %v", doc)
	}
	if doc["state"] != "published" {
		t.Errorf("state = %v, want published", doc["state"])
	}
	ws, _ := workspace.Ensure()
	units, _ := ws.Store().UnitsByLine("acme", "sto", "item-a")
	for _, rel := range units[0].Relationships {
		if rel.Type == "assigned-to" && rel.Target == "mbr:bob" {
			return
		}
	}
	t.Error("reassign must point to mbr:bob")
}

// TestUnassignRemovesEdge: unassign removes the edge.
func TestUnassignRemovesEdge(t *testing.T) {
	repo, cap := assignmentEnv(t)
	if _, err := cap.Assign(newAssignReq(repo, "sto:item-a", "mbr:alice")); err != nil {
		t.Fatal(err)
	}
	data, err := cap.Unassign(newUnassignReq(repo, "sto:item-a"))
	if err != nil {
		t.Fatalf("Unassign failed: %v", err)
	}
	doc := parseAssignmentResult(t, data)
	if doc["action"] != "unassign" || doc["no-assignee"] != true {
		t.Errorf("unassign doc = %v", doc)
	}
	ws, _ := workspace.Ensure()
	units, _ := ws.Store().UnitsByLine("acme", "sto", "item-a")
	for _, rel := range units[0].Relationships {
		if rel.Type == "assigned-to" {
			t.Errorf("unassign must remove assigned-to, got %v", rel)
		}
	}
}

func newUnassignReq(repo, target string) mcp.UnassignRequest {
	return mcp.UnassignRequest{RepoPath: repo, Target: target, By: defaultBy()}
}

// TestAssignUnresolvableMemberRefused: unresolvable member refuses with available list, no partial write.
func TestAssignUnresolvableMemberRefused(t *testing.T) {
	repo, cap := assignmentEnv(t)
	_, err := cap.Assign(newAssignReq(repo, "sto:item-a", "mbr:unknown"))
	if err == nil {
		t.Fatal("unresolvable member must be refused")
	}
	if !strings.Contains(err.Error(), "does not resolve") {
		t.Errorf("refusal = %q, want does not resolve", err.Error())
	}
}

// TestAssignNonWorkItemRefused: non-work-item target refused deterministically.
func TestAssignNonWorkItemRefused(t *testing.T) {
	repo, cap := assignmentEnv(t)
	_, err := cap.Assign(newAssignReq(repo, "adr:001", "mbr:alice"))
	if err == nil {
		t.Fatal("non-work-item assign must be refused")
	}
	if !strings.Contains(err.Error(), "not a work item") {
		t.Errorf("refusal = %q, want not a work item", err.Error())
	}
}

// TestReassignUnassignedRefused: reassign when not assigned is refused.
func TestReassignUnassignedRefused(t *testing.T) {
	repo, cap := assignmentEnv(t)
	_, err := cap.Reassign(newAssignReq(repo, "sto:item-a", "mbr:bob"))
	if err == nil {
		t.Fatal("reassign on unassigned must be refused")
	}
	if !strings.Contains(err.Error(), "not assigned") {
		t.Errorf("refusal = %q, want not assigned", err.Error())
	}
}

// TestUnassignNoOpWhenAbsent: unassign when absent is no-op (unchanged, ok true).
func TestUnassignNoOpWhenAbsent(t *testing.T) {
	repo, cap := assignmentEnv(t)
	data, err := cap.Unassign(newUnassignReq(repo, "sto:item-a"))
	if err != nil {
		t.Fatalf("unassign no-op must succeed: %v", err)
	}
	doc := parseAssignmentResult(t, data)
	if doc["state"] != "unchanged" || doc["no-assignee"] != true {
		t.Errorf("no-op doc = %v, want unchanged + no-assignee", doc)
	}
}
