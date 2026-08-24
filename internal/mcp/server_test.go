package mcp

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/maleolabs/eka-mcp"
)

// fakeCapability is a deterministic stand-in for the EKA capability
// layer, so the server tests are pure protocol tests — no eka-core
// involved. It records calls so tests can assert the dispatch.
type fakeCapability struct {
	statusJSON string
	getErr     error
	domainErr  error
	gotForms   []string
	gotNew     []NewDraftRequest
	gotNote    []NoteRequest
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

func (f *fakeCapability) Context(subject, projectID, depth string) ([]byte, error) {
	return []byte(`{"schema":"eka-context-v1","kind":"context","depth":` + mustQuote(depth) + `,"focus":` + mustQuote(subject) + `}`), nil
}

func (f *fakeCapability) Validate(root string) ([]byte, error) {
	return []byte(`{"schema":"eka-conformance-report-v1","root":` + mustQuote(root) + `,"filesScanned":0,"artifacts":0,"skipped":"","errors":0,"warnings":0,"pass":true,"results":[]}`), nil
}

func (f *fakeCapability) NewDraft(req NewDraftRequest) ([]byte, error) {
	f.gotNew = append(f.gotNew, req)
	return []byte(`{"schema":"eka-draft-v1","project":` + mustQuote(req.Project) + `,"namespace":` + mustQuote(req.Namespace) + `,"type":` + mustQuote(req.Type) + `,"id":` + mustQuote(req.ID) + `,"path":"/tmp/drafts/` + req.Type + `-` + req.ID + `.json","updated":"2026-08-21T00:00:00Z"}`), nil
}

func (f *fakeCapability) Publish(req PublishRequest) ([]byte, error) {
	return []byte(`{"schema":"eka-publish-result-v1","form":` + mustQuote(req.Target+":1") + `,"instanceVersion":1,"objectHash":"abc","note":""}`), nil
}

func (f *fakeCapability) Transition(req TransitionRequest) ([]byte, error) {
	return []byte(`{"schema":"eka-transition-result-v1","target":` + mustQuote(req.Target) + `,"from":"planned","to":"todo","by":{"kind":"agent","name":"mcp-agent"},"objectHash":"abc","lockedPlan":"","lockedPlanHash":"","warning":""}`), nil
}

func (f *fakeCapability) Note(req NoteRequest) ([]byte, error) {
	f.gotNote = append(f.gotNote, req)
	return []byte(`{"schema":"eka-note-result-v1","id":"x-implementation","target":` + mustQuote(req.Target) + `,"subjectState":"","path":"/tmp/drafts/cmt-x-implementation.json","by":{"kind":"agent","name":"mcp-agent"}}`), nil
}

func (f *fakeCapability) DraftRead(target, project string) ([]byte, error) {
	return []byte(`{"namespace":"feather","type":"adr","id":"001","revision":1,"content":{}}`), nil
}

func (f *fakeCapability) View(target, project string) ([]byte, error) {
	return f.DraftRead(target, project)
}

func (f *fakeCapability) DraftList(project string) ([]byte, error) {
	return []byte(`{"schema":"eka-draft-list-v1","count":0,"drafts":[]}`), nil
}

func (f *fakeCapability) IntegrityCheck() ([]byte, error) {
	return []byte(`{"schema":"eka-integrity-report-v1","payloadsChecked":0,"refsChecked":0,"attachmentsChecked":0,"orphanPayloads":0,"violations":[]}`), nil
}

func (f *fakeCapability) Discard(target, project string) ([]byte, error) {
	return []byte(`{"schema":"eka-discard-result-v1","target":` + mustQuote(target) + `,"note":""}`), nil
}

func (f *fakeCapability) SyncPush(repoPath string, adopt, override bool) ([]byte, error) {
	if repoPath == "" {
		repoPath = "."
	}
	return []byte(`{"schema":"eka-sync-push-result-v1","workspace":"` + "/tmp/eka" + `","project":"p","repo":"r","pushedUnits":1,"pushedAttachments":0,"snapshotLabel":"repo:p","snapshotDigest":"abc123","changed":false,"newRepo":false,"warnings":[]}`), nil
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
	if info["version"] != pack.Version {
		t.Errorf("serverInfo.version = %v, want %v", info["version"], pack.Version)
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
	want := []string{"context", "get", "domain", "status", "validate", "new", "publish", "transition", "note", "draft_read", "view", "draft_list", "integrity_check", "discard", "sync_push"}
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
	// Deprecated alias `view` must be flagged with migration note to `draft_read`.
	for _, tl := range tools {
		tm := tl.(map[string]any)
		if tm["name"] == "view" {
			desc, _ := tm["description"].(string)
			if !strings.Contains(strings.ToLower(desc), "deprecated") || !strings.Contains(desc, "draft_read") {
				t.Errorf("tool view description = %q, want deprecated flag with migration note to draft_read", desc)
			}
		}
		if tm["name"] == "draft_read" {
			desc, _ := tm["description"].(string)
			if strings.Contains(strings.ToLower(desc), "deprecated") {
				t.Errorf("tool draft_read must not be marked deprecated, got %q", desc)
			}
		}
	}
}

func TestToolsCallDraftReadAndViewAlias(t *testing.T) {
	cap := &fakeCapability{statusJSON: `{}`}
	s := NewServer(cap)
	for _, name := range []string{"draft_read", "view"} {
		out := mustHandle(t, s, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"`+name+`","arguments":{"target":"feather/adr:001","project":"feather"}}}`)
		res, ok := out["result"].(map[string]any)
		if !ok {
			t.Fatalf("%s: expected result, got %v", name, out)
		}
		if res["isError"] != false {
			t.Errorf("%s: isError = %v, want false", name, res["isError"])
		}
		content := res["content"].([]any)[0].(map[string]any)
		text := content["text"].(string)
		var doc map[string]any
		if err := json.Unmarshal([]byte(text), &doc); err != nil {
			t.Fatalf("%s: tool text must be JSON: %v", name, err)
		}
		if doc["type"] != "adr" {
			t.Errorf("%s: type = %v, want adr", name, doc["type"])
		}
	}
	// Both names are deterministic and return identical verbatim content.
	a := mustHandle(t, s, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"draft_read","arguments":{"target":"feather/adr:001"}}}`)
	b := mustHandle(t, s, `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"view","arguments":{"target":"feather/adr:001"}}}`)
	aText := a["result"].(map[string]any)["content"].([]any)[0].(map[string]any)["text"].(string)
	bText := b["result"].(map[string]any)["content"].([]any)[0].(map[string]any)["text"].(string)
	if aText != bText {
		t.Errorf("draft_read and view alias must return identical verbatim content, got %q vs %q", aText, bText)
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

func TestToolsCallSyncPush(t *testing.T) {
	s := NewServer(&fakeCapability{statusJSON: `{}`})
	// No args (defaults to ".")
	out := mustHandle(t, s, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"sync_push","arguments":{}}}`)
	res := out["result"].(map[string]any)
	if res["isError"] != false {
		t.Errorf("sync_push isError = %v, want false", res["isError"])
	}
	text := res["content"].([]any)[0].(map[string]any)["text"].(string)
	var doc map[string]any
	if err := json.Unmarshal([]byte(text), &doc); err != nil {
		t.Fatalf("sync_push text must be JSON: %v", err)
	}
	if doc["schema"] != "eka-sync-push-result-v1" {
		t.Errorf("schema = %v, want eka-sync-push-result-v1", doc["schema"])
	}
	if doc["snapshotDigest"] == nil {
		t.Error("sync_push result must carry snapshotDigest")
	}
	// With explicit repoPath, adopt, override
	out = mustHandle(t, s, `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"sync_push","arguments":{"repoPath":".","adopt":false,"override":false}}}`)
	if mustHandle(t, s, `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"sync_push","arguments":{"repoPath":"."}}}`) == nil {
		t.Error("sync_push with repoPath must succeed")
	}
	// Deterministic: two calls same state -> same bytes
	a := mustHandle(t, s, `{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"sync_push","arguments":{}}}`)
	b := mustHandle(t, s, `{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"sync_push","arguments":{}}}`)
	aText := a["result"].(map[string]any)["content"].([]any)[0].(map[string]any)["text"].(string)
	bText := b["result"].(map[string]any)["content"].([]any)[0].(map[string]any)["text"].(string)
	if aText != bText {
		t.Errorf("sync_push must be byte-deterministic, got %q vs %q", aText, bText)
	}
}

func TestToolsCallSyncPushPullRefused(t *testing.T) {
	s := NewServer(&fakeCapability{statusJSON: `{}`})
	for _, arg := range []string{
		`{"fromDocs":true}`, `{"from_docs":true}`, `{"from-docs":true}`, `{"pull":true}`,
	} {
		out := mustHandle(t, s, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"sync_push","arguments":`+arg+`}}`)
		res := out["result"].(map[string]any)
		if res["isError"] != true {
			t.Errorf("sync_push %s must refuse with isError=true, got %v", arg, res)
		}
		text := res["content"].([]any)[0].(map[string]any)["text"].(string)
		if !strings.Contains(text, "eka sync pull") {
			t.Errorf("pull refusal text = %q, want eka sync pull hint", text)
		}
	}
}

func TestToolsCallSyncPushUnknownTool(t *testing.T) {
	s := NewServer(&fakeCapability{statusJSON: `{}`})
	out := mustHandle(t, s, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"sync_pull","arguments":{}}}`)
	if out["error"] == nil {
		t.Fatalf("sync_pull unknown tool must error, got %v", out)
	}
	errObj := out["error"].(map[string]any)
	if errObj["code"] != float64(codeToolNotFound) {
		t.Errorf("code = %v, want %v", errObj["code"], codeToolNotFound)
	}
}

func TestResourcesList(t *testing.T) {
	s := NewServer(&fakeCapability{statusJSON: `{}`})
	out := mustHandle(t, s, `{"jsonrpc":"2.0","id":8,"method":"resources/list"}`)
	res := out["result"].(map[string]any)
	resources := res["resources"].([]any)
	// The deterministic resource set: eka://status + every embedded
	// skill + every embedded draft template type.
	wantCount := 1 + len(mustSkillDirs(t)) + len(mustTemplateTypes(t))
	if len(resources) != wantCount {
		t.Fatalf("resources = %d, want %d", len(resources), wantCount)
	}
	r := resources[0].(map[string]any)
	if r["uri"] != "eka://status" {
		t.Errorf("resource uri = %v, want eka://status", r["uri"])
	}
	// Every resource must carry a uri and a mimeType.
	for _, rl := range resources {
		rm := rl.(map[string]any)
		if rm["uri"] == nil || rm["mimeType"] == nil {
			t.Errorf("resource %v must carry uri and mimeType", rm)
		}
	}
}

func mustSkillDirs(t *testing.T) []string {
	t.Helper()
	dirs, err := pack.SkillDirs()
	if err != nil {
		t.Fatalf("SkillDirs: %v", err)
	}
	return dirs
}

func mustTemplateTypes(t *testing.T) []string {
	t.Helper()
	types, err := pack.TemplateTypes()
	if err != nil {
		t.Fatalf("TemplateTypes: %v", err)
	}
	return types
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
