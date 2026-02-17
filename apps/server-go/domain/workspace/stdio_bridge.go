package workspace

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"
)

const (
	// defaultCallTimeout is the default timeout for MCP calls via stdio.
	defaultCallTimeout = 30 * time.Second
)

// JSONRPCRequest represents a JSON-RPC 2.0 request message.
type JSONRPCRequest struct {
	JSONRPC string `json:"jsonrpc"`
	ID      int64  `json:"id"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

// JSONRPCResponse represents a JSON-RPC 2.0 response message.
type JSONRPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int64           `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *JSONRPCError   `json:"error,omitempty"`
}

// JSONRPCError represents a JSON-RPC 2.0 error.
type JSONRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

// StdioBridge manages a bidirectional JSON-RPC connection to a container's stdin/stdout.
// It serializes concurrent calls (one at a time) to prevent interleaved stdio.
type StdioBridge struct {
	mu     sync.Mutex // Serializes calls — only one request/response at a time
	writer io.Writer  // Container's stdin
	reader *bufio.Reader
	log    *slog.Logger
	nextID atomic.Int64

	// State
	closed   atomic.Bool
	closedCh chan struct{}
}

// NewStdioBridge creates a new stdio bridge from a container attachment.
func NewStdioBridge(writer io.Writer, reader io.Reader, log *slog.Logger) *StdioBridge {
	return &StdioBridge{
		writer:   writer,
		reader:   bufio.NewReaderSize(reader, 64*1024), // 64KB buffer for large responses
		log:      log.With("component", "stdio-bridge"),
		closedCh: make(chan struct{}),
	}
}

// Call sends a JSON-RPC request and waits for the response.
// Calls are serialized — concurrent callers block until the previous call completes.
func (b *StdioBridge) Call(method string, params any, timeout time.Duration) (*JSONRPCResponse, error) {
	if b.closed.Load() {
		return nil, fmt.Errorf("stdio bridge is closed")
	}

	if timeout <= 0 {
		timeout = defaultCallTimeout
	}

	// Serialize access — only one call at a time
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.closed.Load() {
		return nil, fmt.Errorf("stdio bridge is closed")
	}

	// Build request
	id := b.nextID.Add(1)
	req := JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      id,
		Method:  method,
		Params:  params,
	}

	// Marshal and write to stdin (one line = one JSON-RPC message)
	reqBytes, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal JSON-RPC request: %w", err)
	}
	reqBytes = append(reqBytes, '\n')

	b.log.Debug("sending JSON-RPC request", "method", method, "id", id)

	if _, err := b.writer.Write(reqBytes); err != nil {
		return nil, fmt.Errorf("failed to write to container stdin: %w", err)
	}

	// Read response with timeout
	responseCh := make(chan readResult, 1)
	go func() {
		line, err := b.reader.ReadBytes('\n')
		responseCh <- readResult{data: line, err: err}
	}()

	select {
	case result := <-responseCh:
		if result.err != nil {
			if result.err == io.EOF {
				return nil, fmt.Errorf("MCP server disconnected (EOF)")
			}
			return nil, fmt.Errorf("failed to read from container stdout: %w", result.err)
		}

		var resp JSONRPCResponse
		if err := json.Unmarshal(result.data, &resp); err != nil {
			return nil, fmt.Errorf("failed to parse JSON-RPC response: %w (raw: %s)", err, string(result.data))
		}

		if resp.ID != id {
			b.log.Warn("JSON-RPC response ID mismatch", "expected", id, "got", resp.ID)
		}

		return &resp, nil

	case <-time.After(timeout):
		return nil, fmt.Errorf("MCP call timed out after %s", timeout)

	case <-b.closedCh:
		return nil, fmt.Errorf("stdio bridge closed during call")
	}
}

// Close closes the stdio bridge.
func (b *StdioBridge) Close() error {
	if b.closed.CompareAndSwap(false, true) {
		close(b.closedCh)
		b.log.Debug("stdio bridge closed")
	}
	return nil
}

// IsClosed returns whether the bridge has been closed.
func (b *StdioBridge) IsClosed() bool {
	return b.closed.Load()
}

// readResult is used internally for async read operations.
type readResult struct {
	data []byte
	err  error
}
