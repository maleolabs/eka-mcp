package mcp

import "encoding/json"

func (s *Server) dispatchCodeContext(args json.RawMessage) (string, error) {
	var p struct {
		Root      string `json:"root"`
		Focus     string `json:"focus"`
		Depth     string `json:"depth"`
		Level     int    `json:"level"`
		NoContent bool   `json:"noContent"`
	}
	if err := s.decodeToolArgs("code_context", args, &p); err != nil {
		return "", err
	}
	data, err := s.cap.CodeContext(CodeContextRequest{Root: p.Root, Focus: p.Focus, Depth: p.Depth, Level: p.Level, NoContent: p.NoContent})
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func (s *Server) dispatchCodeDiscover(args json.RawMessage) (string, error) {
	var p struct {
		Root  string `json:"root"`
		Query string `json:"query"`
		Scope string `json:"scope"`
		Limit int    `json:"limit"`
	}
	if err := s.decodeToolArgs("code_discover", args, &p); err != nil {
		return "", err
	}
	data, err := s.cap.CodeDiscover(CodeDiscoverRequest{Root: p.Root, Query: p.Query, Scope: p.Scope, Limit: p.Limit})
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func (s *Server) dispatchCodeGet(args json.RawMessage) (string, error) {
	var p struct {
		Root string `json:"root"`
		Path string `json:"path"`
	}
	if err := s.decodeToolArgs("code_get", args, &p); err != nil {
		return "", err
	}
	data, err := s.cap.CodeGet(CodeGetRequest{Root: p.Root, Path: p.Path})
	if err != nil {
		return "", err
	}
	return string(data), nil
}
