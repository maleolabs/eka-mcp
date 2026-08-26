package mcp

// Parameter-refusal regression tests (bug:mcp-new-false-refusal).
//
// The old single refusal "<tool> requires {...}" conflated three distinct
// causes — malformed/truncated JSON arguments, a missing required field,
// and a present-but-empty or wrong-typed field — which made the
// intermittent eka_new refusals ("byte-identical retry succeeds")
// undiagnosable. These tests pin the discriminating behavior:
//
//  1. every refusal cause has its own message (parse failure surfaces
//     the sanitized decoder error; missing/empty/wrong-typed name the
//     field);
//  2. the advertised inputSchema "required" arrays are DERIVED from the
//     same toolRequiredFields declaration the runtime enforces — schema
//     == enforcement, asserted both ways;
//  3. every argument-shaped refusal logs one stderr diagnostics line
//     carrying the argument byte length (the truncation-vs-omission
//     discriminator);
//  4. a large (~4 KB) unicode-heavy content payload is accepted —
//     encoding/json does not reject unicode, so a false refusal on such
//     payloads must never originate at this boundary;
//  5. optional-only tools still tolerate absent arguments.

import (
	"bytes"
	"encoding/json"
	"strconv"
	"strings"
	"testing"
)

// diagServer returns a server whose diagnostics go to an in-memory
// buffer (never os.Stderr during tests).
func diagServer() (*Server, *bytes.Buffer) {
	buf := &bytes.Buffer{}
	return NewServerWithDiagnostics(&fakeCapability{statusJSON: `{}`}, buf), buf
}

// callNew runs one tools/call new through the server and returns the
// result envelope plus the tool text.
func callNew(t *testing.T, s *Server, args string) (map[string]any, string) {
	t.Helper()
	out := mustHandle(t, s, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"new","arguments":`+args+`}}`)
	res, ok := out["result"].(map[string]any)
	if !ok {
		t.Fatalf("expected a result envelope, got %v", out)
	}
	text := ""
	if content, ok := res["content"].([]any); ok && len(content) > 0 {
		text = content[0].(map[string]any)["text"].(string)
	}
	return res, text
}

// TestNewRefusalDiscriminatesCauses pins one distinct message per cause:
// truncated JSON surfaces the sanitized decoder error; a missing field
// names WHICH field is absent; an empty or wrong-typed field names the
// field. The old conflated schema-shaped message must not reappear.
func TestNewRefusalDiscriminatesCauses(t *testing.T) {
	cases := []struct {
		name    string
		args    string
		wantIn  []string // substrings the refusal MUST carry
		wantNot []string // substrings the refusal MUST NOT carry
	}{
		{
			name: "arguments not an object (double-encoded string)",
			args: `"bogus"`,
			wantIn: []string{
				"new requires a JSON object argument",
				"got string",
			},
			wantNot: []string{`"namespace"`},
		},
		{
			name:    "missing namespace",
			args:    `{"project":"feather","type":"adr","id":"001"}`,
			wantIn:  []string{`"namespace"`, "missing required field"},
			wantNot: []string{`"project": missing`},
		},
		{
			name:    "missing id only",
			args:    `{"project":"feather","namespace":"feather","type":"adr"}`,
			wantIn:  []string{`"id"`, "missing required field"},
			wantNot: []string{},
		},
		{
			name:    "empty project",
			args:    `{"project":"","namespace":"feather","type":"adr","id":"001"}`,
			wantIn:  []string{`"project"`, "non-empty string"},
			wantNot: []string{"missing required field"},
		},
		{
			name:    "whitespace-only id counts as empty",
			args:    `{"project":"feather","namespace":"feather","type":"adr","id":"   "}`,
			wantIn:  []string{`"id"`, "non-empty string"},
			wantNot: []string{"missing required field"},
		},
		{
			name:    "wrong-typed id",
			args:    `{"project":"feather","namespace":"feather","type":"adr","id":42}`,
			wantIn:  []string{`"id"`, "non-empty string", "got number"},
			wantNot: []string{},
		},
		{
			name:    "absent arguments",
			args:    ``,
			wantIn:  []string{`"project"`, "missing required field"},
			wantNot: []string{},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s, _ := diagServer()
			msg := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"new"`
			if tc.args != "" {
				msg += `,"arguments":` + tc.args
			}
			msg += `}}`
			out := mustHandle(t, s, msg)
			res := out["result"].(map[string]any)
			if res["isError"] != true {
				t.Fatalf("isError = %v, want true", res["isError"])
			}
			text := res["content"].([]any)[0].(map[string]any)["text"].(string)
			for _, want := range tc.wantIn {
				if !strings.Contains(text, want) {
					t.Errorf("refusal %q does not contain %q", text, want)
				}
			}
			for _, forbid := range tc.wantNot {
				if strings.Contains(text, forbid) {
					t.Errorf("refusal %q must not contain %q", text, forbid)
				}
			}
		})
	}
}

// TestNewRefusalDeterministic: the same malformed call twice yields the
// byte-identical refusal — the server side is deterministic, so any
// intermittent behavior lives upstream (client/transport).
func TestNewRefusalDeterministic(t *testing.T) {
	s, _ := diagServer()
	a, _ := callNew(t, s, `{"project":"feather","type":"adr","id":"001"}`)
	b, _ := callNew(t, s, `{"project":"feather","type":"adr","id":"001"}`)
	textA := a["content"].([]any)[0].(map[string]any)["text"].(string)
	textB := b["content"].([]any)[0].(map[string]any)["text"].(string)
	if textA != textB {
		t.Errorf("identical calls must produce identical refusals, got %q vs %q", textA, textB)
	}
}

// TestNewAcceptsLargeUnicodeContent: a ~4KB unicode-heavy content
// payload passes the boundary untouched — encoding/json does not reject
// unicode, so a refusal on such payloads must never originate here.
func TestNewAcceptsLargeUnicodeContent(t *testing.T) {
	s, capFake := func() (*Server, *fakeCapability) {
		fc := &fakeCapability{statusJSON: `{}`}
		return NewServerWithDiagnostics(fc, &bytes.Buffer{}), fc
	}()
	body := strings.Repeat("日本語テキスト 🚀 ünïcodé — émoji 👨‍👩‍👧 family ✅ ", 60)
	content := map[string]any{"summary": "résumé 中文", "body": body}
	raw, err := json.Marshal(map[string]any{
		"project": "feather", "namespace": "feather", "type": "adr", "id": "001-unicode",
		"content": content,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) < 4096 {
		t.Fatalf("payload is only %d bytes, want >= 4096 for the large-payload regression", len(raw))
	}
	res, text := callNew(t, s, string(raw))
	if res["isError"] != false {
		t.Fatalf("large unicode payload must be accepted, refused with: %s", text)
	}
	var doc map[string]any
	if err := json.Unmarshal([]byte(text), &doc); err != nil {
		t.Fatalf("tool text must be JSON: %v", err)
	}
	if doc["schema"] != "eka-draft-v1" {
		t.Errorf("schema = %v, want eka-draft-v1", doc["schema"])
	}
	if len(capFake.gotNew) != 1 {
		t.Fatalf("capability received %d drafts, want 1", len(capFake.gotNew))
	}
	gotBody, _ := capFake.gotNew[0].Content["body"].(string)
	if gotBody != body {
		t.Error("unicode-heavy content did not round-trip byte-exact through the boundary")
	}
}

// TestRequiredFieldContractSchemaMatchesEnforcement asserts BOTH
// directions of the single-source contract (bug:mcp-new-false-refusal):
//
//   - advertisement: every tool's inputSchema "required" array equals
//     its toolRequiredFields entry verbatim (order included), and tools
//     without an entry advertise no "required" key;
//   - enforcement: calling each tool without the required fields is
//     refused naming the FIRST declared field, and calling it with all
//     declared fields present passes the parameter layer.
func TestRequiredFieldContractSchemaMatchesEnforcement(t *testing.T) {
	s, _ := diagServer()

	// Advertisement side.
	out := mustHandle(t, s, `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)
	tools := out["result"].(map[string]any)["tools"].([]any)
	advertised := map[string][]string{}
	for _, tl := range tools {
		tm := tl.(map[string]any)
		name := tm["name"].(string)
		schema := tm["inputSchema"].(map[string]any)
		reqRaw, has := schema["required"]
		if !has {
			if _, declared := toolRequiredFields[name]; declared {
				t.Errorf("tool %v is declared in toolRequiredFields but advertises no required array", name)
			}
			continue
		}
		var req []string
		for _, r := range reqRaw.([]any) {
			req = append(req, r.(string))
		}
		advertised[name] = req
	}
	for name, want := range toolRequiredFields {
		got, ok := advertised[name]
		if !ok {
			t.Errorf("tool %v is declared in toolRequiredFields but not advertised in tools/list", name)
			continue
		}
		if strings.Join(got, ",") != strings.Join(want, ",") {
			t.Errorf("tool %v advertised required = %v, want %v (the single-source order)", name, got, want)
		}
	}
	// Reverse direction (review fix): every ADVERTISED required array
	// must come from a toolRequiredFields entry — a hardcoded required
	// array on a tool missing from the declaration must fail loudly,
	// otherwise drift can reintroduce itself silently.
	for name, req := range advertised {
		if _, declared := toolRequiredFields[name]; !declared {
			t.Errorf("tool %v advertises required %v but has no toolRequiredFields entry — a hardcoded required array broke the single-source contract", name, req)
		}
	}

	// Enforcement side: {} refuses naming the first declared field.
	for name, fields := range toolRequiredFields {
		out := mustHandle(t, s, `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"`+name+`","arguments":{}}}`)
		res := out["result"].(map[string]any)
		if res["isError"] != true {
			t.Errorf("%s with empty arguments must be refused, got %v", name, res)
			continue
		}
		text := res["content"].([]any)[0].(map[string]any)["text"].(string)
		if !strings.Contains(text, `"`+fields[0]+`"`) {
			t.Errorf("%s refusal %q must name the first required field %q", name, text, fields[0])
		}
	}

	// Enforcement side: all declared fields present passes the
	// parameter layer (the fake capability succeeds for every tool).
	for name, fields := range toolRequiredFields {
		// draft_update additionally requires an object "content" beyond the
		// string-field contract (toolRequiredFields is string-only), so the
		// generic string map would miss it; provide a minimal content object.
		if name == "draft_update" {
			raw := []byte(`{"target":"x","content":{"k":"v"}}`)
			out := mustHandle(t, s, `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"`+name+`","arguments":`+string(raw)+`}}`)
			res := out["result"].(map[string]any)
			if res["isError"] != false {
				text := res["content"].([]any)[0].(map[string]any)["text"].(string)
				t.Errorf("%s with all required fields must pass the parameter layer, refused with: %s", name, text)
			}
			continue
		}
		args := map[string]string{}
		for _, f := range fields {
			if f == "type" { // feedback_new's fake validates the closed type set
				args[f] = "bug"
				continue
			}
			args[f] = "x"
		}
		raw, err := json.Marshal(args)
		if err != nil {
			t.Fatal(err)
		}
		out := mustHandle(t, s, `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"`+name+`","arguments":`+string(raw)+`}}`)
		res := out["result"].(map[string]any)
		if res["isError"] != false {
			text := res["content"].([]any)[0].(map[string]any)["text"].(string)
			t.Errorf("%s with all required fields must pass the parameter layer, refused with: %s", name, text)
		}
	}
}

// TestParamRefusalDiagnosticsLine pins the diagnostics record format:
// one line on the diagnostics sink (stderr in production, NEVER stdout),
// fixed field order, the argument BYTE LENGTH, the cause, and the field
// when one is named. No argument values are ever logged.
func TestParamRefusalDiagnosticsLine(t *testing.T) {
	s, buf := diagServer()
	argsJSON := `{"project":"feather","namespace":"feather","type":"adr"}`
	callNew(t, s, argsJSON)

	lines := strings.Split(strings.TrimSuffix(buf.String(), "\n"), "\n")
	if len(lines) != 1 {
		t.Fatalf("expected exactly 1 diagnostic line, got %d:\n%s", len(lines), buf.String())
	}
	want := "eka-mcp param-refusal tool=new args_bytes=" + strconv.Itoa(len(argsJSON)) +
		" cause=missing_field field=id\n"
	if buf.String() != want {
		t.Errorf("diagnostic line =\n%q\nwant\n%q", buf.String(), want)
	}
}

// TestParamRefusalDiagnosticsCausesAndLengths: each cause logs its own
// cause token, and args_bytes equals the exact argument byte length the
// server received (the truncation discriminator). Driven at the
// decodeToolArgs unit: note that a corrupted/truncated ARGUMENTS payload
// is unreachable through the framed protocol (HandleMessage parses the
// whole line first) — it guards direct callers and future transports.
func TestParamRefusalDiagnosticsCausesAndLengths(t *testing.T) {
	cases := []struct {
		name     string
		args     string // exact arguments bytes ("" = absent)
		wantLine string
	}{
		{
			name:     "invalid json",
			args:     `{"project":"feather",`,
			wantLine: "eka-mcp param-refusal tool=new args_bytes=21 cause=invalid_json",
		},
		{
			name:     "non-object json",
			args:     `"bogus"`,
			wantLine: "eka-mcp param-refusal tool=new args_bytes=7 cause=invalid_json",
		},
		{
			name:     "wrong type",
			args:     `{"project":"f","namespace":"f","type":"adr","id":7}`,
			wantLine: "eka-mcp param-refusal tool=new args_bytes=51 cause=wrong_type field=id",
		},
		{
			name:     "empty field",
			args:     `{"project":"","namespace":"f","type":"adr","id":"x"}`,
			wantLine: "eka-mcp param-refusal tool=new args_bytes=52 cause=empty_field field=project",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			buf := &bytes.Buffer{}
			s := NewServerWithDiagnostics(&fakeCapability{statusJSON: `{}`}, buf)
			var raw json.RawMessage
			if tc.args != "" {
				raw = json.RawMessage(tc.args)
			}
			err := s.decodeToolArgs("new", raw, &struct {
				Project   string `json:"project"`
				Namespace string `json:"namespace"`
				Type      string `json:"type"`
				ID        string `json:"id"`
			}{})
			if err == nil {
				t.Fatal("expected a refusal")
			}
			out := buf.String()
			if !strings.HasPrefix(out, tc.wantLine) {
				t.Errorf("diagnostic =\n%q\nwant prefix\n%q", out, tc.wantLine)
			}
			if !strings.HasSuffix(out, "\n") {
				t.Errorf("diagnostic must end with a newline, got %q", out)
			}
			if n := len(tc.args); !strings.Contains(out, "args_bytes="+strconv.Itoa(n)) {
				t.Errorf("diagnostic %q must carry the exact byte length %d", out, n)
			}
		})
	}
}

// TestTruncatedLineRefusedUpstream pins WHERE client-side truncation
// actually surfaces: a truncated request LINE never reaches the tool
// layer — HandleMessage refuses it with the fixed protocol parse error,
// never a schema-shaped message. Any historical schema-shaped refusal
// therefore points at argument SHAPE (double-encoded/non-object) or
// omitted fields, not at mid-line truncation.
func TestTruncatedLineRefusedUpstream(t *testing.T) {
	s, _ := diagServer()
	out := mustHandle(t, s, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"new","arguments":{"project":"feather","id":"00`)
	errObj := out["error"].(map[string]any)
	if errObj["code"] != float64(codeParseError) {
		t.Errorf("error code = %v, want %v", errObj["code"], codeParseError)
	}
	if errObj["message"] != "parse error: invalid JSON" {
		t.Errorf("message = %v, want the fixed parse refusal", errObj["message"])
	}
}

// TestOptionalFieldTypeMismatchLogged (review fix): a wrong-typed
// OPTIONAL field is refused with the invalid_arg cause — message names
// the field and the expected kind, and the diagnostics line carries
// the same cause token and field name.
func TestOptionalFieldTypeMismatchLogged(t *testing.T) {
	buf := &bytes.Buffer{}
	s := NewServerWithDiagnostics(&fakeCapability{statusJSON: `{}`}, buf)
	args := `{"target":"acme/sto:1","forward":"yes"}`
	out := mustHandle(t, s, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"transition","arguments":`+args+`}}`)
	res := out["result"].(map[string]any)
	if res["isError"] != true {
		t.Fatalf("wrong-typed optional field must be refused, got %v", res)
	}
	text := res["content"].([]any)[0].(map[string]any)["text"].(string)
	if !strings.Contains(text, `"forward"`) || !strings.Contains(text, "boolean") {
		t.Errorf("refusal %q must name the field \"forward\" and the boolean kind", text)
	}
	want := "eka-mcp param-refusal tool=transition args_bytes=" + strconv.Itoa(len(args)) +
		" cause=invalid_arg field=forward\n"
	if buf.String() != want {
		t.Errorf("diagnostic =\n%q\nwant\n%q", buf.String(), want)
	}
}

// TestNilDiagnosticsDiscards: a nil diagnostics sink discards records —
// refusals keep working without a writer.
func TestNilDiagnosticsDiscards(t *testing.T) {
	s := NewServerWithDiagnostics(&fakeCapability{statusJSON: `{}`}, nil)
	res, text := callNew(t, s, `{"project":"feather"}`)
	if res["isError"] != true {
		t.Fatalf("refusal must still fire, got %v (%s)", res["isError"], text)
	}
}

// TestOptionalOnlyToolsTolerateAbsentArguments: tools whose contract
// declares no required fields keep accepting absent/null arguments —
// the discrimination must not turn omission into a refusal for them.
func TestOptionalOnlyToolsTolerateAbsentArguments(t *testing.T) {
	for _, tc := range []struct{ name, params string }{
		{"sync_push", `{"name":"sync_push"}`},
		{"sync_push null args", `{"name":"sync_push","arguments":null}`},
		{"draft_list", `{"name":"draft_list"}`},
		{"draft_list null args", `{"name":"draft_list","arguments":null}`},
		{"integrity_check", `{"name":"integrity_check"}`},
		{"feedback_list", `{"name":"feedback_list"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, _ := diagServer()
			out := mustHandle(t, s, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":`+tc.params+`}`)
			res := out["result"].(map[string]any)
			if res["isError"] != false {
				text := res["content"].([]any)[0].(map[string]any)["text"].(string)
				t.Errorf("%s must tolerate absent arguments, refused with: %s", tc.name, text)
			}
		})
	}
}
