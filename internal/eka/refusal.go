// refusal.go adapts the structured authoring refusals of eka-core so
// their full fidelity survives the capability-layer boundary
// (sto:mcp-error-fidelity).
//
// eka-core returns deterministic, data-carrying refusal errors:
//
//   - *runtime.PublishError{Target, Report}        — publish gate;
//   - *runtime.RelateValidationError{Target, Report} — relate gate;
//   - *runtime.TransitionRefusal{Reason, Hint, Warning, Confirmation}
//     — transition gates; Warning/Confirmation mark the retryable
//     active-container membership gate.
//
// Their Error() renders only a count/reason summary; the carried Report
// and affordance fields are exported and reachable via errors.As. The
// MCP boundary used to flatten every failure to the sanitized first
// line, silently dropping that fidelity and forcing agents into a CLI
// fallback for diagnosis. wrapToolRefusal wraps these classes into
// toolRefusal — an error that keeps the original message byte-for-byte
// (the sanitized headline stays stable) while exposing the serialized
// report (eka-conformance-report-v1) and the transition affordances
// through the mcp.refusalFidelity contract. The validation is never
// re-run: the report is embedded verbatim from the carried error.

package eka

import (
	"encoding/json"
	"errors"

	"github.com/maleolabs/eka-core/conformance"
	"github.com/maleolabs/eka-core/runtime"
	"github.com/maleolabs/eka-mcp/internal/mcp"
)

// toolRefusal carries one structured core refusal across the capability
// boundary without losing fidelity. It IS the original error to clients
// that only render messages (Error() delegates verbatim) and it unwraps
// to the original (Unwrap), so errors.As chains keep resolving to the
// core types.
type toolRefusal struct {
	err error
	// reportJSON is the conformance report in the established
	// eka-conformance-report-v1 shape ("" when the refusal carries no
	// report). Findings are path-redacted at serialization time.
	reportJSON string
	// warning is the active-container banner of a transition refusal
	// ("" when not membership-related).
	warning string
	// confirm reports the retryable confirmation gate.
	confirm bool
}

// Error renders the ORIGINAL refusal message verbatim — the MCP
// boundary's SanitizeError headline over this error is byte-identical
// to the pre-fidelity behavior.
func (e *toolRefusal) Error() string { return e.err.Error() }

// Unwrap exposes the original structured refusal.
func (e *toolRefusal) Unwrap() error { return e.err }

// RefusalReport implements mcp.refusalFidelity.
func (e *toolRefusal) RefusalReport() string { return e.reportJSON }

// RefusalWarning implements mcp.refusalFidelity.
func (e *toolRefusal) RefusalWarning() string { return e.warning }

// RefusalConfirmation implements mcp.refusalFidelity.
func (e *toolRefusal) RefusalConfirmation() bool { return e.confirm }

// wrapToolRefusal adapts one capability error: the known structured
// refusal classes are wrapped into toolRefusal; anything else passes
// through unchanged. errors.As drives the detection, so wrapped
// variants (e.g. note resolution's "note resolve: %w" chain around a
// PublishError) are recognized too.
func wrapToolRefusal(err error) error {
	if err == nil {
		return nil
	}
	var pubErr *runtime.PublishError
	if errors.As(err, &pubErr) {
		return &toolRefusal{err: err, reportJSON: marshalConformanceReport(pubErr.Report)}
	}
	var relErr *runtime.RelateValidationError
	if errors.As(err, &relErr) {
		return &toolRefusal{err: err, reportJSON: marshalConformanceReport(relErr.Report)}
	}
	var trErr *runtime.TransitionRefusal
	if errors.As(err, &trErr) {
		return &toolRefusal{err: err, warning: trErr.Warning, confirm: trErr.Confirmation}
	}
	return err
}

// marshalConformanceReport serializes one core conformance report into
// the established eka-conformance-report-v1 shape — the SAME wire
// structs the validate tool serves (conformanceReport/conformanceResult),
// so consumers parse one shape everywhere. Every free-text field (root,
// finding file, finding message) passes the boundary's path-redaction
// policy (mcp.RedactPaths): identity forms survive, store paths cannot
// leak through findings. A nil report serializes to "" (no block).
func marshalConformanceReport(report *conformance.Report) string {
	if report == nil {
		return ""
	}
	results := make([]conformanceResult, 0, len(report.Results))
	for _, r := range report.SortedResults() {
		results = append(results, conformanceResult{
			File:     mcp.RedactPaths(r.File),
			Rule:     r.Rule,
			Severity: string(r.Severity),
			Message:  mcp.RedactPaths(r.Message),
		})
	}
	b, err := json.Marshal(conformanceReport{
		Schema:       "eka-conformance-report-v1",
		Root:         mcp.RedactPaths(report.Root),
		FilesScanned: report.FilesScanned,
		Artifacts:    report.Artifacts,
		Skipped:      report.Skipped,
		Errors:       report.ErrorCount(),
		Warnings:     report.WarningCount(),
		Pass:         report.Pass(),
		Results:      results,
	})
	if err != nil {
		// The shape is plain JSON-safe strings/ints — marshalling
		// cannot fail; the empty result degrades to "no block".
		return ""
	}
	return string(b)
}
