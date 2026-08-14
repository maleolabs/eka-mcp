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
	"encoding/json"
	"fmt"

	"github.com/maleolabs/eka-mcp"
)

// ProtocolVersion is the MCP protocol version the server reports in the
// initialize handshake when the client does not announce one (the
// 2024-11-05 baseline of the MCP spec).
const ProtocolVersion = "2024-11-05"

// JSON-RPC 2.0 error codes (spec §5.1) plus the MCP extensions.
const (
	codeParseError     = -32700
	codeInvalidRequest = -32600
	codeMethodNotFound = -32601
	codeInvalidParams  = -32602
	codeInternalError  = -32603
	codeToolNotFound   = -32003
	codeResourceFound  = -32002 // "Resource not found" (MCP extension)
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
}

// Server is the MCP server: it dispatches JSON-RPC 2.0 requests to the
// capability layer. It is safe for one session at a time (stdio).
type Server struct {
	cap Capability
}

// NewServer wires the server around one capability.
func NewServer(cap Capability) *Server {
	return &Server{cap: cap}
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

// HandleMessage processes one JSON-RPC message and returns the response
// bytes to write, or nil when the message needs no response (a
// notification). It is the pure dispatch unit: parse, route, respond —
// it never touches I/O, so it is directly testable.
func (s *Server) HandleMessage(line []byte) []byte {
	var req request
	if err := json.Unmarshal(line, &req); err != nil {
		return marshalResponse(response{
			JSONRPC: "2.0",
			ID:      json.RawMessage("null"),
			Error:   &rpcError{Code: codeParseError, Message: "parse error: " + err.Error()},
		})
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

// handleToolsList returns the tool set of the server: EKA knowledge
// retrieval as MCP tools, with JSON Schema input definitions.
func (s *Server) handleToolsList(req request) []byte {
	tools := []any{
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
			"content": []any{map[string]any{"type": "text", "text": err.Error()}},
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
	default:
		return "", &toolNotFoundError{name: name}
	}
}

// handleResourcesList returns the resource set of the server: the
// workspace status resource (a real, readable view over eka-core).
func (s *Server) handleResourcesList(req request) []byte {
	resources := []any{
		map[string]any{
			"uri":         "eka://status",
			"name":        "EKA workspace status",
			"description": "The aggregated EKA workspace status: path, schema version, projects, canonical store totals.",
			"mimeType":    "application/json",
		},
	}
	return s.resultResponse(req.ID, map[string]any{"resources": resources})
}

// handleResourcesRead reads one resource URI. Currently only
// "eka://status" is served — the status read from the capability layer.
func (s *Server) handleResourcesRead(req request) []byte {
	var p struct {
		URI string `json:"uri"`
	}
	if err := json.Unmarshal(req.Params, &p); err != nil || p.URI == "" {
		return s.errorResponse(req.ID, codeInvalidParams, "resources/read requires {\"uri\": string}")
	}
	if p.URI != "eka://status" {
		return s.errorResponse(req.ID, codeResourceFound, "resource not found: "+p.URI)
	}
	data, err := s.cap.Status()
	if err != nil {
		return s.errorResponse(req.ID, codeInternalError, "reading eka://status: "+err.Error())
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
}

// resultResponse builds a success response.
func (s *Server) resultResponse(id json.RawMessage, result any) []byte {
	return marshalResponse(response{JSONRPC: "2.0", ID: id, Result: result})
}

// errorResponse builds an error response.
func (s *Server) errorResponse(id json.RawMessage, code int, msg string) []byte {
	return marshalResponse(response{
		JSONRPC: "2.0",
		ID:      id,
		Error:   &rpcError{Code: code, Message: msg},
	})
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
