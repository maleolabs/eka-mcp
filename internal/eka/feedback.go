package eka

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/maleolabs/eka-core/workspace"
	pack "github.com/maleolabs/eka-mcp"
	"github.com/maleolabs/eka-mcp/internal/feedback"
	"github.com/maleolabs/eka-mcp/internal/mcp"
)

// Feedback capability schemas (mirroring CLI eka feedback JSON schemas).
const (
	feedbackNewSchema     = "eka-feedback-new-v1"
	feedbackListSchema    = "eka-feedback-list-v1"
	feedbackPublishSchema = "eka-feedback-publish-v1"
)

// feedbackNewJSON is the machine report of feedback_new.
type feedbackNewJSON struct {
	Schema string `json:"schema"`
	OK     bool   `json:"ok"`
	ID     string `json:"id,omitempty"`
	Path   string `json:"path,omitempty"`
	Status string `json:"status,omitempty"`
}

// feedbackPublishJSON is the machine report of feedback_publish.
type feedbackPublishJSON struct {
	Schema      string `json:"schema"`
	OK          bool   `json:"ok"`
	ID          string `json:"id,omitempty"`
	IssueNumber int    `json:"issueNumber,omitempty"`
	IssueURL    string `json:"issueUrl,omitempty"`
}

// feedbackListJSON is the machine report of feedback_list.
type feedbackListJSON struct {
	Schema   string                 `json:"schema"`
	OK       bool                   `json:"ok"`
	Feedback []feedbackListItemJSON `json:"feedback"`
}

// feedbackListItemJSON mirrors CLI feedbackListItemJSON without body.
type feedbackListItemJSON struct {
	ID          string `json:"id"`
	Type        string `json:"type"`
	Title       string `json:"title"`
	Severity    string `json:"severity"`
	Source      string `json:"source"`
	EkaVersion  string `json:"ekaVersion"`
	OS          string `json:"os"`
	Command     string `json:"command"`
	Status      string `json:"status"`
	IssueURL    string `json:"issueUrl,omitempty"`
	IssueNumber int    `json:"issueNumber,omitempty"`
	Created     string `json:"created"`
}

// FeedbackNew creates a local feedback draft under EKA_HOME/feedback
// (YAML frontmatter + markdown body). It mirrors `eka feedback new`
// semantics exactly: same validation, same scaffold, same id generation,
// same file permissions. The feedback never enters the canonical store
// and never becomes a CKO — it is meta-information about the tool
// (ADR-026).
func (c *Capability) FeedbackNew(req mcp.FeedbackNewRequest) ([]byte, error) {
	typ := strings.TrimSpace(req.Type)
	title := strings.TrimSpace(req.Title)
	severity := strings.TrimSpace(req.Severity)
	source := strings.TrimSpace(req.Source)
	command := strings.TrimSpace(req.Command)
	content := req.Content

	if typ == "" {
		return nil, fmt.Errorf("feedback_new requires {\"type\": string, \"title\": string}")
	}
	if title == "" {
		return nil, fmt.Errorf("feedback_new requires {\"type\": string, \"title\": string}")
	}
	if severity == "" {
		severity = feedback.SeverityLow
	}
	if source == "" {
		source = "human"
	}
	switch severity {
	case feedback.SeverityLow, feedback.SeverityMedium, feedback.SeverityHigh:
	default:
		return nil, fmt.Errorf("invalid --severity %q (low, medium, or high)", severity)
	}
	switch source {
	case "human", "agent":
	default:
		return nil, fmt.Errorf("invalid --source %q (human or agent)", source)
	}
	switch typ {
	case feedback.TypeBug, feedback.TypeSuggestion, feedback.TypeImprovement, feedback.TypeQuestion:
	default:
		return nil, fmt.Errorf("invalid type %q (bug, suggestion, improvement, or question)", typ)
	}

	body := content
	if strings.TrimSpace(body) == "" {
		if typ == feedback.TypeBug {
			body = "## Steps to reproduce\n\n## Expected\n\n## Actual\n"
		} else {
			body = "## Description\n\n"
		}
	}
	// Ensure body ends with newline (Marshal will also ensure, but keep deterministic)
	if !strings.HasSuffix(body, "\n") {
		body += "\n"
	}

	home, err := workspace.HomeDir()
	if err != nil {
		return nil, fmt.Errorf("feedback: cannot resolve the workspace home: %w", err)
	}
	if command == "" {
		command = "mcp:feedback_new"
	}
	now := time.Now()
	fb := &feedback.Feedback{
		Type:       typ,
		Title:      title,
		Severity:   severity,
		Source:     source,
		EkaVersion: pack.Version,
		OS:         runtime.GOOS + "/" + runtime.GOARCH,
		Command:    command,
		Status:     feedback.StatusDraft,
		Created:    now.Format("2006-01-02"),
		Body:       body,
	}
	st := feedback.New(home)
	fb.ID = st.NewID(fb.Title, now)
	if err := st.Save(fb); err != nil {
		return nil, err
	}
	path := filepath.Join(st.Dir, fb.ID+".md")
	return json.Marshal(feedbackNewJSON{
		Schema: feedbackNewSchema,
		OK:     true,
		ID:     fb.ID,
		Path:   path,
		Status: fb.Status,
	})
}

// FeedbackList returns all local feedback deterministically
// (id descending, newest first — ids embed YYYYMMDD), mirroring
// `eka feedback list --json` (schema eka-feedback-list-v1).
func (c *Capability) FeedbackList() ([]byte, error) {
	home, err := workspace.HomeDir()
	if err != nil {
		return nil, fmt.Errorf("feedback: cannot resolve the workspace home: %w", err)
	}
	st := feedback.New(home)
	items, err := st.List()
	if err != nil {
		return nil, err
	}
	out := make([]feedbackListItemJSON, 0, len(items))
	for _, f := range items {
		out = append(out, feedbackListItemJSON{
			ID:          f.ID,
			Type:        f.Type,
			Title:       f.Title,
			Severity:    f.Severity,
			Source:      f.Source,
			EkaVersion:  f.EkaVersion,
			OS:          f.OS,
			Command:     f.Command,
			Status:      f.Status,
			IssueURL:    f.IssueURL,
			IssueNumber: f.IssueNumber,
			Created:     f.Created,
		})
	}
	// Deterministic: empty list is [] not null
	if out == nil {
		out = []feedbackListItemJSON{}
	}
	return json.Marshal(feedbackListJSON{
		Schema:   feedbackListSchema,
		OK:       true,
		Feedback: out,
	})
}

// feedbackIssueTimeout bounds one publish run's network phase (same as CLI).
const feedbackIssueTimeout = 60 * time.Second

// FeedbackPublish files a local draft as a GitHub issue on the fixed
// target repository, mirroring `eka feedback publish` semantics exactly.
// Constraints inherited unchanged:
// - release-binary requirement: empty bundled token refuses deterministically naming remediation
// - missing/invalid token refuses deterministically (never raw HTTP error)
// - token material never appears in outputs/errors/logs
// - idempotent: already-published refuses without duplicate issue
// - unknown id refuses deterministically
func (c *Capability) FeedbackPublish(req mcp.FeedbackPublishRequest) ([]byte, error) {
	id := strings.TrimSpace(req.ID)
	if id == "" {
		return nil, fmt.Errorf("feedback_publish requires {\"id\": string}")
	}
	home, err := workspace.HomeDir()
	if err != nil {
		return nil, fmt.Errorf("feedback: cannot resolve the workspace home: %w", err)
	}
	// Pre-flight load to surface unknown id and already-published
	// deterministically before any network.
	st := feedback.New(home)
	f, err := st.Load(id)
	if err != nil {
		if errors.Is(err, feedback.ErrNotFound) {
			return nil, fmt.Errorf("unknown feedback %q", id)
		}
		return nil, err
	}
	if f.Status == feedback.StatusPublished {
		return nil, fmt.Errorf("already published as #%d %s", f.IssueNumber, f.IssueURL)
	}
	if err := f.Validate(); err != nil {
		return nil, err
	}
	// Publish via the feedback package (same engine as CLI). It enforces
	// the release-binary token gate and creates the issue.
	ctx, cancel := context.WithTimeout(context.Background(), feedbackIssueTimeout)
	defer cancel()
	published, err := feedback.Publish(ctx, home, id)
	if err != nil {
		// Map token/invalid-token/network failures to deterministic
		// remediation messages without leaking token or raw HTTP internals.
		if strings.Contains(err.Error(), "issue token not bundled") {
			return nil, err
		}
		var apiErr *feedback.APIError
		if errors.As(err, &apiErr) {
			if apiErr.Status == 401 || apiErr.Status == 403 {
				return nil, fmt.Errorf("publish failed: invalid token — use a release binary")
			}
			// Other API errors: surface status without raw body details beyond message, but sanitized path already handled.
			return nil, fmt.Errorf("publish failed: GitHub API returned %d %s", apiErr.Status, apiErr.Message)
		}
		// Network errors: deterministic refusal naming remediation (never raw transport error with token)
		if strings.Contains(err.Error(), "cannot create the issue") || strings.Contains(err.Error(), "cannot build the issue request") {
			return nil, fmt.Errorf("publish failed: cannot reach GitHub — check network and retry")
		}
		if errors.Is(err, feedback.ErrNotFound) {
			return nil, fmt.Errorf("unknown feedback %q", id)
		}
		if strings.Contains(err.Error(), "already published") {
			return nil, err
		}
		return nil, err
	}
	// Ensure the command doesn't leak token via published issue URL? The URL is safe.
	return json.Marshal(feedbackPublishJSON{
		Schema:      feedbackPublishSchema,
		OK:          true,
		ID:          published.ID,
		IssueNumber: published.IssueNumber,
		IssueURL:    published.IssueURL,
	})
}
