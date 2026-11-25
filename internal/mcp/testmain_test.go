package mcp

import (
	"os"
	"testing"
)

func TestMain(m *testing.M) {
	// Existing tests were written before gating; they expect canonical
	// writes and external feedback_publish to succeed without explicit
	// approval. For the suite to stay green, the test suite opts into
	// canonical and external writes. Production (no env) remains gated:
	// canonical-write requires EKA_MCP_ALLOW_CANONICAL_WRITE=1 and
	// external requires EKA_MCP_ENABLE_FEEDBACK_PUBLISH=1.
	_ = os.Setenv("EKA_MCP_ALLOW_CANONICAL_WRITE", "1")
	_ = os.Setenv("EKA_MCP_ENABLE_FEEDBACK_PUBLISH", "1")
	os.Exit(m.Run())
}
