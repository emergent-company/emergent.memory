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

// anthropicModel implements model.LLM using the Anthropic Messages API wire
// protocol (https://docs.anthropic.com/en/api/messages), including full
// function/tool calling.
type anthropicModel struct {
	baseURL   string
	apiKey    string
	modelName string
	client    *http.Client
}

// NewAnthropicModel creates a new anthropicModel.
// baseURL is the base URL of the Anthropic API (e.g. "https://api.anthropic.com/v1").
// apiKey is the x-api-key credential.
// modelName is the model to request (e.g. "claude-sonnet-4-5").
func NewAnthropicModel(baseURL, apiKey, modelName string) model.LLM {
	return &anthropicModel{
		baseURL:   strings.TrimSuffix(baseURL, "/"),
		apiKey:    apiKey,
		modelName: modelName,
		client:    &http.Client{Timeout: 900 * time.Second},
	}
}

// Name returns the model name.
func (m *anthropicModel) Name() string {
	return m.modelName
}

// --- Anthropic wire types ---

// anthropicContentBlock is a single content block in a Messages API request or
// response. It covers text, tool_use, and tool_result block types.
type anthropicContentBlock struct {
	Type      string         `json:"type"` // "text", "tool_use", "tool_result"
	Text      string         `json:"text,omitempty"`
	ID        string         `json:"id,omitempty"`
	Name      string         `json:"name,omitempty"`
	Input     map[string]any `json:"input,omitempty"`
	ToolUseID string         `json:"tool_use_id,omitempty"`
	Content   any            `json:"content,omitempty"` // tool_result content
}

type anthropicMessage struct {
	Role    string                  `json:"role"`
	Content []anthropicContentBlock `json:"content"`
}

type anthropicTool struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	InputSchema any    `json:"input_schema"`
}

type anthropicRequest struct {
	Model     string             `json:"model"`
	MaxTokens int32              `json:"max_tokens"`
	System    string             `json:"system,omitempty"`
	Messages  []anthropicMessage `json:"messages"`
	Tools     []anthropicTool    `json:"tools,omitempty"`
}

type anthropicResponse struct {
	Content []anthropicContentBlock `json:"content"`
	Usage   *struct {
		InputTokens  int32 `json:"input_tokens"`
		OutputTokens int32 `json:"output_tokens"`
	} `json:"usage,omitempty"`
}

// --- Role mapping ---

func mapAnthropicRole(role string) string {
	switch role {
	case "model":
		return "assistant"
	default:
		return "user"
	}
}

// --- Tool schema conversion ---

// buildAnthropicTools converts genai.Tool declarations to Anthropic tool format.
func buildAnthropicTools(tools []*genai.Tool) []anthropicTool {
	var result []anthropicTool
	for _, t := range tools {
		for _, fd := range t.FunctionDeclarations {
			at := anthropicTool{
				Name:        fd.Name,
				Description: fd.Description,
			}
			// Prefer ParametersJsonSchema (raw JSON schema) over Parameters (*Schema).
			if fd.ParametersJsonSchema != nil {
				at.InputSchema = fd.ParametersJsonSchema
			} else if fd.Parameters != nil {
				at.InputSchema = fd.Parameters
			} else {
				// Anthropic requires input_schema to be a JSON Schema object.
				at.InputSchema = map[string]any{
					"type":       "object",
					"properties": map[string]any{},
				}
			}
			result = append(result, at)
		}
	}
	return result
}

// --- Message history conversion ---

// buildAnthropicMessages converts ADK content history to Anthropic messages,
// including tool use / tool result turns. Multiple text parts within a single
// turn are joined with newlines into one text block.
func buildAnthropicMessages(contents []*genai.Content) []anthropicMessage {
	var messages []anthropicMessage
	for _, content := range contents {
		role := mapAnthropicRole(content.Role)

		var textParts []string
		var blocks []anthropicContentBlock

		for _, part := range content.Parts {
			if part == nil {
				continue
			}
			if part.Text != "" {
				textParts = append(textParts, part.Text)
			}
			if part.FunctionCall != nil {
				fc := part.FunctionCall
				id := fc.ID
				if id == "" {
					id = "call_" + fc.Name
				}
				args := fc.Args
				if args == nil {
					args = map[string]any{}
				}
				blocks = append(blocks, anthropicContentBlock{
					Type:  "tool_use",
					ID:    id,
					Name:  fc.Name,
					Input: args,
				})
			}
			if part.FunctionResponse != nil {
				fr := part.FunctionResponse
				id := fr.ID
				if id == "" {
					id = "call_" + fr.Name
				}
				resultJSON, _ := json.Marshal(fr.Response)
				blocks = append(blocks, anthropicContentBlock{
					Type:      "tool_result",
					ToolUseID: id,
					Content:   string(resultJSON),
				})
			}
		}

		// Emit a single text block for all text parts in this turn.
		if len(textParts) > 0 {
			blocks = append(blocks, anthropicContentBlock{
				Type: "text",
				Text: strings.Join(textParts, "\n"),
			})
		}

		if len(blocks) > 0 {
			messages = append(messages, anthropicMessage{
				Role:    role,
				Content: blocks,
			})
		}
	}
	return messages
}

// GenerateContent implements model.LLM by calling the Anthropic Messages API,
// including full function/tool calling support. Streaming is not implemented;
// regardless of the stream flag, exactly one final LLMResponse is yielded.
func (m *anthropicModel) GenerateContent(ctx context.Context, req *model.LLMRequest, stream bool) iter.Seq2[*model.LLMResponse, error] {
	return func(yield func(*model.LLMResponse, error) bool) {
		messages := buildAnthropicMessages(req.Contents)

		body := anthropicRequest{
			Model:    m.modelName,
			Messages: messages,
		}

		// max_tokens is mandatory on the Messages API. Use the configured value
		// when positive, otherwise fall back to 4096.
		maxTokens := int32(4096)
		if req.Config != nil && req.Config.MaxOutputTokens > 0 {
			maxTokens = req.Config.MaxOutputTokens
		}
		body.MaxTokens = maxTokens

		// System instruction is a top-level string on the Messages API (not a
		// message role). Join all text parts with newlines.
		if req.Config != nil && req.Config.SystemInstruction != nil {
			var siParts []string
			for _, p := range req.Config.SystemInstruction.Parts {
				if p != nil && p.Text != "" {
					siParts = append(siParts, p.Text)
				}
			}
			if len(siParts) > 0 {
				body.System = strings.Join(siParts, "\n")
			}
		}

		// Attach tool declarations when present.
		if req.Config != nil && len(req.Config.Tools) > 0 {
			body.Tools = buildAnthropicTools(req.Config.Tools)
		}

		// ResponseMIMEType is not supported by the Anthropic Messages API; it is
		// intentionally a no-op here.

		bodyBytes, err := json.Marshal(body)
		if err != nil {
			yield(nil, fmt.Errorf("anthropic: failed to marshal request: %w", err))
			return
		}

		httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
			m.baseURL+"/messages",
			bytes.NewReader(bodyBytes))
		if err != nil {
			yield(nil, fmt.Errorf("anthropic: failed to create request: %w", err))
			return
		}
		httpReq.Header.Set("Content-Type", "application/json")
		httpReq.Header.Set("anthropic-version", "2023-06-01")
		if m.apiKey != "" {
			httpReq.Header.Set("x-api-key", m.apiKey)
		}

		resp, err := m.client.Do(httpReq)
		if err != nil {
			yield(nil, fmt.Errorf("anthropic: request failed: %w", err))
			return
		}
		defer resp.Body.Close()

		respBody, err := io.ReadAll(resp.Body)
		if err != nil {
			yield(nil, fmt.Errorf("anthropic: failed to read response: %w", err))
			return
		}

		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			yield(nil, fmt.Errorf("anthropic: endpoint returned %d (url=%s model=%s req_bytes=%d): %s", resp.StatusCode, m.baseURL, m.modelName, len(bodyBytes), string(respBody)))
			return
		}

		var result anthropicResponse
		if err := json.Unmarshal(respBody, &result); err != nil {
			yield(nil, fmt.Errorf("anthropic: failed to decode response: %w", err))
			return
		}

		var parts []*genai.Part

		for _, block := range result.Content {
			switch block.Type {
			case "text":
				if block.Text != "" {
					parts = append(parts, &genai.Part{Text: block.Text})
				}
			case "tool_use":
				input := block.Input
				if input == nil {
					input = map[string]any{}
				}
				parts = append(parts, &genai.Part{
					FunctionCall: &genai.FunctionCall{
						ID:   block.ID,
						Name: block.Name,
						Args: input,
					},
				})
			}
		}

		if len(parts) == 0 {
			yield(nil, fmt.Errorf("anthropic: response had no content or tool use"))
			return
		}

		llmResp := &model.LLMResponse{
			Content: &genai.Content{
				Role:  "model",
				Parts: parts,
			},
			Partial: false,
		}
		if result.Usage != nil {
			llmResp.UsageMetadata = &genai.GenerateContentResponseUsageMetadata{
				PromptTokenCount:     result.Usage.InputTokens,
				CandidatesTokenCount: result.Usage.OutputTokens,
				TotalTokenCount:      result.Usage.InputTokens + result.Usage.OutputTokens,
			}
		}
		yield(llmResp, nil)
	}
}
