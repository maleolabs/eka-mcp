package mcp

import (
	"encoding/json"
	"fmt"
)

func (s *Server) dispatchFeedbackNew(args json.RawMessage) (string, error) {
	var p struct {
		Type     string `json:"type"`
		Title    string `json:"title"`
		Severity string `json:"severity"`
		Source   string `json:"source"`
		Command  string `json:"command"`
		Content  string `json:"content"`
	}
	if err := s.decodeToolArgs("feedback_new", args, &p); err != nil {
		return "", err
	}
	data, err := s.cap.FeedbackNew(FeedbackNewRequest{
		Type:     p.Type,
		Title:    p.Title,
		Severity: p.Severity,
		Source:   p.Source,
		Command:  p.Command,
		Content:  p.Content,
	})
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// dispatchFeedbackList has no required fields (no toolRequiredFields
// entry) — empty object, absent or null arguments all list. Routed through
// decodeToolArgs so malformed (non-object) arguments refuse through the
// same discriminating, diagnostics-logged path as every other tool — the
// former inline refusal bypassed the param-refusal log line.
func (s *Server) dispatchFeedbackList(args json.RawMessage) (string, error) {
	if err := s.decodeToolArgs("feedback_list", args, nil); err != nil {
		return "", err
	}
	data, err := s.cap.FeedbackList()
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func (s *Server) dispatchFeedbackPublish(args json.RawMessage) (string, error) {
	var p struct {
		ID string `json:"id"`
	}
	if err := s.decodeToolArgs("feedback_publish", args, &p); err != nil {
		return "", err
	}
	if !isExternalAllowed() {
		return "", fmt.Errorf("feedback_publish is an external publish and is isolated/disabled by default: set EKA_MCP_ENABLE_FEEDBACK_PUBLISH=1 (or EKA_MCP_ALLOW_EXTERNAL=1) to enable (riskClass=external)")
	}
	data, err := s.cap.FeedbackPublish(FeedbackPublishRequest{ID: p.ID})
	if err != nil {
		return "", err
	}
	return string(data), nil
}
