package mcp

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

// toolHandler is the body of one dispatched tool case: the args arrive as
// raw JSON-RPC arguments and the result is the MCP tool text content.
type toolHandler func(s *Server, args json.RawMessage) (string, error)

// toolHandlers is the single declaration point for every tool the
// dispatcher serves. The descriptor catalog (toolDescriptors) stays the
// source of truth for tools/list — this map only routes a call to its
// handler.
var toolHandlers = map[string]toolHandler{
	// knowledge (read)
	"context":    (*Server).dispatchContext,
	"get":        (*Server).dispatchGet,
	"domain":     (*Server).dispatchDomain,
	"status":     (*Server).dispatchStatus,
	"validate":   (*Server).dispatchValidate,
	"view":       (*Server).dispatchView,
	"draft_read": (*Server).dispatchDraftRead,
	"draft_list": (*Server).dispatchDraftList,
	// authoring (write)
	"new":          (*Server).dispatchNew,
	"draft_update": (*Server).dispatchDraftUpdate,
	"publish":      (*Server).dispatchPublish,
	"publishBatch": (*Server).dispatchPublishBatch,
	"transition":   (*Server).dispatchTransition,
	"note":         (*Server).dispatchNote,
	"discard":      (*Server).dispatchDiscard,
	// code graph
	"code_context":  (*Server).dispatchCodeContext,
	"code_discover": (*Server).dispatchCodeDiscover,
	"code_get":      (*Server).dispatchCodeGet,
	// workspace
	"sync_push":       (*Server).dispatchSyncPush,
	"assign":          (*Server).dispatchAssign,
	"reassign":        (*Server).dispatchReassign,
	"unassign":        (*Server).dispatchUnassign,
	"integrity_check": (*Server).dispatchIntegrityCheck,
	// feedback
	"feedback_new":     (*Server).dispatchFeedbackNew,
	"feedback_list":    (*Server).dispatchFeedbackList,
	"feedback_publish": (*Server).dispatchFeedbackPublish,
}

// init registers the advertised group handles (sto:retrieval-surface-budget)
// as thin routers over the flat handlers. Registration happens here rather
// than in the map literal to avoid an initialization cycle: dispatchGroup
// resolves members through toolHandlers, which the literal itself builds.
// Each group is not a new capability — it forwards {"op":<member>} to the
// identical flat handler above.
func init() {
	for _, g := range groupSpecs {
		toolHandlers[g.Name] = groupHandler(g.Name)
	}
}

// groupHandler binds a group handle name to the shared group router.
func groupHandler(name string) toolHandler {
	return func(s *Server, args json.RawMessage) (string, error) {
		return s.dispatchGroup(name, args)
	}
}

// groupOp is the discriminator envelope every group handle accepts:
// {"op":"<member>","args":{...}}. args defaults to {} when omitted and is
// forwarded unchanged to the member handler.
type groupOp struct {
	Op   string          `json:"op"`
	Args json.RawMessage `json:"args"`
}

// dispatchGroup resolves the group's op discriminator to its member tool and
// forwards the nested args to the IDENTICAL flat handler, so a group call and
// its flat alias run one code path and return byte-identical content for the
// same arguments. Gating (canonical-write/external) therefore stays correct
// for both: it lives in the member handler, not here.
func (s *Server) dispatchGroup(group string, args json.RawMessage) (string, error) {
	var p groupOp
	if err := s.decodeToolArgs(group, args, &p); err != nil {
		return "", err
	}
	spec, ok := groupByName(group)
	if !ok {
		return "", &toolNotFoundError{name: group}
	}
	member := ""
	for _, m := range spec.Members {
		if m == p.Op {
			member = m
			break
		}
	}
	if member == "" {
		return "", fmt.Errorf("%s requires \"op\" to be one of %s (got %q)", group, strings.Join(spec.Members, ", "), p.Op)
	}
	memberArgs := bytes.TrimSpace(p.Args)
	if len(memberArgs) == 0 || string(memberArgs) == "null" {
		memberArgs = []byte(`{}`)
	} else if memberArgs[0] != '{' {
		return "", fmt.Errorf("%s requires \"args\" to be a JSON object (got %s)", group, jsonKind(memberArgs))
	}
	handler, ok := toolHandlers[member]
	if !ok {
		return "", &toolNotFoundError{name: member}
	}
	return handler(s, json.RawMessage(memberArgs))
}

// dispatchTool dispatches one named tool call to the capability layer.
// The returned text is the MCP tool text content.
func (s *Server) dispatchTool(name string, args json.RawMessage) (string, error) {
	handler, ok := toolHandlers[name]
	if !ok {
		return "", &toolNotFoundError{name: name}
	}
	return handler(s, args)
}
