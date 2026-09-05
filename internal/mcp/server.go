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
	"errors"
	"fmt"
	"io"
	"os"
	"reflect"
	"regexp"
	"strings"

	"github.com/maleolabs/eka-mcp"
)

// ProtocolVersion is the MCP protocol version the server reports in the
// initialize handshake when the client does not announce one (the
// 2024-11-05 baseline of the MCP spec).
const ProtocolVersion = "2024-11-05"

// Risk classes for approval policy (single authoritative classification).
const (
	RiskRead           = "read"
	RiskLocalWrite     = "local-write"
	RiskCanonicalWrite = "canonical-write"
	RiskExternal       = "external"
)

// toolDescriptor is the single authoritative declaration for one MCP tool:
// name, description, risk class, strict JSON schema, deprecation marker
// and deterministic order. Both the advertised tools/list schemas and the
// runtime validation (decodeToolArgs) derive from this source, so schema
// and enforcement cannot drift (bug:mcp-new-false-refusal).
type toolDescriptor struct {
	Name        string
	Description string
	RiskClass   string
	Required    []string
	Properties  map[string]any
	Deprecated  bool
}

// toolDescriptors is THE authoritative tool catalog — the fixed
// deterministic order the server advertises and dispatches. Strict schemas
// use required+types+enums+bounds and additionalProperties:false. Risk
// classes: read | local-write | canonical-write | external.
var toolDescriptors = []toolDescriptor{
	{
		Name:        "context",
		RiskClass:   RiskRead,
		Required:    []string{"subject"},
		Description: "Build the deterministic Engineering Context Object around one knowledge subject (schema eka-context-v1): the focus in full detail, its instance-line history, and — at dependency/engineering depth — the classified one-hop neighborhood and strata landscape.",
		Properties: map[string]any{
			"subject":   map[string]any{"type": "string", "description": "Identity form of the focus, e.g. \"feather/adr:001-serialization:1\"."},
			"projectId": map[string]any{"type": "string", "description": "The project for the issue-number lookup (optional)."},
			"depth":     map[string]any{"type": "string", "enum": []string{"local", "dependency", "engineering"}, "description": "Context depth: \"local\" (default), \"dependency\" or \"engineering\"."},
		},
	},
	{
		Name:        "code_context",
		RiskClass:   RiskRead,
		Required:    []string{"root"},
		Description: "Build bounded deterministic source context from the local code graph. Returns schema eka/code-context/1 with file inventory, symbols, imports and optional source content.",
		Properties: map[string]any{
			"root":      map[string]any{"type": "string", "description": "Repository root to index (logical relative path, no absolute host path)."},
			"focus":     map[string]any{"type": "string", "description": "Optional file path or symbol focus."},
			"depth":     map[string]any{"type": "string", "enum": []string{"local", "dependency", "engineering"}, "description": "Context depth for code graph."},
			"level":     map[string]any{"type": "integer", "minimum": 0, "maximum": 3, "description": "Expansion level 0-3."},
			"noContent": map[string]any{"type": "boolean", "description": "When true omits source content for payload economy."},
		},
	},
	{
		Name:        "code_discover",
		RiskClass:   RiskRead,
		Required:    []string{"root", "query"},
		Description: "Discover code candidates deterministically from a natural-language query and optional scope filter. Returns schema eka/code-discover/1 with bounded candidates carrying reason and confidence (language-agnostic inventory; unsupported files remain inventory entries; fallback to bounded inventory when no match; no RAG canonical).",
		Properties: map[string]any{
			"root":  map[string]any{"type": "string", "description": "Repository root to index (logical relative path)."},
			"query": map[string]any{"type": "string", "minLength": 1, "description": "Natural-language query (tokens matched deterministically against file paths, symbol names and imports)."},
			"scope": map[string]any{"type": "string", "description": "Optional file path scope filter (substring, case-insensitive)."},
			"limit": map[string]any{"type": "integer", "minimum": 1, "maximum": 64, "description": "Max candidates 1-64 (default 16)."},
		},
	},
	{
		Name:        "code_get",
		RiskClass:   RiskRead,
		Required:    []string{"root", "path"},
		Description: "Retrieve exact file content deterministically by slash path. Returns schema eka/code-get/1 with file unit, symbols and imports (language-agnostic; deterministic exact path lookup).",
		Properties: map[string]any{
			"root": map[string]any{"type": "string", "description": "Repository root to index (logical relative path)."},
			"path": map[string]any{"type": "string", "minLength": 1, "description": "Slash path of the file to retrieve (exact, relative to root, logical)."},
		},
	},
	{
		Name:        "get",
		RiskClass:   RiskRead,
		Required:    []string{"form"},
		Description: "Fetch one Canonical Knowledge Object (CKO) by identity form: canonical \"<ns>/<type>:<id>:<v>\" or qualified line form \"<ns>/<type>:<id>\" (the latest instance of the line). Returns the machine document (schema eka-cko-v2). Supports noContent:true to strip the content payload via machine.Document.StripContent at parity with CLI --no-content (content absent, identity/stateVector/relationships intact) for payload economy. Default false (full payloads).",
		Properties: map[string]any{
			"form":      map[string]any{"type": "string", "minLength": 1, "description": "Identity form to resolve, e.g. \"feather/adr:001-serialization:1\"."},
			"noContent": map[string]any{"type": "boolean", "description": "When true, strips the content payload via machine.Document.StripContent (identity/stateVector/relationships intact, content absent) — parity with CLI --no-content. Default false (full payloads)."},
		},
	},
	{
		Name:        "domain",
		RiskClass:   RiskRead,
		Required:    []string{"projectId", "domain"},
		Description: "Return every unit of one Engineering Domain of a project as a machine collection (schema eka-cko-v2, sorted by canonical form). Supports noContent:true to strip each unit's content payload via machine.Document.StripContent at parity with CLI --no-content (content absent per unit, identity/stateVector/relationships intact) for payload economy. Default false (full payloads).",
		Properties: map[string]any{
			"projectId": map[string]any{"type": "string", "minLength": 1, "description": "The project the knowledge belongs to."},
			"domain":    map[string]any{"type": "string", "enum": []string{"Architecture", "Planning", "Execution", "Operations", "Knowledge"}, "description": "The canonical Engineering Domain name, e.g. \"Architecture\"."},
			"noContent": map[string]any{"type": "boolean", "description": "When true, strips each unit's content payload via machine.Document.StripContent (identity/stateVector/relationships intact, content absent per unit) — parity with CLI --no-content. Default false (full payloads)."},
		},
	},
	{
		Name:        "status",
		RiskClass:   RiskRead,
		Required:    nil,
		Description: "Return the aggregated EKA workspace status: path, schema version, registered projects, canonical store totals. Path is logical/relative (no absolute host path).",
		Properties:  map[string]any{},
	},
	{
		Name:        "validate",
		RiskClass:   RiskRead,
		Required:    []string{"root"},
		Description: "Run the authoring conformance gate over a repository and return the machine report (schema eka-conformance-report-v1): scanned counts, blocking errors, warnings and the deterministic findings (rules R0-R13).",
		Properties: map[string]any{
			"root": map[string]any{"type": "string", "minLength": 1, "description": "The repository root to validate (its docs/ tree is scanned) — logical relative path."},
		},
	},
	{
		Name:        "new",
		RiskClass:   RiskLocalWrite,
		Required:    []string{"project", "namespace", "type", "id"},
		Description: "Scaffold one draft in the workspace drafts tree (schema eka-draft-v1): the deterministic v2.0 JSON authoring template with the type's owned state defaults and required content keys. The change-log authority is the resolved agent identity.",
		Properties: map[string]any{
			"project":   map[string]any{"type": "string", "minLength": 1, "description": "The project scope of the draft."},
			"namespace": map[string]any{"type": "string", "minLength": 1, "description": "The frontmatter namespace of the draft."},
			"type":      map[string]any{"type": "string", "minLength": 1, "description": "The EKA artifact type token, e.g. \"adr\", \"sto\", \"cmt\"."},
			"id":        map[string]any{"type": "string", "minLength": 1, "description": "The draft id."},
			"dimension": map[string]any{"type": "string", "description": "Optional primary Knowledge Dimension."},
			"phase":     map[string]any{"type": "string", "description": "Optional phase context (scp-/plan- only)."},
			"domain":    map[string]any{"type": "string", "description": "Optional declared Engineering Domain (canonical spelling)."},
			"by":        map[string]any{"type": "string", "description": "Optional change-log authority name (defaults to the agent identity)."},
			"byKind":    map[string]any{"type": "string", "enum": []string{"user", "agent", "worker"}, "description": "Optional authority kind: user | agent | worker (default agent)."},
			"relationships": map[string]any{
				"type": "array", "description": "Optional authoring references, e.g. {\"type\":\"depends-on\",\"target\":\"plan:x\"}.",
				"items": map[string]any{"type": "object", "properties": map[string]any{"type": map[string]any{"type": "string"}, "target": map[string]any{"type": "string"}}, "required": []string{"type", "target"}, "additionalProperties": false},
			},
			"content": map[string]any{"type": "object", "description": "Optional JSON object merged over the type's required content keys."},
		},
	},
	{
		Name:        "draft_update",
		RiskClass:   RiskLocalWrite,
		Required:    []string{"target"},
		Description: "Apply a partial content merge to one pending draft — read-modify-write through draft_read (schema eka-draft-update-v1) via the runtime-owned Authoring API (not direct store). Supplied content keys overwrite/add; keys not mentioned are preserved. Publish still validates — no validation happens here. Supports optimistic concurrency via expectedRevision (revision integer) and expectedHash (sha256 hex of the draft file); mismatches refuse with a deterministic conflict (re-read with draft_read and retry).",
		Properties: map[string]any{
			"target":           map[string]any{"type": "string", "minLength": 1, "description": "Draft target: \"<ns>/<type>:<id>\" or \"<type>:<id>\"."},
			"project":          map[string]any{"type": "string", "description": "Optional project scope (default: the cwd repository's project)."},
			"content":          map[string]any{"type": "object", "minProperties": 1, "description": "Partial content object to merge into the draft's content — keys overwrite/add, absent keys are preserved. Must be non-empty object."},
			"expectedRevision": map[string]any{"type": "integer", "minimum": 1, "description": "Optional optimistic concurrency guard: expected revision of the draft (refuses with conflict if the on-disk revision differs — re-read with draft_read and retry)."},
			"expectedHash":     map[string]any{"type": "string", "pattern": "^[a-fA-F0-9]{64}$", "description": "Optional optimistic concurrency guard: expected sha256 hex of the draft file (64 hex chars; refuses with conflict if the on-disk hash differs)."},
		},
	},
	{
		Name:        "publish",
		RiskClass:   RiskCanonicalWrite,
		Required:    []string{"target"},
		Description: "Publish one draft as an immutable Canonical Knowledge Object (schema eka-publish-result-v1). All-or-nothing: a failed validation or insert leaves the draft untouched; the draft file is the single-use ticket. Single-draft only — for batch use publishBatch.",
		Properties: map[string]any{
			"target":          map[string]any{"type": "string", "minLength": 1, "description": "Draft target: \"<ns>/<type>:<id>\" or \"<type>:<id>\" — required for single publish."},
			"project":         map[string]any{"type": "string", "description": "Optional project scope (default: the cwd repository's project)."},
			"instanceVersion": map[string]any{"type": "integer", "minimum": 1, "description": "Optional explicit instance version (must exceed the line's highest)."},
		},
	},
	{
		Name:        "publishBatch",
		RiskClass:   RiskCanonicalWrite,
		Required:    nil,
		Description: "Publish every pending draft of the project in topological order (schema eka-publish-batch-v1) — same engine as `eka publish --all` (Kahn's algorithm, cycle and dangling-reference pre-flight refusals, per-draft atomic validation); empty backlog is a valid no-op. Batch counterpart of publish.",
		Properties: map[string]any{
			"project": map[string]any{"type": "string", "description": "Optional project scope (default: the cwd repository's project)."},
			"all":     map[string]any{"type": "boolean", "description": "Batch mode flag — synonym of pending, parity with `eka publish --all` (optional, defaults to true when publishBatch is called)."},
			"pending": map[string]any{"type": "boolean", "description": "Batch mode synonym of all — publish every pending draft in topological order."},
		},
	},
	{
		Name:        "transition",
		RiskClass:   RiskCanonicalWrite,
		Required:    []string{"target"},
		Description: "Move a work item along the D1 transition table (or a plan/container/knowledge artifact along its state table) and publish the transition in place (schema eka-transition-result-v1). The R13 note gates and the active-container confirmation are enforced by the Authoring API — a refused transition publishes nothing.",
		Properties: map[string]any{
			"target":    map[string]any{"type": "string", "minLength": 1, "description": "The work item line: \"<ns>/<type>:<id>\" or \"<type>:<id>\"."},
			"to":        map[string]any{"type": "string", "description": "Explicit destination state (exactly one of to/forward/backward)."},
			"forward":   map[string]any{"type": "boolean", "description": "Take the next step of the D1 table."},
			"backward":  map[string]any{"type": "boolean", "description": "Take the one-step pull-back."},
			"repoPath":  map[string]any{"type": "string", "description": "Optional directory the repository is addressed from (default: the server cwd) — logical relative path."},
			"by":        map[string]any{"type": "string", "description": "Optional change-log authority name (defaults to the agent identity)."},
			"byKind":    map[string]any{"type": "string", "enum": []string{"user", "agent", "worker"}, "description": "Optional authority kind: user | agent | worker (default agent)."},
			"confirmed": map[string]any{"type": "boolean", "description": "Pre-authorize the active-container confirmation gate."},
		},
	},
	{
		Name:        "note",
		RiskClass:   RiskLocalWrite,
		Required:    []string{"target", "role"},
		Description: "Create one cmt- note draft discussing a subject (schema eka-note-result-v1): the per-role template (implementation | review | fix) with the discusses relationship wired to the resolved subject. The draft is visible to the R13 transition gates immediately.",
		Properties: map[string]any{
			"target":   map[string]any{"type": "string", "minLength": 1, "description": "The note's subject: \"<ns>/<type>:<id>\" or \"<type>:<id>\"."},
			"role":     map[string]any{"type": "string", "enum": []string{"implementation", "review", "fix"}, "description": "Note role: implementation | review | fix."},
			"domain":   map[string]any{"type": "string", "description": "Optional declared Engineering Domain of the note."},
			"repoPath": map[string]any{"type": "string", "description": "Optional directory the repository is addressed from (default: the server cwd) — logical relative path."},
			"by":       map[string]any{"type": "string", "description": "Optional change-log authority name (defaults to the agent identity)."},
			"byKind":   map[string]any{"type": "string", "enum": []string{"user", "agent", "worker"}, "description": "Optional authority kind: user | agent | worker (default agent)."},
			"content":  map[string]any{"type": "object", "description": "Optional JSON object merged over the per-role note template."},
		},
	},
	{
		Name:        "draft_read",
		RiskClass:   RiskRead,
		Required:    []string{"target"},
		Description: "Return one draft file content verbatim (the v2.0 JSON authoring document) — the editable draft behind a target.",
		Properties: map[string]any{
			"target":  map[string]any{"type": "string", "minLength": 1, "description": "Draft target: \"<ns>/<type>:<id>\" or \"<type>:<id>\"."},
			"project": map[string]any{"type": "string", "description": "Optional project scope (default: the cwd repository's project)."},
		},
	},
	{
		Name:        "view",
		RiskClass:   RiskRead,
		Required:    []string{"target"},
		Description: "Deprecated: use draft_read. Return one draft file content verbatim (the v2.0 JSON authoring document) — the editable draft behind a target. This alias will be removed in the next minor version; migrate to draft_read.",
		Properties: map[string]any{
			"target":  map[string]any{"type": "string", "minLength": 1, "description": "Draft target: \"<ns>/<type>:<id>\" or \"<type>:<id>\"."},
			"project": map[string]any{"type": "string", "description": "Optional project scope (default: the cwd repository's project)."},
		},
		Deprecated: true,
	},
	{
		Name:        "draft_list",
		RiskClass:   RiskRead,
		Required:    nil,
		Description: "List the draft backlog of one project (or every project) as a machine list (schema eka-draft-list-v1), ordered deterministically. Supports provenance filter human|inferred|reconciled|all (default all; eka.yaml capture.provenanceFilterDefault applies server-side when omitted). Inferred/reconciled drafts carry provenance tags in CLI board/status.",
		Properties: map[string]any{
			"project": map[string]any{"type": "string", "description": "Optional project scope (default: every project)."},
			"provenance": map[string]any{"type": "string", "enum": []string{"human", "inferred", "reconciled", "all"}, "description": "Filter by provenance: human|inferred|reconciled|all (default all)."},
		},
	},
	{
		Name:        "integrity_check",
		RiskClass:   RiskRead,
		Required:    nil,
		Description: "Verify the canonical store and return the deterministic integrity report (schema eka-integrity-report-v1): scanned counts, retained-history orphans and every detected violation (payload hashes, reference targets, attachment digests, registry).",
		Properties:  map[string]any{},
	},
	{
		Name:        "discard",
		RiskClass:   RiskLocalWrite,
		Required:    []string{"target"},
		Description: "Delete one draft file without publishing (schema eka-discard-result-v1). The draft is gone; the identity is free for a new draft.",
		Properties: map[string]any{
			"target":  map[string]any{"type": "string", "minLength": 1, "description": "Draft target: \"<ns>/<type>:<id>\" or \"<type>:<id>\"."},
			"project": map[string]any{"type": "string", "description": "Optional project scope (default: the cwd repository's project)."},
		},
	},
	{
		Name:        "sync_push",
		RiskClass:   RiskCanonicalWrite,
		Required:    nil,
		Description: "Push the repository snapshot from the workspace store (schema eka-sync-push-result-v1): the push side of `eka sync push` — same snapshot compilation, same digest, same refusal classes; crash-safe (staged in .snapshots-tmp, swapped atomically) so a failed push writes nothing partially. Pull / --from-docs re-seed is not exposed (silent regression hazard); use CLI `eka sync pull`.",
		Properties: map[string]any{
			"repoPath": map[string]any{"type": "string", "description": "Optional repository path (default: the server cwd, same as CLI `eka sync push`) — logical relative path."},
			"adopt":    map[string]any{"type": "boolean", "description": "Adopt workspace-native units (source_repo=runtime, `eka publish` provenance) before pushing (ADR-032; same as `eka sync push --adopt`)."},
			"override": map[string]any{"type": "boolean", "description": "Machine override to align the repository identity to the content namespace when they differ (same as `eka sync push --override`)."},
		},
	},
	{
		Name:        "assign",
		RiskClass:   RiskCanonicalWrite,
		Required:    []string{"target", "to"},
		Description: "Assign a work item to a member (schema eka-assignment-v1): the assigned-to edge (work item -> member) is added — same engine as CLI `eka assign` — same target forms, same validation, same refusal classes including deterministic refusal when already assigned to a different member; idempotent on the same member. Single-assignee, deterministic; a failed assignment writes nothing.",
		Properties: map[string]any{
			"target":   map[string]any{"type": "string", "minLength": 1, "description": "The work item line: \"<ns>/<type>:<id>\" or \"<type>:<id>\" (same as CLI `eka assign <item>`)."},
			"to":       map[string]any{"type": "string", "minLength": 1, "description": "The member line to assign to: \"<mbr-id>\", \"mbr:<id>\", \"mbr-<id>\", or \"<ns>/mbr:<id>\" (same as CLI --to)."},
			"repoPath": map[string]any{"type": "string", "description": "Optional repository path (default: the server cwd) — logical relative path."},
			"by":       map[string]any{"type": "string", "description": "Optional change-log authority name (defaults to the agent identity)."},
			"byKind":   map[string]any{"type": "string", "enum": []string{"user", "agent", "worker"}, "description": "Optional authority kind: user | agent | worker (default agent)."},
		},
	},
	{
		Name:        "reassign",
		RiskClass:   RiskCanonicalWrite,
		Required:    []string{"target", "to"},
		Description: "Move a work item's assignment to another member in one operation (schema eka-assignment-v1): same engine as CLI `eka reassign` — same validation and refusal classes; deterministic refusal when not assigned (use assign) and idempotent on the same member; a failed reassign writes nothing.",
		Properties: map[string]any{
			"target":   map[string]any{"type": "string", "minLength": 1, "description": "The work item line: \"<ns>/<type>:<id>\" or \"<type>:<id>\" (same as CLI `eka reassign <item>`)."},
			"to":       map[string]any{"type": "string", "minLength": 1, "description": "The member line to move to: \"<mbr-id>\", \"mbr:<id>\", \"mbr-<id>\", or \"<ns>/mbr:<id>\" (same as CLI --to)."},
			"repoPath": map[string]any{"type": "string", "description": "Optional repository path (default: the server cwd) — logical relative path."},
			"by":       map[string]any{"type": "string", "description": "Optional change-log authority name (defaults to the agent identity)."},
			"byKind":   map[string]any{"type": "string", "enum": []string{"user", "agent", "worker"}, "description": "Optional authority kind: user | agent | worker (default agent)."},
		},
	},
	{
		Name:        "unassign",
		RiskClass:   RiskCanonicalWrite,
		Required:    []string{"target"},
		Description: "Remove a work item's assigned-to edge (schema eka-assignment-v1): same engine as CLI `eka unassign` — same validation and refusal classes; deterministic no-op when already unassigned; a failed unassign writes nothing.",
		Properties: map[string]any{
			"target":   map[string]any{"type": "string", "minLength": 1, "description": "The work item line: \"<ns>/<type>:<id>\" or \"<type>:<id>\" (same as CLI `eka unassign <item>`)."},
			"repoPath": map[string]any{"type": "string", "description": "Optional repository path (default: the server cwd) — logical relative path."},
			"by":       map[string]any{"type": "string", "description": "Optional change-log authority name (defaults to the agent identity)."},
			"byKind":   map[string]any{"type": "string", "enum": []string{"user", "agent", "worker"}, "description": "Optional authority kind: user | agent | worker (default agent)."},
		},
	},
	{
		Name:        "feedback_new",
		RiskClass:   RiskLocalWrite,
		Required:    []string{"type", "title"},
		Description: "Create a feedback draft under EKA_HOME/feedback (YAML frontmatter + markdown body) — schema eka-feedback-new-v1. Same engine as CLI `eka feedback new`: same validation, same per-type scaffold, same id generation (fbk-YYYYMMDD-slug), same 0600/0700 permissions. Feedback is meta-information about the tool (ADR-026) — it NEVER enters the canonical store and never becomes a CKO.",
		Properties: map[string]any{
			"type":     map[string]any{"type": "string", "enum": []string{"bug", "suggestion", "improvement", "question"}, "description": "Feedback type: bug, suggestion, improvement, or question (mirrors --type)."},
			"title":    map[string]any{"type": "string", "minLength": 1, "description": "Feedback title (mirrors --title)."},
			"severity": map[string]any{"type": "string", "enum": []string{"low", "medium", "high"}, "description": "Feedback severity: low, medium, or high (default low, mirrors --severity)."},
			"source":   map[string]any{"type": "string", "enum": []string{"human", "agent"}, "description": "Feedback source: human or agent (default human; agents should pass agent, mirrors --source)."},
			"command":  map[string]any{"type": "string", "description": "The invoked command recorded in the report (default mcp:feedback_new, mirrors --command)."},
			"content":  map[string]any{"type": "string", "description": "Markdown feedback body (mirrors --content-file contents inline); when omitted the per-type scaffold is used (bug: Steps/Expected/Actual, others: Description)."},
		},
	},
	{
		Name:        "feedback_list",
		RiskClass:   RiskRead,
		Required:    nil,
		Description: "List all local feedback under EKA_HOME/feedback (schema eka-feedback-list-v1) — drafts and published, id descending (newest first, mirrors `eka feedback list`). Deterministic; the first malformed file fails the whole list naming the file. Feedback is meta-information outside the knowledge model — never a CKO, never part of the canonical store.",
		Properties:  map[string]any{},
	},
	{
		Name:        "feedback_publish",
		RiskClass:   RiskExternal,
		Required:    []string{"id"},
		Description: "Publish a feedback draft as a GitHub issue on the fixed target repository (schema eka-feedback-publish-v1) — same engine as CLI `eka feedback publish`. Inherited constraints: release-binary token gate (refuses with \"issue token not bundled — use a release binary\" when not a release), missing/invalid token refuses deterministically naming remediation (never raw HTTP error), token never appears in outputs/errors/logs, already-published refuses idempotently, unknown id refuses deterministically. Feedback never enters the canonical store.",
		Properties: map[string]any{
			"id": map[string]any{"type": "string", "minLength": 1, "description": "Feedback id to publish: fbk-YYYYMMDD-slug (with or without .md suffix, mirrors `eka feedback publish <id>`)."},
		},
	},
}

// toolNames is the advertised tool order (non-deprecated only). Deprecated
// alias view is dispatched but not advertised as primary.
var toolNames = func() []string {
	out := make([]string, 0, len(toolDescriptors))
	for _, d := range toolDescriptors {
		if !d.Deprecated {
			out = append(out, d.Name)
		}
	}
	return out
}()

// ToolCount returns the number of advertised tools (deprecated alias excluded).
func ToolCount() int { return len(toolNames) }

// AdvertisedToolCount is the count including deprecated alias (for banner compat check).
func AdvertisedToolCount() int { return len(toolDescriptors) }

// ResourceCount returns the total number of resources the server exposes:
// eka://status + eka://manifest + eka://bootstrap (3) plus every
// embedded skill, draft template type and command file. The counts are
// read from the embedded filesystem so the banner never drifts from the
// actual capability set. Deterministic.
func ResourceCount() (int, error) {
	uris, err := pack.AllResourceURIs()
	if err != nil {
		return 0, err
	}
	return len(uris), nil
}

// toolRequiredFields is DERIVED from toolDescriptors — the single source.
// Do NOT hardcode a "required" array anywhere else; add the fields in the descriptor.
var toolRequiredFields = func() map[string][]string {
	m := make(map[string][]string, len(toolDescriptors))
	for _, d := range toolDescriptors {
		if len(d.Required) > 0 {
			cp := make([]string, len(d.Required))
			copy(cp, d.Required)
			m[d.Name] = cp
		}
	}
	return m
}()

// RiskClassOf returns the risk class of a tool (read | local-write | canonical-write | external).
func RiskClassOf(name string) string {
	for _, d := range toolDescriptors {
		if d.Name == name {
			return d.RiskClass
		}
	}
	return ""
}

// IsReadOnly reports whether a tool is read-only (risk read).
func IsReadOnly(name string) bool { return RiskClassOf(name) == RiskRead }

// isCanonicalWriteAllowed reports whether high-impact canonical writes
// (publish, sync_push, etc., risk canonical-write) are gated via approval.
// Disabled by default unless EKA_MCP_ALLOW_CANONICAL_WRITE=1 (explicit
// operator approval).
func isCanonicalWriteAllowed() bool {
	v := strings.TrimSpace(os.Getenv("EKA_MCP_ALLOW_CANONICAL_WRITE"))
	return v == "1" || strings.EqualFold(v, "true")
}

// isExternalAllowed reports whether external publish (feedback_publish,
// risk external) is allowed. Isolated and disabled by default unless
// EKA_MCP_ENABLE_FEEDBACK_PUBLISH=1 (or EKA_MCP_ALLOW_EXTERNAL=1).
func isExternalAllowed() bool {
	if v := strings.TrimSpace(os.Getenv("EKA_MCP_ENABLE_FEEDBACK_PUBLISH")); v == "1" || strings.EqualFold(v, "true") {
		return true
	}
	v := strings.TrimSpace(os.Getenv("EKA_MCP_ALLOW_EXTERNAL"))
	return v == "1" || strings.EqualFold(v, "true")
}

// isUserImpersonationAllowed reports whether MCP clients may impersonate
// a user authority (byKind=user). Disabled by default — MCP is the agent
// interface; arbitrary user impersonation is a safety risk.
func isUserImpersonationAllowed() bool {
	v := strings.TrimSpace(os.Getenv("EKA_MCP_ALLOW_USER_IMPERSONATION"))
	return v == "1" || strings.EqualFold(v, "true")
}

// authorNameRe validates author names: 1-64 chars, alphanumeric + . _ -
var authorNameRe = regexp.MustCompile(`^[A-Za-z0-9._-]{1,64}$`)

// descriptorByName returns the descriptor for a tool name.
func descriptorByName(name string) *toolDescriptor {
	for i := range toolDescriptors {
		if toolDescriptors[i].Name == name {
			return &toolDescriptors[i]
		}
	}
	return nil
}

// Parameter-refusal diagnostic causes (bug:mcp-new-false-refusal): the
// old single schema-shaped refusal conflated three distinct causes,
// which made the intermittent eka_new refusals undiagnosable. Every
// argument-shaped refusal now names exactly one cause.
const (
	causeInvalidJSON  = "invalid_json"  // arguments are not valid JSON (truncated/malformed)
	causeMissingField = "missing_field" // a required field is absent
	causeEmptyField   = "empty_field"   // a required field is present but empty/whitespace
	causeWrongType    = "wrong_type"    // a required field is present but not a string
	causeInvalidArg   = "invalid_arg"   // an optional field has a wrong JSON type
)

// logParamRefusal writes one diagnostics line for an argument-shaped
// refusal to s.diag (stderr in production). Format (single line, fixed
// field order, no argument VALUES — only shapes and lengths, so no
// workspace content ever leaks into logs):
//
//	eka-mcp param-refusal tool=new args_bytes=4132 cause=missing_field field=project
//
// The args_bytes length is the discriminator between client-side
// truncation (small/unusual byte counts on large intended payloads)
// and plain parameter omission. Diagnostics are best-effort: write
// errors are ignored and never fail the protocol.
func (s *Server) logParamRefusal(tool string, argsLen int, cause, field string) {
	if s.diag == nil {
		return
	}
	var b strings.Builder
	b.WriteString("eka-mcp param-refusal tool=")
	b.WriteString(tool)
	fmt.Fprintf(&b, " args_bytes=%d cause=%s", argsLen, cause)
	if field != "" {
		b.WriteString(" field=")
		b.WriteString(field)
	}
	b.WriteByte('\n')
	_, _ = io.WriteString(s.diag, b.String())
}

// decodeToolArgs validates and decodes the arguments of one tool call.
// It discriminates the three refusal causes that the old conflated
// "<tool> requires {...}" message hid (bug:mcp-new-false-refusal):
//
//  1. malformed/truncated JSON → surfaces the sanitized decoder error;
//  2. missing required field → names WHICH field is absent;
//  3. present-but-empty or wrong-typed field → names the field.
//
// The required-field set comes from toolRequiredFields (the single
// source shared with the advertised inputSchema). Absent ("null" or
// empty) arguments mean "no fields at all": optional-only tools
// proceed with zero values, tools with required fields are refused
// naming the first absent field. dst (the per-tool argument struct) is
// decoded last so optional-field type mismatches are also refused
// deterministically. Every refusal is logged via logParamRefusal.
func (s *Server) decodeToolArgs(tool string, args json.RawMessage, dst any) error {
	trimmed := bytes.TrimSpace(args)
	if len(trimmed) == 0 || string(trimmed) == "null" {
		// Absent arguments: every required field is absent.
		for _, name := range toolRequiredFields[tool] {
			s.logParamRefusal(tool, len(args), causeMissingField, name)
			return fmt.Errorf("%s requires %q: missing required field", tool, name)
		}
		return nil
	}
	// Cause 1a: the arguments are not valid JSON at all (a corrupted
	// payload). Note a TRUNCATED request line never reaches this layer:
	// it is refused upstream by HandleMessage's fixed parse refusal.
	var probe any
	if err := json.Unmarshal(trimmed, &probe); err != nil {
		s.logParamRefusal(tool, len(args), causeInvalidJSON, "")
		return fmt.Errorf("%s requires valid JSON arguments: %s", tool, SanitizeError(err))
	}
	// Cause 1b: valid JSON but not an object (e.g. a double-encoded
	// JSON string — the classic large-payload serialization mistake).
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(trimmed, &fields); err != nil {
		s.logParamRefusal(tool, len(args), causeInvalidJSON, "")
		return fmt.Errorf("%s requires a JSON object argument (got %s)", tool, jsonKind(trimmed))
	}
	for _, name := range toolRequiredFields[tool] {
		raw, ok := fields[name]
		if !ok {
			s.logParamRefusal(tool, len(args), causeMissingField, name)
			return fmt.Errorf("%s requires %q: missing required field", tool, name)
		}
		var sv string
		if err := json.Unmarshal(raw, &sv); err != nil {
			s.logParamRefusal(tool, len(args), causeWrongType, name)
			return fmt.Errorf("%s requires %q to be a non-empty string (got %s)", tool, name, jsonKind(raw))
		}
		if strings.TrimSpace(sv) == "" {
			s.logParamRefusal(tool, len(args), causeEmptyField, name)
			return fmt.Errorf("%s requires %q to be a non-empty string", tool, name)
		}
	}
	// Optional fields: decode the caller's struct; a wrong JSON type on
	// an optional field is refused naming the field.
	if dst != nil {
		if err := json.Unmarshal(trimmed, dst); err != nil {
			s.logParamRefusal(tool, len(args), causeInvalidArg, typeErrorField(err))
			return fmt.Errorf("%s has an invalid argument: %s", tool, typeErrorDetail(err))
		}
	}
	return nil
}

// jsonKind reports the JSON kind of a raw value (for wrong-type
// refusals): string, number, boolean, object, array or null.
func jsonKind(raw json.RawMessage) string {
	t := bytes.TrimSpace(raw)
	switch {
	case len(t) == 0:
		return "empty"
	case t[0] == '"':
		return "string"
	case t[0] == '{':
		return "object"
	case t[0] == '[':
		return "array"
	case bytes.Equal(t, []byte("null")):
		return "null"
	case t[0] == 't' || t[0] == 'f':
		return "boolean"
	default:
		return "number"
	}
}

// typeErrorField extracts the offending field name from a struct
// decode error ("" when the error carries none).
func typeErrorField(err error) string {
	var ute *json.UnmarshalTypeError
	if errors.As(err, &ute) {
		return ute.Field
	}
	return ""
}

// typeErrorDetail renders a deterministic, client-safe detail for a
// struct decode error: the expected JSON kind of the offending field
// (never Go type names or paths).
func typeErrorDetail(err error) string {
	var ute *json.UnmarshalTypeError
	if errors.As(err, &ute) {
		switch ute.Type.String() {
		case "bool":
			return fmt.Sprintf("field %q must be a boolean", ute.Field)
		case "int", "int64":
			return fmt.Sprintf("field %q must be an integer", ute.Field)
		case "string":
			return fmt.Sprintf("field %q must be a string", ute.Field)
		default:
			if ute.Type.Kind() == reflect.Slice {
				return fmt.Sprintf("field %q must be an array", ute.Field)
			}
		}
	}
	return "arguments do not match the tool's input schema"
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
	// Get resolves one identity form to its machine document. When
	// noContent is true the content payload is stripped via
	// machine.Document.StripContent at parity with CLI --no-content
	// (content absent, identity/stateVector/relationships intact).
	Get(form string, noContent bool) ([]byte, error)
	// Domain returns one Engineering Domain's units of a project as a
	// machine collection. When noContent is true each unit's content
	// payload is stripped via StripContent at parity with CLI
	// --no-content (content absent per unit, identity/stateVector/
	// relationships intact).
	Domain(projectID, domain string, noContent bool) ([]byte, error)
	// Status returns the workspace status as JSON.
	Status() ([]byte, error)
	// Context builds the Context Object around one subject at a depth
	// (schema eka-context-v1).
	Context(subject, projectID, depth string) ([]byte, error)
	// CodeContext builds bounded source context from the local code graph.
	CodeContext(req CodeContextRequest) ([]byte, error)
	// CodeDiscover builds deterministic candidates from natural query and scope.
	CodeDiscover(req CodeDiscoverRequest) ([]byte, error)
	// CodeGet retrieves exact file content deterministically.
	CodeGet(req CodeGetRequest) ([]byte, error)
	// Validate runs the authoring conformance gate over a repository
	// root and returns the machine report (schema
	// eka-conformance-report-v1).
	Validate(root string) ([]byte, error)
	// NewDraft scaffolds one draft (schema eka-draft-v1).
	NewDraft(req NewDraftRequest) ([]byte, error)
	// DraftUpdate applies a partial content merge to one pending draft
	// (schema eka-draft-update-v1) — read-modify-write through
	// draft_read; publish still validates.
	DraftUpdate(req DraftUpdateRequest) ([]byte, error)
	// Publish publishes one draft (schema eka-publish-result-v1).
	Publish(req PublishRequest) ([]byte, error)
	// PublishBatch publishes every pending draft in topological order
	// (schema eka-publish-batch-v1) — same engine as `eka publish --all`
	// / `--pending` (Kahn's algorithm, cycle and dangling-reference
	// pre-flight refusals, per-draft atomic validation).
	PublishBatch(req PublishBatchRequest) ([]byte, error)
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

// CodeContextRequest describes one bounded source-context query.
type CodeContextRequest struct {
	Root      string
	Focus     string
	Depth     string
	Level     int
	NoContent bool
}

// CodeDiscoverRequest describes one deterministic discovery query.
type CodeDiscoverRequest struct {
	Root  string
	Query string
	Scope string
	Limit int
}

// CodeGetRequest describes one exact retrieval.
type CodeGetRequest struct {
	Root string
	Path string
}

// DraftUpdateRequest describes one draft partial-content merge with
// optimistic concurrency guards.
type DraftUpdateRequest struct {
	Target           string
	Project          string
	Content          map[string]any
	ExpectedRevision *int   `json:"expectedRevision"`
	ExpectedHash     string `json:"expectedHash"`
}

// PublishRequest describes one publish run.
type PublishRequest struct {
	Target          string
	Project         string
	InstanceVersion int
}

// PublishBatchRequest describes one batch publish run.
type PublishBatchRequest struct {
	Project string
	All     bool
	Pending bool
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
	// diag receives one-line operational diagnostics (parameter-refusal
	// records). It is NEVER the protocol stream: stdout carries JSON-RPC
	// traffic only, diagnostics go to stderr (or the injected writer).
	// A nil diag discards.
	diag io.Writer
}

// NewServer wires the server around one capability. Diagnostics are
// written to os.Stderr (never stdout — stdout is the protocol stream).
func NewServer(cap Capability) *Server {
	return NewServerWithDiagnostics(cap, os.Stderr)
}

// NewServerWithDiagnostics wires the server around one capability and
// routes operational diagnostics to diag. A nil diag discards. Tests
// inject a buffer to assert the diagnostic records.
func NewServerWithDiagnostics(cap Capability, diag io.Writer) *Server {
	return &Server{cap: cap, maxLineSize: defaultMaxLineSize, diag: diag}
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
// it never touches the protocol streams (the only I/O is best-effort
// operational diagnostics on s.diag, never stdout), so it is directly
// testable.
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

// handleToolsList returns the advertised tool set — derived entirely from
// toolDescriptors (single authoritative source). Deprecated tools (view) are
// NOT advertised; they remain dispatched for compatibility. Each schema is
// strict: required+types+enums+bounds and additionalProperties:false.
func (s *Server) handleToolsList(req request) []byte {
	tools := make([]any, 0, len(toolDescriptors))
	for _, d := range toolDescriptors {
		if d.Deprecated {
			continue
		}
		schema := map[string]any{
			"type":                 "object",
			"properties":           d.Properties,
			"additionalProperties": false,
		}
		if len(d.Required) > 0 {
			schema["required"] = d.Required
		}
		entry := map[string]any{
			"name":        d.Name,
			"description": d.Description,
			"inputSchema": schema,
		}
		// Surface risk class and annotations for approval policy.
		entry["annotations"] = map[string]any{
			"riskClass":   d.RiskClass,
			"readOnly":    d.RiskClass == RiskRead,
			"destructive": d.RiskClass == RiskCanonicalWrite || d.RiskClass == RiskExternal,
			"openWorld":   d.RiskClass == RiskExternal,
		}
		tools = append(tools, entry)
	}
	_ = toolRequiredFields // keep reference for linter

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
		return s.resultResponse(req.ID, s.toolFailureResult(err))
	}
	return s.resultResponse(req.ID, map[string]any{
		"content": []any{map[string]any{"type": "text", "text": text}},
		"isError": false,
	})
}

// refusalFidelity is implemented by structured tool-refusal errors that
// carry client-safe fidelity beyond the sanitized headline. The
// capability layer (internal/eka) wraps the structured refusals of
// eka-core — *runtime.PublishError and *runtime.RelateValidationError
// carry a full conformance.Report; *runtime.TransitionRefusal carries
// the active-container warning and its confirmation affordance — into
// an error satisfying this contract, so the boundary can deliver what
// it used to silently drop (sto:mcp-error-fidelity).
//
// The interface keeps the layering intact: the server still knows
// nothing about eka-core (stdlib-only dispatch), while errors.As walks
// the wrap chain to whichever carrier is present.
type refusalFidelity interface {
	error
	// RefusalReport returns the serialized validation report in the
	// established eka-conformance-report-v1 shape ("" when the refusal
	// carries none). Every finding already passed the boundary's path
	// redaction at serialization time.
	RefusalReport() string
	// RefusalWarning returns the deterministic active-container banner
	// of a transition refusal ("" when not membership-related).
	RefusalWarning() string
	// RefusalConfirmation reports that the refusal is the
	// active-container confirmation gate: the caller may retry with
	// confirmed:true. The refused operation wrote nothing.
	RefusalConfirmation() bool
}

// confirmationAffordance is the exact retry instruction surfaced on a
// confirming transition refusal (sto:mcp-error-fidelity AC #3): the
// capability layer supports Confirmed=true, so an agent must be told
// the refusal is retryable instead of being left with a dead end.
const confirmationAffordance = "retry with confirmed:true to proceed anyway (asserts the work item may leave the current active container)"

// toolFailureResult builds the isError:true tool result for one
// execution failure. Content block 0 is ALWAYS the sanitized headline —
// byte-stable by construction (exactly SanitizeError(err), first line +
// path redaction), so existing clients matching refusal classes keep
// working untouched. Structured refusals that carry extra fidelity
// contribute additional text content blocks after the headline (both
// multi-block content and multi-line text are valid on the negotiated
// 2024-11-05 baseline; structuredContent is deliberately NOT used):
//
//	block 1: the full conformance report (eka-conformance-report-v1)
//	         for publish/relate-class validation refusals;
//	block 2: the transition active-container warning plus the
//	         "retry with confirmed:true" affordance.
//
// The report is embedded verbatim from the carried error — the
// validation is NEVER re-run (double cost, TOCTOU drift). The boundary
// re-applies its own path redaction to the report text as the final
// guard of the no-leakage invariant (sto:mcp-error-fidelity AC #2):
// fidelity must not depend on the producer remembering the policy.
// The redacted report is validated with json.Valid before embedding;
// if redaction corrupts the JSON (e.g. a backslash-escaped quote inside
// a finding message causes the path regex to consume the escaping
// backslash), the report block is dropped — no leak and no corrupt
// payload. RedactPaths is idempotent, so the producer's own field-level
// redaction is preserved byte-for-byte when the JSON stays valid.
func (s *Server) toolFailureResult(err error) map[string]any {
	content := []any{map[string]any{"type": "text", "text": SanitizeError(err)}}
	var fidelity refusalFidelity
	if errors.As(err, &fidelity) {
		if report := fidelity.RefusalReport(); report != "" {
			redacted := RedactPaths(report)
			if json.Valid([]byte(redacted)) {
				content = append(content, map[string]any{"type": "text", "text": redacted})
			}
		}
		warning := fidelity.RefusalWarning()
		if warning != "" || fidelity.RefusalConfirmation() {
			var b strings.Builder
			if warning != "" {
				b.WriteString("warning: ")
				b.WriteString(warning)
			}
			if fidelity.RefusalConfirmation() {
				if b.Len() > 0 {
					b.WriteByte('\n')
				}
				b.WriteString(confirmationAffordance)
			}
			content = append(content, map[string]any{"type": "text", "text": b.String()})
		}
	}
	return map[string]any{"content": content, "isError": true}
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
	case "code_context":
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
	case "code_discover":
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
	case "code_get":
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
	case "get":
		var p struct {
			Form      string `json:"form"`
			NoContent bool   `json:"noContent"`
		}
		if err := s.decodeToolArgs("get", args, &p); err != nil {
			return "", err
		}
		data, err := s.cap.Get(p.Form, p.NoContent)
		if err != nil {
			return "", err
		}
		return string(data), nil
	case "domain":
		var p struct {
			ProjectID string `json:"projectId"`
			Domain    string `json:"domain"`
			NoContent bool   `json:"noContent"`
		}
		if err := s.decodeToolArgs("domain", args, &p); err != nil {
			return "", err
		}
		data, err := s.cap.Domain(p.ProjectID, p.Domain, p.NoContent)
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
		if err := s.decodeToolArgs("validate", args, &p); err != nil {
			return "", err
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
	case "draft_update":
		// draft_update has its own required check (target) plus content object.
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
	case "publish":
		// Strict single-draft publish — batch mode is publishBatch (split contract).
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
	case "publishBatch":
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
	case "draft_read":
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
	case "view":
		// Deprecated alias for draft_read (td:mcp-view-naming-fix).
		// TODO(td:mcp-view-naming-fix): remove in next minor version after 1.1.3 — then delete this case and the View method.
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
	case "draft_list":
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
		if typed, ok := s.cap.(interface{ DraftListWithProvenance(string, string) ([]byte, error) }); ok {
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
		if err := s.decodeToolArgs("discard", args, &p); err != nil {
			return "", err
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
	case "assign":
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
	case "reassign":
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
	case "unassign":
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
	case "feedback_new":
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
	case "feedback_list":
		// No required fields (no toolRequiredFields entry) — empty
		// object, absent or null arguments all list. Routed through
		// decodeToolArgs so malformed (non-object) arguments refuse
		// through the same discriminating, diagnostics-logged path as
		// every other tool — the former inline refusal bypassed the
		// param-refusal log line.
		if err := s.decodeToolArgs("feedback_list", args, nil); err != nil {
			return "", err
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
// eka-core "Engineering" fallback can never trigger. It validates that
// arbitrary authority impersonation is blocked: by names are restricted to
// the safe character set and user-kind impersonation via MCP is gated
// (requires EKA_MCP_ALLOW_USER_IMPERSONATION=1).
func resolveAuthor(by, byKind string) (AuthorIdentity, error) {
	name := strings.TrimSpace(by)
	if name == "" {
		return defaultAgentIdentity, nil
	}
	if !authorNameRe.MatchString(name) {
		return AuthorIdentity{}, fmt.Errorf("invalid author name %q: must match %s (1-64 alphanumerics, dot, underscore, hyphen)", by, authorNameRe.String())
	}
	if strings.EqualFold(name, "Engineering") {
		return AuthorIdentity{}, fmt.Errorf("author name %q is reserved (the \"Engineering\" placeholder is never used via MCP)", by)
	}
	kind := strings.TrimSpace(byKind)
	if kind == "" {
		kind = "agent"
	}
	if !isAuthorKind(kind) {
		return AuthorIdentity{}, fmt.Errorf("unknown author kind %q (allowed: user, agent, worker)", byKind)
	}
	if kind == "user" && !isUserImpersonationAllowed() {
		return AuthorIdentity{}, fmt.Errorf("MCP authority impersonation blocked: byKind=user not allowed via MCP (use agent or worker) — set EKA_MCP_ALLOW_USER_IMPERSONATION=1 to override")
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
// the compact manifest, bootstrap, and the read-only embedded pack
// resources — every skill's SKILL.md, every draft template type and
// every command file. The enumeration is deterministic (the embedded
// filesystem is the source of truth). Skill/command resource descriptions
// come from the frontmatter (req:agent-agnostic-skill-pack R9); a skill
// without a parseable description degrades to the generic form. Every
// entry carries MCP annotations (audience, priority) for the client.
// The manifest is the compact index (no bodies); skills/templates/commands
// are lazy — bodies only on resources/read (optionally versioned via
// `@<version>` suffix, current version is pack.Version).
func (s *Server) handleResourcesList(req request) []byte {
	resources := []any{
		map[string]any{
			"uri":         pack.StatusURI,
			"name":        "EKA workspace status",
			"description": "The aggregated EKA workspace status: path, schema version, projects, canonical store totals.",
			"mimeType":    "application/json",
			"annotations": pack.ResourceAnnotations(pack.StatusURI),
		},
		map[string]any{
			"uri":         pack.ManifestURI,
			"name":        "EKA pack manifest",
			"description": "Compact EKA pack manifest/index (eka-pack-manifest-v1) — skills, templates and commands with versions (no bodies).",
			"mimeType":    "application/json",
			"annotations": pack.ResourceAnnotations(pack.ManifestURI),
		},
		map[string]any{
			"uri":         pack.BootstrapURI,
			"name":        "EKA bootstrap",
			"description": "EKA bootstrap guidance — lazy load order, versioned reads and fallback for the pack resources.",
			"mimeType":    "text/markdown",
			"annotations": pack.ResourceAnnotations(pack.BootstrapURI),
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
			"uri":         pack.SkillsPrefix + name,
			"name":        "EKA skill " + name,
			"description": description,
			"mimeType":    "text/markdown",
			"annotations": pack.ResourceAnnotations(pack.SkillsPrefix + name),
		})
	}
	types, err := pack.TemplateTypes()
	if err != nil {
		return s.errorResponse(req.ID, codeInternalError, "listing templates: "+SanitizeError(err))
	}
	for _, t := range types {
		resources = append(resources, map[string]any{
			"uri":         pack.TemplatesPrefix + t,
			"name":        "EKA draft template " + t,
			"description": "The v2.0 JSON draft template of type " + t + " (read-only).",
			"mimeType":    "application/json",
			"annotations": pack.ResourceAnnotations(pack.TemplatesPrefix + t),
		})
	}
	commands, err := pack.CommandFiles()
	if err != nil {
		return s.errorResponse(req.ID, codeInternalError, "listing commands: "+SanitizeError(err))
	}
	for _, c := range commands {
		description := "The command guidance of " + c + " (read-only)."
		if d, err := pack.CommandDescription(c); err == nil && d != "" {
			description = d
		}
		resources = append(resources, map[string]any{
			"uri":         pack.CommandsPrefix + c,
			"name":        "EKA command " + c,
			"description": description,
			"mimeType":    "text/markdown",
			"annotations": pack.ResourceAnnotations(pack.CommandsPrefix + c),
		})
	}
	return s.resultResponse(req.ID, map[string]any{"resources": resources})
}

// handleResourcesRead reads one resource URI: eka://status (the status
// read from the capability layer) or the read-only embedded pack
// resources eka://skills/<name>, eka://templates/<type>,
// eka://commands/<name>, plus the compact manifest eka://manifest and
// the bootstrap eka://bootstrap. All pack resources support lazy
// versioned reads via an `@<version>` suffix (e.g. eka://skills/eka-router@1.3.2);
// unversioned means current. Unknown versions or names are
// -32002 Resource not found with a deterministic hint mentioning the
// available version and the unversioned fallback. Guidance remains
// resource content; operations remain tools.
func (s *Server) handleResourcesRead(req request) []byte {
	var p struct {
		URI string `json:"uri"`
	}
	if err := json.Unmarshal(req.Params, &p); err != nil || p.URI == "" {
		return s.errorResponse(req.ID, codeInvalidParams, "resources/read requires {\"uri\": string}")
	}
	base, ver := pack.ParseVersionedURI(p.URI)
	if ver != "" && !pack.IsCurrentVersion(ver) {
		return s.errorResponse(req.ID, codeResourceNotFound, "resource not found: "+p.URI+" (unknown version "+ver+", available "+pack.Version+"; retry the unversioned URI "+base+" as fallback)")
	}
	switch {
	case base == pack.StatusURI:
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
	case base == pack.ManifestURI:
		data, err := pack.ManifestJSON()
		if err != nil {
			return s.errorResponse(req.ID, codeInternalError, "reading eka://manifest: "+SanitizeError(err))
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
	case base == pack.BootstrapURI:
		data, err := pack.BootstrapContent()
		if err != nil {
			return s.errorResponse(req.ID, codeInternalError, "reading eka://bootstrap: "+SanitizeError(err))
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
	case strings.HasPrefix(base, pack.SkillsPrefix):
		name := strings.TrimPrefix(base, pack.SkillsPrefix)
		if strings.TrimSpace(name) == "" || strings.Contains(name, "/") {
			return s.errorResponse(req.ID, codeResourceNotFound, "resource not found: "+p.URI)
		}
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
	case strings.HasPrefix(base, pack.TemplatesPrefix):
		typeToken := strings.TrimPrefix(base, pack.TemplatesPrefix)
		if strings.TrimSpace(typeToken) == "" || strings.Contains(typeToken, "/") {
			return s.errorResponse(req.ID, codeResourceNotFound, "resource not found: "+p.URI)
		}
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
	case strings.HasPrefix(base, pack.CommandsPrefix):
		name := strings.TrimPrefix(base, pack.CommandsPrefix)
		if strings.TrimSpace(name) == "" || strings.Contains(name, "/") {
			return s.errorResponse(req.ID, codeResourceNotFound, "resource not found: "+p.URI)
		}
		data, err := pack.CommandFile(name)
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

// RedactPaths applies the boundary's path-redaction policy to one run
// of error text: absolute paths (/home/…, C:\…), tilde paths (~/…),
// relative paths (./…, ../…) and file-like slash runs become "<path>".
// Identity forms ("feather/adr:001") survive by design — the anchors
// require a second slash or a file-like last segment. It is the shared
// redaction policy of the sanitized headline AND of every finding the
// embedded conformance reports carry (sto:mcp-error-fidelity): no
// finding may leak a store path the headline itself could not.
func RedactPaths(msg string) string {
	msg = pathRe.ReplaceAllString(msg, "<path>")
	msg = relPathRe.ReplaceAllString(msg, "<path>")
	return msg
}

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
	msg = RedactPaths(msg)
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
