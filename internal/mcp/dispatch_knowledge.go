package mcp

import (
	"encoding/json"
	"fmt"
)

func (s *Server) dispatchContext(args json.RawMessage) (string, error) {
	var p struct {
		Subject   string `json:"subject"`
		ProjectID string `json:"projectId"`
		Depth     string `json:"depth"`
	}
	if err := s.decodeToolArgs("context", args, &p); err != nil {
		return "", err
	}
	if p.Depth == "" {
		p.Depth = "local"
	}
	data, err := s.cap.Context(p.Subject, p.ProjectID, p.Depth)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func (s *Server) dispatchGet(args json.RawMessage) (string, error) {
	var p struct {
		Form      string `json:"form"`
		NoContent bool   `json:"noContent"`
		Level     string `json:"level"`
		Project   string `json:"project"`
		Version   string `json:"version"`
	}
	if err := s.decodeToolArgs("get", args, &p); err != nil {
		return "", err
	}
	var data []byte
	var err error
	if p.Level != "" || p.Project != "" || p.Version != "" {
		data, err = s.cap.GetWithFilters(p.Form, p.NoContent, p.Level, p.Project, p.Version)
	} else {
		data, err = s.cap.Get(p.Form, p.NoContent)
	}
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func (s *Server) dispatchDomain(args json.RawMessage) (string, error) {
	var p struct {
		ProjectID string `json:"projectId"`
		Domain    string `json:"domain"`
		NoContent bool   `json:"noContent"`
		Level     string `json:"level"`
		Project   string `json:"project"`
		Version   string `json:"version"`
		Limit     int    `json:"limit"`
		Offset    int    `json:"offset"`
	}
	if err := s.decodeToolArgs("domain", args, &p); err != nil {
		return "", err
	}
	limit := p.Limit
	if limit == 0 {
		limit = defaultDomainPageLimit
	}
	if limit < 1 || limit > maxDomainPageLimit {
		return "", fmt.Errorf("domain requires \"limit\" 1..%d (got %d): page large domains with limit/offset", maxDomainPageLimit, p.Limit)
	}
	if p.Offset < 0 {
		return "", fmt.Errorf("domain requires \"offset\" >= 0 (got %d)", p.Offset)
	}
	data, err := s.cap.DomainPaged(p.ProjectID, p.Domain, p.NoContent, p.Level, p.Project, p.Version, limit, p.Offset)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func (s *Server) dispatchStatus(args json.RawMessage) (string, error) {
	var sp struct {
		All bool `json:"all"`
	}
	if err := s.decodeToolArgs("status", args, &sp); err != nil {
		return "", err
	}
	data, err := s.cap.StatusWithAll(sp.All)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func (s *Server) dispatchValidate(args json.RawMessage) (string, error) {
	var p struct {
		Root string `json:"root"`
	}
	if err := s.decodeToolArgs("validate", args, &p); err != nil {
		return "", err
	}
	data, err := s.cap.Validate(p.Root)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// dispatchView is the deprecated alias for draft_read (td:mcp-view-naming-fix).
// TODO(td:mcp-view-naming-fix): remove in next minor version after 1.1.3 — then delete this handler and the View method.
func (s *Server) dispatchView(args json.RawMessage) (string, error) {
	var p struct {
		Target  string `json:"target"`
		Project string `json:"project"`
	}
	if err := s.decodeToolArgs("view", args, &p); err != nil {
		return "", err
	}
	data, err := s.cap.View(p.Target, p.Project)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func (s *Server) dispatchDraftRead(args json.RawMessage) (string, error) {
	var p struct {
		Target  string `json:"target"`
		Project string `json:"project"`
	}
	if err := s.decodeToolArgs("draft_read", args, &p); err != nil {
		return "", err
	}
	data, err := s.cap.DraftRead(p.Target, p.Project)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func (s *Server) dispatchDraftList(args json.RawMessage) (string, error) {
	var p struct {
		Project    string `json:"project"`
		Provenance string `json:"provenance"`
	}
	if err := s.decodeToolArgs("draft_list", args, &p); err != nil {
		return "", err
	}
	prov := p.Provenance
	if prov == "" {
		prov = "all"
	}
	// Prefer filtered path when available; fallback to DraftList for backward compat.
	if typed, ok := s.cap.(interface {
		DraftListWithProvenance(string, string) ([]byte, error)
	}); ok {
		data, err := typed.DraftListWithProvenance(p.Project, prov)
		if err != nil {
			return "", err
		}
		return string(data), nil
	}
	data, err := s.cap.DraftList(p.Project)
	if err != nil {
		return "", err
	}
	return string(data), nil
}
