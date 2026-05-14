// Package adk provides Google ADK-Go integration for agent workflows.
package adk

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"iter"
	"net/http"
	"strings"
	"time"

	"google.golang.org/adk/model"
	"google.golang.org/genai"
)

// openaiCompatibleModel implements model.LLM using the OpenAI Chat Completions wire protocol.
// It supports any OpenAI-compatible endpoint (Ollama, llama.cpp, vLLM, LM Studio, etc.)
// including full function/tool calling.
type openaiCompatibleModel struct {
	baseURL   string
	apiKey    string
	modelName string
	client    *http.Client
	// enableThinking overrides the per-request thinking default when non-nil.
	// nil  → keep existing per-request logic (disable when tools present)
	// true → always request thinking tokens
	// false → always suppress thinking tokens
	enableThinking *bool
}

// ThinkingConfigurator is implemented by models that support an explicit
// thinking/chain-of-thought toggle (e.g. Qwen3 via OpenAI-compatible endpoint).
type ThinkingConfigurator interface {
	SetEnableThinking(v *bool)
}

// NewOpenAICompatibleModel creates a new openaiCompatibleModel.
// baseURL is the base URL of the OpenAI-compatible API (e.g. "http://localhost:11434/v1").
// apiKey is optional — pass empty string for keyless local servers.
// modelName is the model to request (e.g. "llama3", "kvasir", "mistral").
func NewOpenAICompatibleModel(baseURL, apiKey, modelName string) model.LLM {
	return &openaiCompatibleModel{
		baseURL:   strings.TrimSuffix(baseURL, "/"),
		apiKey:    apiKey,
		modelName: modelName,
		client:    &http.Client{Timeout: 900 * time.Second},
	}
}

// SetEnableThinking implements ThinkingConfigurator. Pass nil to restore
// the default per-request logic (disable thinking when tools are present).
func (m *openaiCompatibleModel) SetEnableThinking(v *bool) {
	m.enableThinking = v
}

// Name returns the model name.
func (m *openaiCompatibleModel) Name() string {
	return m.modelName
}

// --- OpenAI wire types ---

type openaiMessage struct {
	Role             string           `json:"role"`
	Content          string           `json:"content,omitempty"`
	ReasoningContent string           `json:"reasoning_content,omitempty"`
	ToolCallID       string           `json:"tool_call_id,omitempty"`
	ToolCalls        []openaiToolCall `json:"tool_calls,omitempty"`
	Name             string           `json:"name,omitempty"`
}

type openaiToolCall struct {
	ID       string             `json:"id"`
	Type     string             `json:"type"` // "function"
	Function openaiToolCallFunc `json:"function"`
}

type openaiToolCallFunc struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"` // JSON string
}

type openaiTool struct {
	Type     string             `json:"type"` // "function"
	Function openaiToolFunction `json:"function"`
}

type openaiToolFunction struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Parameters  any    `json:"parameters,omitempty"`
}

type openaiRequest struct {
	Model          string          `json:"model"`
	Messages       []openaiMessage `json:"messages"`
	Tools          []openaiTool    `json:"tools,omitempty"`
	ToolChoice     string          `json:"tool_choice,omitempty"`
	MaxTokens      int32           `json:"max_tokens,omitempty"`
	ResponseFormat *responseFormat `json:"response_format,omitempty"`
	// ChatTemplateKwargs passes model-specific template arguments.
	// For Qwen3/vLLM, {"enable_thinking": false} suppresses chain-of-thought
	// reasoning tokens. This is the correct way to disable thinking on vLLM —
	// the top-level enable_thinking field does not work correctly (it empties
	// the response content field).
	ChatTemplateKwargs map[string]any `json:"chat_template_kwargs,omitempty"`
}

type responseFormat struct {
	Type string `json:"type"`
}

type openaiResponse struct {
	Choices []struct {
		Message struct {
			Content          string           `json:"content"`
			ReasoningContent string           `json:"reasoning_content,omitempty"`
			ToolCalls        []openaiToolCall `json:"tool_calls"`
		} `json:"message"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
}

// --- Role mapping ---

func mapRole(role string) string {
	switch role {
	case "model":
		return "assistant"
	case "system":
		return "system"
	default:
		return "user"
	}
}

// --- Tool schema conversion ---

// coordinationTools are internal housekeeping tools that should not count as
// "substantive" tool calls when deciding whether to force tool_choice=required.
var coordinationTools = map[string]bool{
	"list_available_agents": true,
}

// isReasonerModel returns true for models that do not support the tool_choice
// field — typically chain-of-thought / reasoning models. These models still
// support function calling but reject an explicit tool_choice constraint.
func isReasonerModel(name string) bool {
	lower := strings.ToLower(name)
	reasonerKeywords := []string{"reasoner", "deepseek-v4-flash", "deepseek-r1", "kvasir"}
	for _, kw := range reasonerKeywords {
		if strings.Contains(lower, kw) {
			return true
		}
	}
	return false
}

// hasSubstantiveToolCall returns true when the conversation history contains a
// FunctionCall to a non-coordination tool (e.g. spawn_agents, entity-create).
// Used to decide tool_choice: "required" until the model calls a real tool,
// then "auto" so it can produce a final text response.
func hasSubstantiveToolCall(contents []*genai.Content) bool {
	for _, c := range contents {
		for _, p := range c.Parts {
			if p != nil && p.FunctionCall != nil && !coordinationTools[p.FunctionCall.Name] {
				return true
			}
		}
	}
	return false
}

// hasToolResults returns true when the conversation history contains at least
// one FunctionResponse — meaning a tool has already been called and responded.
func hasToolResults(contents []*genai.Content) bool {
	for _, c := range contents {
		for _, p := range c.Parts {
			if p != nil && p.FunctionResponse != nil {
				return true
			}
		}
	}
	return false
}

// buildOpenAITools converts genai.Tool declarations to OpenAI tool format.
func buildOpenAITools(tools []*genai.Tool) []openaiTool {
	var result []openaiTool
	for _, t := range tools {
		for _, fd := range t.FunctionDeclarations {
			ot := openaiTool{
				Type: "function",
				Function: openaiToolFunction{
					Name:        fd.Name,
					Description: fd.Description,
				},
			}
			// Prefer ParametersJsonSchema (raw JSON schema) over Parameters (*Schema).
			if fd.ParametersJsonSchema != nil {
				ot.Function.Parameters = fd.ParametersJsonSchema
			} else if fd.Parameters != nil {
				ot.Function.Parameters = fd.Parameters
			} else {
				// OpenAI requires parameters to be an object schema even if empty.
				ot.Function.Parameters = map[string]any{
					"type":       "object",
					"properties": map[string]any{},
				}
			}
			result = append(result, ot)
		}
	}
	return result
}

// --- Message history conversion ---

// buildMessages converts ADK content history to OpenAI messages, including
// tool call / tool response turns.
//
// After building the message list, it validates the conversation structure:
// every tool_call_id in an assistant message's tool_calls must have a
// matching tool response. Orphaned calls get synthetic responses patched
// in to prevent DeepSeek's strict API from rejecting the request.
func buildMessages(contents []*genai.Content) []openaiMessage {
	var messages []openaiMessage
	for _, content := range contents {
		role := mapRole(content.Role)

		// Collect text parts and function calls/responses separately.
		var textParts []string
		var reasoningParts []string
		var funcCalls []openaiToolCall
		var funcResponses []struct {
			id   string
			name string
			data map[string]any
		}

		for _, part := range content.Parts {
			if part == nil {
				continue
			}
			// Route thought/reasoning parts to reasoning_content for roundtrip.
			// DeepSeek's API requires reasoning_content to be echoed back
			// on subsequent turns when the model uses thinking mode.
			if part.Thought && part.Text != "" {
				reasoningParts = append(reasoningParts, part.Text)
			} else if part.Text != "" {
				textParts = append(textParts, part.Text)
			}
			if part.FunctionCall != nil {
				fc := part.FunctionCall
				argsJSON, _ := json.Marshal(fc.Args)
				id := fc.ID
				if id == "" {
					id = "call_" + fc.Name
				}
				funcCalls = append(funcCalls, openaiToolCall{
					ID:   id,
					Type: "function",
					Function: openaiToolCallFunc{
						Name:      fc.Name,
						Arguments: string(argsJSON),
					},
				})
			}
			if part.FunctionResponse != nil {
				fr := part.FunctionResponse
				id := fr.ID
				if id == "" {
					id = "call_" + fr.Name
				}
				funcResponses = append(funcResponses, struct {
					id   string
					name string
					data map[string]any
				}{id, fr.Name, fr.Response})
			}
		}

		// Emit assistant message with tool_calls when present.
		// Note: we intentionally omit any pre-tool text (reasoning narrative)
		// from the history message. Including it inflates context on every
		// subsequent turn and can cause context-size errors on long sessions.
		// The tool results themselves convey what was done; the narrative adds
		// no value for continuation.
		if role == "assistant" && len(funcCalls) > 0 {
			msg := openaiMessage{
				Role:      "assistant",
				ToolCalls: funcCalls,
			}
			messages = append(messages, msg)
			continue
		}

		// Emit tool result messages (one per function response).
		if len(funcResponses) > 0 {
			for _, fr := range funcResponses {
				resultJSON, _ := json.Marshal(fr.data)
				messages = append(messages, openaiMessage{
					Role:       "tool",
					ToolCallID: fr.id,
					Name:       fr.name,
					Content:    string(resultJSON),
				})
			}
			continue
		}

		// Plain text message.
		// Per Qwen3 best practices, thinking/reasoning content must NOT be
		// included in multi-turn history — only the final output text is sent
		// back. This prevents reasoning blobs from ballooning context on every
		// subsequent turn.
		if len(textParts) > 0 || len(reasoningParts) > 0 {
			msg := openaiMessage{
				Role: role,
			}
			if len(textParts) > 0 {
				msg.Content = strings.Join(textParts, "\n")
			}
			// Echo reasoning_content back in assistant history turns.
			// DeepSeek requires the reasoning_content it produced to be
			// passed back verbatim on subsequent turns; omitting it causes
			// a 400 "reasoning_content must be passed back" error.
			// Qwen3/vLLM does NOT want this — but since Qwen3 sets
			// enableThinking=false via chat_template_kwargs it won't produce
			// reasoning_content in the first place, so this path is safe for both.
			if role == "assistant" && len(reasoningParts) > 0 {
				msg.ReasoningContent = strings.Join(reasoningParts, "\n")
			}
			messages = append(messages, msg)
		}
	}
	// Validate conversation structure: every tool_call ID must have a matching
	// tool response. Some providers (DeepSeek) enforce this strictly and reject
	// conversations with orphaned tool_calls. Inject synthetic tool responses
	// for any missing call IDs to keep the conversation valid.
	messages = ensureToolCallResponsePairs(messages)
	return messages
}

// ensureToolCallResponsePairs validates that every assistant tool_call has a
// corresponding tool response. This is required by providers like DeepSeek that
// enforce strict conversation ordering. Orphaned tool_calls can occur when ADK
// session history is reconstructed from persisted events and a tool's response
// was not properly serialized. For each orphaned tool_call ID, a synthetic tool
// response is injected with a neutral "tool response not available" message.
func ensureToolCallResponsePairs(messages []openaiMessage) []openaiMessage {
	// Pass 1: collect tool_call IDs (with their call data and assistant message
	// index) in encounter order, and collect all tool response IDs.
	type toolCallEntry struct {
		call         openaiToolCall
		assistantIdx int
	}
	// Use a slice to preserve discovery order and a map for O(1) lookup.
	var toolCallOrder []string // ordered IDs as discovered
	toolCallIndex := make(map[string]toolCallEntry)
	toolResponseIDs := make(map[string]bool)

	for i, msg := range messages {
		if msg.Role == "assistant" {
			for _, tc := range msg.ToolCalls {
				if _, seen := toolCallIndex[tc.ID]; !seen {
					toolCallOrder = append(toolCallOrder, tc.ID)
				}
				toolCallIndex[tc.ID] = toolCallEntry{call: tc, assistantIdx: i}
			}
		}
		if msg.Role == "tool" && msg.ToolCallID != "" {
			toolResponseIDs[msg.ToolCallID] = true
		}
	}

	if len(toolCallIndex) == 0 {
		return messages
	}

	// Pass 2: identify orphaned IDs in discovery order (deterministic).
	// Group synthetic responses by assistant message index.
	type orphanGroup struct {
		responses []openaiMessage
	}
	orphanGroups := make(map[int]*orphanGroup)
	hasOrphans := false
	for _, id := range toolCallOrder {
		if toolResponseIDs[id] {
			continue
		}
		hasOrphans = true
		entry := toolCallIndex[id]
		if orphanGroups[entry.assistantIdx] == nil {
			orphanGroups[entry.assistantIdx] = &orphanGroup{}
		}
		orphanGroups[entry.assistantIdx].responses = append(
			orphanGroups[entry.assistantIdx].responses,
			openaiMessage{
				Role:       "tool",
				ToolCallID: id,
				Name:       entry.call.Function.Name,
				Content:    `{"error":"tool response not available","note":"synthetic response inserted to maintain valid conversation structure"}`,
			},
		)
	}
	if !hasOrphans {
		return messages
	}

	// Inject synthetic responses immediately after their assistant message,
	// iterating left to right over a new result slice (no index shifting).
	result := make([]openaiMessage, 0, len(messages))
	for i, msg := range messages {
		result = append(result, msg)
		if group, ok := orphanGroups[i]; ok {
			result = append(result, group.responses...)
		}
	}
	return result
}

// GenerateContent implements model.LLM by calling the OpenAI Chat Completions API,
// including full function/tool calling support.
func (m *openaiCompatibleModel) GenerateContent(ctx context.Context, req *model.LLMRequest, stream bool) iter.Seq2[*model.LLMResponse, error] {
	return func(yield func(*model.LLMResponse, error) bool) {
		messages := buildMessages(req.Contents)

		body := openaiRequest{
			Model:    m.modelName,
			Messages: messages,
		}

		// Attach tool declarations when present.
		if req.Config != nil && len(req.Config.Tools) > 0 {
			body.Tools = buildOpenAITools(req.Config.Tools)
			// Use "required" when the model has never called a substantive tool yet
			// (i.e. only coordination tools like set_session_title have fired, or
			// nothing has fired). Switch to "auto" once spawn_agents or any
			// user-defined tool has been called, so the model can produce a final
			// text response after completing its work.
			// Skip tool_choice entirely for reasoning models (e.g. deepseek-v4-flash,
			// deepseek-reasoner) — they reject the field outright.
			if !isReasonerModel(m.modelName) {
				if hasSubstantiveToolCall(req.Contents) {
					body.ToolChoice = "auto"
				} else {
					body.ToolChoice = "required"
				}
			}
			// Disable Qwen3 thinking mode when tools are present — thinking mode
			// causes the model to reason independently and ignore system prompt
			// instructions about which tools/agents to use.
			// If the agent definition explicitly sets EnableThinking, honour that
			// instead of the default (disable) behaviour.
			// NOTE: must use chat_template_kwargs (vLLM-level) not top-level
			// enable_thinking — the top-level field empties the content field.
			wantThinking := true // default: allow thinking when no tools
			if m.enableThinking != nil {
				wantThinking = *m.enableThinking
			} else {
				wantThinking = false // default: disable when tools present
			}
			if !wantThinking {
				body.ChatTemplateKwargs = map[string]any{"enable_thinking": false}
			}
		}

		// Apply generation config.
		if req.Config != nil {
			body.MaxTokens = req.Config.MaxOutputTokens
			if req.Config.ResponseMIMEType == "application/json" {
				body.ResponseFormat = &responseFormat{Type: "json_object"}
			}
		}

		bodyBytes, err := json.Marshal(body)
		if err != nil {
			yield(nil, fmt.Errorf("openai-compatible: failed to marshal request: %w", err))
			return
		}

		httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
			m.baseURL+"/chat/completions",
			bytes.NewReader(bodyBytes))
		if err != nil {
			yield(nil, fmt.Errorf("openai-compatible: failed to create request: %w", err))
			return
		}
		httpReq.Header.Set("Content-Type", "application/json")
		if m.apiKey != "" {
			httpReq.Header.Set("Authorization", "Bearer "+m.apiKey)
		}

		resp, err := m.client.Do(httpReq)
		if err != nil {
			yield(nil, fmt.Errorf("openai-compatible: request failed: %w", err))
			return
		}
		defer resp.Body.Close()

		respBody, err := io.ReadAll(resp.Body)
		if err != nil {
			yield(nil, fmt.Errorf("openai-compatible: failed to read response: %w", err))
			return
		}

		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			yield(nil, fmt.Errorf("openai-compatible: endpoint returned %d: %s", resp.StatusCode, string(respBody)))
			return
		}

		var result openaiResponse
		if err := json.Unmarshal(respBody, &result); err != nil {
			yield(nil, fmt.Errorf("openai-compatible: failed to decode response: %w", err))
			return
		}
		if len(result.Choices) == 0 {
			yield(nil, fmt.Errorf("openai-compatible: response had no choices"))
			return
		}

		choice := result.Choices[0]
		var parts []*genai.Part

		// Emit reasoning content (DeepSeek thinking tokens) as a Thought part
		// so it's stored in the ADK content and echoed back in subsequent requests.
		if choice.Message.ReasoningContent != "" {
			parts = append(parts, &genai.Part{
				Text:    choice.Message.ReasoningContent,
				Thought: true,
			})
		}

		// Emit text content when present.
		if choice.Message.Content != "" {
			parts = append(parts, &genai.Part{Text: choice.Message.Content})
		}

		// Emit function calls when the model requested tool use.
		for _, tc := range choice.Message.ToolCalls {
			var args map[string]any
			if tc.Function.Arguments != "" {
				if err := json.Unmarshal([]byte(tc.Function.Arguments), &args); err != nil {
					// Fallback: wrap raw string as {"_raw": "..."}
					args = map[string]any{"_raw": tc.Function.Arguments}
				}
			}
			parts = append(parts, &genai.Part{
				FunctionCall: &genai.FunctionCall{
					ID:   tc.ID,
					Name: tc.Function.Name,
					Args: args,
				},
			})
		}

		if len(parts) == 0 {
			yield(nil, fmt.Errorf("openai-compatible: response had no content or tool calls"))
			return
		}

		yield(&model.LLMResponse{
			Content: &genai.Content{
				Role:  "model",
				Parts: parts,
			},
		}, nil)
	}
}
