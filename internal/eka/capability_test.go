package eka

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/maleolabs/eka-core/exchange"
	"github.com/maleolabs/eka-core/runtime"
	"github.com/maleolabs/eka-core/store"
	"github.com/maleolabs/eka-core/workspace"
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

	_, err = cap.Get("feather/adr:001-serialization:1")
	if err == nil {
		t.Fatal("Get without a workspace must error")
	}
	if !strings.Contains(err.Error(), "not initialized") {
		t.Errorf("detached Get error must report the uninitialized workspace, got %v", err)
	}

	_, err = cap.Status()
	if err == nil {
		t.Fatal("Status without a workspace must error")
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

	data, err := cap.Get("feather/adr:001-serialization:1")
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

	data, err := cap.Get("feather/adr:001-serialization")
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

	_, err = cap.Get("feather/adr:999-nope:1")
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

	data, err := cap.Domain("feather", "Architecture")
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

	data, err := cap.Domain("feather", "Operations")
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
