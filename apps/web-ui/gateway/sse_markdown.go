package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"strings"
)

// askUserInput is the subset of ask_user tool arguments the gateway forwards
// to the client so it can render an interactive question card. Field names are
// snake_case — the tool's wire format.
type askUserInput struct {
	Question        string           `json:"question"`
	Options         []questionOption `json:"options"`
	InteractionType string           `json:"interaction_type"`
	Placeholder     string           `json:"placeholder"`
	MaxLength       int              `json:"max_length"`
}

// questionOption is one selectable answer, matching ask_user's options shape.
type questionOption struct {
	Label       string `json:"label"`
	Value       string `json:"value"`
	Description string `json:"description,omitempty"`
}

// questionEvent is the client-facing `question` SSE event (camelCase) — the
// shape chat.js / sidepanel.js renderQuestion expects. Memory's chat stream
// never emits it, so the gateway synthesizes it from the ask_user tool call.
type questionEvent struct {
	Type            string           `json:"type"`
	QuestionID      string           `json:"questionId"`
	Question        string           `json:"question"`
	QuestionHTML    string           `json:"questionHtml,omitempty"`
	InteractionType string           `json:"interactionType"`
	Options         []questionOption `json:"options"`
	Placeholder     string           `json:"placeholder"`
	MaxLength       int              `json:"maxLength"`
}

// rewriteChatStream transforms a memory-service SSE stream in place:
//   - `token` events are accumulated and re-emitted as full-message `html`
//     events (markdown-rendered);
//   - `ask_user` MCP tool invocations are rewritten into interactive `question`
//     events, and their raw `mcp_tool` events are suppressed (the question card
//     replaces the tool chip);
//   - all other events (`meta`, `mcp_tool`, `done`, unknown) pass through.
func rewriteChatStream(w io.Writer, r io.Reader) error {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	sc.Split(splitSSEEvent)
	var sb strings.Builder
	var askInput *askUserInput // ask_user args awaiting the tool's question_id
	for sc.Scan() {
		raw := sc.Bytes()
		data := extractSSEData(raw)
		if data == "" {
			continue
		}
		var ev struct {
			Type   string          `json:"type"`
			Token  string          `json:"token"`
			Tool   string          `json:"tool"`
			Status string          `json:"status"`
			Result json.RawMessage `json:"result"`
		}
		if err := json.Unmarshal([]byte(data), &ev); err != nil {
			if _, werr := w.Write(raw); werr != nil {
				return werr
			}
			continue
		}
		switch ev.Type {
		case "token":
			sb.WriteString(ev.Token)
			html, err := marshalNoEscape(map[string]string{"type": "html", "html": renderMarkdown(sb.String())})
			if err != nil {
				return err
			}
			if _, err := fmtEvent(w, html); err != nil {
				return err
			}
		case "mcp_tool":
			if ev.Tool != "ask_user" {
				if err := emitToolResultHTML(w, raw, data, ev.Result); err != nil {
					return err
				}
				continue
			}
			if ev.Status == "started" || ev.Status == "running" {
				var in askUserInput
				if err := json.Unmarshal(ev.Result, &in); err == nil {
					askInput = &in
				}
				continue
			}
			var res struct {
				QuestionID string `json:"question_id"`
			}
			_ = json.Unmarshal(ev.Result, &res)
			if res.QuestionID != "" && askInput != nil {
				q := questionEvent{
					Type:            "question",
					QuestionID:      res.QuestionID,
					Question:        askInput.Question,
					QuestionHTML:    renderMarkdown(askInput.Question),
					InteractionType: askInput.InteractionType,
					Options:         askInput.Options,
					Placeholder:     askInput.Placeholder,
					MaxLength:       askInput.MaxLength,
				}
				if q.InteractionType == "" {
					q.InteractionType = "buttons"
				}
				payload, err := marshalNoEscape(q)
				if err != nil {
					return err
				}
				if _, err := fmtEvent(w, payload); err != nil {
					return err
				}
				askInput = nil
				continue
			}
			// ask_user finished without pausing (e.g. validation error on bad
			// options) — surface the raw event so the error shows as a chip.
			askInput = nil
			if _, err := w.Write(raw); err != nil {
				return err
			}
		case "approval":
			// Tool-policy approval request from memory: pass through verbatim so
			// the client renders an approval card (tool + input + questionId).
			if _, err := w.Write(raw); err != nil {
				return err
			}
		default:
			if _, err := w.Write(raw); err != nil {
				return err
			}
		}
	}
	return sc.Err()
}

// marshalNoEscape JSON-encodes v without HTML escaping, so sanitized HTML in
// the payload stays readable; the JSON itself remains valid for any parser.
func marshalNoEscape(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimRight(buf.Bytes(), "\n"), nil
}

// splitSSEEvent splits an SSE stream on blank-line event boundaries, yielding
// the raw event bytes (including any trailing \n\n that terminated it, so
// framing can be re-emitted verbatim) and a final trailing event at EOF.
func splitSSEEvent(data []byte, atEOF bool) (advance int, token []byte, err error) {
	if atEOF && len(data) == 0 {
		return 0, nil, nil
	}
	if i := bytes.Index(data, []byte("\n\n")); i >= 0 {
		return i + 2, data[:i+2], nil
	}
	if atEOF {
		return len(data), data, nil
	}
	return 0, nil, nil
}

// extractSSEData returns the JSON payload of the first `data:` line in a raw
// SSE event, or "" if the event carries none.
func extractSSEData(raw []byte) string {
	for _, line := range bytes.Split(raw, []byte("\n")) {
		line = bytes.TrimSpace(line)
		if !bytes.HasPrefix(line, []byte("data:")) {
			continue
		}
		return strings.TrimSpace(string(bytes.TrimPrefix(line, []byte("data:"))))
	}
	return ""
}

// fmtEvent writes a complete `data: {json}\n\n` SSE event.
func fmtEvent(w io.Writer, payload []byte) (int, error) {
	return w.Write(append(append([]byte("data: "), payload...), []byte("\n\n")...))
}

// emitToolResultHTML re-emits a non-ask_user mcp_tool SSE event, attaching a
// highlighted `resultHtml` field when the result is valid JSON. All original
// fields are preserved verbatim (json.RawMessage), so numbers and unknown
// fields survive exactly. Falls back to the verbatim raw event when the result
// isn't JSON or re-encoding fails.
func emitToolResultHTML(w io.Writer, raw []byte, data string, result json.RawMessage) error {
	hl, ok := highlightJSONValue(result)
	if !ok {
		_, err := w.Write(raw)
		return err
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal([]byte(data), &m); err != nil {
		_, err := w.Write(raw)
		return err
	}
	m["resultHtml"] = rawJSONString(hl)
	payload, err := marshalNoEscape(m)
	if err != nil {
		_, err := w.Write(raw)
		return err
	}
	_, err = fmtEvent(w, payload)
	return err
}

// rawJSONString marshals s as a JSON string with HTML escaping disabled, so a
// highlighted fragment survives intact when embedded in a RawMessage map.
func rawJSONString(s string) json.RawMessage {
	b, err := marshalNoEscape(s)
	if err != nil {
		return nil
	}
	return json.RawMessage(b)
}

// splitLeadingReasoning separates the leading chain-of-thought paragraph
// deepseek-v4 emits before its actual reply. The CoT is the first paragraph
// (up to the first newline). When there is no clear reply after that newline
// the text is returned unchanged as the answer with empty reasoning, so
// single-line messages are never truncated.
func splitLeadingReasoning(text string) (reasoning, answer string) {
	if idx := strings.IndexByte(text, '\n'); idx > 0 {
		r := strings.TrimSpace(text[:idx])
		a := strings.TrimSpace(text[idx+1:])
		if r != "" && a != "" {
			return r, a
		}
	}
	return "", text
}

// renderHistoryHTML converts assistant message text to sanitized HTML,
// injecting it as content.html. User/tool messages and non-message items are
// left untouched; items that fail to parse are kept as-is.
func renderHistoryHTML(items []json.RawMessage) []json.RawMessage {
	out := make([]json.RawMessage, len(items))
	for i, item := range items {
		out[i] = item
		var m map[string]any
		if err := json.Unmarshal(item, &m); err != nil {
			continue
		}
		kind, _ := m["kind"].(string)

		// ask_user tool calls carry the agent's question text; render its
		// markdown so the client can show it formatted rather than raw.
		if kind == "tool_call" {
			toolName, _ := m["tool_name"].(string)
			if toolName == "ask_user" {
				if input, ok := m["tool_input"].(map[string]any); ok {
					if q, ok := input["question"].(string); ok && q != "" {
						input["question_html"] = renderMarkdown(q)
						if re, err := marshalNoEscape(m); err == nil {
							out[i] = re
						}
					}
				}
			} else if re := addToolHighlight(item); re != nil {
				out[i] = re
			}
			continue
		}

		role, _ := m["role"].(string)
		if kind != "message" || role == "user" || role == "tool" {
			continue
		}
		content, ok := m["content"].(map[string]any)
		if !ok {
			continue
		}
		text, ok := content["text"].(string)
		if !ok || text == "" {
			continue
		}
		// Operator planning monologues (function_calls) render as collapsible
		// thinking blocks and are shown whole. Final answers from deepseek-v4
		// leak their chain-of-thought as the leading line; split it out so the
		// client can render it as a Thinking block above the markdown reply.
		if calls, hasCalls := content["function_calls"].([]any); hasCalls && len(calls) > 0 {
			content["html"] = renderMarkdown(text)
		} else if reasoning, answer := splitLeadingReasoning(text); reasoning != "" {
			content["reasoning"] = reasoning
			content["text"] = answer
			content["html"] = renderMarkdown(answer)
		} else {
			content["html"] = renderMarkdown(text)
		}
		re, err := marshalNoEscape(m)
		if err != nil {
			continue
		}
		out[i] = re
	}
	return out
}

// addToolHighlight decodes a tool_call timeline item and adds highlighted
// tool_input_html / tool_output_html fields for JSON I/O, preserving every
// other field verbatim (RawMessage). Returns the re-encoded item, or nil when
// neither field is JSON.
func addToolHighlight(data []byte) []byte {
	var m map[string]json.RawMessage
	if err := json.Unmarshal(data, &m); err != nil {
		return nil
	}
	changed := false
	if hl, ok := highlightJSONValue(m["tool_input"]); ok {
		m["tool_input_html"] = rawJSONString(hl)
		changed = true
	}
	if hl, ok := highlightJSONValue(m["tool_output"]); ok {
		m["tool_output_html"] = rawJSONString(hl)
		changed = true
	}
	if !changed {
		return nil
	}
	out, err := marshalNoEscape(m)
	if err != nil {
		return nil
	}
	return out
}

// injectQuestionAnswers annotates answered ask_user tool calls with the user's
// response (tool_output.response). It collects question ids from the timeline,
// looks them up in one ListAgentQuestions call, and injects them so the client
// can keep the question widget and mark it answered.
func injectQuestionAnswers(ctx context.Context, mem MemoryBackend, items []json.RawMessage) ([]json.RawMessage, error) {
	ids := make(map[string]bool)
	for _, item := range items {
		var m map[string]any
		if err := json.Unmarshal(item, &m); err != nil {
			continue
		}
		if kind, _ := m["kind"].(string); kind != "tool_call" {
			continue
		}
		if tool, _ := m["tool_name"].(string); tool != "ask_user" {
			continue
		}
		if out, ok := m["tool_output"].(map[string]any); ok {
			if qid, _ := out["question_id"].(string); qid != "" {
				ids[qid] = true
			}
		}
	}
	if len(ids) == 0 {
		return items, nil
	}
	questions, err := mem.ListAgentQuestions(ctx)
	if err != nil {
		return items, err
	}
	responses := make(map[string]string, len(questions))
	for _, q := range questions {
		if q.Response != nil && *q.Response != "" {
			responses[q.ID] = *q.Response
		}
	}
	out := make([]json.RawMessage, len(items))
	for i, item := range items {
		out[i] = item
		var m map[string]any
		if err := json.Unmarshal(item, &m); err != nil {
			continue
		}
		if kind, _ := m["kind"].(string); kind != "tool_call" {
			continue
		}
		if tool, _ := m["tool_name"].(string); tool != "ask_user" {
			continue
		}
		toolOut, ok := m["tool_output"].(map[string]any)
		if !ok {
			continue
		}
		qid, _ := toolOut["question_id"].(string)
		answer, ok := responses[qid]
		if qid == "" || !ok {
			continue
		}
		toolOut["response"] = answer
		if re, err := marshalNoEscape(m); err == nil {
			out[i] = re
		}
	}
	return out, nil
}
