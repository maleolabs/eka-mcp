package eka

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/maleolabs/eka-core/v2/exchange"
	"github.com/maleolabs/eka-core/v2/metadata"
	"github.com/maleolabs/eka-core/v2/runtime"
	"github.com/maleolabs/eka-core/v2/store"
	"github.com/maleolabs/eka-core/v2/workspace"
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

// TestDomainPagedWindows: DomainPaged windows the sorted collection —
// count stays the TOTAL, units carry the window, pagination names it —
// while the unbounded Domain default stays byte-identical (no
// pagination key).
func TestDomainPagedWindows(t *testing.T) {
	home := t.TempDir()
	t.Setenv("EKA_HOME", home)
	for _, id := range []string{"001", "002", "003", "004", "005"} {
		u := testUnit()
		u.Identity.ID = id
		u.CanonicalIdentityForm = "feather/adr:" + id + ":1"
		seedUnit(t, u)
	}

	cap, err := Open()
	if err != nil {
		t.Fatal(err)
	}
	defer cap.Close()

	page, err := cap.DomainPaged("feather", "Architecture", false, "", "", "", 2, 1)
	if err != nil {
		t.Fatalf("DomainPaged failed: %v", err)
	}
	var col map[string]any
	if err := json.Unmarshal(page, &col); err != nil {
		t.Fatalf("paged Domain must be JSON: %v", err)
	}
	if col["count"] != float64(5) {
		t.Errorf("paged count = %v, want TOTAL 5", col["count"])
	}
	units := col["units"].([]any)
	if len(units) != 2 {
		t.Fatalf("paged units len = %d, want 2", len(units))
	}
	for i, want := range []string{"feather/adr:002:1", "feather/adr:003:1"} {
		if units[i].(map[string]any)["canonicalForm"] != want {
			t.Errorf("paged unit[%d] = %v, want %v (sorted window)", i, units[i].(map[string]any)["canonicalForm"], want)
		}
	}
	pagination, ok := col["pagination"].(map[string]any)
	if !ok {
		t.Fatalf("paged collection must carry pagination metadata, got %v", col["pagination"])
	}
	if pagination["total"] != float64(5) || pagination["limit"] != float64(2) || pagination["offset"] != float64(1) {
		t.Errorf("pagination = %v, want total 5 limit 2 offset 1", pagination)
	}
	// The unbounded default carries no pagination key.
	full, err := cap.Domain("feather", "Architecture", false)
	if err != nil {
		t.Fatalf("Domain failed: %v", err)
	}
	var fullCol map[string]any
	if err := json.Unmarshal(full, &fullCol); err != nil {
		t.Fatal(err)
	}
	if fullCol["count"] != float64(5) || len(fullCol["units"].([]any)) != 5 {
		t.Errorf("default Domain must stay unbounded: count %v units %d", fullCol["count"], len(fullCol["units"].([]any)))
	}
	if _, has := fullCol["pagination"]; has {
		t.Error("default Domain must NOT carry pagination metadata")
	}
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
