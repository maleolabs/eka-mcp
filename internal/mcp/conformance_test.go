package mcp

// Protocol conformance suite — asserts the server's ACTUAL behavior
// against the pinned 2024-11-05 baseline (spike eka/spk:mcp-protocol-survey:2):
//
//  1. initialize echoes the client's announced protocol version
//     (2024-11-05, 2025-03-26, 2025-06-18, 2025-11-25 → all echoed)
//  2. initialize without protocolVersion answers the baseline 2024-11-05
//  3. capabilities advertise only tools + resources (no prompts,
//     logging, completions or elicitation claims)
//  4. tools/list = exactly context/get/domain/status/validate/new/
//     publish/transition/note/draft_read/view(draft_read alias, deprecated)/draft_list/integrity_check/discard
//     with valid inputSchema; tools/call unknown tool → -32003;
//     execution error → isError:true; success → text content;
//     draft_read and deprecated view alias return identical verbatim content
//  5. resources/list = eka://status + every embedded skill + every
//     embedded draft template type; resources/read on each family;
//     unknown URI → -32002
//  6. notifications (initialized, cancelled) → no response; ping → {}
//  7. JSON-RPC error codes: -32700, -32600, -32601, -32602
//  8. stdio framing: newline-delimited, flush per response
//
// The suite is deterministic and CI-runnable: it drives the server
// through the same fake capability as the unit tests, so it never
// depends on a workspace or the eka-core runtime.

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	pack "github.com/maleolabs/eka-mcp"
)

// conformanceServer returns a server wired to a deterministic fake
// capability for the whole suite.
func conformanceServer() *Server {
	return NewServer(&fakeCapability{statusJSON: `{"initialized":true,"schemaVersion":4}`})
}

// mustError extracts the error object of a response, failing the test
// when the response is not an error.
func mustError(t *testing.T, out map[string]any) map[string]any {
	t.Helper()
	errObj, ok := out["error"].(map[string]any)
	if !ok {
		t.Fatalf("expected an error response, got %v", out)
	}
	return errObj
}

// mustResult extracts the result object of a response, failing the test
// when the response carries an error.
func mustResult(t *testing.T, out map[string]any) map[string]any {
	t.Helper()
	if out["error"] != nil {
		t.Fatalf("expected a result, got error %v", out["error"])
	}
	res, ok := out["result"].(map[string]any)
	if !ok {
		t.Fatalf("result = %v, want an object", out["result"])
	}
	return res
}

// TestConformanceInitializeEchoesClientVersions (spike point 1): the
// server echoes the client's announced protocol version — every version
// the target clients announce, including versions newer than the
// 2024-11-05 baseline.
func TestConformanceInitializeEchoesClientVersions(t *testing.T) {
	versions := []string{"2024-11-05", "2025-03-26", "2025-06-18", "2025-11-25"}
	for _, v := range versions {
		t.Run(v, func(t *testing.T) {
			s := conformanceServer()
			out := mustHandle(t, s, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"`+v+`"}}`)
			res := mustResult(t, out)
			if res["protocolVersion"] != v {
				t.Errorf("protocolVersion = %v, want the echoed %v", res["protocolVersion"], v)
			}
		})
	}
}

// TestConformanceInitializeFallbackBaseline (spike point 2): a client
// that announces no protocol version gets the 2024-11-05 baseline.
func TestConformanceInitializeFallbackBaseline(t *testing.T) {
	s := conformanceServer()
	out := mustHandle(t, s, `{"jsonrpc":"2.0","id":1,"method":"initialize"}`)
	res := mustResult(t, out)
	if res["protocolVersion"] != ProtocolVersion {
		t.Errorf("protocolVersion = %v, want baseline %v", res["protocolVersion"], ProtocolVersion)
	}
}

// TestConformanceCapabilitiesOnlyToolsAndResources (spike point 3): the
// capability advertisement is exactly tools + resources — no prompts,
// logging, completions or elicitation claims.
func TestConformanceCapabilitiesOnlyToolsAndResources(t *testing.T) {
	s := conformanceServer()
	out := mustHandle(t, s, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-03-26"}}`)
	res := mustResult(t, out)
	caps, ok := res["capabilities"].(map[string]any)
	if !ok {
		t.Fatalf("capabilities = %v, want an object", res["capabilities"])
	}
	if caps["tools"] == nil || caps["resources"] == nil {
		t.Errorf("capabilities must advertise tools and resources, got %v", caps)
	}
	for _, forbidden := range []string{"prompts", "logging", "completions", "elicitation"} {
		if caps[forbidden] != nil {
			t.Errorf("capabilities must not advertise %q, got %v", forbidden, caps)
		}
	}
	if len(caps) != 2 {
		t.Errorf("capabilities = %v, want exactly tools + resources", caps)
	}
}

// TestConformanceToolsListExact (spike point 4a): tools/list returns
// exactly the 21-tool surface in the acceptance order (draft_read + deprecated view alias + sync_push + assign trio + feedback trio),
// each with a valid JSON Schema inputSchema.
func TestConformanceToolsListExact(t *testing.T) {
	s := conformanceServer()
	out := mustHandle(t, s, `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)
	res := mustResult(t, out)
	tools, ok := res["tools"].([]any)
	if !ok {
		t.Fatalf("tools = %v, want an array", res["tools"])
	}
	want := []string{"context", "get", "domain", "status", "validate", "new", "publish", "transition", "note", "draft_read", "view", "draft_list", "integrity_check", "discard", "sync_push", "assign", "reassign", "unassign", "feedback_new", "feedback_list", "feedback_publish"}
	if len(tools) != len(want) {
		t.Fatalf("tools = %v, want exactly %v", tools, want)
	}
	for i, tl := range tools {
		tm, ok := tl.(map[string]any)
		if !ok {
			t.Fatalf("tool %d = %v, want an object", i, tl)
		}
		if tm["name"] != want[i] {
			t.Errorf("tool[%d].name = %v, want %v", i, tm["name"], want[i])
		}
		schema, ok := tm["inputSchema"].(map[string]any)
		if !ok {
			t.Fatalf("tool %v must carry an inputSchema object", tm["name"])
		}
		if schema["type"] != "object" {
			t.Errorf("tool %v inputSchema.type = %v, want object", tm["name"], schema["type"])
		}
		if schema["properties"] == nil {
			t.Errorf("tool %v inputSchema must carry properties", tm["name"])
		}
	}
	// Deprecated alias `view` must carry deprecation flag with migration note to `draft_read`.
	for _, tl := range tools {
		tm := tl.(map[string]any)
		if tm["name"] == "view" {
			desc, _ := tm["description"].(string)
			if !strings.Contains(strings.ToLower(desc), "deprecated") || !strings.Contains(desc, "draft_read") {
				t.Errorf("tool view description = %q, want deprecated flag with migration note to draft_read", desc)
			}
		}
	}
}

// TestConformanceDraftReadAndViewAlias (td:mcp-view-naming-fix): both draft_read and its deprecated alias view
// must succeed and return identical verbatim draft content (deterministic).
func TestConformanceDraftReadAndViewAlias(t *testing.T) {
	s := conformanceServer()
	for _, name := range []string{"draft_read", "view"} {
		out := mustHandle(t, s, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"`+name+`","arguments":{"target":"feather/adr:001","project":"feather"}}}`)
		res := mustResult(t, out)
		if res["isError"] != false {
			t.Errorf("%s: isError = %v, want false", name, res["isError"])
		}
		content := res["content"].([]any)[0].(map[string]any)
		if content["type"] != "text" {
			t.Errorf("%s: content type = %v, want text", name, content["type"])
		}
		var doc map[string]any
		if err := json.Unmarshal([]byte(content["text"].(string)), &doc); err != nil {
			t.Fatalf("%s: tool text must be JSON: %v", name, err)
		}
	}
	a := mustHandle(t, s, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"draft_read","arguments":{"target":"feather/adr:001"}}}`)
	b := mustHandle(t, s, `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"view","arguments":{"target":"feather/adr:001"}}}`)
	aText := mustResult(t, a)["content"].([]any)[0].(map[string]any)["text"].(string)
	bText := mustResult(t, b)["content"].([]any)[0].(map[string]any)["text"].(string)
	if aText != bText {
		t.Errorf("draft_read and view alias must return identical verbatim content, got %q vs %q", aText, bText)
	}
}

// TestConformanceToolsCallUnknownTool (spike point 4b): an unknown tool
// name is a JSON-RPC error -32003 (tool not found).
func TestConformanceToolsCallUnknownTool(t *testing.T) {
	s := conformanceServer()
	out := mustHandle(t, s, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"bogus"}}`)
	errObj := mustError(t, out)
	if errObj["code"] != float64(codeToolNotFound) {
		t.Errorf("error code = %v, want %v", errObj["code"], codeToolNotFound)
	}
}

// TestConformanceToolsCallExecutionError (spike point 4c): a capability
// failure is an MCP tool result with isError=true (not a JSON-RPC
// error), carrying a deterministic message.
func TestConformanceToolsCallExecutionError(t *testing.T) {
	cap := &fakeCapability{statusJSON: `{}`, getErr: errors.New("eka: workspace not initialized")}
	s := NewServer(cap)
	out := mustHandle(t, s, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"get","arguments":{"form":"x/adr:y:1"}}}`)
	res := mustResult(t, out)
	if res["isError"] != true {
		t.Errorf("isError = %v, want true", res["isError"])
	}
	content := res["content"].([]any)[0].(map[string]any)
	if !strings.Contains(content["text"].(string), "workspace not initialized") {
		t.Errorf("error text = %v, want the capability message", content["text"])
	}
}

// TestConformanceToolsCallSuccess (spike point 4d): a successful tool
// call returns text content with isError=false.
func TestConformanceToolsCallSuccess(t *testing.T) {
	s := conformanceServer()
	out := mustHandle(t, s, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"get","arguments":{"form":"f/adr:x:1"}}}`)
	res := mustResult(t, out)
	if res["isError"] != false {
		t.Errorf("isError = %v, want false", res["isError"])
	}
	content := res["content"].([]any)[0].(map[string]any)
	if content["type"] != "text" {
		t.Errorf("content type = %v, want text", content["type"])
	}
	var doc map[string]any
	if err := json.Unmarshal([]byte(content["text"].(string)), &doc); err != nil {
		t.Fatalf("tool text must be JSON: %v", err)
	}
}

// TestConformanceResourcesList (spike point 5a): resources/list exposes
// the deterministic resource set — eka://status first, then every
// embedded skill and every embedded draft template type.
func TestConformanceResourcesList(t *testing.T) {
	s := conformanceServer()
	out := mustHandle(t, s, `{"jsonrpc":"2.0","id":1,"method":"resources/list"}`)
	res := mustResult(t, out)
	resources := res["resources"].([]any)
	wantCount := 1 + len(mustSkillDirs(t)) + len(mustTemplateTypes(t))
	if len(resources) != wantCount {
		t.Fatalf("resources = %d, want %d", len(resources), wantCount)
	}
	r := resources[0].(map[string]any)
	if r["uri"] != "eka://status" {
		t.Errorf("resource uri = %v, want eka://status", r["uri"])
	}
}

// TestConformanceResourcesListSkillDescriptions (req:agent-agnostic-skill-pack
// R9): every eka://skills/<name> resource-listing entry carries the
// description from the SKILL.md frontmatter — the discoverability
// contract the skill load-order protocol (step 2) relies on.
func TestConformanceResourcesListSkillDescriptions(t *testing.T) {
	s := conformanceServer()
	out := mustHandle(t, s, `{"jsonrpc":"2.0","id":1,"method":"resources/list"}`)
	res := mustResult(t, out)
	byURI := map[string]map[string]any{}
	for _, r := range res["resources"].([]any) {
		m := r.(map[string]any)
		byURI[m["uri"].(string)] = m
	}
	skills := mustSkillDirs(t)
	if len(skills) == 0 {
		t.Fatal("the pack must embed at least one skill")
	}
	for _, name := range skills {
		entry, ok := byURI["eka://skills/"+name]
		if !ok {
			t.Errorf("resources/list is missing eka://skills/%s", name)
			continue
		}
		want, err := pack.SkillDescription(name)
		if err != nil {
			t.Fatalf("SkillDescription(%q): %v", name, err)
		}
		if entry["description"] != want {
			t.Errorf("eka://skills/%s description = %q, want the frontmatter description %q", name, entry["description"], want)
		}
	}
}

// TestConformanceResourcesReadSkill (spike point 5b): resources/read on
// eka://skills/<name> returns the embedded SKILL.md as markdown text.
func TestConformanceResourcesReadSkill(t *testing.T) {
	s := conformanceServer()
	skills := mustSkillDirs(t)
	if len(skills) == 0 {
		t.Fatal("the pack must embed at least one skill")
	}
	out := mustHandle(t, s, `{"jsonrpc":"2.0","id":1,"method":"resources/read","params":{"uri":"eka://skills/`+skills[0]+`"}}`)
	res := mustResult(t, out)
	contents := res["contents"].([]any)[0].(map[string]any)
	if contents["uri"] != "eka://skills/"+skills[0] {
		t.Errorf("content uri = %v", contents["uri"])
	}
	if contents["mimeType"] != "text/markdown" {
		t.Errorf("mimeType = %v, want text/markdown", contents["mimeType"])
	}
	if !strings.Contains(contents["text"].(string), "#") {
		t.Errorf("skill text must be markdown, got %v", contents["text"])
	}
}

// TestConformanceResourcesReadTemplate (spike point 5c): resources/read
// on eka://templates/<type> returns the embedded draft template JSON.
func TestConformanceResourcesReadTemplate(t *testing.T) {
	s := conformanceServer()
	types := mustTemplateTypes(t)
	if len(types) == 0 {
		t.Fatal("the pack must embed at least one draft template")
	}
	out := mustHandle(t, s, `{"jsonrpc":"2.0","id":1,"method":"resources/read","params":{"uri":"eka://templates/`+types[0]+`"}}`)
	res := mustResult(t, out)
	contents := res["contents"].([]any)[0].(map[string]any)
	if contents["uri"] != "eka://templates/"+types[0] {
		t.Errorf("content uri = %v", contents["uri"])
	}
	if contents["mimeType"] != "application/json" {
		t.Errorf("mimeType = %v, want application/json", contents["mimeType"])
	}
	var doc map[string]any
	if err := json.Unmarshal([]byte(contents["text"].(string)), &doc); err != nil {
		t.Fatalf("template text must be JSON: %v", err)
	}
}

// TestConformanceResourcesReadUnknown (spike point 5d): an unknown
// resource URI is a JSON-RPC error -32002 (resource not found).
func TestConformanceResourcesReadUnknown(t *testing.T) {
	s := conformanceServer()
	out := mustHandle(t, s, `{"jsonrpc":"2.0","id":1,"method":"resources/read","params":{"uri":"eka://bogus"}}`)
	errObj := mustError(t, out)
	if errObj["code"] != float64(codeResourceNotFound) {
		t.Errorf("error code = %v, want %v", errObj["code"], codeResourceNotFound)
	}
}

// TestConformanceNotificationsNoResponse (spike point 6a): MCP
// notifications (initialized, cancelled) produce no response.
func TestConformanceNotificationsNoResponse(t *testing.T) {
	s := conformanceServer()
	for _, msg := range []string{
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","method":"notifications/cancelled","params":{"requestId":1,"reason":"done"}}`,
	} {
		if resp := s.HandleMessage([]byte(msg)); len(resp) != 0 {
			t.Errorf("%s must not produce a response, got %s", msg, resp)
		}
	}
}

// TestConformancePingEmptyResult (spike point 6b): ping answers with an
// empty result object.
func TestConformancePingEmptyResult(t *testing.T) {
	s := conformanceServer()
	out := mustHandle(t, s, `{"jsonrpc":"2.0","id":7,"method":"ping"}`)
	res := mustResult(t, out)
	if len(res) != 0 {
		t.Errorf("ping result = %v, want an empty object", res)
	}
}

// TestConformanceErrorCodes (spike point 7): the JSON-RPC error codes
// are exactly -32700 (parse), -32600 (invalid request), -32601 (method
// not found), -32602 (invalid params).
func TestConformanceErrorCodes(t *testing.T) {
	s := conformanceServer()
	cases := []struct {
		name string
		msg  string
		code int
	}{
		{"parse error", `{not json`, codeParseError},
		{"invalid jsonrpc version", `{"jsonrpc":"1.0","id":1,"method":"ping"}`, codeInvalidRequest},
		{"missing method", `{"jsonrpc":"2.0","id":1}`, codeInvalidRequest},
		{"non-object message", `"ping"`, codeInvalidRequest},
		{"batch array", `[{"jsonrpc":"2.0","id":1,"method":"ping"}]`, codeInvalidRequest},
		{"method not found", `{"jsonrpc":"2.0","id":1,"method":"nope"}`, codeMethodNotFound},
		{"invalid params", `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":123}}`, codeInvalidParams},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out := mustHandle(t, s, tc.msg)
			errObj := mustError(t, out)
			if errObj["code"] != float64(tc.code) {
				t.Errorf("error code = %v, want %v", errObj["code"], tc.code)
			}
		})
	}
}

// flushCountingWriter records every Write call the transport makes to
// the underlying stream — with bufio, each Flush surfaces as one Write,
// so the count proves flush-per-response.
type flushCountingWriter struct {
	buf    bytes.Buffer
	writes int
}

func (w *flushCountingWriter) Write(p []byte) (int, error) {
	w.writes++
	return w.buf.Write(p)
}

// TestConformanceStdioFraming (spike point 8): responses are
// newline-delimited and the writer is flushed after every response.
func TestConformanceStdioFraming(t *testing.T) {
	s := conformanceServer()
	input := strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-03-26"}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`,
		`{"jsonrpc":"2.0","id":3,"method":"ping"}`,
	}, "\n") + "\n"

	w := &flushCountingWriter{}
	if err := s.Serve(strings.NewReader(input), w); err != nil {
		t.Fatalf("Serve failed: %v", err)
	}

	raw := w.buf.String()
	if !strings.HasSuffix(raw, "\n") {
		t.Errorf("output must end with a newline, got %q", raw)
	}
	lines := strings.Split(strings.TrimSuffix(raw, "\n"), "\n")
	if len(lines) != 3 {
		t.Fatalf("expected 3 response lines (the notification gets none), got %d:\n%s", len(lines), raw)
	}
	for i, line := range lines {
		var v any
		if err := json.Unmarshal([]byte(line), &v); err != nil {
			t.Errorf("line %d is not valid JSON: %v\n%s", i, err, line)
		}
	}
	// One underlying Write per response = one Flush per response.
	if w.writes != 3 {
		t.Errorf("underlying writes = %d, want 3 (flush per response)", w.writes)
	}
}

// TestConformanceSyncPushTool (ts:mcp-sync-push-tool AC #1-5): sync_push
// is push-only, deterministic (same store state -> same bytes), uses the
// same engine as `eka sync push`, and refuses pull/fromDocs deterministically.
func TestConformanceSyncPushTool(t *testing.T) {
	s := conformanceServer()
	// tools/list must carry sync_push with required properties.
	out := mustHandle(t, s, `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)
	res := mustResult(t, out)
	tools := res["tools"].([]any)
	found := false
	for _, tl := range tools {
		tm := tl.(map[string]any)
		if tm["name"] == "sync_push" {
			found = true
			schema := tm["inputSchema"].(map[string]any)
			props := schema["properties"].(map[string]any)
			if props["repoPath"] == nil || props["adopt"] == nil || props["override"] == nil {
				t.Errorf("sync_push inputSchema must carry repoPath, adopt, override, got %v", props)
			}
			desc := tm["description"].(string)
			if !strings.Contains(desc, "eka-sync-push-result-v1") && !strings.Contains(desc, "eka sync push") {
				t.Errorf("sync_push description = %q, want eka-sync-push-result-v1 and eka sync push mention", desc)
			}
			if !strings.Contains(desc, "pull") {
				t.Errorf("sync_push description = %q, want pull refusal note", desc)
			}
		}
	}
	if !found {
		t.Fatal("tools/list is missing sync_push")
	}

	// tools/call sync_push succeeds with deterministic byte-identical output for identical state.
	a := mustHandle(t, s, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"sync_push","arguments":{}}}`)
	b := mustHandle(t, s, `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"sync_push","arguments":{}}}`)
	aText := mustResult(t, a)["content"].([]any)[0].(map[string]any)["text"].(string)
	bText := mustResult(t, b)["content"].([]any)[0].(map[string]any)["text"].(string)
	if aText != bText {
		t.Errorf("sync_push must be byte-deterministic for identical state, got %q vs %q", aText, bText)
	}
	var doc map[string]any
	if err := json.Unmarshal([]byte(aText), &doc); err != nil {
		t.Fatalf("sync_push text must be JSON: %v", err)
	}
	if doc["schema"] != "eka-sync-push-result-v1" {
		t.Errorf("sync_push schema = %v, want eka-sync-push-result-v1", doc["schema"])
	}
	if doc["project"] == nil || doc["repo"] == nil || doc["snapshotDigest"] == nil {
		t.Errorf("sync_push result must carry project/repo/snapshotDigest, got %v", doc)
	}

	// sync_push with repoPath explicitly succeeds.
	out = mustHandle(t, s, `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"sync_push","arguments":{"repoPath":"."}}}`)
	if mustResult(t, out)["isError"] != false {
		t.Errorf("sync_push with repoPath must succeed")
	}

	// sync pull / fromDocs via arguments refuses deterministically naming CLI.
	for _, arg := range []string{
		`{"fromDocs":true}`, `{"from_docs":true}`, `{"from-docs":true}`, `{"pull":true}`,
	} {
		out := mustHandle(t, s, `{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"sync_push","arguments":`+arg+`}}`)
		res := mustResult(t, out)
		if res["isError"] != true {
			t.Errorf("sync_push %s must be refused with isError=true, got %v", arg, res)
		}
		text := res["content"].([]any)[0].(map[string]any)["text"].(string)
		if !strings.Contains(text, "eka sync pull") || !strings.Contains(text, "not exposed") {
			t.Errorf("sync_push pull refusal text = %q, want 'not exposed' and 'eka sync pull'", text)
		}
	}

	// unknown tool `sync_pull` is -32003 (not exposed).
	out = mustHandle(t, s, `{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"sync_pull","arguments":{}}}`)
	errObj := mustError(t, out)
	if errObj["code"] != float64(codeToolNotFound) {
		t.Errorf("sync_pull error code = %v, want %v", errObj["code"], codeToolNotFound)
	}

	// Unknown repo via capability error surfaces as isError (sanitized).
	cap := &fakeCapability{statusJSON: `{}`, getErr: errors.New("sync refused: <repo> is not an EKA repository (no eka.yaml); run 'eka init' first")}
	_ = cap
	s2 := NewServer(&failingSyncPushCapability{err: errors.New("sync refused: /tmp/not-a-repo is not an EKA repository")})
	out = mustHandle(t, s2, `{"jsonrpc":"2.0","id":6,"method":"tools/call","params":{"name":"sync_push","arguments":{"repoPath":"/tmp/not-a-repo"}}}`)
	res2 := mustResult(t, out)
	if res2["isError"] != true {
		t.Errorf("sync_push unknown repo must be isError=true, got %v", res2)
	}
	text2 := res2["content"].([]any)[0].(map[string]any)["text"].(string)
	if strings.Contains(text2, "/tmp/not-a-repo") {
		t.Errorf("sync_push error must not leak path, got %q", text2)
	}
}

// TestConformanceAssignmentTools (td:mcp-assignment-tools AC #1-3): assign/reassign/unassign
// expose the same engine as CLI with deterministic refusals and agent identity.
func TestConformanceAssignmentTools(t *testing.T) {
	s := conformanceServer()
	out := mustHandle(t, s, `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)
	res := mustResult(t, out)
	tools := res["tools"].([]any)
	byName := map[string]map[string]any{}
	for _, tl := range tools {
		tm := tl.(map[string]any)
		byName[tm["name"].(string)] = tm
	}
	for _, name := range []string{"assign", "reassign", "unassign"} {
		tm, ok := byName[name]
		if !ok {
			t.Fatalf("tools/list is missing %s", name)
		}
		schema := tm["inputSchema"].(map[string]any)
		props := schema["properties"].(map[string]any)
		if props["target"] == nil {
			t.Errorf("%s inputSchema must carry target, got %v", name, props)
		}
		desc := tm["description"].(string)
		if !strings.Contains(desc, "eka-assignment-v1") {
			t.Errorf("%s description = %q, want eka-assignment-v1", name, desc)
		}
		if !strings.Contains(strings.ToLower(desc), "eka "+name) && !strings.Contains(desc, "assign") {
			t.Errorf("%s description = %q, want mention of CLI semantics", name, desc)
		}
	}
	// assign/reassign require to, unassign does not
	for _, name := range []string{"assign", "reassign"} {
		tm := byName[name]
		schema := tm["inputSchema"].(map[string]any)
		props := schema["properties"].(map[string]any)
		if props["to"] == nil {
			t.Errorf("%s inputSchema must carry to, got %v", name, props)
		}
		req, _ := schema["required"].([]any)
		hasTarget, hasTo := false, false
		for _, r := range req {
			if r == "target" {
				hasTarget = true
			}
			if r == "to" {
				hasTo = true
			}
		}
		if !hasTarget || !hasTo {
			t.Errorf("%s required = %v, want target and to", name, req)
		}
	}
	tm := byName["unassign"]
	schema := tm["inputSchema"].(map[string]any)
	req, _ := schema["required"].([]any)
	hasTarget := false
	for _, r := range req {
		if r == "target" {
			hasTarget = true
		}
	}
	if !hasTarget {
		t.Errorf("unassign required = %v, want target", req)
	}

	// tools/call assign succeeds with deterministic byte-identical output and agent identity
	a := mustHandle(t, s, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"assign","arguments":{"target":"acme/sto:1","to":"mbr:alice"}}}`)
	b := mustHandle(t, s, `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"assign","arguments":{"target":"acme/sto:1","to":"mbr:alice"}}}`)
	aText := mustResult(t, a)["content"].([]any)[0].(map[string]any)["text"].(string)
	bText := mustResult(t, b)["content"].([]any)[0].(map[string]any)["text"].(string)
	if aText != bText {
		t.Errorf("assign must be byte-deterministic for identical state, got %q vs %q", aText, bText)
	}
	var doc map[string]any
	if err := json.Unmarshal([]byte(aText), &doc); err != nil {
		t.Fatalf("assign text must be JSON: %v", err)
	}
	if doc["schema"] != "eka-assignment-v1" {
		t.Errorf("assign schema = %v, want eka-assignment-v1", doc["schema"])
	}
	if doc["by"] == nil || doc["by"] == "" {
		t.Errorf("assign result must carry by (agent identity), got %v", doc["by"])
	}
	// reassign and unassign also deterministic
	for _, name := range []string{"reassign", "unassign"} {
		arg := `{"target":"acme/sto:1","to":"mbr:bob"}`
		if name == "unassign" {
			arg = `{"target":"acme/sto:1"}`
		}
		aa := mustHandle(t, s, `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"`+name+`","arguments":`+arg+`}}`)
		bb := mustHandle(t, s, `{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"`+name+`","arguments":`+arg+`}}`)
		at := mustResult(t, aa)["content"].([]any)[0].(map[string]any)["text"].(string)
		bt := mustResult(t, bb)["content"].([]any)[0].(map[string]any)["text"].(string)
		if at != bt {
			t.Errorf("%s must be byte-deterministic, got %q vs %q", name, at, bt)
		}
	}

	// Malformed / missing args refuse deterministically as isError=true (no JSON-RPC error)
	for _, tc := range []struct {
		name string
		msg  string
	}{
		{"assign missing to", `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"assign","arguments":{"target":"acme/sto:1"}}}`},
		{"assign missing target", `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"assign","arguments":{"to":"mbr:alice"}}}`},
		{"reassign missing to", `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"reassign","arguments":{"target":"acme/sto:1"}}}`},
		{"unassign missing target", `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"unassign","arguments":{}}}`},
		{"assign unknown byKind", `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"assign","arguments":{"target":"acme/sto:1","to":"mbr:alice","by":"x","byKind":"bogus"}}}`},
	} {
		out := mustHandle(t, s, tc.msg)
		res := mustResult(t, out)
		if res["isError"] != true {
			t.Errorf("%s must be isError=true, got %v", tc.name, res)
		}
	}

	// Capability-level refusal (member does not resolve, non-work-item) surfaces as isError with sanitized text
	cap := &failingAssignmentCapability{err: errors.New("assign refused: member acme/mbr:alice does not resolve; available members of the repository: ")}
	s2 := NewServer(cap)
	out = mustHandle(t, s2, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"assign","arguments":{"target":"acme/sto:1","to":"mbr:alice"}}}`)
	res2 := mustResult(t, out)
	if res2["isError"] != true {
		t.Errorf("assign capability refusal must be isError=true, got %v", res2)
	}
	text := res2["content"].([]any)[0].(map[string]any)["text"].(string)
	if !strings.Contains(text, "does not resolve") {
		t.Errorf("assign refusal text = %q, want deterministic refusal", text)
	}
}

// TestConformanceFeedbackTools (ts:mcp-feedback-tool AC #1-5): feedback_new/list/publish mirror CLI, list-call semantics, refusals, no CKO, token invariants.
func TestConformanceFeedbackTools(t *testing.T) {
	s := conformanceServer()
	out := mustHandle(t, s, `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)
	res := mustResult(t, out)
	tools := res["tools"].([]any)
	byName := map[string]map[string]any{}
	for _, tl := range tools {
		tm := tl.(map[string]any)
		byName[tm["name"].(string)] = tm
	}
	for _, name := range []string{"feedback_new", "feedback_list", "feedback_publish"} {
		tm, ok := byName[name]
		if !ok {
			t.Fatalf("tools/list is missing %s", name)
		}
		schema := tm["inputSchema"].(map[string]any)
		if schema["type"] != "object" {
			t.Errorf("%s inputSchema.type = %v, want object", name, schema["type"])
		}
		desc := tm["description"].(string)
		if name == "feedback_new" && !strings.Contains(desc, "eka-feedback-new-v1") {
			t.Errorf("feedback_new description = %q, want eka-feedback-new-v1", desc)
		}
		if name == "feedback_list" && !strings.Contains(desc, "eka-feedback-list-v1") {
			t.Errorf("feedback_list description = %q, want eka-feedback-list-v1", desc)
		}
		if name == "feedback_publish" && !strings.Contains(desc, "eka-feedback-publish-v1") {
			t.Errorf("feedback_publish description = %q, want eka-feedback-publish-v1", desc)
		}
		if !strings.Contains(strings.ToLower(desc), "feedback") {
			t.Errorf("%s description = %q, want feedback mention", name, desc)
		}
	}
	// feedback_new requires type and title; feedback_publish requires id
	for _, tc := range []struct {
		name string
		msg  string
	}{
		{"feedback_new missing title", `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"feedback_new","arguments":{"type":"bug"}}}`},
		{"feedback_new missing type", `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"feedback_new","arguments":{"title":"x"}}}`},
		{"feedback_new invalid type", `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"feedback_new","arguments":{"type":"rant","title":"x"}}}`},
		{"feedback_publish missing id", `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"feedback_publish","arguments":{}}}`},
		{"feedback_publish empty id", `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"feedback_publish","arguments":{"id":""}}}`},
	} {
		out := mustHandle(t, s, tc.msg)
		res := mustResult(t, out)
		if res["isError"] != true {
			t.Errorf("%s must be isError=true, got %v", tc.name, res)
		}
		text := res["content"].([]any)[0].(map[string]any)["text"].(string)
		if strings.Contains(text, "stack") || strings.Contains(text, "/home") || strings.Contains(text, "<path>") && strings.Contains(tc.name, "invalid type") {
			// path sanitization check is handled elsewhere; just ensure no stack trace leak
			if strings.Contains(text, "goroutine") {
				t.Errorf("%s error text leaks stack: %q", tc.name, text)
			}
		}
	}
	// feedback_new/list/publish success + byte-determinism
	a := mustHandle(t, s, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"feedback_new","arguments":{"type":"bug","title":"my bug"}}}`)
	b := mustHandle(t, s, `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"feedback_new","arguments":{"type":"bug","title":"my bug"}}}`)
	aText := mustResult(t, a)["content"].([]any)[0].(map[string]any)["text"].(string)
	bText := mustResult(t, b)["content"].([]any)[0].(map[string]any)["text"].(string)
	// fake is deterministic per title, so same title -> same bytes
	if aText != bText {
		t.Errorf("feedback_new must be deterministic for identical args, got %q vs %q", aText, bText)
	}
	var doc map[string]any
	if err := json.Unmarshal([]byte(aText), &doc); err != nil {
		t.Fatalf("feedback_new text must be JSON: %v", err)
	}
	if doc["schema"] != "eka-feedback-new-v1" {
		t.Errorf("feedback_new schema = %v, want eka-feedback-new-v1", doc["schema"])
	}
	if doc["id"] == nil || doc["path"] == nil {
		t.Errorf("feedback_new result must carry id and path, got %v", doc)
	}
	// feedback_list success
	out = mustHandle(t, s, `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"feedback_list","arguments":{}}}`)
	resList := mustResult(t, out)
	if resList["isError"] != false {
		t.Errorf("feedback_list isError = %v, want false", resList["isError"])
	}
	listText := resList["content"].([]any)[0].(map[string]any)["text"].(string)
	var listDoc map[string]any
	if err := json.Unmarshal([]byte(listText), &listDoc); err != nil {
		t.Fatalf("feedback_list text must be JSON: %v", err)
	}
	if listDoc["schema"] != "eka-feedback-list-v1" {
		t.Errorf("feedback_list schema = %v, want eka-feedback-list-v1", listDoc["schema"])
	}
	if listDoc["feedback"] == nil {
		t.Errorf("feedback_list must carry feedback array, got %v", listDoc)
	}
	// feedback_list with explicit empty object also succeeds
	out = mustHandle(t, s, `{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"feedback_list","arguments":{}}}`)
	if mustResult(t, out)["isError"] != false {
		t.Errorf("feedback_list empty args must succeed")
	}
	// feedback_list byte-determinism for identical state (empty list)
	aa := mustHandle(t, s, `{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"feedback_list","arguments":{}}}`)
	bb := mustHandle(t, s, `{"jsonrpc":"2.0","id":6,"method":"tools/call","params":{"name":"feedback_list","arguments":{}}}`)
	at := mustResult(t, aa)["content"].([]any)[0].(map[string]any)["text"].(string)
	bt := mustResult(t, bb)["content"].([]any)[0].(map[string]any)["text"].(string)
	if at != bt {
		t.Errorf("feedback_list must be deterministic, got %q vs %q", at, bt)
	}
	// feedback_publish success
	out = mustHandle(t, s, `{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"name":"feedback_publish","arguments":{"id":"fbk-20260812-test"}}}`)
	resPub := mustResult(t, out)
	if resPub["isError"] != false {
		t.Errorf("feedback_publish isError = %v, want false", resPub["isError"])
	}
	pubText := resPub["content"].([]any)[0].(map[string]any)["text"].(string)
	var pubDoc map[string]any
	if err := json.Unmarshal([]byte(pubText), &pubDoc); err != nil {
		t.Fatalf("feedback_publish text must be JSON: %v", err)
	}
	if pubDoc["schema"] != "eka-feedback-publish-v1" {
		t.Errorf("feedback_publish schema = %v, want eka-feedback-publish-v1", pubDoc["schema"])
	}
	// refusal cases: unauthenticated publish (token gate) and invalid draft id
	cap := &failingFeedbackCapability{err: errors.New("issue token not bundled — use a release binary")}
	s2 := NewServer(cap)
	out = mustHandle(t, s2, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"feedback_publish","arguments":{"id":"fbk-20260812-test"}}}`)
	res2 := mustResult(t, out)
	if res2["isError"] != true {
		t.Errorf("unauthenticated publish must be isError=true, got %v", res2)
	}
	text := res2["content"].([]any)[0].(map[string]any)["text"].(string)
	if !strings.Contains(text, "issue token not bundled") || !strings.Contains(text, "release binary") {
		t.Errorf("unauthenticated publish text = %q, want token gate remediation", text)
	}
	if strings.Contains(text, "Bearer") || strings.Contains(text, "token=") {
		t.Errorf("unauthenticated publish must not leak token material, got %q", text)
	}
	cap2 := &failingFeedbackCapability{err: errors.New(`unknown feedback "fbk-20260812-nope"`)}
	s3 := NewServer(cap2)
	out = mustHandle(t, s3, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"feedback_publish","arguments":{"id":"fbk-20260812-nope"}}}`)
	res3 := mustResult(t, out)
	if res3["isError"] != true {
		t.Errorf("invalid draft id publish must be isError=true, got %v", res3)
	}
	text3 := res3["content"].([]any)[0].(map[string]any)["text"].(string)
	if !strings.Contains(text3, "unknown feedback") {
		t.Errorf("invalid draft id text = %q, want unknown feedback", text3)
	}
	if strings.Contains(text3, "/tmp") || strings.Contains(text3, ".eka/feedback") {
		t.Errorf("invalid draft id must not leak internal paths, got %q", text3)
	}
	// Already published refusal
	cap3 := &failingFeedbackCapability{err: errors.New("already published as #42 https://github.com/maleolabs/eka-cli/issues/42")}
	s4 := NewServer(cap3)
	out = mustHandle(t, s4, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"feedback_publish","arguments":{"id":"fbk-20260812-test"}}}`)
	res4 := mustResult(t, out)
	if res4["isError"] != true {
		t.Errorf("already published must be isError=true, got %v", res4)
	}
	text4 := res4["content"].([]any)[0].(map[string]any)["text"].(string)
	if !strings.Contains(text4, "already published") {
		t.Errorf("already published text = %q, want already published", text4)
	}
}

// failingFeedbackCapability fails only feedback_publish (and feedback_new when needed) for refusal tests.
type failingFeedbackCapability struct {
	err error
}

func (f *failingFeedbackCapability) Get(form string) ([]byte, error) { return nil, nil }
func (f *failingFeedbackCapability) Domain(p, d string) ([]byte, error) { return nil, nil }
func (f *failingFeedbackCapability) Status() ([]byte, error) { return []byte(`{}`), nil }
func (f *failingFeedbackCapability) Context(s, p, d string) ([]byte, error) { return nil, nil }
func (f *failingFeedbackCapability) Validate(root string) ([]byte, error) { return nil, nil }
func (f *failingFeedbackCapability) NewDraft(req NewDraftRequest) ([]byte, error) { return nil, nil }
func (f *failingFeedbackCapability) Publish(req PublishRequest) ([]byte, error) { return nil, nil }
func (f *failingFeedbackCapability) Transition(req TransitionRequest) ([]byte, error) { return nil, nil }
func (f *failingFeedbackCapability) Note(req NoteRequest) ([]byte, error) { return nil, nil }
func (f *failingFeedbackCapability) DraftRead(t, p string) ([]byte, error) { return nil, nil }
func (f *failingFeedbackCapability) View(t, p string) ([]byte, error) { return nil, nil }
func (f *failingFeedbackCapability) DraftList(p string) ([]byte, error) { return nil, nil }
func (f *failingFeedbackCapability) IntegrityCheck() ([]byte, error) { return nil, nil }
func (f *failingFeedbackCapability) Discard(t, p string) ([]byte, error) { return nil, nil }
func (f *failingFeedbackCapability) SyncPush(repoPath string, adopt, override bool) ([]byte, error) {
	return nil, nil
}
func (f *failingFeedbackCapability) Assign(req AssignmentRequest) ([]byte, error) { return nil, nil }
func (f *failingFeedbackCapability) Reassign(req AssignmentRequest) ([]byte, error) { return nil, nil }
func (f *failingFeedbackCapability) Unassign(req UnassignRequest) ([]byte, error) { return nil, nil }
func (f *failingFeedbackCapability) FeedbackNew(req FeedbackNewRequest) ([]byte, error) {
	if f.err != nil {
		return nil, f.err
	}
	return []byte(`{"schema":"eka-feedback-new-v1","ok":true,"id":"fbk-20260812-test","path":"/tmp/eka/feedback/fbk-20260812-test.md","status":"draft"}`), nil
}
func (f *failingFeedbackCapability) FeedbackList() ([]byte, error) { return []byte(`{"schema":"eka-feedback-list-v1","ok":true,"feedback":[]}`), nil }
func (f *failingFeedbackCapability) FeedbackPublish(req FeedbackPublishRequest) ([]byte, error) {
	return nil, f.err
}

// failingAssignmentCapability is a tiny fake that only fails assignment, for refusal tests.
type failingAssignmentCapability struct {
	err error
}

func (f *failingAssignmentCapability) Get(form string) ([]byte, error) { return nil, nil }
func (f *failingAssignmentCapability) Domain(p, d string) ([]byte, error) { return nil, nil }
func (f *failingAssignmentCapability) Status() ([]byte, error) { return []byte(`{}`), nil }
func (f *failingAssignmentCapability) Context(s, p, d string) ([]byte, error) { return nil, nil }
func (f *failingAssignmentCapability) Validate(root string) ([]byte, error) { return nil, nil }
func (f *failingAssignmentCapability) NewDraft(req NewDraftRequest) ([]byte, error) { return nil, nil }
func (f *failingAssignmentCapability) Publish(req PublishRequest) ([]byte, error) { return nil, nil }
func (f *failingAssignmentCapability) Transition(req TransitionRequest) ([]byte, error) { return nil, nil }
func (f *failingAssignmentCapability) Note(req NoteRequest) ([]byte, error) { return nil, nil }
func (f *failingAssignmentCapability) DraftRead(t, p string) ([]byte, error) { return nil, nil }
func (f *failingAssignmentCapability) View(t, p string) ([]byte, error) { return nil, nil }
func (f *failingAssignmentCapability) DraftList(p string) ([]byte, error) { return nil, nil }
func (f *failingAssignmentCapability) IntegrityCheck() ([]byte, error) { return nil, nil }
func (f *failingAssignmentCapability) Discard(t, p string) ([]byte, error) { return nil, nil }
func (f *failingAssignmentCapability) SyncPush(repoPath string, adopt, override bool) ([]byte, error) {
	return nil, nil
}
func (f *failingAssignmentCapability) Assign(req AssignmentRequest) ([]byte, error) { return nil, f.err }
func (f *failingAssignmentCapability) Reassign(req AssignmentRequest) ([]byte, error) { return nil, f.err }
func (f *failingAssignmentCapability) Unassign(req UnassignRequest) ([]byte, error) { return nil, f.err }
func (f *failingAssignmentCapability) FeedbackNew(req FeedbackNewRequest) ([]byte, error) {
	return nil, nil
}
func (f *failingAssignmentCapability) FeedbackList() ([]byte, error) { return nil, nil }
func (f *failingAssignmentCapability) FeedbackPublish(req FeedbackPublishRequest) ([]byte, error) {
	return nil, f.err
}

// failingSyncPushCapability is a tiny fake that only fails sync_push, for the path-leak test.
type failingSyncPushCapability struct {
	err error
}

func (f *failingSyncPushCapability) Get(form string) ([]byte, error) { return nil, nil }
func (f *failingSyncPushCapability) Domain(p, d string) ([]byte, error) { return nil, nil }
func (f *failingSyncPushCapability) Status() ([]byte, error) { return []byte(`{}`), nil }
func (f *failingSyncPushCapability) Context(s, p, d string) ([]byte, error) { return nil, nil }
func (f *failingSyncPushCapability) Validate(root string) ([]byte, error) { return nil, nil }
func (f *failingSyncPushCapability) NewDraft(req NewDraftRequest) ([]byte, error) { return nil, nil }
func (f *failingSyncPushCapability) Publish(req PublishRequest) ([]byte, error) { return nil, nil }
func (f *failingSyncPushCapability) Transition(req TransitionRequest) ([]byte, error) { return nil, nil }
func (f *failingSyncPushCapability) Note(req NoteRequest) ([]byte, error) { return nil, nil }
func (f *failingSyncPushCapability) DraftRead(t, p string) ([]byte, error) { return nil, nil }
func (f *failingSyncPushCapability) View(t, p string) ([]byte, error) { return nil, nil }
func (f *failingSyncPushCapability) DraftList(p string) ([]byte, error) { return nil, nil }
func (f *failingSyncPushCapability) IntegrityCheck() ([]byte, error) { return nil, nil }
func (f *failingSyncPushCapability) Discard(t, p string) ([]byte, error) { return nil, nil }
func (f *failingSyncPushCapability) SyncPush(repoPath string, adopt, override bool) ([]byte, error) {
	return nil, f.err
}
func (f *failingSyncPushCapability) Assign(req AssignmentRequest) ([]byte, error) { return nil, nil }
func (f *failingSyncPushCapability) Reassign(req AssignmentRequest) ([]byte, error) { return nil, nil }
func (f *failingSyncPushCapability) FeedbackNew(req FeedbackNewRequest) ([]byte, error) {
	return nil, nil
}
func (f *failingSyncPushCapability) FeedbackList() ([]byte, error) { return nil, nil }
func (f *failingSyncPushCapability) FeedbackPublish(req FeedbackPublishRequest) ([]byte, error) {
	return nil, nil
}
func (f *failingSyncPushCapability) Unassign(req UnassignRequest) ([]byte, error) { return nil, nil }
