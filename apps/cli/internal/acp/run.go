package acp

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sync"
)

// Run serves the ACP stdio loop until stdin closes. JSON-RPC responses and
// notifications are written to out (stdout); diagnostics are written to errLog
// (stderr). Nothing other than ACP messages may be written to out.
//
// session/prompt is dispatched on a goroutine so that a session/cancel
// notification arriving mid-turn can interrupt the in-flight Memory request.
// All other methods are answered inline.
func Run(ctx context.Context, in io.Reader, out io.Writer, errLog io.Writer, agent *Agent) error {
	s := newStream(in, out)
	var wg sync.WaitGroup

	for {
		req, err := s.next()
		if err == io.EOF {
			wg.Wait()
			return nil
		}
		if err != nil {
			_, _ = fmt.Fprintf(errLog, "acp: %v\n", err)
			continue
		}

		if req.isNotification() {
			handleNotification(agent, req, errLog)
			continue
		}

		if req.Method == "session/prompt" {
			// Run concurrently so a subsequent session/cancel can interrupt it.
			wg.Add(1)
			go func(r *request) {
				defer wg.Done()
				result, derr := dispatch(ctx, agent, r, s.send)
				if derr != nil {
					_ = s.send(response{JSONRPC: jsonrpcVersion, ID: r.ID, Error: &rpcError{Code: -32000, Message: derr.Error()}})
					return
				}
				_ = s.send(response{JSONRPC: jsonrpcVersion, ID: r.ID, Result: result})
			}(req)
			continue
		}

		result, derr := dispatch(ctx, agent, req, s.send)
		if derr != nil {
			_ = s.send(response{JSONRPC: jsonrpcVersion, ID: req.ID, Error: &rpcError{Code: -32000, Message: derr.Error()}})
			continue
		}
		_ = s.send(response{JSONRPC: jsonrpcVersion, ID: req.ID, Result: result})
	}
}

// dispatch routes a request to the agent and returns its result.
func dispatch(ctx context.Context, a *Agent, req *request, send func(any) error) (any, error) {
	switch req.Method {
	case "initialize":
		return a.initialize(), nil
	case "session/new":
		return a.newSession(), nil
	case "session/prompt":
		var p PromptParams
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return nil, fmt.Errorf("session/prompt: invalid params: %w", err)
		}
		return a.prompt(ctx, p, send)
	default:
		return nil, fmt.Errorf("method not found: %s", req.Method)
	}
}

// handleNotification handles an incoming notification. Unknown notifications
// are ignored.
func handleNotification(a *Agent, req *request, errLog io.Writer) {
	switch req.Method {
	case "session/cancel":
		var p CancelParams
		if err := json.Unmarshal(req.Params, &p); err != nil {
			_, _ = fmt.Fprintf(errLog, "acp: session/cancel: invalid params: %v\n", err)
			return
		}
		a.cancel(p)
	}
}
