package mcp

// Hardening tests — the production-hardening contract of the server:
// deterministic batch rejection, the bounded read line (64 MiB cap with
// a deterministic refusal), and the error message policy (refusal-class
// messages, no internal paths, no store details, no stack traces).

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

// TestBatchRejectedDeterministically: a JSON-RPC batch array (multiple
// requests in one message) is refused with a fixed invalid-request
// error — the server never processes batches.
func TestBatchRejectedDeterministically(t *testing.T) {
	s := newTestServer(&fakeCapability{statusJSON: `{}`})
	cases := []string{
		`[{"jsonrpc":"2.0","id":1,"method":"ping"}]`,
		`[{"jsonrpc":"2.0","id":1,"method":"ping"},{"jsonrpc":"2.0","id":2,"method":"tools/list"}]`,
		`[]`,
		`[{"jsonrpc":"2.0","method":"notifications/initialized"}]`,
		` [ {"jsonrpc":"2.0","id":1,"method":"ping"} ] `,
	}
	for _, msg := range cases {
		out := mustHandle(t, s, msg)
		errObj := mustError(t, out)
		if errObj["code"] != float64(codeInvalidRequest) {
			t.Errorf("%s: error code = %v, want %v", msg, errObj["code"], codeInvalidRequest)
		}
		if errObj["message"] != "batch requests are not supported" {
			t.Errorf("%s: message = %v, want the fixed batch refusal", msg, errObj["message"])
		}
		if out["id"] != nil {
			t.Errorf("%s: batch refusal id = %v, want null", msg, out["id"])
		}
	}
}

// TestBatchNeverDispatched: a batch must never reach the capability
// layer — no tool executes, no resource reads.
func TestBatchNeverDispatched(t *testing.T) {
	cap := &fakeCapability{statusJSON: `{}`}
	s := newTestServer(cap)
	msg := `[{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"get","arguments":{"form":"x/adr:y:1"}}}]`
	out := mustHandle(t, s, msg)
	mustError(t, out)
	if len(cap.gotForms) != 0 {
		t.Errorf("batch must not dispatch tool calls, got forms %v", cap.gotForms)
	}
}

// TestNonObjectRejected: a valid JSON message that is not a request
// object (scalar, string, null, boolean) is refused deterministically.
func TestNonObjectRejected(t *testing.T) {
	s := newTestServer(&fakeCapability{statusJSON: `{}`})
	for _, msg := range []string{`null`, `true`, `false`, `0`, `-1`, `1.5`, `"ping"`} {
		out := mustHandle(t, s, msg)
		errObj := mustError(t, out)
		if errObj["code"] != float64(codeInvalidRequest) {
			t.Errorf("%s: error code = %v, want %v", msg, errObj["code"], codeInvalidRequest)
		}
	}
}

// TestParseErrorFixedMessage: a parse error carries the fixed message —
// never the Go JSON parser's internals.
func TestParseErrorFixedMessage(t *testing.T) {
	s := newTestServer(&fakeCapability{statusJSON: `{}`})
	for _, msg := range []string{`{not json`, `{"jsonrpc":"2.0"`, `[`, `{"a":}`, "\x00\x01\x02"} {
		out := mustHandle(t, s, msg)
		errObj := mustError(t, out)
		if errObj["code"] != float64(codeParseError) {
			t.Errorf("%q: error code = %v, want %v", msg, errObj["code"], codeParseError)
		}
		if errObj["message"] != "parse error: invalid JSON" {
			t.Errorf("%q: message = %v, want the fixed parse refusal", msg, errObj["message"])
		}
	}
}

// TestMalformedRequestObject: a valid JSON object with malformed fields
// (e.g. a non-string method) is an invalid request, not a parse error.
func TestMalformedRequestObject(t *testing.T) {
	s := newTestServer(&fakeCapability{statusJSON: `{}`})
	for _, msg := range []string{
		`{"jsonrpc":"2.0","id":1,"method":123}`,
		`{"jsonrpc":1,"id":1,"method":"ping"}`,
		`{"jsonrpc":"2.0","id":{},"method":"ping"}`,
	} {
		out := mustHandle(t, s, msg)
		errObj := mustError(t, out)
		if errObj["code"] != float64(codeInvalidRequest) {
			t.Errorf("%s: error code = %v, want %v", msg, errObj["code"], codeInvalidRequest)
		}
	}
}

// TestErrorResponseMissingIDIsNull: an invalid request without an id
// (wrong jsonrpc version, missing method, malformed fields) must still
// carry "id": null in the error response — JSON-RPC 2.0 §4.3: the id
// MUST be Null when the detection of the id failed.
func TestErrorResponseMissingIDIsNull(t *testing.T) {
	s := newTestServer(&fakeCapability{statusJSON: `{}`})
	for _, msg := range []string{
		`{"jsonrpc":"1.0","method":"ping"}`,
		`{"jsonrpc":"2.0"}`,
		`{"jsonrpc":"2.0","method":123}`,
	} {
		resp := s.HandleMessage([]byte(msg))
		if len(resp) == 0 {
			t.Fatalf("%s: expected an error response, got none", msg)
		}
		if !bytes.Contains(resp, []byte(`"id":null`)) {
			t.Errorf("%s: error response must carry \"id\":null, got %s", msg, resp)
		}
	}
}

// TestSanitizeError: capability errors are reduced to deterministic
// refusal-class messages — first line only, no paths, no store details.
func TestSanitizeError(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"eka: workspace not initialized", "eka: workspace not initialized"},
		{"runtime: search requires a project id", "runtime: search requires a project id"},
		{"unable to open database file: /home/user/.eka/workspace.db", "unable to open database file: <path>"},
		{"no such file or directory: ./data/store.db", "no such file or directory: <path>"},
		{"open C:\\Users\\eka\\.eka\\store.db: permission denied", "open <path>: permission denied"},
		// Paths with spaces in a parent directory must not leak fragments.
		{"unable to open database file: /home/john doe/.eka/store.db", "unable to open database file: <path>"},
		// Tilde paths must not leak fragments.
		{"no such file or directory: ~/.eka/store.db", "no such file or directory: <path>"},
		// Dot-less relative paths must not leak fragments.
		{"no such file or directory: data/store.db", "no such file or directory: <path>"},
		{"first line\nsecond line\nat goroutine 1 [running]:", "first line"},
		{"", "tool execution failed"},
	}
	for _, tc := range cases {
		got := SanitizeError(errors.New(tc.in))
		if got != tc.want {
			t.Errorf("SanitizeError(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// TestToolErrorNoInternalLeakage: a capability error carrying a path
// and a stack trace reaches the client sanitized — no path, no trace.
func TestToolErrorNoInternalLeakage(t *testing.T) {
	cap := &fakeCapability{
		statusJSON: `{}`,
		getErr:     errors.New("unable to open database file: /home/user/.eka/workspace.db\ngoroutine 1 [running]:\nmain.main()\n\t/home/user/go/src/main.go:42"),
	}
	s := newTestServer(cap)
	out := mustHandle(t, s, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"get","arguments":{"form":"x/adr:y:1"}}}`)
	res := mustResult(t, out)
	text := res["content"].([]any)[0].(map[string]any)["text"].(string)
	if strings.Contains(text, "/home/user") {
		t.Errorf("tool error leaks a path: %q", text)
	}
	if strings.Contains(text, "goroutine") || strings.Contains(text, "main.go") {
		t.Errorf("tool error leaks a stack trace: %q", text)
	}
	if !strings.Contains(text, "<path>") {
		t.Errorf("tool error must keep the sanitized refusal, got %q", text)
	}
}

// TestResourceReadErrorSanitized: a resource read failure is sanitized
// the same way (no internal paths).
func TestResourceReadErrorSanitized(t *testing.T) {
	// The fake capability never fails Status; a failing wrapper
	// exercises the resource-read error path.
	s := newTestServer(&failingStatusCapability{})
	out := mustHandle(t, s, `{"jsonrpc":"2.0","id":1,"method":"resources/read","params":{"uri":"eka://status"}}`)
	errObj := mustError(t, out)
	if errObj["code"] != float64(codeInternalError) {
		t.Errorf("error code = %v, want %v", errObj["code"], codeInternalError)
	}
	msg := errObj["message"].(string)
	if strings.Contains(msg, "/home/user") || strings.Contains(msg, "goroutine") {
		t.Errorf("resource read error leaks internals: %q", msg)
	}
}

// failingStatusCapability fails Status with a path-carrying error.
type failingStatusCapability struct{}

func (f *failingStatusCapability) Get(form string, noContent bool) ([]byte, error) {
	return nil, errors.New("unreachable")
}

func (f *failingStatusCapability) Domain(projectID, domain string, noContent bool) ([]byte, error) {
	return nil, errors.New("unreachable")
}

func (f *failingStatusCapability) Status() ([]byte, error) {
	return nil, errors.New("unable to open database file: /home/user/.eka/workspace.db")
}

func (f *failingStatusCapability) Context(subject, projectID, depth string) ([]byte, error) {
	return nil, errors.New("unreachable")
}
func (f *failingStatusCapability) CodeContext(req CodeContextRequest) ([]byte, error) {
	return nil, errors.New("unreachable")
}
func (f *failingStatusCapability) CodeDiscover(req CodeDiscoverRequest) ([]byte, error) {
	return nil, errors.New("unreachable")
}
func (f *failingStatusCapability) CodeGet(req CodeGetRequest) ([]byte, error) {
	return nil, errors.New("unreachable")
}

func (f *failingStatusCapability) Validate(root string) ([]byte, error) {
	return nil, errors.New("unreachable")
}

func (f *failingStatusCapability) NewDraft(req NewDraftRequest) ([]byte, error) {
	return nil, errors.New("unreachable")
}

func (f *failingStatusCapability) DraftUpdate(req DraftUpdateRequest) ([]byte, error) {
	return nil, errors.New("unreachable")
}

func (f *failingStatusCapability) Publish(req PublishRequest) ([]byte, error) {
	return nil, errors.New("unreachable")
}

func (f *failingStatusCapability) PublishBatch(req PublishBatchRequest) ([]byte, error) {
	return nil, errors.New("unreachable")
}

func (f *failingStatusCapability) Transition(req TransitionRequest) ([]byte, error) {
	return nil, errors.New("unreachable")
}

func (f *failingStatusCapability) Note(req NoteRequest) ([]byte, error) {
	return nil, errors.New("unreachable")
}

func (f *failingStatusCapability) DraftRead(target, project string) ([]byte, error) {
	return nil, errors.New("unreachable")
}

func (f *failingStatusCapability) View(target, project string) ([]byte, error) {
	return nil, errors.New("unreachable")
}

func (f *failingStatusCapability) DraftList(project string) ([]byte, error) {
	return nil, errors.New("unreachable")
}

func (f *failingStatusCapability) IntegrityCheck() ([]byte, error) {
	return nil, errors.New("unreachable")
}

func (f *failingStatusCapability) Discard(target, project string) ([]byte, error) {
	return nil, errors.New("unreachable")
}

func (f *failingStatusCapability) SyncPush(repoPath string, adopt, override bool) ([]byte, error) {
	return nil, errors.New("unreachable")
}

func (f *failingStatusCapability) Assign(req AssignmentRequest) ([]byte, error) {
	return nil, errors.New("unreachable")
}

func (f *failingStatusCapability) Reassign(req AssignmentRequest) ([]byte, error) {
	return nil, errors.New("unreachable")
}

func (f *failingStatusCapability) Unassign(req UnassignRequest) ([]byte, error) {
	return nil, errors.New("unreachable")
}

func (f *failingStatusCapability) FeedbackNew(req FeedbackNewRequest) ([]byte, error) {
	return nil, errors.New("unreachable")
}

func (f *failingStatusCapability) FeedbackList() ([]byte, error) {
	return nil, errors.New("unreachable")
}

func (f *failingStatusCapability) FeedbackPublish(req FeedbackPublishRequest) ([]byte, error) {
	return nil, errors.New("unreachable")
}

// TestServeLineTooLong: a line exceeding the 64 MiB cap is refused
// deterministically and the session continues at the next line.
func TestServeLineTooLong(t *testing.T) {
	s := newTestServer(&fakeCapability{statusJSON: `{}`})
	oversized := strings.Repeat("a", defaultMaxLineSize+1)
	input := oversized + "\n" + `{"jsonrpc":"2.0","id":1,"method":"ping"}` + "\n"

	var out bytes.Buffer
	if err := s.Serve(strings.NewReader(input), &out); err != nil {
		t.Fatalf("Serve failed: %v", err)
	}

	lines := strings.Split(strings.TrimSuffix(out.String(), "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 response lines (refusal + ping), got %d", len(lines))
	}
	var refusal map[string]any
	if err := json.Unmarshal([]byte(lines[0]), &refusal); err != nil {
		t.Fatal(err)
	}
	errObj := mustError(t, refusal)
	if errObj["code"] != float64(codeInvalidRequest) {
		t.Errorf("refusal code = %v, want %v", errObj["code"], codeInvalidRequest)
	}
	if errObj["message"] != "message exceeds the 64 MiB line limit" {
		t.Errorf("refusal message = %v, want the fixed line-limit refusal", errObj["message"])
	}
	// The session continues: the ping after the oversized line answers.
	var ping map[string]any
	if err := json.Unmarshal([]byte(lines[1]), &ping); err != nil {
		t.Fatal(err)
	}
	if ping["id"] != float64(1) || ping["error"] != nil {
		t.Errorf("session must continue after the refusal, got %v", ping)
	}
}

// TestServeLineAtCap: a line of exactly the cap is accepted (the cap is
// inclusive) — it parses as JSON and answers. Runs at a small cap; the
// real 64 MiB cap is exercised by TestServeLineTooLong.
func TestServeLineAtCap(t *testing.T) {
	s := newTestServer(&fakeCapability{statusJSON: `{}`})
	s.maxLineSize = 1024
	// A valid request padded with whitespace to exactly the cap.
	base := `{"jsonrpc":"2.0","id":1,"method":"ping"}`
	pad := s.maxLineSize - len(base)
	if pad < 0 {
		t.Fatal("base message larger than the cap")
	}
	input := base + strings.Repeat(" ", pad) + "\n"
	var out bytes.Buffer
	if err := s.Serve(strings.NewReader(input), &out); err != nil {
		t.Fatalf("Serve failed: %v", err)
	}
	if !strings.Contains(out.String(), `"id":1`) {
		t.Errorf("a line at the cap must be processed, got %q", out.String())
	}
}

// TestServeOversizedLineAtEOF: an oversized final line (no trailing
// newline) is still refused deterministically and Serve terminates.
func TestServeOversizedLineAtEOF(t *testing.T) {
	s := newTestServer(&fakeCapability{statusJSON: `{}`})
	s.maxLineSize = 1024
	input := strings.Repeat("a", s.maxLineSize+1)
	var out bytes.Buffer
	if err := s.Serve(strings.NewReader(input), &out); err != nil {
		t.Fatalf("Serve failed: %v", err)
	}
	if !strings.Contains(out.String(), "64 MiB line limit") {
		t.Errorf("oversized final line must be refused, got %q", out.String())
	}
}

// TestServeResyncAfterOversizedLine: after an oversized line the stream
// is resynchronized at the next newline — a following valid message is
// processed.
func TestServeResyncAfterOversizedLine(t *testing.T) {
	s := newTestServer(&fakeCapability{statusJSON: `{}`})
	s.maxLineSize = 1024
	// Oversized line + newline + valid message: the drain consumes up to
	// the newline, so the following message is processed normally.
	input := strings.Repeat("a", s.maxLineSize+1) + "\n" + `{"jsonrpc":"2.0","id":2,"method":"tools/list"}` + "\n"
	var out bytes.Buffer
	if err := s.Serve(strings.NewReader(input), &out); err != nil {
		t.Fatalf("Serve failed: %v", err)
	}
	if !strings.Contains(out.String(), `"tools"`) {
		t.Errorf("stream must resync after the oversized line, got %q", out.String())
	}
}
