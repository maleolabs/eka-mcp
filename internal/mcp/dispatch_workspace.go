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

func (s *Server) dispatchAssign(args json.RawMessage) (string, error) {
	var p struct {
		Target   string `json:"target"`
		To       string `json:"to"`
		RepoPath string `json:"repoPath"`
		By       string `json:"by"`
		ByKind   string `json:"byKind"`
	}
	if err := s.decodeToolArgs("assign", args, &p); err != nil {
		return "", err
	}
	if !isCanonicalWriteAllowed() {
		return "", fmt.Errorf("assign is a canonical-write (high-impact) and is gated: set EKA_MCP_ALLOW_CANONICAL_WRITE=1 to enable (riskClass=canonical-write requires approval)")
	}
	by, err := resolveAuthor(p.By, p.ByKind)
	if err != nil {
		return "", err
	}
	data, err := s.cap.Assign(AssignmentRequest{
		RepoPath: p.RepoPath,
		Target:   p.Target,
		To:       p.To,
		By:       by,
	})
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func (s *Server) dispatchReassign(args json.RawMessage) (string, error) {
	var p struct {
		Target   string `json:"target"`
		To       string `json:"to"`
		RepoPath string `json:"repoPath"`
		By       string `json:"by"`
		ByKind   string `json:"byKind"`
	}
	if err := s.decodeToolArgs("reassign", args, &p); err != nil {
		return "", err
	}
	if !isCanonicalWriteAllowed() {
		return "", fmt.Errorf("reassign is a canonical-write (high-impact) and is gated: set EKA_MCP_ALLOW_CANONICAL_WRITE=1 to enable (riskClass=canonical-write requires approval)")
	}
	by, err := resolveAuthor(p.By, p.ByKind)
	if err != nil {
		return "", err
	}
	data, err := s.cap.Reassign(AssignmentRequest{
		RepoPath: p.RepoPath,
		Target:   p.Target,
		To:       p.To,
		By:       by,
	})
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func (s *Server) dispatchUnassign(args json.RawMessage) (string, error) {
	var p struct {
		Target   string `json:"target"`
		RepoPath string `json:"repoPath"`
		By       string `json:"by"`
		ByKind   string `json:"byKind"`
	}
	if err := s.decodeToolArgs("unassign", args, &p); err != nil {
		return "", err
	}
	if !isCanonicalWriteAllowed() {
		return "", fmt.Errorf("unassign is a canonical-write (high-impact) and is gated: set EKA_MCP_ALLOW_CANONICAL_WRITE=1 to enable (riskClass=canonical-write requires approval)")
	}
	by, err := resolveAuthor(p.By, p.ByKind)
	if err != nil {
		return "", err
	}
	data, err := s.cap.Unassign(UnassignRequest{
		RepoPath: p.RepoPath,
		Target:   p.Target,
		By:       by,
	})
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
