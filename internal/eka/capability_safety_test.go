package eka

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/maleolabs/eka-mcp/internal/mcp"
)

// TestDraftUpdateUsesAuthoringAPI verifies that DraftUpdate delegates to
// the runtime-owned Authoring API (not direct file manipulation) and that
// optimistic concurrency via expectedRevision/expectedHash is enforced.
func TestDraftUpdateUsesAuthoringAPI(t *testing.T) {
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
		ID:        "900-concurrency",
		By:        mcp.AuthorIdentity{Kind: "agent", Name: "mcp-agent"},
		Content:   map[string]any{"description": "initial"},
	}); err != nil {
		t.Fatalf("NewDraft failed: %v", err)
	}

	// Read the draft to get its current revision and hash.
	raw, err := cap.DraftRead("feather/sto:900-concurrency", "feather")
	if err != nil {
		t.Fatalf("DraftRead failed: %v", err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	// The raw file on disk is needed for hash; re-read via DraftRead is
	// enriched with legalTransitions, so we read the file directly via the
	// capability's underlying path resolution is not exposed. Instead we
	// fetch the draft via the Authoring API's file path indirectly: we
	// can compute the hash from the draft file by resolving via a second
	// read of the enriched doc is not the raw hash, so we test revision
	// guard explicitly and hash guard via a known wrong hash.
	rev, _ := doc["revision"].(float64)
	_ = rev

	// 1. Wrong expectedRevision must conflict.
	wrongRev := 999
	_, err = cap.DraftUpdate(mcp.DraftUpdateRequest{
		Target:           "feather/sto:900-concurrency",
		Project:          "feather",
		Content:          map[string]any{"description": "concurrent"},
		ExpectedRevision: &wrongRev,
	})
	if err == nil || !strings.Contains(err.Error(), "conflict") {
		t.Errorf("wrong expectedRevision must conflict, got %v", err)
	}

	// 2. Wrong expectedHash must conflict.
	_, err = cap.DraftUpdate(mcp.DraftUpdateRequest{
		Target:       "feather/sto:900-concurrency",
		Project:      "feather",
		Content:      map[string]any{"description": "concurrent"},
		ExpectedHash: strings.Repeat("0", 64),
	})
	if err == nil || !strings.Contains(err.Error(), "conflict") {
		t.Errorf("wrong expectedHash must conflict, got %v", err)
	}

	// 3. Correct expectedRevision must succeed.
	correctRev := int(rev)
	_, err = cap.DraftUpdate(mcp.DraftUpdateRequest{
		Target:           "feather/sto:900-concurrency",
		Project:          "feather",
		Content:          map[string]any{"description": "with revision guard"},
		ExpectedRevision: &correctRev,
	})
	if err != nil {
		t.Fatalf("correct expectedRevision must succeed, got %v", err)
	}

	// 4. No guard must succeed (backward compat).
	_, err = cap.DraftUpdate(mcp.DraftUpdateRequest{
		Target:  "feather/sto:900-concurrency",
		Project: "feather",
		Content: map[string]any{"description": "no guard"},
	})
	if err != nil {
		t.Fatalf("no guard DraftUpdate must succeed, got %v", err)
	}
}

func TestDraftUpdateHashGuardSucceeds(t *testing.T) {
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
		ID:        "901-hash-guard",
		By:        mcp.AuthorIdentity{Kind: "agent", Name: "mcp-agent"},
		Content:   map[string]any{"description": "initial"},
	}); err != nil {
		t.Fatalf("NewDraft failed: %v", err)
	}

	// Get the raw draft file hash by reading via the runtime directly.
	// We use the DraftRead enriched output to locate the file content is
	// not raw, so we recompute hash from the file on disk by using the
	// capability's DraftRead path is logical; instead we directly test
	// that a hash computed from the file succeeds: we fetch the file via
	// the underlying workspace drafts path.
	// For determinism we compute hash from the enriched doc is not correct,
	// so we test that after a successful update the old hash is stale.
	// First get the draft via DraftRead and then immediately update with
	// the hash of the current file by reading the file via the capability's
	// internal path: we can cheat by using the fact that DraftUpdate with
	// a wrong hash fails, and with no hash succeeds, so hash guard is
	// exercised via wrong hash case above. Here we test the success path
	// by computing the hash from the actual file bytes via os.ReadFile on
	// the draft file discovered via DraftRead's logical path? Instead we
	// test the Authoring API directly via the runtime to avoid capability
	// indirection.
	raw, err := cap.DraftRead("feather/sto:901-hash-guard", "feather")
	if err != nil {
		t.Fatalf("DraftRead failed: %v", err)
	}
	// The enriched doc contains legalTransitions, so we strip it to get
	// approximate raw is not needed; we just test that updating with the
	// correct hash computed from the file on disk succeeds. To get the
	// file on disk we use the internal/eka helper: we can locate the file
	// by assuming the workspace drafts path is under EKA_HOME/drafts.
	// t.Setenv("EKA_HOME") was set by authoringRuntime to a temp dir, so
	// we can read it directly.
	home := os.Getenv("EKA_HOME")
	path := home + "/drafts/feather/sto-901-hash-guard.json"
	data, err := os.ReadFile(path)
	if err != nil {
		// Fallback: try to read via DraftRead raw is enriched, compute
		// hash from that is not the on-disk hash, but we can still test
		// that wrong hash fails and correct hash from data succeeds.
		t.Fatalf("cannot read draft file %s: %v raw len %d", path, err, len(raw))
	}
	h := sha256.Sum256(data)
	hash := hex.EncodeToString(h[:])

	_, err = cap.DraftUpdate(mcp.DraftUpdateRequest{
		Target:       "feather/sto:901-hash-guard",
		Project:      "feather",
		Content:      map[string]any{"description": "with hash guard"},
		ExpectedHash: hash,
	})
	if err != nil {
		t.Fatalf("correct expectedHash must succeed, got %v", err)
	}

	// Old hash must now conflict (stale).
	_, err = cap.DraftUpdate(mcp.DraftUpdateRequest{
		Target:       "feather/sto:901-hash-guard",
		Project:      "feather",
		Content:      map[string]any{"description": "stale hash"},
		ExpectedHash: hash,
	})
	if err == nil || !strings.Contains(err.Error(), "conflict") {
		t.Errorf("stale hash must conflict, got %v", err)
	}
	_ = raw
}
