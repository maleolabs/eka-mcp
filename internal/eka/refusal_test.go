package eka

// Refusal-fidelity tests (sto:mcp-error-fidelity): the structured core
// refusals are wrapped so the MCP boundary can embed the full
// conformance report (eka-conformance-report-v1) and the transition
// confirmation affordances without losing the byte-stable headline.

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/maleolabs/eka-core/conformance"
	"github.com/maleolabs/eka-core/runtime"
	"github.com/maleolabs/eka-mcp/internal/mcp"
)

// refusalCarrier is the structural view of the boundary's fidelity
// contract (mcp.refusalFidelity): toolRefusal must satisfy it.
type refusalCarrier interface {
	error
	RefusalReport() string
	RefusalWarning() string
	RefusalConfirmation() bool
}

// sampleReport builds a deterministic conformance report: one blocking
// error, one warning, path-carrying message text.
func sampleReport() *conformance.Report {
	return &conformance.Report{
		Root:         "/home/user/.eka",
		FilesScanned: 0,
		Artifacts:    1,
		Results: []conformance.Result{
			{File: "feather/adr:004", Rule: "R10", Severity: conformance.SeverityWarning, Message: "no upward traceability"},
			{File: "feather/adr:004", Rule: "R5", Severity: conformance.SeverityError, Message: "reference target feather/adr:nope does not resolve"},
			{File: "feather/adr:004", Rule: "R6", Severity: conformance.SeverityError, Message: "cannot read draft /home/user/.eka/drafts/adr-004.json"},
		},
	}
}

// TestWrapPublishErrorCarriesReport: a *runtime.PublishError is wrapped
// into the fidelity carrier — Error() stays verbatim, Unwrap resolves,
// and RefusalReport serializes the FULL report in the established
// eka-conformance-report-v1 shape (per-finding rule/severity/message,
// warnings included, deterministic order).
func TestWrapPublishErrorCarriesReport(t *testing.T) {
	pubErr := &runtime.PublishError{Target: "feather/adr:004", Report: sampleReport()}
	wrapped := wrapToolRefusal(pubErr)

	carrier, ok := wrapped.(refusalCarrier)
	if !ok {
		t.Fatalf("wrapToolRefusal must return the fidelity carrier, got %T", wrapped)
	}
	if wrapped.Error() != pubErr.Error() {
		t.Errorf("wrapped message = %q, want verbatim %q", wrapped.Error(), pubErr.Error())
	}
	var unwound *runtime.PublishError
	if !errors.As(wrapped, &unwound) || unwound != pubErr {
		t.Errorf("Unwrap must resolve to the original PublishError")
	}

	report := carrier.RefusalReport()
	if report == "" {
		t.Fatal("RefusalReport must serialize the carried report")
	}
	var doc map[string]any
	if err := json.Unmarshal([]byte(report), &doc); err != nil {
		t.Fatalf("report must be valid JSON: %v\n%s", err, report)
	}
	if doc["schema"] != "eka-conformance-report-v1" {
		t.Errorf("schema = %v, want eka-conformance-report-v1", doc["schema"])
	}
	if doc["errors"] != float64(2) || doc["warnings"] != float64(1) || doc["pass"] != false {
		t.Errorf("counts = %v/%v/%v, want 2 errors / 1 warning / fail", doc["errors"], doc["warnings"], doc["pass"])
	}
	results := doc["results"].([]any)
	if len(results) != 3 {
		t.Fatalf("findings = %d, want 3 (warnings included)", len(results))
	}
	first := results[0].(map[string]any)
	// SortedResults order: file, rule rank, severity, message — R5 first.
	if first["rule"] != "R5" || first["severity"] != "error" {
		t.Errorf("first finding = %v, want R5/error (deterministic order)", first)
	}
	for _, key := range []string{"file", "rule", "severity", "message"} {
		if first[key] == nil || first[key] == "" {
			t.Errorf("finding missing %q: %v", key, first)
		}
	}
}

// TestWrapRelateValidationErrorCarriesReport: the relate validation
// class rides the same report mechanism.
func TestWrapRelateValidationErrorCarriesReport(t *testing.T) {
	relErr := &runtime.RelateValidationError{Target: "feather/adr:004", Report: sampleReport()}
	wrapped := wrapToolRefusal(relErr)

	carrier, ok := wrapped.(refusalCarrier)
	if !ok {
		t.Fatalf("wrapToolRefusal must return the fidelity carrier, got %T", wrapped)
	}
	if !strings.Contains(carrier.RefusalReport(), "eka-conformance-report-v1") {
		t.Errorf("relate refusal must embed the report, got %q", carrier.RefusalReport())
	}
	if carrier.RefusalWarning() != "" || carrier.RefusalConfirmation() {
		t.Errorf("a validation refusal carries no transition affordances")
	}
}

// TestWrapTransitionRefusalCarriesAffordances: the membership-gate
// transition refusal exposes its warning and confirmation flag; plain
// gate refusals carry neither.
func TestWrapTransitionRefusalCarriesAffordances(t *testing.T) {
	membership := &runtime.TransitionRefusal{
		Reason:       "test-ns/sto:x is not registered in the current active container",
		Hint:         "confirm in a terminal or pass --force to proceed",
		Warning:      "test-ns/sto:x is not registered in the current active container (no ticket deriving from an active ctr- references it)",
		Confirmation: true,
	}
	wrapped := wrapToolRefusal(membership)
	carrier, ok := wrapped.(refusalCarrier)
	if !ok {
		t.Fatalf("wrapToolRefusal must return the fidelity carrier, got %T", wrapped)
	}
	if carrier.RefusalWarning() != membership.Warning {
		t.Errorf("warning = %q, want %q", carrier.RefusalWarning(), membership.Warning)
	}
	if !carrier.RefusalConfirmation() {
		t.Error("confirmation flag must be carried")
	}
	if carrier.RefusalReport() != "" {
		t.Errorf("a transition refusal carries no report, got %q", carrier.RefusalReport())
	}

	plain := wrapToolRefusal(&runtime.TransitionRefusal{
		Reason: "transition planned -> done is not in the D1 table",
		Hint:   "legal transitions from \"planned\": todo",
	})
	pcarrier, ok := plain.(refusalCarrier)
	if !ok {
		t.Fatalf("plain refusals ride the same carrier, got %T", plain)
	}
	if pcarrier.RefusalWarning() != "" || pcarrier.RefusalConfirmation() {
		t.Errorf("a plain gate refusal must carry no affordances")
	}
}

// TestMarshalConformanceReportRedactsFindings: every free-text field of
// the serialized report passes the boundary's path-redaction policy —
// store paths cannot leak through findings; identity forms survive.
func TestMarshalConformanceReportRedactsFindings(t *testing.T) {
	report := sampleReport()
	report.Results = append(report.Results, conformance.Result{
		File:     "/abs/leak/store.db",
		Rule:     "R2",
		Severity: conformance.SeverityError,
		Message:  "store failure at ~/x/y.db and at relative rel/sub/file.json",
	})
	out := marshalConformanceReport(report)
	for _, leak := range []string{"/home/user", ".eka/drafts", "/abs/leak", "~/x/y.db", "rel/sub/file.json"} {
		if strings.Contains(out, leak) {
			t.Errorf("serialized report leaks %q:\n%s", leak, out)
		}
	}
	// The redacted serialization stays valid JSON; its string values
	// carry <path> markers (json.Marshal escapes < as \u003c in the raw
	// bytes, so assert on the decoded values).
	var doc map[string]any
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("redacted report must stay valid JSON: %v\n%s", err, out)
	}
	var marked bool
	walkStrings(doc, func(s string) {
		if strings.Contains(s, "<path>") {
			marked = true
		}
	})
	if !marked {
		t.Errorf("redacted markers expected:\n%s", out)
	}
	if !strings.Contains(out, "feather/adr:004") {
		t.Errorf("identity forms must survive redaction:\n%s", out)
	}
}

// walkStrings visits every string value of a decoded JSON document.
func walkStrings(v any, visit func(string)) {
	switch t := v.(type) {
	case string:
		visit(t)
	case []any:
		for _, e := range t {
			walkStrings(e, visit)
		}
	case map[string]any:
		for _, e := range t {
			walkStrings(e, visit)
		}
	}
}

// TestMarshalConformanceReportNilIsEmpthless: a nil report serializes
// to "" — the boundary emits no block.
func TestMarshalConformanceReportNilIsEmpty(t *testing.T) {
	if got := marshalConformanceReport(nil); got != "" {
		t.Errorf("nil report = %q, want \"\"", got)
	}
}

// TestWrapToolRefusalPassThrough: ordinary errors pass through
// unchanged (same instance) — no wrapping overhead on the happy paths.
func TestWrapToolRefusalPassThrough(t *testing.T) {
	err := errors.New("eka: workspace not initialized")
	if got := wrapToolRefusal(err); got != err {
		t.Errorf("plain error must pass through unchanged, got %v", got)
	}
	if got := wrapToolRefusal(nil); got != nil {
		t.Errorf("nil must pass through, got %v", got)
	}
}

// TestWrapToolRefusalThroughWrapChain: detection is errors.As-based, so
// a structured refusal re-wrapped by an intermediate layer (e.g. note
// resolution's "note resolve: %w" chain around a PublishError) is still
// recognized.
func TestWrapToolRefusalThroughWrapChain(t *testing.T) {
	pubErr := &runtime.PublishError{Target: "feather/cmt:x", Report: sampleReport()}
	chained := fmt.Errorf("note resolve: cmt:x: %w", pubErr)
	wrapped := wrapToolRefusal(chained)

	carrier, ok := wrapped.(refusalCarrier)
	if !ok {
		t.Fatalf("chained PublishError must be recognized, got %T", wrapped)
	}
	if carrier.RefusalReport() == "" {
		t.Error("chained refusal must still carry the report")
	}
	if wrapped.Error() != chained.Error() {
		t.Errorf("wrapped message = %q, want the chain verbatim %q", wrapped.Error(), chained.Error())
	}
}

// TestPublishRefusalCarriesReportEndToEnd: the full capability path —
// scaffolding a draft whose relationship target does not resolve and
// publishing it returns the fidelity carrier with the embedded
// eka-conformance-report-v1 findings (the exact data the CLI renders).
func TestPublishRefusalCarriesReportEndToEnd(t *testing.T) {
	authoringRuntime(t)
	cap, err := Open()
	if err != nil {
		t.Fatal(err)
	}
	defer cap.Close()

	_, err = cap.NewDraft(mcp.NewDraftRequest{
		Project:   "feather",
		Namespace: "feather",
		Type:      "adr",
		ID:        "005-broken-ref",
		Dimension: "decisions",
		By:        mcp.AuthorIdentity{Kind: "agent", Name: "mcp-agent"},
		Relationships: []mcp.Relationship{
			{Type: "depends-on", Target: "feather/adr:does-not-resolve"},
		},
	})
	if err != nil {
		t.Fatalf("NewDraft failed: %v", err)
	}

	_, err = cap.Publish(mcp.PublishRequest{Target: "feather/adr:005-broken-ref", Project: "feather"})
	if err == nil {
		t.Fatal("publishing a draft with an unresolvable reference must be refused")
	}
	carrier, ok := err.(refusalCarrier)
	if !ok {
		t.Fatalf("publish refusal must be the fidelity carrier, got %T: %v", err, err)
	}
	report := carrier.RefusalReport()
	if report == "" {
		t.Fatal("the publish refusal must embed the validation report")
	}
	var doc map[string]any
	if err := json.Unmarshal([]byte(report), &doc); err != nil {
		t.Fatalf("embedded report must be valid JSON: %v\n%s", err, report)
	}
	if doc["schema"] != "eka-conformance-report-v1" {
		t.Errorf("schema = %v, want eka-conformance-report-v1", doc["schema"])
	}
	results, _ := doc["results"].([]any)
	if len(results) == 0 {
		t.Fatalf("the embedded report must carry findings:\n%s", report)
	}
	if !strings.Contains(report, "does-not-resolve") {
		t.Errorf("findings must name the unresolvable target:\n%s", report)
	}
	// The draft was kept (all-or-nothing publish).
	if _, err := cap.DraftRead("feather/adr:005-broken-ref", "feather"); err != nil {
		t.Errorf("the refused publish must keep the draft: %v", err)
	}
}
