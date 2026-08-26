package mcp

// Fidelity tests (sto:mcp-error-fidelity) — the boundary contract for
// structured tool refusals: the sanitized headline stays byte-stable
// (block 0 is exactly SanitizeError(err)), the carried conformance
// report is embedded as an additional text content block in the
// established eka-conformance-report-v1 shape, transition refusals
// surface the active-container warning plus the retry-with-confirmed
// affordance, and NO finding leaks a path the headline could not.
// Plain errors keep the historical single-block result shape.

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
)

// stubFidelity is a deterministic stand-in for the capability layer's
// wrapped structured refusals (internal/eka.toolRefusal): it satisfies
// the refusalFidelity contract without importing eka-core — these stay
// pure protocol tests.
type stubFidelity struct {
	msg     string
	report  string
	warning string
	confirm bool
}

func (f *stubFidelity) Error() string             { return f.msg }
func (f *stubFidelity) RefusalReport() string     { return f.report }
func (f *stubFidelity) RefusalWarning() string    { return f.warning }
func (f *stubFidelity) RefusalConfirmation() bool { return f.confirm }

const stubReportJSON = `{"schema":"eka-conformance-report-v1","root":"","filesScanned":0,"artifacts":1,"skipped":"","errors":2,"warnings":1,"pass":false,"results":[` +
	`{"file":"feather/adr:004","rule":"R5","severity":"error","message":"reference target feather/adr:nope does not resolve"},` +
	`{"file":"feather/adr:004","rule":"R6","severity":"error","message":"missing dimension on knowledge artifact type \"adr\""},` +
	`{"file":"feather/adr:004","rule":"R10","severity":"warning","message":"no upward traceability"}` +
	`]}`

// mustFailureContent extracts the text blocks of an isError tool result.
func mustFailureContent(t *testing.T, out map[string]any) []string {
	t.Helper()
	res, ok := out["result"].(map[string]any)
	if !ok {
		t.Fatalf("expected a result envelope, got %v", out)
	}
	if res["isError"] != true {
		t.Fatalf("isError = %v, want true", res["isError"])
	}
	blocks, ok := res["content"].([]any)
	if !ok || len(blocks) == 0 {
		t.Fatalf("expected non-empty content blocks, got %v", res["content"])
	}
	texts := make([]string, 0, len(blocks))
	for _, b := range blocks {
		bm, ok := b.(map[string]any)
		if !ok {
			t.Fatalf("content block is not an object: %v", b)
		}
		if bm["type"] != "text" {
			t.Fatalf("content block type = %v, want text", bm["type"])
		}
		text, ok := bm["text"].(string)
		if !ok {
			t.Fatalf("content block text is not a string: %v", bm["text"])
		}
		texts = append(texts, text)
	}
	return texts
}

// callPublish runs one publish tools/call against a capability whose
// Publish fails with err.
func callPublish(t *testing.T, err error) map[string]any {
	t.Helper()
	cap := &fakeCapability{statusJSON: `{}`, publishErr: err}
	s := newTestServer(cap)
	return mustHandle(t, s, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"publish","arguments":{"target":"feather/adr:004"}}}`)
}

// TestToolFailureEmbedsConformanceReport: a validation-class refusal
// carries the FULL report as a second text content block — per-finding
// rule id, severity and message, warnings included — not just the
// blocking-error count of the headline.
func TestToolFailureEmbedsConformanceReport(t *testing.T) {
	refusal := &stubFidelity{
		msg:    "publish refused: draft feather/adr:004 failed CKO-level validation with 2 blocking error(s); the draft was kept",
		report: stubReportJSON,
	}
	out := callPublish(t, refusal)
	texts := mustFailureContent(t, out)

	if len(texts) != 2 {
		t.Fatalf("content blocks = %d, want 2 (headline + report)", len(texts))
	}
	var report map[string]any
	if err := json.Unmarshal([]byte(texts[1]), &report); err != nil {
		t.Fatalf("the embedded report must be valid JSON: %v\n%s", err, texts[1])
	}
	if report["schema"] != "eka-conformance-report-v1" {
		t.Errorf("report schema = %v, want eka-conformance-report-v1", report["schema"])
	}
	results, ok := report["results"].([]any)
	if !ok || len(results) != 3 {
		t.Fatalf("report results = %v, want 3 findings", report["results"])
	}
	first := results[0].(map[string]any)
	for _, key := range []string{"rule", "severity", "message"} {
		if first[key] == nil || first[key] == "" {
			t.Errorf("finding missing %q: %v", key, first)
		}
	}
	if first["rule"] != "R5" || first["severity"] != "error" {
		t.Errorf("first finding = %v, want R5/error", first)
	}
	warn := results[2].(map[string]any)
	if warn["severity"] != "warning" {
		t.Errorf("warnings must be included in the embedded report, got %v", warn)
	}
}

// TestPublishRefusalHeadlineByteStable: the sanitized headline (content
// block 0) is EXACTLY SanitizeError(err) — byte-identical to the
// pre-fidelity count-only summary, so existing clients matching refusal
// classes keep working untouched.
func TestPublishRefusalHeadlineByteStable(t *testing.T) {
	msg := "publish refused: draft feather/adr:004 failed CKO-level validation with 2 blocking error(s); the draft was kept"
	refusal := &stubFidelity{msg: msg, report: stubReportJSON}
	out := callPublish(t, refusal)
	texts := mustFailureContent(t, out)

	if texts[0] != msg {
		t.Errorf("headline = %q, want the byte-stable %q", texts[0], msg)
	}
	if texts[0] != SanitizeError(refusal) {
		t.Errorf("headline diverged from SanitizeError: %q vs %q", texts[0], SanitizeError(refusal))
	}
}

// TestRefusalFindingsNoPathLeakage: finding text passes the SAME
// path-redaction policy as the headline — no store/workspace path may
// leak through the embedded report into the response body.
func TestRefusalFindingsNoPathLeakage(t *testing.T) {
	report := `{"schema":"eka-conformance-report-v1","root":"/home/user/.eka","filesScanned":0,"artifacts":1,"skipped":"","errors":1,"warnings":0,"pass":false,"results":[` +
		`{"file":"docs/adr/004.json","rule":"R5","severity":"error","message":"cannot read draft /home/user/.eka/drafts/adr-004.json: resolver store failure at /home/user/.eka/workspace.db"},` +
		`{"file":"feather/adr:004","rule":"R6","severity":"warning","message":"identity form feather/adr:004 survives redaction"}` +
		`]}`
	refusal := &stubFidelity{
		msg:    "publish refused: draft feather/adr:004 failed CKO-level validation with 1 blocking error(s); the draft was kept",
		report: report,
	}
	out := callPublish(t, refusal)
	texts := mustFailureContent(t, out)
	if len(texts) != 2 {
		t.Fatalf("content blocks = %d, want 2 (headline + report)", len(texts))
	}
	body := strings.Join(texts, "\n")
	for _, leak := range []string{"/home/user", ".eka", "workspace.db", "docs/adr/004.json"} {
		if strings.Contains(body, leak) {
			t.Errorf("response leaks %q inside findings:\n%s", leak, body)
		}
	}
	if !strings.Contains(body, "<path>") {
		t.Errorf("findings must carry redacted <path> markers, got:\n%s", body)
	}
	// Identity forms survive the redaction (they are not paths).
	if !strings.Contains(body, "feather/adr:004") {
		t.Errorf("identity forms must survive redaction, got:\n%s", body)
	}
	// The boundary guard must not corrupt the report: still valid JSON.
	var parsed map[string]any
	if err := json.Unmarshal([]byte(texts[1]), &parsed); err != nil {
		t.Fatalf("the redacted report must stay valid JSON: %v\n%s", err, texts[1])
	}
}

// TestTransitionConfirmationAffordanceSurfaced: a membership-gate
// transition refusal surfaces the active-container warning AND the
// explicit retry affordance ("retry with confirmed:true") — previously
// silently dropped at this boundary.
func TestTransitionConfirmationAffordanceSurfaced(t *testing.T) {
	cap := &fakeCapability{
		statusJSON: `{}`,
		transitionErr: &stubFidelity{
			msg:     "transition refused: test-ns/sto:x is not registered in the current active container; confirm in a terminal or pass --force to proceed",
			warning: "test-ns/sto:x is not registered in the current active container (no ticket deriving from an active ctr- references it)",
			confirm: true,
		},
	}
	s := newTestServer(cap)
	out := mustHandle(t, s, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"transition","arguments":{"target":"test-ns/sto:x","forward":true}}}`)
	texts := mustFailureContent(t, out)

	if len(texts) != 2 {
		t.Fatalf("content blocks = %d, want 2 (headline + affordance)", len(texts))
	}
	if !strings.Contains(texts[1], "warning: test-ns/sto:x is not registered in the current active container") {
		t.Errorf("affordance block must carry the warning, got %q", texts[1])
	}
	if !strings.Contains(texts[1], "retry with confirmed:true") {
		t.Errorf("affordance block must name the retry path, got %q", texts[1])
	}
}

// TestTransitionPlainRefusalStaysSingleBlock: a transition refusal
// without membership fields (illegal D1 step, unmet gate) keeps the
// historical single-block shape — no empty fidelity blocks.
func TestTransitionPlainRefusalStaysSingleBlock(t *testing.T) {
	cap := &fakeCapability{
		statusJSON:    `{}`,
		transitionErr: &stubFidelity{msg: "transition refused: transition planned -> done is not in the D1 table; legal transitions from \"planned\": todo"},
	}
	s := newTestServer(cap)
	out := mustHandle(t, s, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"transition","arguments":{"target":"test-ns/sto:x","to":"done"}}}`)
	texts := mustFailureContent(t, out)

	if len(texts) != 1 {
		t.Fatalf("content blocks = %d, want 1 for a plain refusal", len(texts))
	}
	if !strings.Contains(texts[0], "not in the D1 table") {
		t.Errorf("headline = %q, want the refusal message", texts[0])
	}
}

// TestPlainToolErrorStaysSingleBlock: an ordinary capability error (no
// fidelity carrier) produces exactly the historical single-block isError
// result — the default path is unchanged.
func TestPlainToolErrorStaysSingleBlock(t *testing.T) {
	out := callPublish(t, errors.New("eka: workspace not initialized"))
	texts := mustFailureContent(t, out)
	if len(texts) != 1 {
		t.Fatalf("content blocks = %d, want 1 for a plain error", len(texts))
	}
	if texts[0] != "eka: workspace not initialized" {
		t.Errorf("headline = %q, want the plain error message", texts[0])
	}
}

// TestCarrierWithoutReportAddsNoBlock: a fidelity carrier that carries
// no report contributes no report block (defensive: never emit empty
// blocks).
func TestCarrierWithoutReportAddsNoBlock(t *testing.T) {
	out := callPublish(t, &stubFidelity{msg: "publish refused: nothing to embed"})
	texts := mustFailureContent(t, out)
	if len(texts) != 1 {
		t.Fatalf("content blocks = %d, want 1 when no report is carried", len(texts))
	}
}

// TestFidelityUnwrappedThroughErrorChain: the boundary unwraps the
// carrier through wrap chains (errors.As) — a refusal re-wrapped by an
// intermediate layer still embeds its report.
func TestFidelityUnwrappedThroughErrorChain(t *testing.T) {
	inner := &stubFidelity{msg: "publish refused: inner", report: stubReportJSON}
	out := callPublish(t, fmt.Errorf("note resolve: cmt:x: %w", inner))
	texts := mustFailureContent(t, out)
	if len(texts) != 2 {
		t.Fatalf("content blocks = %d, want 2 through the wrap chain", len(texts))
	}
	if !strings.Contains(texts[1], "eka-conformance-report-v1") {
		t.Errorf("embedded report lost through the wrap chain: %q", texts[1])
	}
}
