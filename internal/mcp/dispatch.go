package mcp

import "encoding/json"

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

// dispatchTool dispatches one named tool call to the capability layer.
// The returned text is the MCP tool text content.
func (s *Server) dispatchTool(name string, args json.RawMessage) (string, error) {
	handler, ok := toolHandlers[name]
	if !ok {
		return "", &toolNotFoundError{name: name}
	}
	return handler(s, args)
}
