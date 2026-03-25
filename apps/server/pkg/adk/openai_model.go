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
// It supports any OpenAI-compatible endpoint (Ollama, llama.cpp, vLLM, LM Studio, etc.).
type openaiCompatibleModel struct {
	baseURL   string
	apiKey    string
	modelName string
	client    *http.Client
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
		client:    &http.Client{Timeout: 120 * time.Second},
	}
}

// Name returns the model name.
func (m *openaiCompatibleModel) Name() string {
	return m.modelName
}

// openaiMessage is a single message in the Chat Completions request.
type openaiMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// openaiRequest is the Chat Completions request body.
type openaiRequest struct {
	Model          string          `json:"model"`
	Messages       []openaiMessage `json:"messages"`
	MaxTokens      int32           `json:"max_tokens,omitempty"`
	ResponseFormat *responseFormat `json:"response_format,omitempty"`
}

type responseFormat struct {
	Type string `json:"type"`
}

// openaiResponse is the Chat Completions response body.
type openaiResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
}

// mapRole converts an ADK message role to an OpenAI role.
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

// GenerateContent implements model.LLM by calling the OpenAI Chat Completions API.
func (m *openaiCompatibleModel) GenerateContent(ctx context.Context, req *model.LLMRequest, stream bool) iter.Seq2[*model.LLMResponse, error] {
	return func(yield func(*model.LLMResponse, error) bool) {
		// Build messages from ADK request contents.
		var messages []openaiMessage
		for _, content := range req.Contents {
			role := mapRole(content.Role)
			// Concatenate all text parts into a single content string.
			var parts []string
			for _, part := range content.Parts {
				if part.Text != "" {
					parts = append(parts, part.Text)
				}
			}
			if len(parts) > 0 {
				messages = append(messages, openaiMessage{
					Role:    role,
					Content: strings.Join(parts, "\n"),
				})
			}
		}

		// Build request body.
		body := openaiRequest{
			Model:    m.modelName,
			Messages: messages,
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

		text := result.Choices[0].Message.Content
		yield(&model.LLMResponse{
			Content: &genai.Content{
				Role: "model",
				Parts: []*genai.Part{
					{Text: text},
				},
			},
		}, nil)
	}
}
