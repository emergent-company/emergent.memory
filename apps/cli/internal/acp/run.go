package acp

import (
	"context"
	"encoding/json"
	"errors"
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

	// runCtx is cancelled on a terminal read error (not on EOF) so in-flight
	// prompts are aborted instead of leaking. On EOF, in-flight prompts are
	// drained normally — EOF signals "no more requests", not cancellation.
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	for {
		req, err := s.next()
		if err == io.EOF {
			wg.Wait()
			return nil
		}
		if err != nil {
			if errors.Is(err, errReadTerminal) {
				// Non-recoverable read error (e.g. an over-long line): abort
				// in-flight work and stop rather than looping on the same error.
				cancel()
				wg.Wait()
				return err
			}
			_, _ = fmt.Fprintf(errLog, "acp: %v\n", err)
			// A malformed inbound message still gets a JSON-RPC error response so
			// conformant clients are not left waiting on a reply that never comes.
			// The id is echoed when it was recoverable, otherwise null.
			var werr *wireError
			if errors.As(err, &werr) {
				_ = s.send(response{
					JSONRPC: jsonrpcVersion,
					ID:      werr.ID,
					Error:   &rpcError{Code: werr.Code, Message: werr.Message},
				})
			}
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
				result, rpcErr := dispatch(runCtx, agent, r, s.send)
				if rpcErr != nil {
					_ = s.send(response{JSONRPC: jsonrpcVersion, ID: r.ID, Error: rpcErr})
					return
				}
				_ = s.send(response{JSONRPC: jsonrpcVersion, ID: r.ID, Result: result})
			}(req)
			continue
		}

		result, rpcErr := dispatch(runCtx, agent, req, s.send)
		if rpcErr != nil {
			_ = s.send(response{JSONRPC: jsonrpcVersion, ID: req.ID, Error: rpcErr})
			continue
		}
		_ = s.send(response{JSONRPC: jsonrpcVersion, ID: req.ID, Result: result})
	}
}

// dispatch routes a request to the agent and returns its result. On failure it
// returns a JSON-RPC error carrying the appropriate standard code: -32601 for
// unknown methods, -32602 for invalid params, and -32000 for internal errors.
func dispatch(ctx context.Context, a *Agent, req *request, send func(any) error) (any, *rpcError) {
	switch req.Method {
	case "initialize":
		return a.initialize(), nil
	case "session/new":
		resp, err := a.newSession()
		if err != nil {
			// Strict cap: the map is full and nothing idle could be evicted.
			return nil, &rpcError{Code: codeInternal, Message: err.Error()}
		}
		return resp, nil
	case "session/prompt":
		var p PromptParams
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return nil, &rpcError{Code: codeInvalidParams, Message: fmt.Sprintf("session/prompt: invalid params: %v", err)}
		}
		if p.SessionID == "" {
			return nil, &rpcError{Code: codeInvalidParams, Message: "session/prompt: missing sessionId"}
		}
		result, err := a.prompt(ctx, p, send)
		if err != nil {
			return nil, &rpcError{Code: codeInternal, Message: err.Error()}
		}
		return result, nil
	case "session/delete":
		var p DeleteSessionParams
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return nil, &rpcError{Code: codeInvalidParams, Message: fmt.Sprintf("session/delete: invalid params: %v", err)}
		}
		if p.SessionID == "" {
			return nil, &rpcError{Code: codeInvalidParams, Message: "session/delete: missing sessionId"}
		}
		a.deleteSession(p.SessionID)
		return struct{}{}, nil
	case "session/close":
		var p CloseSessionParams
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return nil, &rpcError{Code: codeInvalidParams, Message: fmt.Sprintf("session/close: invalid params: %v", err)}
		}
		if p.SessionID == "" {
			return nil, &rpcError{Code: codeInvalidParams, Message: "session/close: missing sessionId"}
		}
		a.closeSession(p.SessionID)
		return struct{}{}, nil
	case "session/set_mode":
		var p SetModeParams
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return nil, &rpcError{Code: codeInvalidParams, Message: fmt.Sprintf("session/set_mode: invalid params: %v", err)}
		}
		if p.SessionID == "" {
			return nil, &rpcError{Code: codeInvalidParams, Message: "session/set_mode: missing sessionId"}
		}
		if p.ModeID == "" {
			return nil, &rpcError{Code: codeInvalidParams, Message: "session/set_mode: missing modeId"}
		}
		state, err := a.setMode(p)
		if err != nil {
			return nil, &rpcError{Code: codeInvalidParams, Message: err.Error()}
		}
		return state, nil
	case "session/set_config_option":
		var p SetConfigOptionParams
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return nil, &rpcError{Code: codeInvalidParams, Message: fmt.Sprintf("session/set_config_option: invalid params: %v", err)}
		}
		if p.SessionID == "" {
			return nil, &rpcError{Code: codeInvalidParams, Message: "session/set_config_option: missing sessionId"}
		}
		opts, err := a.setConfigOption(p)
		if err != nil {
			return nil, &rpcError{Code: codeInvalidParams, Message: err.Error()}
		}
		return map[string]any{"configOptions": opts}, nil
	default:
		// session/list is deliberately unimplemented and unadvertised: this agent
		// is an ephemeral in-memory bridge with loadSession=false, so it cannot
		// honestly list loadable sessions. It falls through to -32601 here.
		return nil, &rpcError{Code: codeMethodNotFound, Message: fmt.Sprintf("method not found: %s", req.Method)}
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
	case "session/delete":
		// ACP defines session/delete as a request, but accept the notification
		// form too so a fire-and-forget client still frees the session.
		var p DeleteSessionParams
		if err := json.Unmarshal(req.Params, &p); err != nil {
			_, _ = fmt.Fprintf(errLog, "acp: session/delete: invalid params: %v\n", err)
			return
		}
		if p.SessionID == "" {
			_, _ = fmt.Fprintf(errLog, "acp: session/delete: missing sessionId\n")
			return
		}
		a.deleteSession(p.SessionID)
	case "session/close":
		// ACP defines session/close as a request, but accept the notification
		// form too so a fire-and-forget client still frees the session.
		var p CloseSessionParams
		if err := json.Unmarshal(req.Params, &p); err != nil {
			_, _ = fmt.Fprintf(errLog, "acp: session/close: invalid params: %v\n", err)
			return
		}
		if p.SessionID == "" {
			_, _ = fmt.Fprintf(errLog, "acp: session/close: missing sessionId\n")
			return
		}
		a.closeSession(p.SessionID)
	}
}
