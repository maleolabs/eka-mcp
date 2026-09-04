package mcp

import (
	"encoding/json"
	"strings"
	"testing"
)

// Approval policy tests — risk classes and write gating (sto:mcp-surface-contract).

func TestRiskClassesAreAuthoritativeAndComplete(t *testing.T) {
	// Every descriptor must have a risk class in the allowed set and no duplicates.
	allowed := map[string]bool{RiskRead: true, RiskLocalWrite: true, RiskCanonicalWrite: true, RiskExternal: true}
	seen := map[string]bool{}
	counts := map[string]int{}
	for _, d := range toolDescriptors {
		if !allowed[d.RiskClass] {
			t.Errorf("tool %q has invalid risk class %q", d.Name, d.RiskClass)
		}
		if seen[d.Name] {
			t.Errorf("duplicate descriptor %q", d.Name)
		}
		seen[d.Name] = true
		counts[d.RiskClass]++
	}
	// Ensure each class has at least one tool.
	for _, rc := range []string{RiskRead, RiskLocalWrite, RiskCanonicalWrite, RiskExternal} {
		if counts[rc] == 0 {
			t.Errorf("risk class %q has no tools", rc)
		}
	}
	// ToolCount must equal non-deprecated count.
	if got, want := ToolCount(), len(toolNames); got != want {
		t.Errorf("ToolCount %d != len(toolNames) %d", got, want)
	}
	// RiskClassOf must be authoritative (no drift).
	for _, d := range toolDescriptors {
		if got := RiskClassOf(d.Name); got != d.RiskClass {
			t.Errorf("RiskClassOf(%q)=%q want %q", d.Name, got, d.RiskClass)
		}
	}
	if got := RiskClassOf("bogus"); got != "" {
		t.Errorf("RiskClassOf unknown should be empty, got %q", got)
	}
}

func TestApprovalPolicyReadVsWriteSeparation(t *testing.T) {
	// Read tools are IsReadOnly true; writes are false.
	for _, d := range toolDescriptors {
		want := d.RiskClass == RiskRead
		if got := IsReadOnly(d.Name); got != want {
			t.Errorf("IsReadOnly(%q)=%v want %v (risk %q)", d.Name, got, want, d.RiskClass)
		}
	}
	// Explicit pin: canonical-write and external must not be read-only.
	for _, name := range []string{"publish", "publishBatch", "transition", "assign", "reassign", "unassign", "sync_push", "feedback_publish"} {
		if IsReadOnly(name) {
			t.Errorf("tool %q must not be read-only", name)
		}
	}
	// Local-write must not be read-only.
	for _, name := range []string{"new", "draft_update", "discard", "note", "feedback_new"} {
		if IsReadOnly(name) {
			t.Errorf("local-write tool %q must not be read-only", name)
		}
	}
	// Read tools should be read-only.
	for _, name := range []string{"context", "code_context", "code_discover", "code_get", "get", "domain", "status", "validate", "draft_read", "draft_list", "integrity_check", "feedback_list"} {
		if !IsReadOnly(name) {
			t.Errorf("read tool %q must be read-only", name)
		}
	}
}

func TestToolsListStrictSchemasAndAnnotations(t *testing.T) {
	s := newTestServer(&fakeCapability{statusJSON: `{}`})
	out := mustHandle(t, s, `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)
	res := out["result"].(map[string]any)
	tools := res["tools"].([]any)
	byName := map[string]map[string]any{}
	for _, tl := range tools {
		tm := tl.(map[string]any)
		byName[tm["name"].(string)] = tm
	}
	// Advertised count must equal ToolCount (view not advertised).
	if len(tools) != ToolCount() {
		t.Errorf("advertised tools %d != ToolCount %d", len(tools), ToolCount())
	}
	if _, has := byName["view"]; has {
		t.Errorf("view must not be advertised (deprecated alias)")
	}
	if _, has := byName["publishBatch"]; !has {
		t.Errorf("publishBatch must be advertised (split contract)")
	}
	// Every schema must be strict: type object, additionalProperties false, required subset of properties, enums/bounds.
	for _, d := range toolDescriptors {
		if d.Deprecated {
			continue
		}
		tm, ok := byName[d.Name]
		if !ok {
			t.Errorf("tool %q not advertised", d.Name)
			continue
		}
		schema := tm["inputSchema"].(map[string]any)
		if schema["type"] != "object" {
			t.Errorf("tool %q schema type=%v want object", d.Name, schema["type"])
		}
		if schema["additionalProperties"] != false {
			t.Errorf("tool %q must have additionalProperties:false for strict schema", d.Name)
		}
		props, _ := schema["properties"].(map[string]any)
		// Required subset check
		if req, has := schema["required"]; has {
			for _, r := range req.([]any) {
				if _, ok := props[r.(string)]; !ok {
					t.Errorf("tool %q required %q not in properties", d.Name, r)
				}
			}
		}
		// Annotations must carry risk class and readOnly/destructive/openWorld.
		ann, ok := tm["annotations"].(map[string]any)
		if !ok {
			t.Errorf("tool %q missing annotations", d.Name)
			continue
		}
		if ann["riskClass"] != d.RiskClass {
			t.Errorf("tool %q annotation riskClass=%v want %q", d.Name, ann["riskClass"], d.RiskClass)
		}
		if ann["readOnly"] != (d.RiskClass == RiskRead) {
			t.Errorf("tool %q readOnly annotation mismatch", d.Name)
		}
	}
	// Pin enums and bounds for strict schemas.
	// context depth enum
	if tm := byName["context"]; tm != nil {
		props := tm["inputSchema"].(map[string]any)["properties"].(map[string]any)
		depth := props["depth"].(map[string]any)
		if depth["enum"] == nil {
			t.Errorf("context depth must have enum")
		}
	}
	// code_context: depth enum, level bounds
	if tm := byName["code_context"]; tm != nil {
		props := tm["inputSchema"].(map[string]any)["properties"].(map[string]any)
		if props["depth"].(map[string]any)["enum"] == nil {
			t.Errorf("code_context depth must have enum")
		}
		lvl := props["level"].(map[string]any)
		if lvl["minimum"] != float64(0) && lvl["minimum"] != 0 {
			t.Errorf("code_context level minimum must be 0")
		}
		if lvl["maximum"] != float64(3) && lvl["maximum"] != 3 {
			t.Errorf("code_context level maximum must be 3")
		}
	}
	// code_discover limit 1-64
	if tm := byName["code_discover"]; tm != nil {
		props := tm["inputSchema"].(map[string]any)["properties"].(map[string]any)
		lim := props["limit"].(map[string]any)
		if lim["minimum"] != float64(1) && lim["minimum"] != 1 {
			t.Errorf("code_discover limit minimum must be 1")
		}
		if lim["maximum"] != float64(64) && lim["maximum"] != 64 {
			t.Errorf("code_discover limit maximum must be 64")
		}
		if props["query"] == nil {
			t.Errorf("code_discover must have query")
		}
	}
	// domain enum
	if tm := byName["domain"]; tm != nil {
		props := tm["inputSchema"].(map[string]any)["properties"].(map[string]any)
		dom := props["domain"].(map[string]any)
		if dom["enum"] == nil {
			t.Errorf("domain must have enum")
		}
	}
	// feedback_new enums
	if tm := byName["feedback_new"]; tm != nil {
		props := tm["inputSchema"].(map[string]any)["properties"].(map[string]any)
		typ := props["type"].(map[string]any)
		if typ["enum"] == nil {
			t.Errorf("feedback_new type must have enum")
		}
		if props["severity"] != nil {
			if sev := props["severity"].(map[string]any)["enum"]; sev == nil {
				t.Errorf("feedback_new severity must have enum")
			}
		}
	}
	// note role enum
	if tm := byName["note"]; tm != nil {
		props := tm["inputSchema"].(map[string]any)["properties"].(map[string]any)
		role := props["role"].(map[string]any)
		if role["enum"] == nil {
			t.Errorf("note role must have enum")
		}
	}
}

func TestToolsListPublishSplitStrict(t *testing.T) {
	s := newTestServer(&fakeCapability{statusJSON: `{}`})
	out := mustHandle(t, s, `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)
	tools := out["result"].(map[string]any)["tools"].([]any)
	byName := map[string]map[string]any{}
	for _, tl := range tools {
		tm := tl.(map[string]any)
		byName[tm["name"].(string)] = tm
	}
	pub := byName["publish"]
	batch := byName["publishBatch"]
	if pub == nil || batch == nil {
		t.Fatalf("publish and publishBatch must both be advertised")
	}
	pubSchema := pub["inputSchema"].(map[string]any)
	batchSchema := batch["inputSchema"].(map[string]any)
	// publish must require target, must not have all/pending.
	if req, _ := pubSchema["required"].([]any); len(req) == 0 || req[0] != "target" {
		t.Errorf("publish required=%v want [target]", req)
	}
	props := pubSchema["properties"].(map[string]any)
	if _, has := props["all"]; has {
		t.Errorf("publish must not have all (use publishBatch)")
	}
	if _, has := props["pending"]; has {
		t.Errorf("publish must not have pending (use publishBatch)")
	}
	if props["instanceVersion"] == nil {
		t.Errorf("publish must have instanceVersion")
	}
	// publishBatch must NOT require target.
	if req, has := batchSchema["required"]; has && len(req.([]any)) > 0 {
		for _, r := range req.([]any) {
			if r.(string) == "target" {
				t.Errorf("publishBatch must not require target")
			}
		}
	}
	bprops := batchSchema["properties"].(map[string]any)
	if _, has := bprops["target"]; has {
		t.Errorf("publishBatch must not have target")
	}
	if _, has := bprops["instanceVersion"]; has {
		t.Errorf("publishBatch must not have instanceVersion")
	}
}

func TestDeprecatedViewDispatchStillWorks(t *testing.T) {
	s := newTestServer(&fakeCapability{statusJSON: `{}`})
	// tools/list must NOT contain view, but tools/call view must still succeed (compat dispatch).
	out := mustHandle(t, s, `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)
	tools := out["result"].(map[string]any)["tools"].([]any)
	for _, tl := range tools {
		if tl.(map[string]any)["name"] == "view" {
			t.Fatal("view must not be in tools/list")
		}
	}
	// dispatch still works
	out = mustHandle(t, s, `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"view","arguments":{"target":"feather/adr:001"}}}`)
	res := out["result"].(map[string]any)
	if res["isError"] != false {
		t.Errorf("view dispatch must still succeed for compat, got %v", res)
	}
	// draft_read and view return identical via server (already tested elsewhere), but pin here too.
	a := mustHandle(t, s, `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"draft_read","arguments":{"target":"feather/adr:001"}}}`)
	b := mustHandle(t, s, `{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"view","arguments":{"target":"feather/adr:001"}}}`)
	atext := a["result"].(map[string]any)["content"].([]any)[0].(map[string]any)["text"].(string)
	btext := b["result"].(map[string]any)["content"].([]any)[0].(map[string]any)["text"].(string)
	if atext != btext {
		t.Errorf("draft_read and view must return identical content")
	}
	_ = strings.Contains // ensure import used
}

func TestLogicalPathsEnforcedInSchemas(t *testing.T) {
	s := newTestServer(&fakeCapability{statusJSON: `{}`})
	out := mustHandle(t, s, `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)
	tools := out["result"].(map[string]any)["tools"].([]any)
	// Tools that accept paths must describe logical relative paths in description.
	for _, tl := range tools {
		tm := tl.(map[string]any)
		name := tm["name"].(string)
		desc := tm["description"].(string)
		// Check path-bearing tools mention logical/relative.
		switch name {
		case "code_context", "code_discover", "code_get", "validate", "sync_push", "transition", "note", "assign", "reassign", "unassign":
			if !strings.Contains(strings.ToLower(desc), "logical") && !strings.Contains(desc, "relative") {
				// Schema properties also should mention logical for path fields.
				// At least one of description or property description should mention logical.
				schema := tm["inputSchema"].(map[string]any)
				props, ok := schema["properties"].(map[string]any)
				if !ok {
					t.Errorf("tool %q should mention logical path handling", name)
					continue
				}
				found := false
				for _, v := range props {
					pm := v.(map[string]any)
					if d, ok := pm["description"].(string); ok && (strings.Contains(strings.ToLower(d), "logical") || strings.Contains(d, "relative")) {
						found = true
					}
				}
				if !found && !strings.Contains(strings.ToLower(desc), "logical") {
					t.Errorf("tool %q schema should mention logical/relative path (no absolute leakage)", name)
				}
			}
		}
	}
	// Also verify JSON marshal of a fake that would have absolute path does not appear in real capability — checked in capability tests.
	_ = json.Valid
}
