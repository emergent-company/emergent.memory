// Package runner drives opencode via its HTTP API.
//
// Usage:
//
//	srv, err := runner.StartServer(ctx, workspaceDir, 0) // 0 = auto port
//	defer srv.Close()
//
//	sessionID, err := srv.NewSession("test")
//	result, err := srv.RunConversation(ctx, sessionID, []string{
//	    "/emergent-onboard",
//	    "Yes, continue.",
//	}, "github-copilot/claude-sonnet-4.6")
package runner

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"
)

// ─────────────────────────────────────────────────────────────────────────────
// Public types
// ─────────────────────────────────────────────────────────────────────────────

// ToolCall represents a single tool invocation captured from the event stream.
type ToolCall struct {
	Tool   string
	Input  map[string]interface{}
	Output string
	IsErr  bool
}

// Turn represents a single prompt/response exchange in the conversation.
type Turn struct {
	// Number is 1-based.
	Number int
	// Prompt is the text sent by the test harness (human message).
	Prompt string
	// Text is the model's response text for this turn.
	Text string
	// ToolCalls are the tool invocations made during this turn.
	ToolCalls []ToolCall
	// Cost in USD for this turn.
	Cost float64
	// StartedAt is when the prompt was sent.
	StartedAt time.Time
	// Elapsed is the wall-clock duration for this turn.
	Elapsed time.Duration
}

// Result holds the parsed output of one or more prompt turns.
type Result struct {
	SessionID string
	// Text is the concatenated model response text across all turns.
	Text string
	// ToolCalls is the list of completed tool invocations in order.
	ToolCalls []ToolCall
	// Turns holds per-turn data for structured logging.
	Turns []Turn
	// Cost in USD (sum of all step-finish parts).
	Cost float64
	// Elapsed is the wall-clock duration.
	Elapsed time.Duration
	// StartedAt is the wall-clock time the first turn began.
	StartedAt time.Time
	// Model is the model identifier used for this run.
	Model string
}

// Merge accumulates another Result into this one.
func (r *Result) Merge(other *Result) {
	if r.SessionID == "" {
		r.SessionID = other.SessionID
	}
	if r.StartedAt.IsZero() {
		r.StartedAt = other.StartedAt
	}
	if r.Model == "" {
		r.Model = other.Model
	}
	if r.Text != "" && other.Text != "" {
		r.Text += "\n" + other.Text
	} else {
		r.Text += other.Text
	}
	r.ToolCalls = append(r.ToolCalls, other.ToolCalls...)
	r.Turns = append(r.Turns, other.Turns...)
	r.Cost += other.Cost
	r.Elapsed += other.Elapsed
}

// ─────────────────────────────────────────────────────────────────────────────
// Server
// ─────────────────────────────────────────────────────────────────────────────

// Server is a running opencode server process.
type Server struct {
	URL  string
	cmd  *exec.Cmd
	port int
}

// StartServer starts `opencode serve --port <port>` in workspaceDir and waits
// until it reports "listening on". If port is 0, a random port in [14100,14999]
// is chosen.
//
// The caller must call Close when done.
func StartServer(ctx context.Context, workspaceDir string, port int) (*Server, error) {
	if port == 0 {
		port = 14100 + rand.Intn(900)
	}

	args := []string{"serve", "--port", fmt.Sprintf("%d", port)}
	cmd := exec.CommandContext(ctx, "opencode", args...)
	cmd.Dir = workspaceDir

	// Capture stdout so we can detect "listening on".
	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("stdout pipe: %w", err)
	}
	// Tee stderr to our stderr so errors are visible.
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start opencode serve: %w", err)
	}

	srv := &Server{
		URL:  fmt.Sprintf("http://127.0.0.1:%d", port),
		cmd:  cmd,
		port: port,
	}

	// Wait for the server to announce it's ready, with a deadline.
	ready := make(chan error, 1)
	go func() {
		scanner := bufio.NewScanner(stdoutPipe)
		for scanner.Scan() {
			line := scanner.Text()
			fmt.Fprintf(os.Stderr, "[opencode-serve] %s\n", line)
			if strings.Contains(line, "listening on") {
				ready <- nil
				// Keep draining so the process doesn't block on full pipe.
				for scanner.Scan() {
					fmt.Fprintf(os.Stderr, "[opencode-serve] %s\n", scanner.Text())
				}
				return
			}
		}
		ready <- fmt.Errorf("opencode serve stdout closed before 'listening on'")
	}()

	select {
	case err := <-ready:
		if err != nil {
			_ = cmd.Process.Kill()
			return nil, err
		}
	case <-time.After(30 * time.Second):
		_ = cmd.Process.Kill()
		return nil, fmt.Errorf("timed out waiting for opencode serve to start")
	case <-ctx.Done():
		_ = cmd.Process.Kill()
		return nil, ctx.Err()
	}

	return srv, nil
}

// Close kills the opencode server process.
func (s *Server) Close() {
	if s.cmd != nil && s.cmd.Process != nil {
		_ = s.cmd.Process.Kill()
		_ = s.cmd.Wait()
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Session management
// ─────────────────────────────────────────────────────────────────────────────

// NewSession creates a new opencode session and returns its ID.
func (s *Server) NewSession(title string) (string, error) {
	body, _ := json.Marshal(map[string]string{"title": title})
	resp, err := http.Post(s.URL+"/session", "application/json", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("create session: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		b, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("create session HTTP %d: %s", resp.StatusCode, b)
	}

	var sess struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&sess); err != nil {
		return "", fmt.Errorf("decode session: %w", err)
	}
	return sess.ID, nil
}

// ─────────────────────────────────────────────────────────────────────────────
// Prompt sending
// ─────────────────────────────────────────────────────────────────────────────

// Prompt sends a text prompt to the session and waits until the session goes
// idle (i.e. the model finishes responding). It collects text output, tool
// calls, and cost from the SSE event stream.
//
// If the text starts with "/" it is treated as a slash command and sent via
// POST /session/{id}/command. Otherwise it is sent via POST /session/{id}/prompt_async.
func (s *Server) Prompt(ctx context.Context, sessionID, text, model string) (*Result, error) {
	start := time.Now()

	// Subscribe to SSE *before* sending the prompt so we don't miss early events.
	eventCh, errCh, cancelSSE := s.subscribeSSE(ctx)

	// Send the prompt (or command).
	if err := s.sendPrompt(ctx, sessionID, text, model); err != nil {
		cancelSSE()
		return nil, err
	}

	result := &Result{SessionID: sessionID, StartedAt: start, Model: model}
	turn := Turn{Number: 1, Prompt: text, StartedAt: start}

	// finalizeTurn stamps elapsed/cost on the turn, copies text+tool calls from
	// result into it, appends it, and resets the per-turn accumulators.
	finalizeTurn := func() {
		turn.Elapsed = time.Since(start)
		turn.Text = result.Text
		turn.ToolCalls = result.ToolCalls
		turn.Cost = result.Cost
		result.Turns = append(result.Turns, turn)
	}

	// Track per-tool state to deduplicate repeated events.
	// SSE fires message.part.updated on every state transition (pending→running→completed).
	// text parts also resend the full accumulated string each time, so we track length seen.
	toolSeen := map[string]string{} // partID → last status printed
	textSeen := map[string]int{}    // partID → chars already printed

	// Drain events until we see session.idle for our session.
	for {
		select {
		case ev, ok := <-eventCh:
			if !ok {
				result.Text = strings.TrimPrefix(result.Text, text) // strip echoed prompt
				result.Elapsed = time.Since(start)
				finalizeTurn()
				return result, fmt.Errorf("SSE stream closed before session.idle")
			}

			switch ev.Type {
			case "session.idle":
				sid, _ := ev.Properties["sessionID"].(string)
				if sid == sessionID {
					fmt.Fprintf(os.Stderr, "\n[idle] %.2fs  $%.4f\n", time.Since(start).Seconds(), result.Cost)
					cancelSSE()
					result.Text = strings.TrimPrefix(result.Text, text) // strip echoed prompt
					result.Elapsed = time.Since(start)
					finalizeTurn()
					return result, nil
				}

			case "message.part.updated":
				part, _ := ev.Properties["part"].(map[string]interface{})
				if part == nil {
					continue
				}
				partType, _ := part["type"].(string)
				if part["sessionID"] != sessionID {
					continue
				}
				partID, _ := part["id"].(string)

				switch partType {
				case "text":
					full, _ := part["text"].(string)
					already := textSeen[partID]
					delta := full[already:]
					if delta != "" {
						fmt.Fprint(os.Stderr, delta)
						result.Text += delta
						textSeen[partID] = len(full)
					}

				case "tool":
					state, _ := part["state"].(map[string]interface{})
					if state == nil {
						continue
					}
					status, _ := state["status"].(string)
					toolName := strField(part, "tool")
					prev := toolSeen[partID]

					if status == "running" && prev != "running" {
						toolSeen[partID] = "running"
						inp, _ := state["input"].(map[string]interface{})
						fmt.Fprintf(os.Stderr, "\n  [%s] %s\n", toolName, toolInputPreview(toolName, inp))
					}

					if (status == "completed" || status == "error") && prev != status {
						toolSeen[partID] = status
						tc := ToolCall{Tool: toolName, IsErr: status == "error"}
						if inp, ok := state["input"].(map[string]interface{}); ok {
							tc.Input = inp
						}
						if status == "completed" {
							tc.Output = strField(state, "output")
							out := strings.ReplaceAll(tc.Output, "\n", " ")
							fmt.Fprintf(os.Stderr, "  [%s] -> %s\n", toolName, truncate(out, 120))
						} else {
							tc.Output = strField(state, "error")
							fmt.Fprintf(os.Stderr, "  [%s] ERROR: %s\n", toolName, truncate(tc.Output, 120))
						}
						result.ToolCalls = append(result.ToolCalls, tc)
					}

				case "step-finish":
					if cost, ok := part["cost"].(float64); ok {
						result.Cost += cost
					}
				}
			}

		case err := <-errCh:
			cancelSSE()
			if err != nil && err != io.EOF {
				return nil, fmt.Errorf("SSE error: %w", err)
			}
			result.Text = strings.TrimPrefix(result.Text, text)
			result.Elapsed = time.Since(start)
			finalizeTurn()
			return result, nil

		case <-ctx.Done():
			cancelSSE()
			result.Text = strings.TrimPrefix(result.Text, text)
			result.Elapsed = time.Since(start)
			return result, ctx.Err()
		}
	}
}

// RunConversation sends multiple prompts in the same session sequentially and
// returns the accumulated Result.
func (s *Server) RunConversation(ctx context.Context, sessionID string, turns []string, model string) (*Result, error) {
	if len(turns) == 0 {
		return nil, fmt.Errorf("RunConversation: at least one turn required")
	}
	accumulated := &Result{SessionID: sessionID}
	for i, turn := range turns {
		r, err := s.Prompt(ctx, sessionID, turn, model)
		if err != nil {
			return accumulated, fmt.Errorf("turn %d (%q): %w", i+1, truncate(turn, 60), err)
		}
		accumulated.Merge(r)
	}
	return accumulated, nil
}

// RunUntilDone sends the initial prompt and then automatically replies
// "Yes, proceed." each time the model goes idle with a question, until the
// model stops asking questions or maxTurns is reached.
//
// This handles skills (like emergent-onboard) that have multiple confirmation
// points throughout their workflow — it drives them to completion without
// human interaction.
func (s *Server) RunUntilDone(ctx context.Context, sessionID, initialPrompt, model string, maxTurns int) (*Result, error) {
	accumulated := &Result{SessionID: sessionID}

	fmt.Fprintf(os.Stderr, "\n>>> turn 1: %s\n", truncate(initialPrompt, 100))
	r, err := s.Prompt(ctx, sessionID, initialPrompt, model)
	if err != nil {
		return accumulated, fmt.Errorf("turn 1: %w", err)
	}
	// Assign turn number after the fact (Prompt always uses 1 internally;
	// for multi-turn sessions RunConversation assigns numbers via RunUntilDone).
	for i := range r.Turns {
		r.Turns[i].Number = 1
	}
	accumulated.Merge(r)

	for turn := 2; turn <= maxTurns; turn++ {
		if !endsWithQuestion(r.Text) {
			fmt.Fprintf(os.Stderr, "\n[done after %d turns]\n", turn-1)
			break
		}
		reply := "Yes, proceed. Continue all remaining steps without asking any further questions."
		fmt.Fprintf(os.Stderr, "\n>>> turn %d (auto-reply): %s\n", turn, reply)
		r, err = s.Prompt(ctx, sessionID, reply, model)
		if err != nil {
			return accumulated, fmt.Errorf("turn %d: %w", turn, err)
		}
		for i := range r.Turns {
			r.Turns[i].Number = turn
		}
		accumulated.Merge(r)
	}

	return accumulated, nil
}

// ─────────────────────────────────────────────────────────────────────────────
// Internal helpers
// ─────────────────────────────────────────────────────────────────────────────

// sseEvent is a parsed event from the opencode global event stream.
type sseEvent struct {
	Type       string                 `json:"type"`
	Properties map[string]interface{} `json:"properties"`
}

// subscribeSSE opens GET /global/event as an SSE stream and returns a channel
// of parsed events plus an error channel. The caller must call cancel to close
// the underlying HTTP request.
func (s *Server) subscribeSSE(ctx context.Context) (<-chan sseEvent, <-chan error, context.CancelFunc) {
	ctx2, cancel := context.WithCancel(ctx)
	eventCh := make(chan sseEvent, 64)
	errCh := make(chan error, 1)

	go func() {
		defer close(eventCh)
		req, err := http.NewRequestWithContext(ctx2, "GET", s.URL+"/global/event", nil)
		if err != nil {
			errCh <- err
			return
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			errCh <- err
			return
		}
		defer resp.Body.Close()

		scanner := bufio.NewScanner(resp.Body)
		scanner.Buffer(make([]byte, 256*1024), 256*1024)

		var dataBuf strings.Builder
		for scanner.Scan() {
			line := scanner.Text()
			if strings.HasPrefix(line, "data:") {
				dataBuf.WriteString(strings.TrimPrefix(line, "data:"))
				dataBuf.WriteString("\n")
			} else if line == "" && dataBuf.Len() > 0 {
				// End of SSE message — parse the accumulated data.
				raw := strings.TrimSpace(dataBuf.String())
				dataBuf.Reset()
				if raw == "" {
					continue
				}
				// SSE envelope: {"directory":"...","payload":{...}}
				// The event is under "payload".
				var envelope struct {
					Payload sseEvent `json:"payload"`
				}
				if err := json.Unmarshal([]byte(raw), &envelope); err != nil {
					// Skip unparseable events.
					continue
				}
				ev := envelope.Payload
				if ev.Type == "" {
					continue
				}
				select {
				case eventCh <- ev:
				case <-ctx2.Done():
					errCh <- nil
					return
				}
			}
		}
		if err := scanner.Err(); err != nil && ctx2.Err() == nil {
			errCh <- err
		} else {
			errCh <- nil
		}
	}()

	return eventCh, errCh, cancel
}

// sendPrompt sends text to the session via prompt_async.
// Slash commands (e.g. "/emergent-onboard") are sent as plain text parts —
// opencode recognises them and dispatches the skill automatically.
//
// model should be "providerID/modelID" (e.g. "github-copilot/claude-sonnet-4.6").
func (s *Server) sendPrompt(ctx context.Context, sessionID, text, model string) error {
	providerID, modelID := splitModel(model)
	url := fmt.Sprintf("%s/session/%s/prompt_async", s.URL, sessionID)
	body := map[string]interface{}{
		"model": map[string]string{
			"providerID": providerID,
			"modelID":    modelID,
		},
		"parts": []map[string]interface{}{
			{"type": "text", "text": text},
		},
	}

	b, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("marshal prompt body: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(b))
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("POST %s: %w", url, err)
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body) // drain

	if resp.StatusCode >= 400 {
		return fmt.Errorf("POST %s HTTP %d", url, resp.StatusCode)
	}
	return nil
}

// endsWithQuestion returns true if the text appears to end with a genuine question
// directed at the user. It looks at the last non-empty paragraph of the text and
// checks whether it ends with "?". It deliberately ignores "?" inside fenced code
// blocks (``` lines) and bare URLs so that commands and query parameters do not
// trigger a spurious auto-reply.
func endsWithQuestion(text string) bool {
	text = strings.TrimSpace(text)
	if text == "" {
		return false
	}

	// Split into paragraphs (double newline separated).
	paragraphs := strings.Split(text, "\n\n")
	// Find the last non-empty paragraph that isn't a fenced code block.
	for i := len(paragraphs) - 1; i >= 0; i-- {
		p := strings.TrimSpace(paragraphs[i])
		if p == "" {
			continue
		}
		// Skip fenced code blocks.
		if strings.HasPrefix(p, "```") {
			continue
		}
		// The last line of this paragraph must end with "?" (not inside a URL).
		lines := strings.Split(p, "\n")
		lastLine := strings.TrimSpace(lines[len(lines)-1])
		// A line that ends with "?" and doesn't look like a URL fragment.
		if strings.HasSuffix(lastLine, "?") && !strings.Contains(lastLine, "://") {
			return true
		}
		return false
	}
	return false
}

func strField(m map[string]interface{}, key string) string {
	v, _ := m[key].(string)
	return v
}

// toolInputPreview returns a short human-readable summary of a tool's input.
func toolInputPreview(tool string, inp map[string]interface{}) string {
	if inp == nil {
		return ""
	}
	switch tool {
	case "bash":
		cmd, _ := inp["command"].(string)
		return truncate(strings.ReplaceAll(cmd, "\n", " "), 120)
	case "read":
		path, _ := inp["filePath"].(string)
		return path
	case "write", "edit":
		path, _ := inp["filePath"].(string)
		return path
	case "glob":
		pattern, _ := inp["pattern"].(string)
		return pattern
	case "grep":
		pattern, _ := inp["pattern"].(string)
		path, _ := inp["path"].(string)
		if path != "" {
			return pattern + " in " + path
		}
		return pattern
	case "skill":
		name, _ := inp["name"].(string)
		return name
	default:
		// Fallback: render first string value found.
		for _, v := range inp {
			if s, ok := v.(string); ok {
				return truncate(s, 80)
			}
		}
		return ""
	}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

// splitModel splits "providerID/modelID" into two parts.
// If there is no "/" the whole string is used as modelID with an empty providerID.
func splitModel(model string) (providerID, modelID string) {
	idx := strings.Index(model, "/")
	if idx < 0 {
		return "", model
	}
	return model[:idx], model[idx+1:]
}
