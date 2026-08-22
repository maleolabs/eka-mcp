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
//     publish/transition/note/view/draft_list/integrity_check/discard
//     with valid inputSchema; tools/call unknown tool → -32003;
//     execution error → isError:true; success → text content
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
// exactly the 13-tool surface in the acceptance order, each with a
// valid JSON Schema inputSchema.
func TestConformanceToolsListExact(t *testing.T) {
	s := conformanceServer()
	out := mustHandle(t, s, `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)
	res := mustResult(t, out)
	tools, ok := res["tools"].([]any)
	if !ok {
		t.Fatalf("tools = %v, want an array", res["tools"])
	}
	want := []string{"context", "get", "domain", "status", "validate", "new", "publish", "transition", "note", "view", "draft_list", "integrity_check", "discard"}
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
