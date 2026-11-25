package mcp

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

func (s *Server) dispatchNew(args json.RawMessage) (string, error) {
	var p struct {
		Project       string          `json:"project"`
		Namespace     string          `json:"namespace"`
		Type          string          `json:"type"`
		ID            string          `json:"id"`
		Dimension     string          `json:"dimension"`
		Phase         string          `json:"phase"`
		Domain        string          `json:"domain"`
		By            string          `json:"by"`
		ByKind        string          `json:"byKind"`
		Relationships []Relationship  `json:"relationships"`
		Content       json.RawMessage `json:"content"`
	}
	if err := s.decodeToolArgs("new", args, &p); err != nil {
		return "", err
	}
	by, err := resolveAuthor(p.By, p.ByKind)
	if err != nil {
		return "", err
	}
	content, err := parseContent(p.Content)
	if err != nil {
		return "", err
	}
	data, err := s.cap.NewDraft(NewDraftRequest{
		Project:       p.Project,
		Namespace:     p.Namespace,
		Type:          p.Type,
		ID:            p.ID,
		Dimension:     p.Dimension,
		Phase:         p.Phase,
		Domain:        p.Domain,
		By:            by,
		Relationships: p.Relationships,
		Content:       content,
	})
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// dispatchDraftUpdate has its own required check (target) plus content object.
func (s *Server) dispatchDraftUpdate(args json.RawMessage) (string, error) {
	var p struct {
		Target           string          `json:"target"`
		Project          string          `json:"project"`
		Content          json.RawMessage `json:"content"`
		ExpectedRevision *int            `json:"expectedRevision"`
		ExpectedHash     string          `json:"expectedHash"`
	}
	if err := s.decodeToolArgs("draft_update", args, &p); err != nil {
		return "", err
	}
	// Explicit content required and must be an object (not null/array/string).
	if len(bytes.TrimSpace(p.Content)) == 0 || string(bytes.TrimSpace(p.Content)) == "null" {
		s.logParamRefusal("draft_update", len(args), causeMissingField, "content")
		return "", fmt.Errorf("draft_update requires \"content\": missing required field")
	}
	content, err := parseContent(p.Content)
	if err != nil {
		s.logParamRefusal("draft_update", len(args), causeInvalidArg, "content")
		return "", fmt.Errorf("draft_update has an invalid argument: %w", err)
	}
	if content == nil {
		s.logParamRefusal("draft_update", len(args), causeMissingField, "content")
		return "", fmt.Errorf("draft_update requires \"content\": missing required field")
	}
	if len(content) == 0 {
		return "", fmt.Errorf("draft_update requires content to be a non-empty JSON object")
	}
	if p.ExpectedRevision != nil && *p.ExpectedRevision < 1 {
		s.logParamRefusal("draft_update", len(args), causeInvalidArg, "expectedRevision")
		return "", fmt.Errorf("draft_update has an invalid argument: field \"expectedRevision\" must be >= 1")
	}
	if p.ExpectedHash != "" {
		hm := strings.TrimSpace(p.ExpectedHash)
		matched, _ := regexp.MatchString("^[a-fA-F0-9]{64}$", hm)
		if !matched {
			s.logParamRefusal("draft_update", len(args), causeInvalidArg, "expectedHash")
			return "", fmt.Errorf("draft_update has an invalid argument: field \"expectedHash\" must be 64 hex characters")
		}
	}
	data, err := s.cap.DraftUpdate(DraftUpdateRequest{
		Target:           p.Target,
		Project:          p.Project,
		Content:          content,
		ExpectedRevision: p.ExpectedRevision,
		ExpectedHash:     p.ExpectedHash,
	})
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// dispatchPublish is the strict single-draft publish — batch mode is
// publishBatch (split contract).
func (s *Server) dispatchPublish(args json.RawMessage) (string, error) {
	var p struct {
		Target          string `json:"target"`
		Project         string `json:"project"`
		InstanceVersion int    `json:"instanceVersion"`
	}
	if err := s.decodeToolArgs("publish", args, &p); err != nil {
		return "", err
	}
	if !isCanonicalWriteAllowed() {
		return "", fmt.Errorf("publish is a canonical-write (high-impact) and is gated: set EKA_MCP_ALLOW_CANONICAL_WRITE=1 to enable (riskClass=canonical-write requires approval)")
	}
	// Reject batch flags on single publish (client must use publishBatch).
	if len(args) != 0 && string(args) != "null" {
		var raw map[string]json.RawMessage
		if err := json.Unmarshal(args, &raw); err == nil {
			for _, k := range []string{"all", "pending"} {
				if _, has := raw[k]; has {
					return "", fmt.Errorf("publish: batch flags not available on single publish; use publishBatch")
				}
			}
		}
	}
	data, err := s.cap.Publish(PublishRequest{
		Target:          p.Target,
		Project:         p.Project,
		InstanceVersion: p.InstanceVersion,
	})
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func (s *Server) dispatchPublishBatch(args json.RawMessage) (string, error) {
	// Reject single-publish fields on batch.
	if len(args) != 0 && string(bytes.TrimSpace(args)) != "null" && string(bytes.TrimSpace(args)) != "" {
		var raw map[string]json.RawMessage
		if err := json.Unmarshal(args, &raw); err == nil {
			for _, k := range []string{"all", "pending"} {
				if v, ok := raw[k]; ok {
					var b bool
					if err := json.Unmarshal(v, &b); err != nil {
						s.logParamRefusal("publishBatch", len(args), causeInvalidArg, k)
						return "", fmt.Errorf("publishBatch has an invalid argument: field %q must be a boolean", k)
					}
				}
			}
			if _, has := raw["target"]; has {
				return "", fmt.Errorf("publishBatch: target is not available; use publish for single draft")
			}
			if _, has := raw["instanceVersion"]; has {
				return "", fmt.Errorf("publishBatch: instanceVersion not available in batch mode")
			}
		}
	}
	var pb struct {
		Project string `json:"project"`
		All     bool   `json:"all"`
		Pending bool   `json:"pending"`
	}
	if err := s.decodeToolArgs("publishBatch", args, &pb); err != nil {
		return "", err
	}
	if !isCanonicalWriteAllowed() {
		return "", fmt.Errorf("publishBatch is a canonical-write (high-impact) and is gated: set EKA_MCP_ALLOW_CANONICAL_WRITE=1 to enable (riskClass=canonical-write requires approval)")
	}
	data, err := s.cap.PublishBatch(PublishBatchRequest{
		Project: pb.Project,
		All:     pb.All,
		Pending: pb.Pending,
	})
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func (s *Server) dispatchTransition(args json.RawMessage) (string, error) {
	var p struct {
		RepoPath  string `json:"repoPath"`
		Target    string `json:"target"`
		To        string `json:"to"`
		Forward   bool   `json:"forward"`
		Backward  bool   `json:"backward"`
		By        string `json:"by"`
		ByKind    string `json:"byKind"`
		Confirmed bool   `json:"confirmed"`
	}
	if err := s.decodeToolArgs("transition", args, &p); err != nil {
		return "", err
	}
	if !isCanonicalWriteAllowed() {
		return "", fmt.Errorf("transition is a canonical-write (high-impact) and is gated: set EKA_MCP_ALLOW_CANONICAL_WRITE=1 to enable (riskClass=canonical-write requires approval)")
	}
	by, err := resolveAuthor(p.By, p.ByKind)
	if err != nil {
		return "", err
	}
	data, err := s.cap.Transition(TransitionRequest{
		RepoPath:  p.RepoPath,
		Target:    p.Target,
		To:        p.To,
		Forward:   p.Forward,
		Backward:  p.Backward,
		By:        by,
		Confirmed: p.Confirmed,
	})
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func (s *Server) dispatchNote(args json.RawMessage) (string, error) {
	var p struct {
		RepoPath string          `json:"repoPath"`
		Target   string          `json:"target"`
		Role     string          `json:"role"`
		Domain   string          `json:"domain"`
		By       string          `json:"by"`
		ByKind   string          `json:"byKind"`
		Content  json.RawMessage `json:"content"`
	}
	if err := s.decodeToolArgs("note", args, &p); err != nil {
		return "", err
	}
	by, err := resolveAuthor(p.By, p.ByKind)
	if err != nil {
		return "", err
	}
	content, err := parseContent(p.Content)
	if err != nil {
		return "", err
	}
	data, err := s.cap.Note(NoteRequest{
		RepoPath: p.RepoPath,
		Target:   p.Target,
		Role:     p.Role,
		Domain:   p.Domain,
		By:       by,
		Content:  content,
	})
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func (s *Server) dispatchDiscard(args json.RawMessage) (string, error) {
	var p struct {
		Target    string `json:"target"`
		Project   string `json:"project"`
		Confirmed bool   `json:"confirmed"`
	}
	if err := s.decodeToolArgs("discard", args, &p); err != nil {
		return "", err
	}
	// Discard is irreversible (the draft file is destroyed and the
	// identity is immediately reusable), so it is gated here and not
	// only in the capability layer: an unconfirmed call is refused
	// before s.cap.Discard is reached, i.e. it writes nothing.
	if !p.Confirmed {
		return "", &discardConfirmationGate{target: p.Target}
	}
	data, err := s.cap.Discard(p.Target, p.Project)
	if err != nil {
		return "", err
	}
	return string(data), nil
}
