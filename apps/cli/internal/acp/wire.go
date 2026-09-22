// Package acp implements an Agent Client Protocol (ACP) v1 agent over stdio.
//
// It is a minimal, dependency-free implementation of the ACP wire protocol
// (newline-delimited JSON-RPC 2.0) sufficient to run as a spawnable agent for
// ACP clients such as Paseo, Claude Code, and Gemini CLI. Only the baseline
// methods are required: initialize, session/new, session/prompt, and the
// session/cancel notification. The session/update notification is emitted to
// stream agent output back to the client.
//
// Spec: https://agentclientprotocol.com/protocol/v1/
package acp

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"
)

// ProtocolVersion is the ACP protocol version this agent speaks.
const ProtocolVersion = 1

// jsonrpcVersion is the JSON-RPC version string carried on every message.
const jsonrpcVersion = "2.0"

// Stop reasons for a session/prompt response.
const (
	StopReasonEndTurn   = "end_turn"
	StopReasonCancelled = "cancelled"
	StopReasonRefusal   = "refusal"
)

// JSON-RPC 2.0 error codes.
const (
	codeParseError     = -32700
	codeInvalidRequest = -32600
	codeInvalidParams  = -32602
	codeMethodNotFound = -32601
	codeInternal       = -32000
)

// wireError is a JSON-RPC error produced while decoding an inbound message. It
// carries the code and message to return to the client, plus the request id to
// echo when one was recoverable from the malformed input (nil otherwise, which
// marshals to id: null as the spec requires).
type wireError struct {
	Code    int
	Message string
	ID      json.RawMessage
}

func (e *wireError) Error() string { return e.Message }

// errReadTerminal marks a non-recoverable read error from stdin (e.g. a line
// exceeding the scanner buffer). No further messages can be read after it, so
// the dispatch loop must terminate rather than retry.
var errReadTerminal = errors.New("acp: terminal read error")

// request is an incoming JSON-RPC request or notification from the client.
type request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// isNotification reports whether the message is a notification (no id).
func (r *request) isNotification() bool { return len(r.ID) == 0 }

// response is an outgoing JSON-RPC response.
type response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

// rpcError is a JSON-RPC error object.
type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// InitializeResponse is the result of the initialize method.
type InitializeResponse struct {
	ProtocolVersion   int               `json:"protocolVersion"`
	AgentCapabilities AgentCapabilities `json:"agentCapabilities"`
	AgentInfo         *AgentInfo        `json:"agentInfo,omitempty"`
	AuthMethods       []any             `json:"authMethods,omitempty"`
}

// AgentCapabilities advertises optional capabilities. Every field is emitted
// explicitly (no omitempty) so the client sees an honest false rather than an
// absent (and therefore ambiguous) value.
type AgentCapabilities struct {
	LoadSession        bool               `json:"loadSession"`
	PromptCapabilities PromptCapabilities `json:"promptCapabilities"`
	MCPCapabilities    MCPCapabilities    `json:"mcpCapabilities"`
}

// PromptCapabilities advertises prompt input modalities.
type PromptCapabilities struct {
	Image           bool `json:"image"`
	Audio           bool `json:"audio"`
	EmbeddedContext bool `json:"embeddedContext"`
}

// MCPCapabilities advertises MCP transport support.
type MCPCapabilities struct {
	HTTP bool `json:"http"`
	SSE  bool `json:"sse"`
}

// AgentInfo is the implementation-identifying metadata returned on initialize.
type AgentInfo struct {
	Name    string `json:"name"`
	Title   string `json:"title"`
	Version string `json:"version"`
}

// SessionNewResponse is the result of the session/new method.
type SessionNewResponse struct {
	SessionID string `json:"sessionId"`
}

// PromptParams is the params of the session/prompt method.
type PromptParams struct {
	SessionID string         `json:"sessionId"`
	Prompt    []ContentBlock `json:"prompt"`
	MessageID string         `json:"messageId,omitempty"`
}

// ContentBlock is a single content block of a prompt. Only text and
// resource_link are consumed; other modalities are ignored for now.
type ContentBlock struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
	URI  string `json:"uri,omitempty"`
}

// PromptResponse is the result of the session/prompt method.
type PromptResponse struct {
	StopReason string `json:"stopReason"`
}

// CancelParams is the params of the session/cancel notification.
type CancelParams struct {
	SessionID string `json:"sessionId"`
}

// stream reads newline-delimited JSON-RPC 2.0 messages from in and writes
// single-line JSON messages to out. Writes are serialized so notifications
// emitted from concurrently-running prompts never interleave mid-line.
type stream struct {
	r   *bufio.Scanner
	enc *json.Encoder
	wmu sync.Mutex
}

func newStream(in io.Reader, out io.Writer) *stream {
	sc := bufio.NewScanner(in)
	// ACP prompts can carry large embedded-context blocks; accept lines up to
	// 16 MiB instead of the default 64 KiB.
	sc.Buffer(make([]byte, 0, 64*1024), 16<<20)
	return &stream{
		r:   sc,
		enc: json.NewEncoder(out),
	}
}

// send marshals m to a single JSON line on out. It is safe for concurrent use.
func (s *stream) send(m any) error {
	s.wmu.Lock()
	defer s.wmu.Unlock()
	return s.enc.Encode(m)
}

// next reads the next message from in, skipping blank lines. It returns
// io.EOF when the stream is closed.
func (s *stream) next() (*request, error) {
	for s.r.Scan() {
		line := bytes.TrimSpace(s.r.Bytes())
		if len(line) == 0 {
			continue
		}
		var req request
		if err := json.Unmarshal(line, &req); err != nil {
			// Malformed JSON is a Parse error. Valid JSON that is not a
			// Request object (an array, a string, or a non-string field type)
			// fails with a type error and is an Invalid Request. req.ID may
			// still be populated for a type error, so echo it when recoverable.
			var typeErr *json.UnmarshalTypeError
			if errors.As(err, &typeErr) {
				return nil, &wireError{
					Code:    codeInvalidRequest,
					Message: fmt.Sprintf("invalid request: %v", err),
					ID:      req.ID,
				}
			}
			return nil, &wireError{
				Code:    codeParseError,
				Message: fmt.Sprintf("parse error: %v", err),
			}
		}
		if req.JSONRPC != jsonrpcVersion {
			return nil, &wireError{
				Code:    codeInvalidRequest,
				Message: fmt.Sprintf("invalid request: unsupported JSON-RPC version %q", req.JSONRPC),
				ID:      req.ID,
			}
		}
		if req.Method == "" {
			return nil, &wireError{
				Code:    codeInvalidRequest,
				Message: "invalid request: missing method",
				ID:      req.ID,
			}
		}
		return &req, nil
	}
	if err := s.r.Err(); err != nil {
		return nil, fmt.Errorf("%w: %v", errReadTerminal, err)
	}
	return nil, io.EOF
}
