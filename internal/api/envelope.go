package api

import (
	"encoding/json"
	"fmt"
)

// JSONRPCVersion is the only wire version spoken by the gate.
const JSONRPCVersion = "2.0"

// Request is a JSON-RPC 2.0 request object. A request without ID is a
// notification: the gate applies it and sends no response.
type Request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// NewRequest builds a request with a string id.
func NewRequest(id string, method string, params any) (*Request, error) {
	rawID, err := json.Marshal(id)
	if err != nil {
		return nil, fmt.Errorf("api: marshal request id: %w", err)
	}
	r := &Request{JSONRPC: JSONRPCVersion, ID: rawID, Method: method}
	if params != nil {
		raw, err := json.Marshal(params)
		if err != nil {
			return nil, fmt.Errorf("api: marshal params: %w", err)
		}
		r.Params = raw
	}
	return r, nil
}

// IDString decodes a string request id.
func (r *Request) IDString() (string, error) {
	var s string
	if err := json.Unmarshal(r.ID, &s); err != nil {
		return "", fmt.Errorf("api: bad string id: %w", err)
	}
	return s, nil
}

// Validate checks the envelope version and the presence of a method.
func (r *Request) Validate() error {
	if r.JSONRPC != JSONRPCVersion {
		return fmt.Errorf("api: unsupported jsonrpc version %q", r.JSONRPC)
	}
	if r.Method == "" {
		return &Error{Code: CodeInvalidRequest, Message: "missing method"}
	}
	return nil
}

// Response is a JSON-RPC 2.0 response object: exactly one of Result or
// Error is set.
type Response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *Error          `json:"error,omitempty"`
}

// NewResult builds a success response for id.
func NewResult(id json.RawMessage, result any) (*Response, error) {
	raw, err := json.Marshal(result)
	if err != nil {
		return nil, fmt.Errorf("api: marshal result: %w", err)
	}
	return &Response{JSONRPC: JSONRPCVersion, ID: id, Result: raw}, nil
}

// NewError builds an error response for id.
func NewError(id json.RawMessage, apiErr *Error) *Response {
	return &Response{JSONRPC: JSONRPCVersion, ID: id, Error: apiErr}
}

// Error is a JSON-RPC 2.0 error object with an optional data payload.
type Error struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

// Error implements the error interface.
func (e *Error) Error() string { return fmt.Sprintf("api: code %d: %s", e.Code, e.Message) }

// Standard JSON-RPC 2.0 error codes.
const (
	CodeParseError     = -32700
	CodeInvalidRequest = -32600
	CodeMethodNotFound = -32601
	CodeInvalidParams  = -32602
	CodeInternal       = -32603
)

// Application error codes in the -32000 range.
const (
	// CodeUnknownTarget: the referenced run, task or agent does not exist.
	CodeUnknownTarget = -32001
	// CodeAmbiguousTarget: a control target resolved to several candidates.
	CodeAmbiguousTarget = -32002
	// CodeScopeDenied: the caller may not read or write that scope.
	CodeScopeDenied = -32003
	// CodeBudgetExceeded: the operation would exceed a budget.
	CodeBudgetExceeded = -32004
	// CodeConflict: illegal state transition or concurrent modification.
	CodeConflict = -32005
	// CodeAppError: generic application failure.
	CodeAppError = -32099
)
