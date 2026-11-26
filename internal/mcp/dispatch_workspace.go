package mcp

import (
	"encoding/json"
	"fmt"
)

// dispatchSyncPush refuses pull re-seed attempts via MCP (AC #3)
// deterministically: the MCP surface is push-only; `eka sync pull` /
// --from-docs stays operator-supervised CLI because it re-points line
// references to older instances (silent regression hazard). The raw
// arguments are inspected before any capability call.
func (s *Server) dispatchSyncPush(args json.RawMessage) (string, error) {
	if len(args) != 0 && string(args) != "null" {
		var raw map[string]json.RawMessage
		if err := json.Unmarshal(args, &raw); err == nil {
			if _, has := raw["fromDocs"]; has {
				return "", fmt.Errorf("sync pull is not exposed via MCP (silent regression hazard — it can re-point line references to older instances); use the operator-supervised CLI `eka sync pull`")
			}
			if _, has := raw["from_docs"]; has {
				return "", fmt.Errorf("sync pull is not exposed via MCP (silent regression hazard — it can re-point line references to older instances); use the operator-supervised CLI `eka sync pull`")
			}
			if _, has := raw["from-docs"]; has {
				return "", fmt.Errorf("sync pull is not exposed via MCP (silent regression hazard — it can re-point line references to older instances); use the operator-supervised CLI `eka sync pull`")
			}
			if _, has := raw["pull"]; has {
				return "", fmt.Errorf("sync pull is not exposed via MCP (silent regression hazard — it can re-point line references to older instances); use the operator-supervised CLI `eka sync pull`")
			}
		}
	}
	var p struct {
		RepoPath string `json:"repoPath"`
		Adopt    bool   `json:"adopt"`
		Override bool   `json:"override"`
	}
	if err := s.decodeToolArgs("sync_push", args, &p); err != nil {
		return "", err
	}
	if !isCanonicalWriteAllowed() {
		return "", fmt.Errorf("sync_push is a canonical-write (high-impact) and is gated: set EKA_MCP_ALLOW_CANONICAL_WRITE=1 to enable (riskClass=canonical-write requires approval)")
	}
	data, err := s.cap.SyncPush(p.RepoPath, p.Adopt, p.Override)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func (s *Server) dispatchIntegrityCheck(args json.RawMessage) (string, error) {
	data, err := s.cap.IntegrityCheck()
	if err != nil {
		return "", err
	}
	return string(data), nil
}
