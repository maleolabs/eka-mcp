package mcp

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

// fakeCapability is a deterministic stand-in for the EKA capability
// layer, so the server tests are pure protocol tests — no eka-core
// involved. It records calls so tests can assert the dispatch.
type fakeCapability struct {
	statusJSON string
	getErr     error
	domainErr  error
	gotForms   []string
}

func (f *fakeCapability) Get(form string) ([]byte, error) {
	f.gotForms = append(f.gotForms, form)
	if f.getErr != nil {
		return nil, f.getErr
	}
	return []byte(`{"schema":"eka-cko-v2","canonicalForm":` + mustQuote(form) + `}`), nil
}

func (f *fakeCapability) Domain(projectID, domain string) ([]byte, error) {
	if f.domainErr != nil {
		return nil, f.domainErr
	}
	return []byte(`{"schema":"eka-cko-v2","collection":"domain","domain":` + mustQuote(domain) + `,"count":0,"units":[]}`), nil
}

func (f *fakeCapability) Status() ([]byte, error) {
	return []byte(f.statusJSON), nil
}

func mustQuote(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

// mustHandle runs one message through the server and unmarshals the
// response envelope.
func mustHandle(t *testing.T, s *Server, msg string) map[string]any {
	t.Helper()
	resp := s.HandleMessage([]byte(msg))
	if len(resp) == 0 {
		t.Fatalf("expected a response for %s, got none", msg)
	}
	var out map[string]any
	if err := json.Unmarshal(resp, &out); err != nil {
		t.Fatalf("response is not valid JSON: %v\n%s", err, resp)
	}
	return out
}

func TestInitialize(t *testing.T) {
	s := NewServer(&fakeCapability{statusJSON: `{}`})
	out := mustHandle(t, s, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05"}}`)

	if out["jsonrpc"] != "2.0" {
		t.Errorf("jsonrpc = %v, want 2.0", out["jsonrpc"])
	}
	if out["id"] != float64(1) {
		t.Errorf("id = %v, want 1", out["id"])
	}
	res, ok := out["result"].(map[string]any)
	if !ok {
		t.Fatalf("result = %v, want an object", out["result"])
	}
	if res["protocolVersion"] != "2024-11-05" {
		t.Errorf("protocolVersion = %v, want 2024-11-05", res["protocolVersion"])
	}
	info := res["serverInfo"].(map[string]any)
	if info["name"] != "mcp" {
		t.Errorf("serverInfo.name = %v, want mcp", info["name"])
	}
	if info["version"] != "0.1.0" {
		t.Errorf("serverInfo.version = %v, want 0.1.0", info["version"])
	}
	if caps, ok := res["capabilities"].(map[string]any); !ok || caps["tools"] == nil || caps["resources"] == nil {
		t.Errorf("capabilities = %v, want tools+resources", res["capabilities"])
	}
}

// TestInitializeWithoutProtocolVersion: the handshake must succeed even
// when the client omits the protocol version (baseline fallback).
func TestInitializeWithoutProtocolVersion(t *testing.T) {
	s := NewServer(&fakeCapability{statusJSON: `{}`})
	out := mustHandle(t, s, `{"jsonrpc":"2.0","id":"a","method":"initialize"}`)
	res := out["result"].(map[string]any)
	if res["protocolVersion"] != ProtocolVersion {
		t.Errorf("protocolVersion = %v, want baseline %v", res["protocolVersion"], ProtocolVersion)
	}
}

func TestPing(t *testing.T) {
	s := NewServer(&fakeCapability{statusJSON: `{}`})
	out := mustHandle(t, s, `{"jsonrpc":"2.0","id":7,"method":"ping"}`)
	if out["id"] != float64(7) {
		t.Errorf("id = %v, want 7", out["id"])
	}
	if out["result"] == nil {
		t.Error("ping must carry a result (empty object)")
	}
}

func TestToolsList(t *testing.T) {
	s := NewServer(&fakeCapability{statusJSON: `{}`})
	out := mustHandle(t, s, `{"jsonrpc":"2.0","id":2,"method":"tools/list"}`)
	res := out["result"].(map[string]any)
	tools := res["tools"].([]any)
	want := []string{"get", "domain", "status"}
	got := make([]string, 0, len(tools))
	for _, tl := range tools {
		tm := tl.(map[string]any)
		got = append(got, tm["name"].(string))
		if tm["inputSchema"] == nil {
			t.Errorf("tool %v must carry an inputSchema", tm["name"])
		}
	}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("tools = %v, want %v", got, want)
	}
}

func TestToolsCallGet(t *testing.T) {
	cap := &fakeCapability{statusJSON: `{}`}
	s := NewServer(cap)
	out := mustHandle(t, s, `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"get","arguments":{"form":"feather/adr:001-serialization:1"}}}`)

	res := out["result"].(map[string]any)
	if res["isError"] != false {
		t.Errorf("isError = %v, want false", res["isError"])
	}
	content := res["content"].([]any)[0].(map[string]any)
	text := content["text"].(string)
	var doc map[string]any
	if err := json.Unmarshal([]byte(text), &doc); err != nil {
		t.Fatalf("tool get text must be JSON: %v\n%s", err, text)
	}
	if doc["canonicalForm"] != "feather/adr:001-serialization:1" {
		t.Errorf("canonicalForm = %v, want the requested form", doc["canonicalForm"])
	}
	if len(cap.gotForms) != 1 || cap.gotForms[0] != "feather/adr:001-serialization:1" {
		t.Errorf("capability.Get forms = %v", cap.gotForms)
	}
}

// TestToolsCallGetError: a capability failure is reported as an MCP
// tool result with isError=true (the message reaches the client).
func TestToolsCallGetError(t *testing.T) {
	cap := &fakeCapability{getErr: errors.New("eka: workspace not initialized")}
	s := NewServer(cap)
	out := mustHandle(t, s, `{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"get","arguments":{"form":"x/adr:y:1"}}}`)
	res := out["result"].(map[string]any)
	if res["isError"] != true {
		t.Errorf("isError = %v, want true", res["isError"])
	}
	text := res["content"].([]any)[0].(map[string]any)["text"].(string)
	if !strings.Contains(text, "workspace not initialized") {
		t.Errorf("error text = %q, want the capability message", text)
	}
	if out["error"] != nil {
		t.Errorf("tool errors must not be JSON-RPC errors, got %v", out["error"])
	}
}

func TestToolsCallUnknownTool(t *testing.T) {
	s := NewServer(&fakeCapability{statusJSON: `{}`})
	out := mustHandle(t, s, `{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"bogus"}}`)
	if out["result"] != nil {
		t.Errorf("unknown tool must not produce a result, got %v", out["result"])
	}
	errObj := out["error"].(map[string]any)
	if errObj["code"] != float64(codeToolNotFound) {
		t.Errorf("error code = %v, want %v", errObj["code"], codeToolNotFound)
	}
}

func TestToolsCallMissingArguments(t *testing.T) {
	s := NewServer(&fakeCapability{statusJSON: `{}`})
	out := mustHandle(t, s, `{"jsonrpc":"2.0","id":6,"method":"tools/call","params":{"name":"get"}}`)
	res := out["result"].(map[string]any)
	if res["isError"] != true {
		t.Errorf("missing arguments must be a tool error result, got %v", out)
	}
}

func TestResourcesList(t *testing.T) {
	s := NewServer(&fakeCapability{statusJSON: `{}`})
	out := mustHandle(t, s, `{"jsonrpc":"2.0","id":8,"method":"resources/list"}`)
	res := out["result"].(map[string]any)
	resources := res["resources"].([]any)
	if len(resources) != 1 {
		t.Fatalf("resources = %v, want exactly the status resource", resources)
	}
	r := resources[0].(map[string]any)
	if r["uri"] != "eka://status" {
		t.Errorf("resource uri = %v, want eka://status", r["uri"])
	}
}

func TestResourcesReadStatus(t *testing.T) {
	cap := &fakeCapability{statusJSON: `{"path":"/tmp/eka","initialized":true}`}
	s := NewServer(cap)
	out := mustHandle(t, s, `{"jsonrpc":"2.0","id":9,"method":"resources/read","params":{"uri":"eka://status"}}`)
	res := out["result"].(map[string]any)
	contents := res["contents"].([]any)[0].(map[string]any)
	if contents["uri"] != "eka://status" {
		t.Errorf("content uri = %v", contents["uri"])
	}
	if contents["mimeType"] != "application/json" {
		t.Errorf("mimeType = %v", contents["mimeType"])
	}
	text := contents["text"].(string)
	if !strings.Contains(text, `"initialized":true`) {
		t.Errorf("status text = %q, want the capability status", text)
	}
}

func TestResourcesReadUnknown(t *testing.T) {
	s := NewServer(&fakeCapability{statusJSON: `{}`})
	out := mustHandle(t, s, `{"jsonrpc":"2.0","id":10,"method":"resources/read","params":{"uri":"eka://bogus"}}`)
	if out["result"] != nil {
		t.Errorf("unknown resource must not produce a result, got %v", out["result"])
	}
	if out["error"] == nil {
		t.Error("unknown resource must produce an error")
	}
}

func TestMethodNotFound(t *testing.T) {
	s := NewServer(&fakeCapability{statusJSON: `{}`})
	out := mustHandle(t, s, `{"jsonrpc":"2.0","id":11,"method":"nope"}`)
	errObj := out["error"].(map[string]any)
	if errObj["code"] != float64(codeMethodNotFound) {
		t.Errorf("error code = %v, want %v", errObj["code"], codeMethodNotFound)
	}
}

func TestParseError(t *testing.T) {
	s := NewServer(&fakeCapability{statusJSON: `{}`})
	out := mustHandle(t, s, `{not json`)
	errObj := out["error"].(map[string]any)
	if errObj["code"] != float64(codeParseError) {
		t.Errorf("error code = %v, want %v", errObj["code"], codeParseError)
	}
}

func TestInvalidJSONRPCVersion(t *testing.T) {
	s := NewServer(&fakeCapability{statusJSON: `{}`})
	out := mustHandle(t, s, `{"jsonrpc":"1.0","id":1,"method":"ping"}`)
	if out["error"] == nil {
		t.Error("non-2.0 jsonrpc must error")
	}
}

// TestNotificationNoResponse: notifications (e.g.
// notifications/initialized) must not produce a response.
func TestNotificationNoResponse(t *testing.T) {
	s := NewServer(&fakeCapability{statusJSON: `{}`})
	resp := s.HandleMessage([]byte(`{"jsonrpc":"2.0","method":"notifications/initialized"}`))
	if len(resp) != 0 {
		t.Errorf("a notification must not produce a response, got %s", resp)
	}
}

// TestServeEndToEnd drives the full transport: a scripted client
// session over a byte stream, asserting the wire responses and the
// clean EOF termination.
func TestServeEndToEnd(t *testing.T) {
	s := NewServer(&fakeCapability{statusJSON: `{"initialized":false}`})
	input := strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize"}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`,
		`{"jsonrpc":"2.0","id":3,"method":"resources/read","params":{"uri":"eka://status"}}`,
		`{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"get","arguments":{"form":"f/adr:x:1"}}}`,
	}, "\n") + "\n"

	var out bytes.Buffer
	if err := s.Serve(strings.NewReader(input), &out); err != nil {
		t.Fatalf("Serve failed: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 4 {
		t.Fatalf("expected 4 response lines (notifications get none), got %d:\n%s", len(lines), out.String())
	}
	// First response is initialize with the server info.
	var initResp map[string]any
	if err := json.Unmarshal([]byte(lines[0]), &initResp); err != nil {
		t.Fatal(err)
	}
	if initResp["id"] != float64(1) {
		t.Errorf("first response id = %v, want 1", initResp["id"])
	}
	// Last response is the tool result with isError=false.
	var callResp map[string]any
	if err := json.Unmarshal([]byte(lines[3]), &callResp); err != nil {
		t.Fatal(err)
	}
	res := callResp["result"].(map[string]any)
	if res["isError"] != false {
		t.Errorf("final tool result isError = %v, want false", res["isError"])
	}
}

// TestServeFinalLineWithoutNewline: a stream that ends without a
// trailing newline still processes its last message.
func TestServeFinalLineWithoutNewline(t *testing.T) {
	s := NewServer(&fakeCapability{statusJSON: `{}`})
	input := `{"jsonrpc":"2.0","id":1,"method":"ping"}`
	var out bytes.Buffer
	if err := s.Serve(strings.NewReader(input), &out); err != nil {
		t.Fatalf("Serve failed: %v", err)
	}
	if !strings.Contains(out.String(), `"id":1`) {
		t.Errorf("final line without newline must be processed, got %q", out.String())
	}
}
