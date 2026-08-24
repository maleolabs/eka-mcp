// Package mcp implements the MCP core of eka-mcp: a minimal MCP server
// (JSON-RPC 2.0) over a stdio transport, exposing the EKA capability
// layer (internal/eka) as MCP tools and resources.
//
// Layering:
//
//	MCP Transport (stdio)   transport.go — newline-delimited JSON-RPC
//	MCP Server (dispatch)   server.go    — JSON-RPC 2.0 request handling
//	EKA capability layer    internal/eka — wraps eka-core behind the
//	                                      Capability interface
//
// The transport and the server use the standard library only
// (encoding/json, bufio, os, fmt); the Capability interface keeps the
// server decoupled from eka-core — the server dispatches, the capability
// layer owns the EKA semantics.
package mcp

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/maleolabs/eka-mcp"
)

// ProtocolVersion is the MCP protocol version the server reports in the
// initialize handshake when the client does not announce one (the
// 2024-11-05 baseline of the MCP spec).
const ProtocolVersion = "2024-11-05"

// toolNames is the fixed deterministic tool order the server advertises
// in tools/list and dispatches in tools/call. The order is the contract.
var toolNames = []string{
	"context", "get", "domain", "status", "validate", "new", "publish",
	"transition", "note", "draft_read", "view", "draft_list", "integrity_check",
	"discard", "sync_push", "assign", "reassign", "unassign",
	"feedback_new", "feedback_list", "feedback_publish",
}

// ToolCount returns the number of tools the server exposes (including the
// deprecated alias — the capability count the banner reports).
func ToolCount() int { return len(toolNames) }

// ResourceCount returns the total number of resources the server exposes:
// eka://status (1) plus every embedded skill plus every draft template
// type. The counts are read from the embedded filesystem so the banner
// never drifts from the actual capability set. Deterministic.
func ResourceCount() (int, error) {
	skills, err := pack.SkillDirs()
	if err != nil {
		return 0, err
	}
	types, err := pack.TemplateTypes()
	if err != nil {
		return 0, err
	}
	return 1 + len(skills) + len(types), nil
}

// JSON-RPC 2.0 error codes (spec §5.1) plus the MCP extensions.
const (
	codeParseError       = -32700
	codeInvalidRequest   = -32600
	codeMethodNotFound   = -32601
	codeInvalidParams    = -32602
	codeInternalError    = -32603
	codeToolNotFound     = -32003
	codeResourceNotFound = -32002 // "Resource not found" (MCP extension)
)

// Capability is the EKA capability surface the MCP server dispatches
// to. It is implemented by internal/eka.Capability; the server knows
// nothing about eka-core — the interface keeps the layering explicit
// and the server stdlib-only.
type Capability interface {
	// Get resolves one identity form to its machine document.
	Get(form string) ([]byte, error)
	// Domain returns one Engineering Domain's units of a project as a
	// machine collection.
	Domain(projectID, domain string) ([]byte, error)
	// Status returns the workspace status as JSON.
	Status() ([]byte, error)
	// Context builds the Context Object around one subject at a depth
	// (schema eka-context-v1).
	Context(subject, projectID, depth string) ([]byte, error)
	// Validate runs the authoring conformance gate over a repository
	// root and returns the machine report (schema
	// eka-conformance-report-v1).
	Validate(root string) ([]byte, error)
	// NewDraft scaffolds one draft (schema eka-draft-v1).
	NewDraft(req NewDraftRequest) ([]byte, error)
	// Publish publishes one draft (schema eka-publish-result-v1).
	Publish(req PublishRequest) ([]byte, error)
	// Transition performs one transition (schema
	// eka-transition-result-v1); gate refusals surface as errors and
	// nothing is published.
	Transition(req TransitionRequest) ([]byte, error)
	// Note creates one cmt- note draft (schema eka-note-result-v1).
	Note(req NoteRequest) ([]byte, error)
	// DraftRead returns one draft file content verbatim (the v2.0 JSON
	// authoring document) — the editable draft behind a target. It is
	// the renamed MCP tool for draft reading (td:mcp-view-naming-fix);
	// deterministic and verbatim.
	DraftRead(target, project string) ([]byte, error)
	// View is the deprecated alias of DraftRead (td:mcp-view-naming-fix).
	// It remains functional for one minor version after 1.1.3 and will be
	// removed in the next minor. Use DraftRead. This alias exists to
	// avoid breaking existing MCP clients during migration.
	View(target, project string) ([]byte, error)
	// DraftList lists the draft backlog (schema eka-draft-list-v1).
	DraftList(project string) ([]byte, error)
	// IntegrityCheck verifies the canonical store (schema
	// eka-integrity-report-v1).
	IntegrityCheck() ([]byte, error)
	// Discard deletes one draft without publishing (schema
	// eka-discard-result-v1).
	Discard(target, project string) ([]byte, error)
	// SyncPush refreshes the repository snapshot from the workspace store
	// (push side of sync, schema eka-sync-push-result-v1). Same engine as
	// `eka sync push` (deterministic snapshot, same digest, same refusals);
	// a failed push writes nothing partially.
	SyncPush(repoPath string, adopt, override bool) ([]byte, error)
	// Assign sets the assigned-to edge of a work item (schema eka-assignment-v1).
	// Same semantics, validation and refusal classes as `eka assign`.
	Assign(req AssignmentRequest) ([]byte, error)
	// Reassign moves the assigned-to edge in one operation (schema eka-assignment-v1).
	// Same semantics as `eka reassign`.
	Reassign(req AssignmentRequest) ([]byte, error)
	// Unassign removes the assigned-to edge (schema eka-assignment-v1).
	// Same semantics as `eka unassign`.
	Unassign(req UnassignRequest) ([]byte, error)
	// FeedbackNew creates a local feedback draft under EKA_HOME/feedback
	// (YAML frontmatter + markdown body). It mirrors `eka feedback new`
	// semantics exactly — same validation, same scaffold, same id generation.
	// Feedback is meta-information about the tool (ADR-026) and never enters
	// the canonical store nor becomes a CKO.
	FeedbackNew(req FeedbackNewRequest) ([]byte, error)
	// FeedbackList lists all local feedback deterministically (schema
	// eka-feedback-list-v1), mirroring `eka feedback list`.
	FeedbackList() ([]byte, error)
	// FeedbackPublish files a feedback draft as a GitHub issue on the fixed
	// target repository, mirroring `eka feedback publish`. It inherits every
	// existing constraint (release-binary token gate, deterministic refusals,
	// token never appears in outputs).
	FeedbackPublish(req FeedbackPublishRequest) ([]byte, error)
}

// AuthorIdentity is the change-log authority of a write tool: the kind
// (user | agent | worker) plus the display name. The MCP boundary
// always resolves a non-empty identity — never the "Engineering"
// placeholder.
type AuthorIdentity struct {
	Kind string
	Name string
}

// Relationship is one authoring reference of a draft.
type Relationship struct {
	Type   string `json:"type"`
	Target string `json:"target"`
}

// NewDraftRequest describes one draft to scaffold.
type NewDraftRequest struct {
	Project       string
	Namespace     string
	Type          string
	ID            string
	Dimension     string
	Phase         string
	Domain        string
	By            AuthorIdentity
	Relationships []Relationship
	Content       map[string]any
}

// PublishRequest describes one publish run.
type PublishRequest struct {
	Target          string
	Project         string
	InstanceVersion int
}

// TransitionRequest describes one requested transition.
type TransitionRequest struct {
	RepoPath  string
	Target    string
	To        string
	Forward   bool
	Backward  bool
	By        AuthorIdentity
	Confirmed bool
}

// NoteRequest describes one note draft to create.
type NoteRequest struct {
	RepoPath string
	Target   string
	Role     string
	Domain   string
	By       AuthorIdentity
	Content  map[string]any
}

// AssignmentRequest describes one assign/reassign operation.
type AssignmentRequest struct {
	RepoPath string
	Target   string
	To       string
	By       AuthorIdentity
}

// UnassignRequest describes one unassign operation.
type UnassignRequest struct {
	RepoPath string
	Target   string
	By       AuthorIdentity
}

// FeedbackNewRequest describes one feedback draft to create (mirrors `eka feedback new`).
type FeedbackNewRequest struct {
	Type     string `json:"type"`
	Title    string `json:"title"`
	Severity string `json:"severity"`
	Source   string `json:"source"`
	Command  string `json:"command"`
	Content  string `json:"content"`
}

// FeedbackPublishRequest describes one feedback publish (mirrors `eka feedback publish`).
type FeedbackPublishRequest struct {
	ID string `json:"id"`
}

// Server is the MCP server: it dispatches JSON-RPC 2.0 requests to the
// capability layer. It is safe for one session at a time (stdio).
type Server struct {
	cap Capability
	// maxLineSize caps one stdio message line (defaultMaxLineSize).
	// Tests shrink it to exercise the boundary cheaply.
	maxLineSize int
}

// NewServer wires the server around one capability.
func NewServer(cap Capability) *Server {
	return &Server{cap: cap, maxLineSize: defaultMaxLineSize}
}

// request is a JSON-RPC 2.0 request object. ID is kept raw so string,
// numeric and null ids round-trip byte-exact; a notification is a
// request without an id.
type request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

// response is a JSON-RPC 2.0 response object (either result or error).
type response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

// rpcError is a JSON-RPC 2.0 error object.
type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// isNotification reports whether the request is a notification: it has
// no id member, so no response is expected.
func (r *request) isNotification() bool {
	return len(r.ID) == 0
}

// validID reports whether id is a JSON-RPC 2.0 id: a string, a number,
// or null (spec §4.2). An absent id (a notification) is valid. Objects,
// arrays and booleans are not ids — a request carrying one is invalid.
func validID(id json.RawMessage) bool {
	if len(id) == 0 {
		return true
	}
	switch id[0] {
	case '"', '0', '1', '2', '3', '4', '5', '6', '7', '8', '9', '-', 'n':
		return true
	}
	return false
}

// HandleMessage processes one JSON-RPC message and returns the response
// bytes to write, or nil when the message needs no response (a
// notification). It is the pure dispatch unit: parse, route, respond —
// it never touches I/O, so it is directly testable.
//
// The message must be exactly one JSON-RPC request object. A JSON-RPC
// batch (an array of requests) is rejected deterministically — the
// server processes one request per message and never splits or merges
// batches. Every error path returns a fixed, client-safe message (the
// CLI refusal-class policy): no Go internals, no paths, no store
// details.
func (s *Server) HandleMessage(line []byte) []byte {
	line = bytes.TrimSpace(line)
	if len(line) == 0 {
		return nil
	}
	var raw json.RawMessage
	if err := json.Unmarshal(line, &raw); err != nil {
		return parseErrorResponse()
	}
	if len(raw) == 0 {
		return parseErrorResponse()
	}
	switch raw[0] {
	case '[':
		// JSON-RPC batch: rejected deterministically.
		return s.errorResponse(json.RawMessage("null"), codeInvalidRequest, "batch requests are not supported")
	case '{':
		// A request object — proceed.
	default:
		return s.errorResponse(json.RawMessage("null"), codeInvalidRequest, "invalid request: expected a JSON-RPC request object")
	}
	var req request
	if err := json.Unmarshal(line, &req); err != nil {
		// Valid JSON object with malformed fields (e.g. a non-string
		// method): an invalid request, not a parse error.
		return s.errorResponse(json.RawMessage("null"), codeInvalidRequest, "invalid request: malformed request object")
	}
	if !validID(req.ID) {
		return s.errorResponse(json.RawMessage("null"), codeInvalidRequest, "invalid request: malformed request id")
	}
	if req.JSONRPC != "2.0" {
		return s.errorResponse(req.ID, codeInvalidRequest, `"jsonrpc" must be "2.0"`)
	}
	if req.Method == "" {
		return s.errorResponse(req.ID, codeInvalidRequest, "missing method")
	}
	if req.isNotification() {
		s.handleNotification(req)
		return nil
	}
	return s.dispatch(req)
}

// handleNotification absorbs a notification: MCP notifications
// (notifications/initialized, notifications/cancelled, …) carry no
// response by protocol. This milestone has nothing to do with them.
func (s *Server) handleNotification(req request) {
	// Intentionally empty: notifications are fire-and-forget.
}

// dispatch routes one request (with id) to its method handler.
func (s *Server) dispatch(req request) []byte {
	switch req.Method {
	case "initialize":
		return s.handleInitialize(req)
	case "ping":
		return s.resultResponse(req.ID, map[string]any{})
	case "tools/list":
		return s.handleToolsList(req)
	case "tools/call":
		return s.handleToolsCall(req)
	case "resources/list":
		return s.handleResourcesList(req)
	case "resources/read":
		return s.handleResourcesRead(req)
	default:
		return s.errorResponse(req.ID, codeMethodNotFound, "method not found: "+req.Method)
	}
}

// handleInitialize implements the MCP initialize handshake: it reports
// the protocol version (echoing the client's announced version, falling
// back to the server baseline), the server capabilities (tools +
// resources) and the server identity (name, version — the shared plugin
// version constant).
func (s *Server) handleInitialize(req request) []byte {
	var p struct {
		ProtocolVersion string `json:"protocolVersion"`
	}
	// Params may be absent or carry client fields we do not negotiate;
	// a missing version is not an error — we fall back to the baseline.
	_ = json.Unmarshal(req.Params, &p)
	version := p.ProtocolVersion
	if version == "" {
		version = ProtocolVersion
	}
	return s.resultResponse(req.ID, map[string]any{
		"protocolVersion": version,
		"capabilities": map[string]any{
			"tools":     map[string]any{},
			"resources": map[string]any{},
		},
		"serverInfo": map[string]any{
			"name":    pack.Name,
			"version": pack.Version,
		},
	})
}

// handleToolsList returns the tool set of the server: the EKA
// knowledge-retrieval, context and authoring surfaces as MCP tools,
// with JSON Schema input definitions. The order is fixed and
// deterministic (the acceptance contract of the milestone).
func (s *Server) handleToolsList(req request) []byte {
	tools := []any{
		map[string]any{
			"name": "context",
			"description": "Build the deterministic Engineering Context Object around one knowledge subject " +
				"(schema eka-context-v1): the focus in full detail, its instance-line history, and — at " +
				"dependency/engineering depth — the classified one-hop neighborhood and strata landscape.",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"subject": map[string]any{
						"type":        "string",
						"description": "Identity form of the focus, e.g. \"feather/adr:001-serialization:1\".",
					},
					"projectId": map[string]any{
						"type":        "string",
						"description": "The project for the issue-number lookup (optional).",
					},
					"depth": map[string]any{
						"type":        "string",
						"description": "Context depth: \"local\" (default), \"dependency\" or \"engineering\".",
					},
				},
				"required": []string{"subject"},
			},
		},
		map[string]any{
			"name": "get",
			"description": "Fetch one Canonical Knowledge Object (CKO) by identity form: " +
				"canonical \"<ns>/<type>:<id>:<v>\" or qualified line form \"<ns>/<type>:<id>\" " +
				"(the latest instance of the line). Returns the machine document (schema eka-cko-v2).",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"form": map[string]any{
						"type":        "string",
						"description": "Identity form to resolve, e.g. \"feather/adr:001-serialization:1\".",
					},
				},
				"required": []string{"form"},
			},
		},
		map[string]any{
			"name": "domain",
			"description": "Return every unit of one Engineering Domain of a project as a machine " +
				"collection (schema eka-cko-v2, sorted by canonical form).",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"projectId": map[string]any{
						"type":        "string",
						"description": "The project the knowledge belongs to.",
					},
					"domain": map[string]any{
						"type":        "string",
						"description": "The canonical Engineering Domain name, e.g. \"Architecture\".",
					},
				},
				"required": []string{"projectId", "domain"},
			},
		},
		map[string]any{
			"name": "status",
			"description": "Return the aggregated EKA workspace status: path, schema version, " +
				"registered projects, canonical store totals.",
			"inputSchema": map[string]any{
				"type":       "object",
				"properties": map[string]any{},
			},
		},
		map[string]any{
			"name": "validate",
			"description": "Run the authoring conformance gate over a repository and return the machine " +
				"report (schema eka-conformance-report-v1): scanned counts, blocking errors, warnings and " +
				"the deterministic findings (rules R0-R13).",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"root": map[string]any{
						"type":        "string",
						"description": "The repository root to validate (its docs/ tree is scanned).",
					},
				},
				"required": []string{"root"},
			},
		},
		map[string]any{
			"name": "new",
			"description": "Scaffold one draft in the workspace drafts tree (schema eka-draft-v1): the " +
				"deterministic v2.0 JSON authoring template with the type's owned state defaults and " +
				"required content keys. The change-log authority is the resolved agent identity.",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"project": map[string]any{
						"type":        "string",
						"description": "The project scope of the draft.",
					},
					"namespace": map[string]any{
						"type":        "string",
						"description": "The frontmatter namespace of the draft.",
					},
					"type": map[string]any{
						"type":        "string",
						"description": "The EKA artifact type token, e.g. \"adr\", \"sto\", \"cmt\".",
					},
					"id": map[string]any{
						"type":        "string",
						"description": "The draft id.",
					},
					"dimension": map[string]any{
						"type":        "string",
						"description": "Optional primary Knowledge Dimension.",
					},
					"phase": map[string]any{
						"type":        "string",
						"description": "Optional phase context (scp-/plan- only).",
					},
					"domain": map[string]any{
						"type":        "string",
						"description": "Optional declared Engineering Domain (canonical spelling).",
					},
					"by": map[string]any{
						"type":        "string",
						"description": "Optional change-log authority name (defaults to the agent identity).",
					},
					"byKind": map[string]any{
						"type":        "string",
						"description": "Optional authority kind: user | agent | worker (default agent).",
					},
					"relationships": map[string]any{
						"type": "array",
						"items": map[string]any{
							"type": "object",
							"properties": map[string]any{
								"type":   map[string]any{"type": "string"},
								"target": map[string]any{"type": "string"},
							},
						},
						"description": "Optional authoring references, e.g. {\"type\":\"depends-on\",\"target\":\"plan:x\"}.",
					},
					"content": map[string]any{
						"type":        "object",
						"description": "Optional JSON object merged over the type's required content keys.",
					},
				},
				"required": []string{"project", "namespace", "type", "id"},
			},
		},
		map[string]any{
			"name": "publish",
			"description": "Publish one draft as an immutable Canonical Knowledge Object (schema " +
				"eka-publish-result-v1). All-or-nothing: a failed validation or insert leaves the draft " +
				"untouched; the draft file is the single-use ticket.",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"target": map[string]any{
						"type":        "string",
						"description": "Draft target: \"<ns>/<type>:<id>\" or \"<type>:<id>\".",
					},
					"project": map[string]any{
						"type":        "string",
						"description": "Optional project scope (default: the cwd repository's project).",
					},
					"instanceVersion": map[string]any{
						"type":        "integer",
						"description": "Optional explicit instance version (must exceed the line's highest).",
					},
				},
				"required": []string{"target"},
			},
		},
		map[string]any{
			"name": "transition",
			"description": "Move a work item along the D1 transition table (or a plan/container/knowledge " +
				"artifact along its state table) and publish the transition in place (schema " +
				"eka-transition-result-v1). The R13 note gates and the active-container confirmation are " +
				"enforced by the Authoring API — a refused transition publishes nothing.",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"target": map[string]any{
						"type":        "string",
						"description": "The work item line: \"<ns>/<type>:<id>\" or \"<type>:<id>\".",
					},
					"to": map[string]any{
						"type":        "string",
						"description": "Explicit destination state (exactly one of to/forward/backward).",
					},
					"forward": map[string]any{
						"type":        "boolean",
						"description": "Take the next step of the D1 table.",
					},
					"backward": map[string]any{
						"type":        "boolean",
						"description": "Take the one-step pull-back.",
					},
					"repoPath": map[string]any{
						"type":        "string",
						"description": "Optional directory the repository is addressed from (default: the server cwd).",
					},
					"by": map[string]any{
						"type":        "string",
						"description": "Optional change-log authority name (defaults to the agent identity).",
					},
					"byKind": map[string]any{
						"type":        "string",
						"description": "Optional authority kind: user | agent | worker (default agent).",
					},
					"confirmed": map[string]any{
						"type":        "boolean",
						"description": "Pre-authorize the active-container confirmation gate.",
					},
				},
				"required": []string{"target"},
			},
		},
		map[string]any{
			"name": "note",
			"description": "Create one cmt- note draft discussing a subject (schema eka-note-result-v1): " +
				"the per-role template (implementation | review | fix) with the discusses relationship wired " +
				"to the resolved subject. The draft is visible to the R13 transition gates immediately.",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"target": map[string]any{
						"type":        "string",
						"description": "The note's subject: \"<ns>/<type>:<id>\" or \"<type>:<id>\".",
					},
					"role": map[string]any{
						"type":        "string",
						"description": "Note role: implementation | review | fix.",
					},
					"domain": map[string]any{
						"type":        "string",
						"description": "Optional declared Engineering Domain of the note.",
					},
					"repoPath": map[string]any{
						"type":        "string",
						"description": "Optional directory the repository is addressed from (default: the server cwd).",
					},
					"by": map[string]any{
						"type":        "string",
						"description": "Optional change-log authority name (defaults to the agent identity).",
					},
					"byKind": map[string]any{
						"type":        "string",
						"description": "Optional authority kind: user | agent | worker (default agent).",
					},
					"content": map[string]any{
						"type":        "object",
						"description": "Optional JSON object merged over the per-role note template.",
					},
				},
				"required": []string{"target", "role"},
			},
		},
		map[string]any{
			"name": "draft_read",
			"description": "Return one draft file content verbatim (the v2.0 JSON authoring document) — " +
				"the editable draft behind a target.",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"target": map[string]any{
						"type":        "string",
						"description": "Draft target: \"<ns>/<type>:<id>\" or \"<type>:<id>\".",
					},
					"project": map[string]any{
						"type":        "string",
						"description": "Optional project scope (default: the cwd repository's project).",
					},
				},
				"required": []string{"target"},
			},
		},
		// TODO(td:mcp-view-naming-fix): remove deprecated `view` alias in next minor version after 1.1.3.
		// It remains for one minor version as migration window for MCP clients.
		map[string]any{
			"name": "view",
			"description": "Deprecated: use draft_read. Return one draft file content verbatim (the v2.0 JSON authoring document) — " +
				"the editable draft behind a target. This alias will be removed in the next minor version; migrate to draft_read.",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"target": map[string]any{
						"type":        "string",
						"description": "Draft target: \"<ns>/<type>:<id>\" or \"<type>:<id>\".",
					},
					"project": map[string]any{
						"type":        "string",
						"description": "Optional project scope (default: the cwd repository's project).",
					},
				},
				"required": []string{"target"},
			},
		},
		map[string]any{
			"name": "draft_list",
			"description": "List the draft backlog of one project (or every project) as a machine list " +
				"(schema eka-draft-list-v1), ordered deterministically.",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"project": map[string]any{
						"type":        "string",
						"description": "Optional project scope (default: every project).",
					},
				},
			},
		},
		map[string]any{
			"name": "integrity_check",
			"description": "Verify the canonical store and return the deterministic integrity report " +
				"(schema eka-integrity-report-v1): scanned counts, retained-history orphans and every " +
				"detected violation (payload hashes, reference targets, attachment digests, registry).",
			"inputSchema": map[string]any{
				"type":       "object",
				"properties": map[string]any{},
			},
		},
		map[string]any{
			"name": "discard",
			"description": "Delete one draft file without publishing (schema eka-discard-result-v1). " +
				"The draft is gone; the identity is free for a new draft.",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"target": map[string]any{
						"type":        "string",
						"description": "Draft target: \"<ns>/<type>:<id>\" or \"<type>:<id>\".",
					},
					"project": map[string]any{
						"type":        "string",
						"description": "Optional project scope (default: the cwd repository's project).",
					},
				},
				"required": []string{"target"},
			},
		},
		map[string]any{
			"name": "sync_push",
			"description": "Push the repository snapshot from the workspace store (schema eka-sync-push-result-v1): " +
				"the push side of `eka sync push` — same snapshot compilation, same digest, same refusal classes; " +
				"crash-safe (staged in .snapshots-tmp, swapped atomically) so a failed push writes nothing partially. " +
				"Pull / --from-docs re-seed is not exposed (silent regression hazard); use CLI `eka sync pull`.",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"repoPath": map[string]any{
						"type":        "string",
						"description": "Optional repository path (default: the server cwd, same as CLI `eka sync push`).",
					},
					"adopt": map[string]any{
						"type":        "boolean",
						"description": "Adopt workspace-native units (source_repo=runtime, `eka publish` provenance) before pushing (ADR-032; same as `eka sync push --adopt`).",
					},
					"override": map[string]any{
						"type":        "boolean",
						"description": "Machine override to align the repository identity to the content namespace when they differ (same as `eka sync push --override`).",
					},
				},
			},
		},
		map[string]any{
			"name":        "assign",
			"description": "Assign a work item to a member (schema eka-assignment-v1): the assigned-to edge (work item -> member) is added — same engine as CLI `eka assign` — same target forms, same validation, same refusal classes including deterministic refusal when already assigned to a different member; idempotent on the same member. Single-assignee, deterministic; a failed assignment writes nothing.",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"target": map[string]any{
						"type":        "string",
						"description": "The work item line: \"<ns>/<type>:<id>\" or \"<type>:<id>\" (same as CLI `eka assign <item>`).",
					},
					"to": map[string]any{
						"type":        "string",
						"description": "The member line to assign to: \"<mbr-id>\", \"mbr:<id>\", \"mbr-<id>\", or \"<ns>/mbr:<id>\" (same as CLI --to).",
					},
					"repoPath": map[string]any{
						"type":        "string",
						"description": "Optional repository path (default: the server cwd).",
					},
					"by": map[string]any{
						"type":        "string",
						"description": "Optional change-log authority name (defaults to the agent identity).",
					},
					"byKind": map[string]any{
						"type":        "string",
						"description": "Optional authority kind: user | agent | worker (default agent).",
					},
				},
				"required": []string{"target", "to"},
			},
		},
		map[string]any{
			"name":        "reassign",
			"description": "Move a work item's assignment to another member in one operation (schema eka-assignment-v1): same engine as CLI `eka reassign` — same validation and refusal classes; deterministic refusal when not assigned (use assign) and idempotent on the same member; a failed reassign writes nothing.",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"target": map[string]any{
						"type":        "string",
						"description": "The work item line: \"<ns>/<type>:<id>\" or \"<type>:<id>\" (same as CLI `eka reassign <item>`).",
					},
					"to": map[string]any{
						"type":        "string",
						"description": "The member line to move to: \"<mbr-id>\", \"mbr:<id>\", \"mbr-<id>\", or \"<ns>/mbr:<id>\" (same as CLI --to).",
					},
					"repoPath": map[string]any{
						"type":        "string",
						"description": "Optional repository path (default: the server cwd).",
					},
					"by": map[string]any{
						"type":        "string",
						"description": "Optional change-log authority name (defaults to the agent identity).",
					},
					"byKind": map[string]any{
						"type":        "string",
						"description": "Optional authority kind: user | agent | worker (default agent).",
					},
				},
				"required": []string{"target", "to"},
			},
		},
		map[string]any{
			"name":        "unassign",
			"description": "Remove a work item's assigned-to edge (schema eka-assignment-v1): same engine as CLI `eka unassign` — same validation and refusal classes; deterministic no-op when already unassigned; a failed unassign writes nothing.",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"target": map[string]any{
						"type":        "string",
						"description": "The work item line: \"<ns>/<type>:<id>\" or \"<type>:<id>\" (same as CLI `eka unassign <item>`).",
					},
					"repoPath": map[string]any{
						"type":        "string",
						"description": "Optional repository path (default: the server cwd).",
					},
					"by": map[string]any{
						"type":        "string",
						"description": "Optional change-log authority name (defaults to the agent identity).",
					},
					"byKind": map[string]any{
						"type":        "string",
						"description": "Optional authority kind: user | agent | worker (default agent).",
					},
				},
				"required": []string{"target"},
			},
		},
		map[string]any{
			"name":        "feedback_new",
			"description": "Create a feedback draft under EKA_HOME/feedback (YAML frontmatter + markdown body) — schema eka-feedback-new-v1. Same engine as CLI `eka feedback new`: same validation, same per-type scaffold, same id generation (fbk-YYYYMMDD-slug), same 0600/0700 permissions. Feedback is meta-information about the tool (ADR-026) — it NEVER enters the canonical store and never becomes a CKO.",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"type": map[string]any{
						"type":        "string",
						"description": "Feedback type: bug, suggestion, improvement, or question (mirrors --type).",
					},
					"title": map[string]any{
						"type":        "string",
						"description": "Feedback title (mirrors --title).",
					},
					"severity": map[string]any{
						"type":        "string",
						"description": "Feedback severity: low, medium, or high (default low, mirrors --severity).",
					},
					"source": map[string]any{
						"type":        "string",
						"description": "Feedback source: human or agent (default human; agents should pass agent, mirrors --source).",
					},
					"command": map[string]any{
						"type":        "string",
						"description": "The invoked command recorded in the report (default mcp:feedback_new, mirrors --command).",
					},
					"content": map[string]any{
						"type":        "string",
						"description": "Markdown feedback body (mirrors --content-file contents inline); when omitted the per-type scaffold is used (bug: Steps/Expected/Actual, others: Description).",
					},
				},
				"required": []string{"type", "title"},
			},
		},
		map[string]any{
			"name":        "feedback_list",
			"description": "List all local feedback under EKA_HOME/feedback (schema eka-feedback-list-v1) — drafts and published, id descending (newest first, mirrors `eka feedback list`). Deterministic; the first malformed file fails the whole list naming the file. Feedback is meta-information outside the knowledge model — never a CKO, never part of the canonical store.",
			"inputSchema": map[string]any{
				"type":       "object",
				"properties": map[string]any{},
			},
		},
		map[string]any{
			"name":        "feedback_publish",
			"description": "Publish a feedback draft as a GitHub issue on the fixed target repository (schema eka-feedback-publish-v1) — same engine as CLI `eka feedback publish`. Inherited constraints: release-binary token gate (refuses with \"issue token not bundled — use a release binary\" when not a release), missing/invalid token refuses deterministically naming remediation (never raw HTTP error), token never appears in outputs/errors/logs, already-published refuses idempotently, unknown id refuses deterministically. Feedback never enters the canonical store.",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"id": map[string]any{
						"type":        "string",
						"description": "Feedback id to publish: fbk-YYYYMMDD-slug (with or without .md suffix, mirrors `eka feedback publish <id>`).",
					},
				},
				"required": []string{"id"},
			},
		},
	}
	return s.resultResponse(req.ID, map[string]any{"tools": tools})
}

// handleToolsCall executes one tool against the capability layer. Tool
// execution failures are reported as MCP tool results with isError=true
// (the MCP convention — the client renders the message); an unknown tool
// name is a JSON-RPC error (-32003 tool not found).
func (s *Server) handleToolsCall(req request) []byte {
	var p struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if err := json.Unmarshal(req.Params, &p); err != nil || p.Name == "" {
		return s.errorResponse(req.ID, codeInvalidParams, "tools/call requires {\"name\": string}")
	}
	text, err := s.callTool(p.Name, p.Arguments)
	if err != nil {
		if _, ok := err.(*toolNotFoundError); ok {
			return s.errorResponse(req.ID, codeToolNotFound, "tool not found: "+p.Name)
		}
		return s.resultResponse(req.ID, map[string]any{
			"content": []any{map[string]any{"type": "text", "text": SanitizeError(err)}},
			"isError": true,
		})
	}
	return s.resultResponse(req.ID, map[string]any{
		"content": []any{map[string]any{"type": "text", "text": text}},
		"isError": false,
	})
}

// toolNotFoundError marks an unknown tool name (JSON-RPC -32003, not a
// tool execution failure).
type toolNotFoundError struct{ name string }

func (e *toolNotFoundError) Error() string { return "tool not found: " + e.name }

// callTool dispatches one named tool call to the capability layer. The
// returned text is the MCP tool text content.
func (s *Server) callTool(name string, args json.RawMessage) (string, error) {
	switch name {
	case "context":
		var p struct {
			Subject   string `json:"subject"`
			ProjectID string `json:"projectId"`
			Depth     string `json:"depth"`
		}
		if err := json.Unmarshal(args, &p); err != nil || p.Subject == "" {
			return "", fmt.Errorf("context requires {\"subject\": string}")
		}
		if p.Depth == "" {
			p.Depth = "local"
		}
		data, err := s.cap.Context(p.Subject, p.ProjectID, p.Depth)
		if err != nil {
			return "", err
		}
		return string(data), nil
	case "get":
		var p struct {
			Form string `json:"form"`
		}
		if err := json.Unmarshal(args, &p); err != nil || p.Form == "" {
			return "", fmt.Errorf("get requires {\"form\": string}")
		}
		data, err := s.cap.Get(p.Form)
		if err != nil {
			return "", err
		}
		return string(data), nil
	case "domain":
		var p struct {
			ProjectID string `json:"projectId"`
			Domain    string `json:"domain"`
		}
		if err := json.Unmarshal(args, &p); err != nil || p.ProjectID == "" || p.Domain == "" {
			return "", fmt.Errorf("domain requires {\"projectId\": string, \"domain\": string}")
		}
		data, err := s.cap.Domain(p.ProjectID, p.Domain)
		if err != nil {
			return "", err
		}
		return string(data), nil
	case "status":
		data, err := s.cap.Status()
		if err != nil {
			return "", err
		}
		return string(data), nil
	case "validate":
		var p struct {
			Root string `json:"root"`
		}
		if err := json.Unmarshal(args, &p); err != nil || p.Root == "" {
			return "", fmt.Errorf("validate requires {\"root\": string}")
		}
		data, err := s.cap.Validate(p.Root)
		if err != nil {
			return "", err
		}
		return string(data), nil
	case "new":
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
		if err := json.Unmarshal(args, &p); err != nil || p.Project == "" || p.Namespace == "" || p.Type == "" || p.ID == "" {
			return "", fmt.Errorf("new requires {\"project\": string, \"namespace\": string, \"type\": string, \"id\": string}")
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
	case "publish":
		var p struct {
			Target          string `json:"target"`
			Project         string `json:"project"`
			InstanceVersion int    `json:"instanceVersion"`
		}
		if err := json.Unmarshal(args, &p); err != nil || p.Target == "" {
			return "", fmt.Errorf("publish requires {\"target\": string}")
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
	case "transition":
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
		if err := json.Unmarshal(args, &p); err != nil || p.Target == "" {
			return "", fmt.Errorf("transition requires {\"target\": string}")
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
	case "note":
		var p struct {
			RepoPath string          `json:"repoPath"`
			Target   string          `json:"target"`
			Role     string          `json:"role"`
			Domain   string          `json:"domain"`
			By       string          `json:"by"`
			ByKind   string          `json:"byKind"`
			Content  json.RawMessage `json:"content"`
		}
		if err := json.Unmarshal(args, &p); err != nil || p.Target == "" || p.Role == "" {
			return "", fmt.Errorf("note requires {\"target\": string, \"role\": string}")
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
	case "draft_read":
		var p struct {
			Target  string `json:"target"`
			Project string `json:"project"`
		}
		if err := json.Unmarshal(args, &p); err != nil || p.Target == "" {
			return "", fmt.Errorf("draft_read requires {\"target\": string}")
		}
		data, err := s.cap.DraftRead(p.Target, p.Project)
		if err != nil {
			return "", err
		}
		return string(data), nil
	case "view":
		// Deprecated alias for draft_read (td:mcp-view-naming-fix).
		// TODO(td:mcp-view-naming-fix): remove in next minor version after 1.1.3 — then delete this case and the View method.
		var p struct {
			Target  string `json:"target"`
			Project string `json:"project"`
		}
		if err := json.Unmarshal(args, &p); err != nil || p.Target == "" {
			return "", fmt.Errorf("view requires {\"target\": string}")
		}
		data, err := s.cap.View(p.Target, p.Project)
		if err != nil {
			return "", err
		}
		return string(data), nil
	case "draft_list":
		var p struct {
			Project string `json:"project"`
		}
		_ = json.Unmarshal(args, &p)
		data, err := s.cap.DraftList(p.Project)
		if err != nil {
			return "", err
		}
		return string(data), nil
	case "integrity_check":
		data, err := s.cap.IntegrityCheck()
		if err != nil {
			return "", err
		}
		return string(data), nil
	case "discard":
		var p struct {
			Target  string `json:"target"`
			Project string `json:"project"`
		}
		if err := json.Unmarshal(args, &p); err != nil || p.Target == "" {
			return "", fmt.Errorf("discard requires {\"target\": string}")
		}
		data, err := s.cap.Discard(p.Target, p.Project)
		if err != nil {
			return "", err
		}
		return string(data), nil
	case "sync_push":
		// Deterministic refusal for pull re-seed attempts via MCP (AC #3):
		// the MCP surface is push-only; `eka sync pull` / --from-docs stays
		// operator-supervised CLI because it re-points line references to
		// older instances (silent regression hazard).
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
		if len(args) != 0 && string(args) != "null" {
			if err := json.Unmarshal(args, &p); err != nil {
				return "", fmt.Errorf("sync_push requires {\"repoPath\": string, \"adopt\": boolean, \"override\": boolean} (all optional)")
			}
		}
		data, err := s.cap.SyncPush(p.RepoPath, p.Adopt, p.Override)
		if err != nil {
			return "", err
		}
		return string(data), nil
	case "assign":
		var p struct {
			Target   string `json:"target"`
			To       string `json:"to"`
			RepoPath string `json:"repoPath"`
			By       string `json:"by"`
			ByKind   string `json:"byKind"`
		}
		if err := json.Unmarshal(args, &p); err != nil || p.Target == "" || p.To == "" {
			return "", fmt.Errorf("assign requires {\"target\": string, \"to\": string}")
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
	case "reassign":
		var p struct {
			Target   string `json:"target"`
			To       string `json:"to"`
			RepoPath string `json:"repoPath"`
			By       string `json:"by"`
			ByKind   string `json:"byKind"`
		}
		if err := json.Unmarshal(args, &p); err != nil || p.Target == "" || p.To == "" {
			return "", fmt.Errorf("reassign requires {\"target\": string, \"to\": string}")
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
	case "unassign":
		var p struct {
			Target   string `json:"target"`
			RepoPath string `json:"repoPath"`
			By       string `json:"by"`
			ByKind   string `json:"byKind"`
		}
		if err := json.Unmarshal(args, &p); err != nil || p.Target == "" {
			return "", fmt.Errorf("unassign requires {\"target\": string}")
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
	case "feedback_new":
		var p struct {
			Type     string `json:"type"`
			Title    string `json:"title"`
			Severity string `json:"severity"`
			Source   string `json:"source"`
			Command  string `json:"command"`
			Content  string `json:"content"`
		}
		if err := json.Unmarshal(args, &p); err != nil || strings.TrimSpace(p.Type) == "" || strings.TrimSpace(p.Title) == "" {
			return "", fmt.Errorf("feedback_new requires {\"type\": string, \"title\": string}")
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
	case "feedback_list":
		// No required args — empty object or absent args both list.
		if len(args) != 0 && string(args) != "null" && strings.TrimSpace(string(args)) != "{}" {
			// Allow any object but validate it's an object to fuzz deterministically.
			var raw map[string]json.RawMessage
			if err := json.Unmarshal(args, &raw); err != nil {
				return "", fmt.Errorf("feedback_list requires {}")
			}
			// Unknown fields are ignored (deterministic), but non-object already refused.
		}
		data, err := s.cap.FeedbackList()
		if err != nil {
			return "", err
		}
		return string(data), nil
	case "feedback_publish":
		var p struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(args, &p); err != nil || strings.TrimSpace(p.ID) == "" {
			return "", fmt.Errorf("feedback_publish requires {\"id\": string}")
		}
		data, err := s.cap.FeedbackPublish(FeedbackPublishRequest{ID: p.ID})
		if err != nil {
			return "", err
		}
		return string(data), nil
	default:
		return "", &toolNotFoundError{name: name}
	}
}

// defaultAgentIdentity is the deterministic agent identity the MCP
// boundary stamps on write tools when the client does not pass one —
// the boundary never falls back to the "Engineering" placeholder.
var defaultAgentIdentity = AuthorIdentity{Kind: "agent", Name: "mcp-agent"}

// resolveAuthor resolves the change-log authority of a write tool: the
// client's by/byKind when a name is given (the kind defaults to agent —
// the MCP boundary is the agent interface), else the deterministic
// default agent identity. The result is never empty-named, so the
// eka-core "Engineering" fallback can never trigger.
func resolveAuthor(by, byKind string) (AuthorIdentity, error) {
	name := strings.TrimSpace(by)
	if name == "" {
		return defaultAgentIdentity, nil
	}
	kind := strings.TrimSpace(byKind)
	if kind == "" {
		kind = "agent"
	}
	if !isAuthorKind(kind) {
		return AuthorIdentity{}, fmt.Errorf("unknown author kind %q (allowed: user, agent, worker)", byKind)
	}
	return AuthorIdentity{Kind: kind, Name: name}, nil
}

// isAuthorKind reports whether kind is one of the three canonical
// author identity kinds (user | agent | worker).
func isAuthorKind(kind string) bool {
	switch kind {
	case "user", "agent", "worker":
		return true
	}
	return false
}

// parseContent decodes the optional content object of a write tool: an
// absent or null value is nil (the empty template scaffolds), anything
// that is not a JSON object is refused deterministically.
func parseContent(raw json.RawMessage) (map[string]any, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, fmt.Errorf("content must be a JSON object")
	}
	return m, nil
}

// handleResourcesList returns the resource set of the server: the
// workspace status resource (a real, readable view over eka-core) plus
// the read-only embedded pack resources — every skill's SKILL.md and
// every draft template type. The enumeration is deterministic (the
// embedded filesystem is the source of truth). Skill resource
// descriptions come from the SKILL.md frontmatter
// (req:agent-agnostic-skill-pack R9); a skill without a parseable
// description degrades to the generic form.
func (s *Server) handleResourcesList(req request) []byte {
	resources := []any{
		map[string]any{
			"uri":         "eka://status",
			"name":        "EKA workspace status",
			"description": "The aggregated EKA workspace status: path, schema version, projects, canonical store totals.",
			"mimeType":    "application/json",
		},
	}
	skills, err := pack.SkillDirs()
	if err != nil {
		return s.errorResponse(req.ID, codeInternalError, "listing skills: "+SanitizeError(err))
	}
	for _, name := range skills {
		description := "The SKILL.md of the embedded " + name + " skill (read-only)."
		if d, err := pack.SkillDescription(name); err == nil && d != "" {
			description = d
		}
		resources = append(resources, map[string]any{
			"uri":         "eka://skills/" + name,
			"name":        "EKA skill " + name,
			"description": description,
			"mimeType":    "text/markdown",
		})
	}
	types, err := pack.TemplateTypes()
	if err != nil {
		return s.errorResponse(req.ID, codeInternalError, "listing templates: "+SanitizeError(err))
	}
	for _, t := range types {
		resources = append(resources, map[string]any{
			"uri":         "eka://templates/" + t,
			"name":        "EKA draft template " + t,
			"description": "The v2.0 JSON draft template of type " + t + " (read-only).",
			"mimeType":    "application/json",
		})
	}
	return s.resultResponse(req.ID, map[string]any{"resources": resources})
}

// handleResourcesRead reads one resource URI: eka://status (the status
// read from the capability layer) or the read-only embedded pack
// resources eka://skills/<name> and eka://templates/<type>.
func (s *Server) handleResourcesRead(req request) []byte {
	var p struct {
		URI string `json:"uri"`
	}
	if err := json.Unmarshal(req.Params, &p); err != nil || p.URI == "" {
		return s.errorResponse(req.ID, codeInvalidParams, "resources/read requires {\"uri\": string}")
	}
	switch {
	case p.URI == "eka://status":
		data, err := s.cap.Status()
		if err != nil {
			return s.errorResponse(req.ID, codeInternalError, "reading eka://status: "+SanitizeError(err))
		}
		return s.resultResponse(req.ID, map[string]any{
			"contents": []any{
				map[string]any{
					"uri":      p.URI,
					"mimeType": "application/json",
					"text":     string(data),
				},
			},
		})
	case strings.HasPrefix(p.URI, "eka://skills/"):
		name := strings.TrimPrefix(p.URI, "eka://skills/")
		data, err := pack.SkillFile(name)
		if err != nil {
			return s.errorResponse(req.ID, codeResourceNotFound, "resource not found: "+p.URI)
		}
		return s.resultResponse(req.ID, map[string]any{
			"contents": []any{
				map[string]any{
					"uri":      p.URI,
					"mimeType": "text/markdown",
					"text":     string(data),
				},
			},
		})
	case strings.HasPrefix(p.URI, "eka://templates/"):
		typeToken := strings.TrimPrefix(p.URI, "eka://templates/")
		data, err := pack.TemplateFile(typeToken)
		if err != nil {
			return s.errorResponse(req.ID, codeResourceNotFound, "resource not found: "+p.URI)
		}
		return s.resultResponse(req.ID, map[string]any{
			"contents": []any{
				map[string]any{
					"uri":      p.URI,
					"mimeType": "application/json",
					"text":     string(data),
				},
			},
		})
	default:
		return s.errorResponse(req.ID, codeResourceNotFound, "resource not found: "+p.URI)
	}
}

// resultResponse builds a success response.
func (s *Server) resultResponse(id json.RawMessage, result any) []byte {
	return marshalResponse(response{JSONRPC: "2.0", ID: id, Result: result})
}

// errorResponse builds an error response. When the request carried no
// id (or an undetectable one), the response id is null — JSON-RPC 2.0
// §4.3: the id MUST be Null when the detection of the id failed.
func (s *Server) errorResponse(id json.RawMessage, code int, msg string) []byte {
	if len(id) == 0 {
		id = json.RawMessage("null")
	}
	return marshalResponse(response{
		JSONRPC: "2.0",
		ID:      id,
		Error:   &rpcError{Code: code, Message: msg},
	})
}

// parseErrorResponse is the deterministic response for JSON that does
// not parse: a fixed message with no Go internals (the JSON parser's
// errors are implementation details and must never reach the client).
func parseErrorResponse() []byte {
	return marshalResponse(response{
		JSONRPC: "2.0",
		ID:      json.RawMessage("null"),
		Error:   &rpcError{Code: codeParseError, Message: "parse error: invalid JSON"},
	})
}

// pathRe matches file paths in error messages: absolute paths
// (/home/…, C:\…), tilde paths (~/…), relative paths (./…, ../…)
// and any slash- or backslash-separated run that looks like a path.
// Segments may contain spaces (a parent directory can be "john doe");
// the second-slash requirement anchors matches, so single-slash tokens
// like "a/b" survive. The MCP boundary must never leak store paths or
// workspace locations.
var pathRe = regexp.MustCompile(`(?:[A-Za-z]:[\\/]|~[\\/]|\.{1,2}[\\/]|[\\/])[^\r\n"':]*[\\/][^\r\n"':]*`)

// relPathRe matches dot-less relative paths whose last segment looks
// like a file (contains a dot), e.g. "data/store.db". The first segment
// excludes whitespace so a leading space before the path survives.
// Single-slash tokens without a file-like last segment (e.g.
// "feather/adr:…" identity forms) survive.
var relPathRe = regexp.MustCompile(`[^\r\n"':/\\ ]*[\\/][^\r\n"':/\\]*\.[^\r\n"':/\\]*`)

// SanitizeError reduces an error to a deterministic, client-safe
// refusal-class message: first line only (stack traces and wrapped
// context live below), no file paths, no store details. The result is
// stable for a given error, so clients can match on it. It is the
// shared policy of the MCP boundary and the executable's startup
// errors (stderr is captured by the MCP client).
func SanitizeError(err error) string {
	msg := err.Error()
	if i := strings.IndexByte(msg, '\n'); i >= 0 {
		msg = msg[:i]
	}
	msg = pathRe.ReplaceAllString(msg, "<path>")
	msg = relPathRe.ReplaceAllString(msg, "<path>")
	msg = strings.TrimSpace(msg)
	if msg == "" {
		return "tool execution failed"
	}
	return msg
}

// marshalResponse serializes a response; it cannot fail for the shapes
// this package builds (no cycles, no channels), so the error is dropped
// deterministically to nil.
func marshalResponse(r response) []byte {
	out, err := json.Marshal(r)
	if err != nil {
		return []byte(`{"jsonrpc":"2.0","id":null,"error":{"code":-32603,"message":"internal error"}}`)
	}
	return out
}
