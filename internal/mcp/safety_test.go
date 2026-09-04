package mcp

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestImpersonationBlocked(t *testing.T) {
	s := newTestServer(&fakeCapability{statusJSON: `{}`})

	// byKind=user must be blocked via MCP (agent interface cannot impersonate user).
	t.Setenv("EKA_MCP_ALLOW_USER_IMPERSONATION", "0")
	out := mustHandle(t, s, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"new","arguments":{"project":"feather","namespace":"feather","type":"sto","id":"imp-1","by":"alice","byKind":"user"}}}`)
	if out["error"] != nil {
		t.Fatalf("expected tool result with isError, got %v", out)
	}
	res := out["result"].(map[string]any)
	if res["isError"] != true {
		t.Error("user impersonation via MCP must be blocked")
	}
	text := res["content"].([]any)[0].(map[string]any)["text"].(string)
	if !strings.Contains(text, "impersonation") {
		t.Errorf("impersonation error must mention impersonation, got %q", text)
	}

	// Invalid author name (injection) must be blocked.
	out = mustHandle(t, s, `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"new","arguments":{"project":"feather","namespace":"feather","type":"sto","id":"imp-2","by":"evil; rm -rf","byKind":"agent"}}}`)
	res = out["result"].(map[string]any)
	if res["isError"] != true {
		t.Error("invalid author name must be blocked")
	}
	text = res["content"].([]any)[0].(map[string]any)["text"].(string)
	if !strings.Contains(text, "invalid author") {
		t.Errorf("invalid author error must mention invalid author, got %q", text)
	}

	// Reserved name Engineering must be blocked.
	out = mustHandle(t, s, `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"new","arguments":{"project":"feather","namespace":"feather","type":"sto","id":"imp-3","by":"Engineering","byKind":"agent"}}}`)
	res = out["result"].(map[string]any)
	if res["isError"] != true {
		t.Error("reserved name Engineering must be blocked")
	}

	// Allowed agent must succeed.
	t.Setenv("EKA_MCP_ALLOW_USER_IMPERSONATION", "1")
	out = mustHandle(t, s, `{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"new","arguments":{"project":"feather","namespace":"feather","type":"sto","id":"ok-1","by":"mcp-agent","byKind":"agent"}}}`)
	res = out["result"].(map[string]any)
	if res["isError"] == true {
		t.Errorf("allowed agent must succeed, got %v", res)
	}

	// User impersonation allowed when env set.
	t.Setenv("EKA_MCP_ALLOW_USER_IMPERSONATION", "1")
	out = mustHandle(t, s, `{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"new","arguments":{"project":"feather","namespace":"feather","type":"sto","id":"ok-2","by":"alice","byKind":"user"}}}`)
	res = out["result"].(map[string]any)
	if res["isError"] == true {
		t.Errorf("user impersonation with env allow must succeed, got %v", res)
	}
	// Reset for other tests.
	t.Setenv("EKA_MCP_ALLOW_USER_IMPERSONATION", "0")
}

func TestHighImpactWritesGated(t *testing.T) {
	s := newTestServer(&fakeCapability{statusJSON: `{}`})

	// Canonical-write gated when not allowed.
	t.Setenv("EKA_MCP_ALLOW_CANONICAL_WRITE", "0")
	out := mustHandle(t, s, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"publish","arguments":{"target":"feather/sto:001"}}}`)
	res := out["result"].(map[string]any)
	if res["isError"] != true {
		t.Error("publish must be gated when canonical not allowed")
	}
	text := res["content"].([]any)[0].(map[string]any)["text"].(string)
	if !strings.Contains(text, "canonical-write") || !strings.Contains(text, "gated") {
		t.Errorf("gating error must mention canonical-write and gated, got %q", text)
	}

	out = mustHandle(t, s, `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"sync_push","arguments":{}}}`)
	res = out["result"].(map[string]any)
	if res["isError"] != true {
		t.Error("sync_push must be gated")
	}

	// When allowed, it succeeds (via fake).
	t.Setenv("EKA_MCP_ALLOW_CANONICAL_WRITE", "1")
	out = mustHandle(t, s, `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"publish","arguments":{"target":"feather/sto:001"}}}`)
	res = out["result"].(map[string]any)
	if res["isError"] == true {
		t.Errorf("publish with approval must succeed, got %v", res)
	}

	// External isolated/disabled by default.
	t.Setenv("EKA_MCP_ENABLE_FEEDBACK_PUBLISH", "0")
	t.Setenv("EKA_MCP_ALLOW_EXTERNAL", "0")
	out = mustHandle(t, s, `{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"feedback_publish","arguments":{"id":"fbk-20260812-test"}}}`)
	res = out["result"].(map[string]any)
	if res["isError"] != true {
		t.Error("feedback_publish must be disabled by default")
	}
	text = res["content"].([]any)[0].(map[string]any)["text"].(string)
	if !strings.Contains(text, "external") || !strings.Contains(text, "isolated") {
		t.Errorf("external gating error must mention external and isolated, got %q", text)
	}

	t.Setenv("EKA_MCP_ENABLE_FEEDBACK_PUBLISH", "1")
	out = mustHandle(t, s, `{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"feedback_publish","arguments":{"id":"fbk-20260812-test"}}}`)
	res = out["result"].(map[string]any)
	if res["isError"] == true {
		t.Errorf("feedback_publish with env allow must succeed, got %v", res)
	}

	// Restore for suite (TestMain sets to 1, but this test leaves 1).
	t.Setenv("EKA_MCP_ALLOW_CANONICAL_WRITE", "1")
	t.Setenv("EKA_MCP_ENABLE_FEEDBACK_PUBLISH", "1")
}

func TestDraftUpdateOptimisticConcurrencyValidation(t *testing.T) {
	s := newTestServer(&fakeCapability{statusJSON: `{}`})

	// Invalid expectedHash pattern must be refused at validation layer.
	out := mustHandle(t, s, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"draft_update","arguments":{"target":"feather/sto:001","content":{"description":"x"},"expectedHash":"not-a-hash"}}}`)
	res := out["result"].(map[string]any)
	if res["isError"] != true {
		t.Error("invalid expectedHash must be refused")
	}
	text := res["content"].([]any)[0].(map[string]any)["text"].(string)
	if !strings.Contains(text, "expectedHash") {
		t.Errorf("expectedHash validation must mention field, got %q", text)
	}

	// Invalid expectedRevision must be refused.
	out = mustHandle(t, s, `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"draft_update","arguments":{"target":"feather/sto:001","content":{"description":"x"},"expectedRevision":0}}}`)
	res = out["result"].(map[string]any)
	if res["isError"] != true {
		t.Error("invalid expectedRevision must be refused")
	}

	// Valid expectedRevision and expectedHash must pass validation (via fake).
	out = mustHandle(t, s, `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"draft_update","arguments":{"target":"feather/sto:001","content":{"description":"x"},"expectedRevision":1,"expectedHash":"`+strings.Repeat("a", 64)+`"}}}`)
	res = out["result"].(map[string]any)
	if res["isError"] == true {
		t.Errorf("valid concurrency guards must pass validation, got %v", res)
	}

	// Tools list must expose the new concurrency fields.
	outList := mustHandle(t, s, `{"jsonrpc":"2.0","id":10,"method":"tools/list"}`)
	tools := outList["result"].(map[string]any)["tools"].([]any)
	var found bool
	for _, tl := range tools {
		m := tl.(map[string]any)
		if m["name"] == "draft_update" {
			schema := m["inputSchema"].(map[string]any)
			props := schema["properties"].(map[string]any)
			if _, has := props["expectedRevision"]; !has {
				t.Error("draft_update schema must expose expectedRevision")
			}
			if _, has := props["expectedHash"]; !has {
				t.Error("draft_update schema must expose expectedHash")
			}
			found = true
		}
	}
	if !found {
		t.Error("draft_update not found in tools/list")
	}
	_ = json.Valid
}
