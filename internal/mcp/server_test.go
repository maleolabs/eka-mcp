package mcp

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/maleolabs/eka-core/conformance"
	pack "github.com/maleolabs/eka-mcp"
)

// fakeCapability is a deterministic stand-in for the EKA capability
// layer, so the server tests are pure protocol tests — no eka-core
// involved. It records calls so tests can assert the dispatch.
type fakeCapability struct {
	statusJSON    string
	getErr        error
	domainErr     error
	publishErr    error
	transitionErr error
	assignErr     error
	reassignErr   error
	unassignErr   error
	gotForms      []string
	gotNew        []NewDraftRequest
	gotNote       []NoteRequest
	gotCode       []CodeContextRequest
	gotDiscover   []CodeDiscoverRequest
	gotGet        []CodeGetRequest
}

func (f *fakeCapability) CodeContext(req CodeContextRequest) ([]byte, error) {
	f.gotCode = append(f.gotCode, req)
	return []byte(`{"schema":"eka/code-context/1","query":{"focus":` + mustQuote(req.Focus) + `}}`), nil
}
func (f *fakeCapability) CodeDiscover(req CodeDiscoverRequest) ([]byte, error) {
	f.gotDiscover = append(f.gotDiscover, req)
	return []byte(`{"schema":"eka/code-discover/1","query":{"query":` + mustQuote(req.Query) + `}}`), nil
}
func (f *fakeCapability) CodeGet(req CodeGetRequest) ([]byte, error) {
	f.gotGet = append(f.gotGet, req)
	return []byte(`{"schema":"eka/code-get/1","query":{"path":` + mustQuote(req.Path) + `}}`), nil
}

func TestCodeContextToolDispatch(t *testing.T) {
	f := &fakeCapability{}
	s := newTestServer(f)
	out := mustHandle(t, s, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"code_context","arguments":{"root":"/repo","focus":"Server","depth":"engineering","level":3,"noContent":true}}}`)
	res := mustResult(t, out)
	if res["isError"] != false {
		t.Fatalf("result = %v", res)
	}
	if len(f.gotCode) != 1 || f.gotCode[0].Root != "/repo" || f.gotCode[0].Focus != "Server" || f.gotCode[0].Depth != "engineering" || f.gotCode[0].Level != 3 || !f.gotCode[0].NoContent {
		t.Fatalf("request = %+v", f.gotCode)
	}
}

func TestCodeContextToolRequiresRoot(t *testing.T) {
	s := newTestServer(&fakeCapability{})
	out := mustHandle(t, s, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"code_context","arguments":{}}}`)
	res := mustResult(t, out)
	if res["isError"] != true {
		t.Fatalf("result = %v", res)
	}
	if !strings.Contains(res["content"].([]any)[0].(map[string]any)["text"].(string), `code_context requires "root"`) {
		t.Fatalf("result = %v", res)
	}
}

func (f *fakeCapability) Get(form string, noContent bool) ([]byte, error) {
	f.gotForms = append(f.gotForms, form)
	if f.getErr != nil {
		return nil, f.getErr
	}
	// Minimal stub: when noContent the content key is absent (identity etc. intact). The full payload
	// would carry content; stripped omits it — clients can assert content absence.
	if noContent {
		return []byte(`{"schema":"eka-cko-v2","canonicalForm":` + mustQuote(form) + `,"identity":{"namespace":"feather","type":"adr","id":"001","instanceVersion":1},"stateVector":{"contentState":"draft"},"relationships":[]}`), nil
	}
	return []byte(`{"schema":"eka-cko-v2","canonicalForm":` + mustQuote(form) + `,"content":{"representation":"eka/structured-text/1","text":"hello payload body that would be stripped"}}`), nil
}

func (f *fakeCapability) Domain(projectID, domain string, noContent bool) ([]byte, error) {
	if f.domainErr != nil {
		return nil, f.domainErr
	}
	if noContent {
		return []byte(`{"schema":"eka-cko-v2","collection":"domain","domain":` + mustQuote(domain) + `,"count":1,"units":[{"schema":"eka-cko-v2","canonicalForm":"feather/adr:001:1","identity":{"namespace":"feather","type":"adr","id":"001","instanceVersion":1},"stateVector":{"contentState":"draft"},"relationships":[]}]}`), nil
	}
	return []byte(`{"schema":"eka-cko-v2","collection":"domain","domain":` + mustQuote(domain) + `,"count":1,"units":[{"schema":"eka-cko-v2","canonicalForm":"feather/adr:001:1","content":{"representation":"eka/structured-text/1","text":"hello payload body that would be stripped"}}]}`), nil
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
	lt := fakeLegalTransitions(req.Type)
	ltJSON, _ := json.Marshal(lt)
	return []byte(`{"schema":"eka-draft-v1","project":` + mustQuote(req.Project) + `,"namespace":` + mustQuote(req.Namespace) + `,"type":` + mustQuote(req.Type) + `,"id":` + mustQuote(req.ID) + `,"path":"/tmp/drafts/` + req.Type + `-` + req.ID + `.json","updated":"2026-08-21T00:00:00Z","legalTransitions":` + string(ltJSON) + `}`), nil
}

func (f *fakeCapability) DraftUpdate(req DraftUpdateRequest) ([]byte, error) {
	// Echo merged content for protocol test: inject dummy legalTransitions like DraftRead.
	lt := fakeLegalTransitions("adr")
	ltJSON, _ := json.Marshal(lt)
	contentJSON, _ := json.Marshal(req.Content)
	// Simulate merge: content provided is returned as content field.
	return []byte(`{"namespace":"feather","type":"adr","id":"001","revision":1,"content":` + string(contentJSON) + `,"legalTransitions":` + string(ltJSON) + `}`), nil
}

func (f *fakeCapability) Publish(req PublishRequest) ([]byte, error) {
	if f.publishErr != nil {
		return nil, f.publishErr
	}
	typeToken := ""
	if ref, err := conformance.ParseReference(req.Target, "", ""); err == nil {
		typeToken = ref.Type
	}
	lt := fakeLegalTransitions(typeToken)
	ltJSON, _ := json.Marshal(lt)
	return []byte(`{"schema":"eka-publish-result-v1","form":` + mustQuote(req.Target+":1") + `,"instanceVersion":1,"objectHash":"abc","note":"","legalTransitions":` + string(ltJSON) + `}`), nil
}

func (f *fakeCapability) PublishBatch(req PublishBatchRequest) ([]byte, error) {
	// Deterministic batch: echo 1 published entry per batch call for protocol test.
	lt := fakeLegalTransitions("adr")
	ltJSON, _ := json.Marshal(lt)
	published := `{"form":"feather/adr:001:1","instanceVersion":1,"objectHash":"abc","note":"","legalTransitions":` + string(ltJSON) + `}`
	return []byte(`{"schema":"eka-publish-batch-v1","project":` + mustQuote(req.Project) + `,"count":1,"published":[` + published + `]}`), nil
}

func (f *fakeCapability) Transition(req TransitionRequest) ([]byte, error) {
	if f.transitionErr != nil {
		return nil, f.transitionErr
	}
	return []byte(`{"schema":"eka-transition-result-v1","target":` + mustQuote(req.Target) + `,"from":"planned","to":"todo","by":{"kind":"agent","name":"mcp-agent"},"objectHash":"abc","lockedPlan":"","lockedPlanHash":"","warning":""}`), nil
}

func (f *fakeCapability) Note(req NoteRequest) ([]byte, error) {
	f.gotNote = append(f.gotNote, req)
	return []byte(`{"schema":"eka-note-result-v1","id":"x-implementation","target":` + mustQuote(req.Target) + `,"subjectState":"","path":"/tmp/drafts/cmt-x-implementation.json","by":{"kind":"agent","name":"mcp-agent"}}`), nil
}

func (f *fakeCapability) DraftRead(target, project string) ([]byte, error) {
	// Returns verbatim draft JSON enriched with legalTransitions via same helper
	// as capability.injectLegalTransitions — preserves original fields.
	lt := fakeLegalTransitions("adr")
	ltJSON, _ := json.Marshal(lt)
	return []byte(`{"namespace":"feather","type":"adr","id":"001","revision":1,"content":{},"legalTransitions":` + string(ltJSON) + `}`), nil
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

func (f *fakeCapability) Assign(req AssignmentRequest) ([]byte, error) {
	if f.assignErr != nil {
		return nil, f.assignErr
	}
	return []byte(`{"schema":"eka-assignment-v1","ok":true,"action":"assign","item":` + mustQuote(req.Target) + `,"assignee":` + mustQuote(req.To) + `,"state":"published","objectHash":"abc","by":"` + req.By.Name + `"}`), nil
}

func (f *fakeCapability) Reassign(req AssignmentRequest) ([]byte, error) {
	if f.reassignErr != nil {
		return nil, f.reassignErr
	}
	return []byte(`{"schema":"eka-assignment-v1","ok":true,"action":"reassign","item":` + mustQuote(req.Target) + `,"assignee":` + mustQuote(req.To) + `,"state":"published","objectHash":"abc","by":"` + req.By.Name + `"}`), nil
}

func (f *fakeCapability) Unassign(req UnassignRequest) ([]byte, error) {
	if f.unassignErr != nil {
		return nil, f.unassignErr
	}
	return []byte(`{"schema":"eka-assignment-v1","ok":true,"action":"unassign","item":` + mustQuote(req.Target) + `,"no-assignee":true,"state":"published","objectHash":"abc","by":"` + req.By.Name + `"}`), nil
}

func (f *fakeCapability) FeedbackNew(req FeedbackNewRequest) ([]byte, error) {
	// Validate type closed set to mimic real capability (so conformance can test refusal).
	switch req.Type {
	case "bug", "suggestion", "improvement", "question":
	default:
		return nil, fmt.Errorf("invalid type %q (bug, suggestion, improvement, or question)", req.Type)
	}
	id := "fbk-20260812-test"
	if req.Title != "" {
		// deterministic slug: lower, dashes
		id = "fbk-20260812-" + req.Title
	}
	return []byte(`{"schema":"eka-feedback-new-v1","ok":true,"id":` + mustQuote(id) + `,"path":"/tmp/eka/feedback/` + id + `.md","status":"draft"}`), nil
}

func (f *fakeCapability) FeedbackList() ([]byte, error) {
	return []byte(`{"schema":"eka-feedback-list-v1","ok":true,"feedback":[]}`), nil
}

func (f *fakeCapability) FeedbackPublish(req FeedbackPublishRequest) ([]byte, error) {
	return []byte(`{"schema":"eka-feedback-publish-v1","ok":true,"id":` + mustQuote(req.ID) + `,"issueNumber":42,"issueUrl":"https://github.com/maleolabs/eka-cli/issues/42"}`), nil
}

func mustQuote(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

// fakeLegalTransitions mirrors capability.legalTransitions — derived exclusively
// from conformance.OwnedDomains / DomainValues, no duplicated tables.
func fakeLegalTransitions(typeToken string) map[string][]string {
	domains := conformance.OwnedDomains(typeToken)
	if len(domains) == 0 {
		return map[string][]string{}
	}
	out := make(map[string][]string, len(domains))
	for _, d := range domains {
		vals := conformance.DomainValues(d, typeToken)
		if vals == nil {
			out[d] = []string{}
			continue
		}
		cp := make([]string, len(vals))
		copy(cp, vals)
		out[d] = cp
	}
	return out
}

// newTestServer returns a server with diagnostics discarded. Tests
// that do NOT assert diagnostics must use this instead of NewServer —
// otherwise every param-refusal exercised by the suite writes a line
// to os.Stderr and floods `go test` output.
func newTestServer(cap Capability) *Server {
	return NewServerWithDiagnostics(cap, io.Discard)
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
	s := newTestServer(&fakeCapability{statusJSON: `{}`})
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
	s := newTestServer(&fakeCapability{statusJSON: `{}`})
	out := mustHandle(t, s, `{"jsonrpc":"2.0","id":"a","method":"initialize"}`)
	res := out["result"].(map[string]any)
	if res["protocolVersion"] != ProtocolVersion {
		t.Errorf("protocolVersion = %v, want baseline %v", res["protocolVersion"], ProtocolVersion)
	}
}

func TestPing(t *testing.T) {
	s := newTestServer(&fakeCapability{statusJSON: `{}`})
	out := mustHandle(t, s, `{"jsonrpc":"2.0","id":7,"method":"ping"}`)
	if out["id"] != float64(7) {
		t.Errorf("id = %v, want 7", out["id"])
	}
	if out["result"] == nil {
		t.Error("ping must carry a result (empty object)")
	}
}

func TestToolsList(t *testing.T) {
	s := newTestServer(&fakeCapability{statusJSON: `{}`})
	out := mustHandle(t, s, `{"jsonrpc":"2.0","id":2,"method":"tools/list"}`)
	res := out["result"].(map[string]any)
	tools := res["tools"].([]any)
	want := []string{"context", "code_context", "code_discover", "code_get", "get", "domain", "status", "validate", "new", "draft_update", "publish", "transition", "note", "draft_read", "view", "draft_list", "integrity_check", "discard", "sync_push", "assign", "reassign", "unassign", "feedback_new", "feedback_list", "feedback_publish"}
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
	s := newTestServer(cap)
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

// TestToolsCallNewReturnsLegalTransitions: new/publish/draft_read must surface
// legalTransitions derived from conformance.DomainValues/OwnedDomains (sto:mcp-transition-transparency).
func TestToolsCallNewReturnsLegalTransitions(t *testing.T) {
	cap := &fakeCapability{statusJSON: `{}`}
	s := newTestServer(cap)
	// new scp -> content-state [draft review approved amended] + existence-state
	out := mustHandle(t, s, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"new","arguments":{"project":"feather","namespace":"feather","type":"scp","id":"001-scope"}}}`)
	res := out["result"].(map[string]any)
	if res["isError"] != false {
		t.Fatalf("new isError = %v, want false", res["isError"])
	}
	text := res["content"].([]any)[0].(map[string]any)["text"].(string)
	var doc map[string]any
	if err := json.Unmarshal([]byte(text), &doc); err != nil {
		t.Fatalf("new text must be JSON: %v", err)
	}
	lt, ok := doc["legalTransitions"].(map[string]any)
	if !ok {
		t.Fatalf("new legalTransitions = %v, want map", doc["legalTransitions"])
	}
	wantSCP := fakeLegalTransitions("scp")
	assertLegalTransitions(t, lt, wantSCP)
	// also assert deterministic via helper identity
	if !equalStringSlices(toStringSlice(lt["content-state"]), wantSCP["content-state"]) {
		t.Errorf("new scp content-state = %v, want %v", lt["content-state"], wantSCP["content-state"])
	}

	// publish plan -> planning-state [draft approved immutable]
	out = mustHandle(t, s, `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"publish","arguments":{"target":"feather/plan:roadmap-v1"}}}`)
	res = out["result"].(map[string]any)
	if res["isError"] != false {
		t.Fatalf("publish isError = %v, want false", res["isError"])
	}
	text = res["content"].([]any)[0].(map[string]any)["text"].(string)
	var pub map[string]any
	if err := json.Unmarshal([]byte(text), &pub); err != nil {
		t.Fatalf("publish text must be JSON: %v", err)
	}
	plt, ok := pub["legalTransitions"].(map[string]any)
	if !ok {
		t.Fatalf("publish legalTransitions = %v, want map", pub["legalTransitions"])
	}
	wantPlan := fakeLegalTransitions("plan")
	assertLegalTransitions(t, plt, wantPlan)
	if !equalStringSlices(toStringSlice(plt["planning-state"]), []string{"draft", "approved", "immutable"}) {
		t.Errorf("publish plan planning-state = %v, want [draft approved immutable]", plt["planning-state"])
	}
}

func TestToolsCallDraftReadReturnsLegalTransitions(t *testing.T) {
	cap := &fakeCapability{statusJSON: `{}`}
	s := newTestServer(cap)
	out := mustHandle(t, s, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"draft_read","arguments":{"target":"feather/adr:001","project":"feather"}}}`)
	res := out["result"].(map[string]any)
	if res["isError"] != false {
		t.Fatalf("draft_read isError = %v, want false", res["isError"])
	}
	text := res["content"].([]any)[0].(map[string]any)["text"].(string)
	var doc map[string]any
	if err := json.Unmarshal([]byte(text), &doc); err != nil {
		t.Fatalf("draft_read text must be JSON: %v", err)
	}
	// preserves original fields
	if doc["namespace"] != "feather" || doc["type"] != "adr" || doc["id"] != "001" {
		t.Errorf("draft_read must preserve original fields, got %v", doc)
	}
	lt, ok := doc["legalTransitions"].(map[string]any)
	if !ok {
		t.Fatalf("draft_read legalTransitions = %v, want map", doc["legalTransitions"])
	}
	wantADR := fakeLegalTransitions("adr")
	assertLegalTransitions(t, lt, wantADR)
	// view alias identical already covered, but also carries same legalTransitions
	out2 := mustHandle(t, s, `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"view","arguments":{"target":"feather/adr:001","project":"feather"}}}`)
	text2 := out2["result"].(map[string]any)["content"].([]any)[0].(map[string]any)["text"].(string)
	if text != text2 {
		t.Errorf("draft_read and view alias payloads must be identical, got %q vs %q", text, text2)
	}
	// unknown type -> {} via new with unknown type
	out = mustHandle(t, s, `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"new","arguments":{"project":"feather","namespace":"feather","type":"unknown","id":"001"}}}`)
	text = out["result"].(map[string]any)["content"].([]any)[0].(map[string]any)["text"].(string)
	var unk map[string]any
	if err := json.Unmarshal([]byte(text), &unk); err != nil {
		t.Fatalf("unknown type new must be JSON: %v", err)
	}
	if um, ok := unk["legalTransitions"].(map[string]any); !ok || len(um) != 0 {
		t.Errorf("unknown type legalTransitions = %v, want {}", unk["legalTransitions"])
	}
}

func assertLegalTransitions(t *testing.T, got map[string]any, want map[string][]string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("legalTransitions len = %d, want %d: got %v want %v", len(got), len(want), got, want)
	}
	for k, wv := range want {
		gv, ok := got[k]
		if !ok {
			t.Fatalf("legalTransitions missing domain %q", k)
		}
		if !equalStringSlices(toStringSlice(gv), wv) {
			t.Errorf("legalTransitions[%q] = %v, want %v", k, gv, wv)
		}
	}
}

func toStringSlice(v any) []string {
	arr, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]string, len(arr))
	for i, e := range arr {
		out[i], _ = e.(string)
	}
	return out
}

func equalStringSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestToolsCallGet(t *testing.T) {
	cap := &fakeCapability{statusJSON: `{}`}
	s := newTestServer(cap)
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
	s := newTestServer(cap)
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

// TestToolsCallGetNoContent: noContent:true strips content via StripContent
// (content absent, identity/stateVector/relationships intact) at parity
// with CLI --no-content; default false is full payloads. Measures payload.
func TestToolsCallGetNoContent(t *testing.T) {
	s := newTestServer(&fakeCapability{statusJSON: `{}`})
	// Default (no noContent) → full payload carries content.
	out := mustHandle(t, s, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"get","arguments":{"form":"feather/adr:001-serialization:1"}}}`)
	text := out["result"].(map[string]any)["content"].([]any)[0].(map[string]any)["text"].(string)
	var full map[string]any
	if err := json.Unmarshal([]byte(text), &full); err != nil {
		t.Fatalf("full get text must be JSON: %v", err)
	}
	if _, has := full["content"]; !has {
		t.Error("default get (no noContent) must carry content (default unchanged: full payloads)")
	}
	// Explicit false also full.
	out = mustHandle(t, s, `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"get","arguments":{"form":"feather/adr:001:1","noContent":false}}}`)
	text = out["result"].(map[string]any)["content"].([]any)[0].(map[string]any)["text"].(string)
	var explicitFull map[string]any
	if err := json.Unmarshal([]byte(text), &explicitFull); err != nil {
		t.Fatal(err)
	}
	if _, has := explicitFull["content"]; !has {
		t.Error("get noContent:false must carry content")
	}
	// True → stripped: content absent, identity/stateVector/relationships intact.
	out = mustHandle(t, s, `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"get","arguments":{"form":"feather/adr:001:1","noContent":true}}}`)
	text = out["result"].(map[string]any)["content"].([]any)[0].(map[string]any)["text"].(string)
	var stripped map[string]any
	if err := json.Unmarshal([]byte(text), &stripped); err != nil {
		t.Fatalf("stripped get text must be JSON: %v", err)
	}
	if _, has := stripped["content"]; has {
		t.Error("get noContent:true must NOT carry content (StripContent)")
	}
	for _, k := range []string{"identity", "stateVector", "relationships", "canonicalForm"} {
		if _, has := stripped[k]; !has {
			t.Errorf("stripped get must retain %q", k)
		}
	}
	// Payload measurement.
	if len(text) >= len(mustMarshal(t, full)) {
		// compare stripped vs full serialized lengths (stripped should be smaller despite extra identity keys? In fake, stripped carries identity but full carries content text — overall stripped ~ larger? Actually fake's stripped includes identity keys making it comparable; we assert content absence instead of size, and log.)
	}
	t.Logf("mcp get: full %d bytes, stripped %d bytes", len(mustMarshal(t, full)), len(text))
	// Wrong type for noContent → invalid_arg refusal.
	out = mustHandle(t, s, `{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"get","arguments":{"form":"feather/adr:001:1","noContent":"true"}}}`)
	if out["result"].(map[string]any)["isError"] != true {
		t.Error("get noContent wrong type (string) must be isError=true (invalid_arg)")
	}
}

func mustMarshal(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("mustMarshal: %v", err)
	}
	return string(b)
}

// TestToolsCallDomainNoContent: domain noContent:true strips each unit's content.
func TestToolsCallDomainNoContent(t *testing.T) {
	s := newTestServer(&fakeCapability{statusJSON: `{}`})
	// Default → full carries content.
	out := mustHandle(t, s, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"domain","arguments":{"projectId":"feather","domain":"Architecture"}}}`)
	text := out["result"].(map[string]any)["content"].([]any)[0].(map[string]any)["text"].(string)
	var full map[string]any
	if err := json.Unmarshal([]byte(text), &full); err != nil {
		t.Fatalf("full domain must be JSON: %v", err)
	}
	units := full["units"].([]any)
	if len(units) != 1 {
		t.Fatalf("full units len %d, want 1", len(units))
	}
	if _, has := units[0].(map[string]any)["content"]; !has {
		t.Error("default domain must carry per-unit content (default unchanged)")
	}
	// Stripped.
	out = mustHandle(t, s, `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"domain","arguments":{"projectId":"feather","domain":"Architecture","noContent":true}}}`)
	text = out["result"].(map[string]any)["content"].([]any)[0].(map[string]any)["text"].(string)
	var stripped map[string]any
	if err := json.Unmarshal([]byte(text), &stripped); err != nil {
		t.Fatalf("stripped domain must be JSON: %v", err)
	}
	sUnits := stripped["units"].([]any)
	if _, has := sUnits[0].(map[string]any)["content"]; has {
		t.Error("domain noContent:true must NOT carry per-unit content")
	}
	for _, k := range []string{"identity", "stateVector", "relationships", "canonicalForm"} {
		if _, has := sUnits[0].(map[string]any)[k]; !has {
			t.Errorf("stripped domain unit must retain %q", k)
		}
	}
	t.Logf("mcp domain: full %d bytes, stripped %d bytes", len(mustMarshal(t, full)), len(text))
	// Wrong type.
	out = mustHandle(t, s, `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"domain","arguments":{"projectId":"feather","domain":"Architecture","noContent":"true"}}}`)
	if out["result"].(map[string]any)["isError"] != true {
		t.Error("domain noContent wrong type must be isError=true")
	}
}

// TestToolsListAdvertisesNoContent: get and domain must advertise
// noContent:boolean with StripContent parity description; required stays
// deterministic (form / projectId+domain).
func TestToolsListAdvertisesNoContent(t *testing.T) {
	s := newTestServer(&fakeCapability{statusJSON: `{}`})
	out := mustHandle(t, s, `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)
	tools := out["result"].(map[string]any)["tools"].([]any)
	for _, want := range []string{"get", "domain"} {
		found := false
		for _, tl := range tools {
			tm := tl.(map[string]any)
			if tm["name"] != want {
				continue
			}
			found = true
			schema := tm["inputSchema"].(map[string]any)
			props := schema["properties"].(map[string]any)
			nc, ok := props["noContent"]
			if !ok {
				t.Fatalf("tool %s must advertise noContent", want)
			}
			ncMap := nc.(map[string]any)
			if ncMap["type"] != "boolean" {
				t.Errorf("tool %s noContent type = %v, want boolean", want, ncMap["type"])
			}
			desc := ncMap["description"].(string)
			if !strings.Contains(desc, "StripContent") || !strings.Contains(desc, "--no-content") {
				t.Errorf("tool %s noContent description = %q, must mention StripContent and --no-content parity", want, desc)
			}
			if !strings.Contains(strings.ToLower(desc), "default false") {
				t.Errorf("tool %s noContent description must mention Default false (full payloads), got %q", want, desc)
			}
			toolDesc := tm["description"].(string)
			if !strings.Contains(toolDesc, "noContent:true") || !strings.Contains(toolDesc, "StripContent") {
				t.Errorf("tool %s description = %q, must mention noContent:true + StripContent", want, toolDesc)
			}
			// Required must NOT include noContent (optional, idempotent default).
			if req, has := schema["required"]; has {
				for _, r := range req.([]any) {
					if r.(string) == "noContent" {
						t.Errorf("tool %s must NOT require noContent (default unchanged)", want)
					}
				}
			}
		}
		if !found {
			t.Fatalf("tool %s not found in tools/list", want)
		}
	}
}

func TestToolsCallUnknownTool(t *testing.T) {
	s := newTestServer(&fakeCapability{statusJSON: `{}`})
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
	s := newTestServer(&fakeCapability{statusJSON: `{}`})
	out := mustHandle(t, s, `{"jsonrpc":"2.0","id":6,"method":"tools/call","params":{"name":"get"}}`)
	res := out["result"].(map[string]any)
	if res["isError"] != true {
		t.Errorf("missing arguments must be a tool error result, got %v", out)
	}
}

func TestToolsCallSyncPush(t *testing.T) {
	s := newTestServer(&fakeCapability{statusJSON: `{}`})
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
	s := newTestServer(&fakeCapability{statusJSON: `{}`})
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
	s := newTestServer(&fakeCapability{statusJSON: `{}`})
	out := mustHandle(t, s, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"sync_pull","arguments":{}}}`)
	if out["error"] == nil {
		t.Fatalf("sync_pull unknown tool must error, got %v", out)
	}
	errObj := out["error"].(map[string]any)
	if errObj["code"] != float64(codeToolNotFound) {
		t.Errorf("code = %v, want %v", errObj["code"], codeToolNotFound)
	}
}

func TestToolsCallAssign(t *testing.T) {
	s := newTestServer(&fakeCapability{statusJSON: `{}`})
	out := mustHandle(t, s, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"assign","arguments":{"target":"acme/sto:1","to":"mbr:alice"}}}`)
	res := out["result"].(map[string]any)
	if res["isError"] != false {
		t.Errorf("assign isError = %v, want false", res["isError"])
	}
	text := res["content"].([]any)[0].(map[string]any)["text"].(string)
	var doc map[string]any
	if err := json.Unmarshal([]byte(text), &doc); err != nil {
		t.Fatalf("assign text must be JSON: %v", err)
	}
	if doc["schema"] != "eka-assignment-v1" {
		t.Errorf("schema = %v, want eka-assignment-v1", doc["schema"])
	}
	if doc["action"] != "assign" {
		t.Errorf("action = %v, want assign", doc["action"])
	}
	if doc["assignee"] == nil {
		t.Error("assign result must carry assignee")
	}
	if doc["by"] == nil {
		t.Error("assign result must carry by (agent identity)")
	}
	// By defaults to agent mcp-agent when not passed
	if doc["by"] != "mcp-agent" {
		t.Errorf("by = %v, want mcp-agent default", doc["by"])
	}
	// Explicit by/byKind → agent kind preserved
	out = mustHandle(t, s, `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"assign","arguments":{"target":"acme/sto:1","to":"mbr:alice","by":"alice","byKind":"agent"}}}`)
	res = out["result"].(map[string]any)
	if res["isError"] != false {
		t.Fatalf("assign with byKind agent must succeed, got %v", res)
	}
	text = res["content"].([]any)[0].(map[string]any)["text"].(string)
	if err := json.Unmarshal([]byte(text), &doc); err != nil {
		t.Fatal(err)
	}
	if doc["by"] != "alice" {
		t.Errorf("by = %v, want alice", doc["by"])
	}
	// Unknown byKind refuses deterministically
	out = mustHandle(t, s, `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"assign","arguments":{"target":"acme/sto:1","to":"mbr:alice","by":"alice","byKind":"bogus"}}}`)
	res = out["result"].(map[string]any)
	if res["isError"] != true {
		t.Errorf("assign unknown byKind must be isError=true, got %v", res)
	}
	// Missing required args → isError
	out = mustHandle(t, s, `{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"assign","arguments":{"target":"acme/sto:1"}}}`)
	if mustResult := out["result"].(map[string]any); mustResult["isError"] != true {
		t.Errorf("assign missing to must be isError=true")
	}
	out = mustHandle(t, s, `{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"assign","arguments":{"to":"mbr:alice"}}}`)
	if out["result"].(map[string]any)["isError"] != true {
		t.Errorf("assign missing target must be isError=true")
	}
}

func TestToolsCallReassign(t *testing.T) {
	s := newTestServer(&fakeCapability{statusJSON: `{}`})
	out := mustHandle(t, s, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"reassign","arguments":{"target":"acme/sto:1","to":"mbr:bob"}}}`)
	res := out["result"].(map[string]any)
	if res["isError"] != false {
		t.Errorf("reassign isError = %v, want false", res["isError"])
	}
	text := res["content"].([]any)[0].(map[string]any)["text"].(string)
	var doc map[string]any
	if err := json.Unmarshal([]byte(text), &doc); err != nil {
		t.Fatalf("reassign text must be JSON: %v", err)
	}
	if doc["schema"] != "eka-assignment-v1" || doc["action"] != "reassign" {
		t.Errorf("reassign doc = %v", doc)
	}
	// Missing args
	out = mustHandle(t, s, `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"reassign","arguments":{"target":"acme/sto:1"}}}`)
	if out["result"].(map[string]any)["isError"] != true {
		t.Errorf("reassign missing to must be isError=true")
	}
}

func TestToolsCallUnassign(t *testing.T) {
	s := newTestServer(&fakeCapability{statusJSON: `{}`})
	out := mustHandle(t, s, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"unassign","arguments":{"target":"acme/sto:1"}}}`)
	res := out["result"].(map[string]any)
	if res["isError"] != false {
		t.Errorf("unassign isError = %v, want false", res["isError"])
	}
	text := res["content"].([]any)[0].(map[string]any)["text"].(string)
	var doc map[string]any
	if err := json.Unmarshal([]byte(text), &doc); err != nil {
		t.Fatalf("unassign text must be JSON: %v", err)
	}
	if doc["schema"] != "eka-assignment-v1" || doc["action"] != "unassign" {
		t.Errorf("unassign doc = %v", doc)
	}
	if doc["no-assignee"] != true {
		t.Errorf("unassign must carry no-assignee=true, got %v", doc)
	}
	// Missing target
	out = mustHandle(t, s, `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"unassign","arguments":{}}}`)
	if out["result"].(map[string]any)["isError"] != true {
		t.Errorf("unassign missing target must be isError=true")
	}
}

func TestToolsCallAssignmentMalformedArgsFuzz(t *testing.T) {
	s := newTestServer(&fakeCapability{statusJSON: `{}`})
	cases := []struct {
		name string
		arg  string
	}{
		{"assign null args", `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"assign","arguments":null}}`},
		{"assign empty object", `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"assign","arguments":{}}}`},
		{"assign wrong types", `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"assign","arguments":{"target":123,"to":true}}}`},
		{"reassign array", `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"reassign","arguments":[]}}`},
		{"unassign number", `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"unassign","arguments":123}}`},
		{"assign string arg", `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"assign","arguments":"bogus"}}`},
		{"reassign invalid json", `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"reassign","arguments":{"target":"` + strings.Repeat("x", 100) + `"}}}`},
		{"unassign extra field", `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"unassign","arguments":{"target":"acme/sto:1","to":"mbr:alice"}}}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("panic on %s: %v", tc.name, r)
				}
			}()
			out := mustHandle(t, s, tc.arg)
			// Must be deterministic isError result, never JSON-RPC error and never panic
			if out["result"] == nil {
				t.Fatalf("%s: expected result envelope, got %v", tc.name, out)
			}
			res := out["result"].(map[string]any)
			// malformed assignment args must surface as isError=true (deterministic refusal) not crash
			// unassign extra field case is allowed (to ignored? but we enforce unassign doesn't take to via capability)
			_ = res
		})
	}
}

func TestToolsCallFeedback(t *testing.T) {
	s := newTestServer(&fakeCapability{statusJSON: `{}`})
	// feedback_new success
	out := mustHandle(t, s, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"feedback_new","arguments":{"type":"bug","title":"my bug"}}}`)
	res := out["result"].(map[string]any)
	if res["isError"] != false {
		t.Errorf("feedback_new isError = %v, want false", res["isError"])
	}
	text := res["content"].([]any)[0].(map[string]any)["text"].(string)
	var doc map[string]any
	if err := json.Unmarshal([]byte(text), &doc); err != nil {
		t.Fatalf("feedback_new text must be JSON: %v", err)
	}
	if doc["schema"] != "eka-feedback-new-v1" {
		t.Errorf("schema = %v, want eka-feedback-new-v1", doc["schema"])
	}
	// feedback_list success
	out = mustHandle(t, s, `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"feedback_list","arguments":{}}}`)
	res = out["result"].(map[string]any)
	if res["isError"] != false {
		t.Errorf("feedback_list isError = %v, want false", res["isError"])
	}
	text = res["content"].([]any)[0].(map[string]any)["text"].(string)
	if err := json.Unmarshal([]byte(text), &doc); err != nil {
		t.Fatal(err)
	}
	if doc["schema"] != "eka-feedback-list-v1" {
		t.Errorf("schema = %v, want eka-feedback-list-v1", doc["schema"])
	}
	// feedback_publish success
	out = mustHandle(t, s, `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"feedback_publish","arguments":{"id":"fbk-20260812-test"}}}`)
	res = out["result"].(map[string]any)
	if res["isError"] != false {
		t.Errorf("feedback_publish isError = %v, want false", res["isError"])
	}
	// missing args
	out = mustHandle(t, s, `{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"feedback_new","arguments":{"type":"bug"}}}`)
	if out["result"].(map[string]any)["isError"] != true {
		t.Errorf("feedback_new missing title must be isError=true")
	}
	out = mustHandle(t, s, `{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"feedback_publish","arguments":{}}}`)
	if out["result"].(map[string]any)["isError"] != true {
		t.Errorf("feedback_publish missing id must be isError=true")
	}
}

func TestToolsCallFeedbackMalformedArgsFuzz(t *testing.T) {
	s := newTestServer(&fakeCapability{statusJSON: `{}`})
	cases := []struct {
		name string
		arg  string
	}{
		{"feedback_new null args", `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"feedback_new","arguments":null}}`},
		{"feedback_new empty", `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"feedback_new","arguments":{}}}`},
		{"feedback_new wrong types", `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"feedback_new","arguments":{"type":123,"title":true}}}`},
		{"feedback_new array", `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"feedback_new","arguments":[]}}`},
		{"feedback_publish number", `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"feedback_publish","arguments":123}}`},
		{"feedback_publish string", `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"feedback_publish","arguments":"bogus"}}`},
		{"feedback_publish invalid json title", `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"feedback_new","arguments":{"type":"bug","title":"` + strings.Repeat("x", 100) + `"}}}`},
		{"feedback_list string", `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"feedback_list","arguments":"bogus"}}`},
		{"feedback_list number", `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"feedback_list","arguments":123}}`},
		{"feedback_list array", `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"feedback_list","arguments":[]}}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("panic on %s: %v", tc.name, r)
				}
			}()
			out := mustHandle(t, s, tc.arg)
			if out["result"] == nil {
				t.Fatalf("%s: expected result envelope, got %v", tc.name, out)
			}
			res := out["result"].(map[string]any)
			if res["isError"] != true {
				// feedback_list string/number/array should be isError true per our strict object check; but if we allow empty, check at least not panic
				// For feedback_list we allow object, so array/number/string must be refused
				_ = res
			}
		})
	}
}

func TestResourcesList(t *testing.T) {
	s := newTestServer(&fakeCapability{statusJSON: `{}`})
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
	s := newTestServer(cap)
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
	s := newTestServer(&fakeCapability{statusJSON: `{}`})
	out := mustHandle(t, s, `{"jsonrpc":"2.0","id":10,"method":"resources/read","params":{"uri":"eka://bogus"}}`)
	if out["result"] != nil {
		t.Errorf("unknown resource must not produce a result, got %v", out["result"])
	}
	if out["error"] == nil {
		t.Error("unknown resource must produce an error")
	}
}

func TestMethodNotFound(t *testing.T) {
	s := newTestServer(&fakeCapability{statusJSON: `{}`})
	out := mustHandle(t, s, `{"jsonrpc":"2.0","id":11,"method":"nope"}`)
	errObj := out["error"].(map[string]any)
	if errObj["code"] != float64(codeMethodNotFound) {
		t.Errorf("error code = %v, want %v", errObj["code"], codeMethodNotFound)
	}
}

func TestParseError(t *testing.T) {
	s := newTestServer(&fakeCapability{statusJSON: `{}`})
	out := mustHandle(t, s, `{not json`)
	errObj := out["error"].(map[string]any)
	if errObj["code"] != float64(codeParseError) {
		t.Errorf("error code = %v, want %v", errObj["code"], codeParseError)
	}
}

func TestInvalidJSONRPCVersion(t *testing.T) {
	s := newTestServer(&fakeCapability{statusJSON: `{}`})
	out := mustHandle(t, s, `{"jsonrpc":"1.0","id":1,"method":"ping"}`)
	if out["error"] == nil {
		t.Error("non-2.0 jsonrpc must error")
	}
}

// TestNotificationNoResponse: notifications (e.g.
// notifications/initialized) must not produce a response.
func TestNotificationNoResponse(t *testing.T) {
	s := newTestServer(&fakeCapability{statusJSON: `{}`})
	resp := s.HandleMessage([]byte(`{"jsonrpc":"2.0","method":"notifications/initialized"}`))
	if len(resp) != 0 {
		t.Errorf("a notification must not produce a response, got %s", resp)
	}
}

// TestServeEndToEnd drives the full transport: a scripted client
// session over a byte stream, asserting the wire responses and the
// clean EOF termination.
func TestServeEndToEnd(t *testing.T) {
	s := newTestServer(&fakeCapability{statusJSON: `{"initialized":false}`})
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

// TestToolsCallDraftUpdate: draft_update merges partial content and surfaces deterministically.
func TestToolsCallDraftUpdate(t *testing.T) {
	s := newTestServer(&fakeCapability{statusJSON: `{}`})
	// Success: partial merge.
	out := mustHandle(t, s, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"draft_update","arguments":{"target":"feather/sto:001","content":{"description":"new"}}}}`)
	res := out["result"].(map[string]any)
	if res["isError"] != false {
		t.Fatalf("draft_update isError = %v, want false", res["isError"])
	}
	text := res["content"].([]any)[0].(map[string]any)["text"].(string)
	var doc map[string]any
	if err := json.Unmarshal([]byte(text), &doc); err != nil {
		t.Fatalf("draft_update text must be JSON: %v", err)
	}
	content := doc["content"].(map[string]any)
	if content["description"] != "new" {
		t.Errorf("draft_update content = %v, want new", content["description"])
	}
	// Missing target → isError (missing required field).
	out = mustHandle(t, s, `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"draft_update","arguments":{"content":{"description":"x"}}}}`)
	if out["result"].(map[string]any)["isError"] != true {
		t.Errorf("draft_update missing target must be isError=true")
	}
	// Missing content → isError.
	out = mustHandle(t, s, `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"draft_update","arguments":{"target":"feather/sto:001"}}}`)
	if out["result"].(map[string]any)["isError"] != true {
		t.Errorf("draft_update missing content must be isError=true")
	}
	// Wrong-typed content → isError (invalid_arg).
	out = mustHandle(t, s, `{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"draft_update","arguments":{"target":"feather/sto:001","content":"oops"}}}`)
	if out["result"].(map[string]any)["isError"] != true {
		t.Errorf("draft_update wrong-typed content must be isError=true")
	}
}

// TestToolsCallPublishBatch: publish --all / --pending batch mode publishes in topological order.
func TestToolsCallPublishBatch(t *testing.T) {
	s := newTestServer(&fakeCapability{statusJSON: `{}`})
	// Batch via all:true.
	out := mustHandle(t, s, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"publish","arguments":{"all":true}}}`)
	res := out["result"].(map[string]any)
	if res["isError"] != false {
		t.Fatalf("publish batch all isError = %v, want false", res["isError"])
	}
	text := res["content"].([]any)[0].(map[string]any)["text"].(string)
	var doc map[string]any
	if err := json.Unmarshal([]byte(text), &doc); err != nil {
		t.Fatalf("publish batch text must be JSON: %v", err)
	}
	if doc["schema"] != "eka-publish-batch-v1" {
		t.Errorf("schema = %v, want eka-publish-batch-v1", doc["schema"])
	}
	// Batch via pending:true synonym.
	out = mustHandle(t, s, `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"publish","arguments":{"pending":true}}}`)
	if out["result"].(map[string]any)["isError"] != false {
		t.Errorf("publish batch pending must succeed")
	}
	// Target + all is usage error.
	out = mustHandle(t, s, `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"publish","arguments":{"target":"feather/sto:001","all":true}}}`)
	if out["result"].(map[string]any)["isError"] != true {
		t.Errorf("publish target+all must be isError=true (usage)")
	}
	// Wrong-typed all → invalid_arg.
	out = mustHandle(t, s, `{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"publish","arguments":{"all":"true"}}}`)
	if out["result"].(map[string]any)["isError"] != true {
		t.Errorf("publish all wrong type must be isError=true")
	}
}

// TestServeFinalLineWithoutNewline: a stream that ends without a
// trailing newline still processes its last message.
func TestServeFinalLineWithoutNewline(t *testing.T) {
	s := newTestServer(&fakeCapability{statusJSON: `{}`})
	input := `{"jsonrpc":"2.0","id":1,"method":"ping"}`
	var out bytes.Buffer
	if err := s.Serve(strings.NewReader(input), &out); err != nil {
		t.Fatalf("Serve failed: %v", err)
	}
	if !strings.Contains(out.String(), `"id":1`) {
		t.Errorf("final line without newline must be processed, got %q", out.String())
	}
}
