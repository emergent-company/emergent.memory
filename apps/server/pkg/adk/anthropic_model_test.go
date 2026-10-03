package adk

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"google.golang.org/adk/model"
	"google.golang.org/genai"
)

// makeAnthropicResponse builds a minimal Anthropic Messages JSON response with
// one text block, one tool_use block, and a usage object.
func makeAnthropicResponse(text string) string {
	return `{
		"content": [
			{"type": "text", "text": "` + text + `"},
			{"type": "tool_use", "id": "toolu_01", "name": "get_weather", "input": {"city": "Paris"}}
		],
		"usage": {"input_tokens": 12, "output_tokens": 34}
	}`
}

// collectAnthropicResponse drains the iterator returned by GenerateContent and
// returns the first response and first error encountered.
func collectAnthropicResponse(seq func(yield func(*model.LLMResponse, error) bool)) (*model.LLMResponse, error) {
	var resp *model.LLMResponse
	var firstErr error
	seq(func(r *model.LLMResponse, err error) bool {
		if err != nil && firstErr == nil {
			firstErr = err
		}
		if r != nil && resp == nil {
			resp = r
		}
		return true
	})
	return resp, firstErr
}

// TestAnthropicModel_Name verifies Name() returns the configured model name.
func TestAnthropicModel_Name(t *testing.T) {
	m := NewAnthropicModel("https://api.anthropic.com/v1", "sk-ant-test", "claude-sonnet-4-5")
	if m.Name() != "claude-sonnet-4-5" {
		t.Errorf("Name() = %q, want %q", m.Name(), "claude-sonnet-4-5")
	}
}

// TestAnthropicModel_RequestFormat verifies the HTTP request sent to the
// endpoint has the correct path, headers, model, max_tokens, and role mapping.
func TestAnthropicModel_RequestFormat(t *testing.T) {
	var captured anthropicRequest
	var path, apiKeyHeader, versionHeader, contentType string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		apiKeyHeader = r.Header.Get("x-api-key")
		versionHeader = r.Header.Get("anthropic-version")
		contentType = r.Header.Get("Content-Type")
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &captured)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(makeAnthropicResponse("hello")))
	}))
	defer srv.Close()

	m := NewAnthropicModel(srv.URL, "sk-ant-test", "claude-sonnet-4-5")
	req := &model.LLMRequest{
		Contents: []*genai.Content{
			{Role: "user", Parts: []*genai.Part{{Text: "Say hello"}}},
			{Role: "model", Parts: []*genai.Part{{Text: "Hi there!"}}},
		},
		Config: &genai.GenerateContentConfig{
			MaxOutputTokens: 128,
			SystemInstruction: &genai.Content{
				Parts: []*genai.Part{{Text: "You are helpful."}, {Text: "Be concise."}},
			},
		},
	}

	resp, err := collectAnthropicResponse(m.GenerateContent(context.Background(), req, false))
	if err != nil {
		t.Fatalf("GenerateContent() error = %v", err)
	}
	if resp == nil {
		t.Fatal("GenerateContent() returned nil response")
	}

	if path != "/messages" {
		t.Errorf("request path = %q, want %q", path, "/messages")
	}
	if apiKeyHeader != "sk-ant-test" {
		t.Errorf("x-api-key header = %q, want %q", apiKeyHeader, "sk-ant-test")
	}
	if versionHeader != "2023-06-01" {
		t.Errorf("anthropic-version header = %q, want %q", versionHeader, "2023-06-01")
	}
	if contentType != "application/json" {
		t.Errorf("Content-Type header = %q, want %q", contentType, "application/json")
	}
	if captured.Model != "claude-sonnet-4-5" {
		t.Errorf("request model = %q, want %q", captured.Model, "claude-sonnet-4-5")
	}
	if captured.MaxTokens != 128 {
		t.Errorf("max_tokens = %d, want 128", captured.MaxTokens)
	}
	if captured.System != "You are helpful.\nBe concise." {
		t.Errorf("system = %q, want %q", captured.System, "You are helpful.\nBe concise.")
	}

	if len(captured.Messages) != 2 {
		t.Fatalf("messages count = %d, want 2", len(captured.Messages))
	}
	if captured.Messages[0].Role != "user" {
		t.Errorf("messages[0].role = %q, want %q", captured.Messages[0].Role, "user")
	}
	if captured.Messages[1].Role != "assistant" {
		t.Errorf("messages[1].role = %q, want %q", captured.Messages[1].Role, "assistant")
	}
}

// TestAnthropicModel_DefaultMaxTokens verifies max_tokens defaults to 4096 when
// MaxOutputTokens is not set (or <= 0).
func TestAnthropicModel_DefaultMaxTokens(t *testing.T) {
	var captured anthropicRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &captured)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(makeAnthropicResponse("ok")))
	}))
	defer srv.Close()

	m := NewAnthropicModel(srv.URL, "sk-ant-test", "claude-sonnet-4-5")
	req := &model.LLMRequest{
		Contents: []*genai.Content{
			{Role: "user", Parts: []*genai.Part{{Text: "hi"}}},
		},
	}

	_, err := collectAnthropicResponse(m.GenerateContent(context.Background(), req, false))
	if err != nil {
		t.Fatalf("GenerateContent() error = %v", err)
	}

	if captured.MaxTokens != 4096 {
		t.Errorf("max_tokens = %d, want 4096", captured.MaxTokens)
	}
}

// TestAnthropicModel_RoleMapping verifies ADK roles are mapped correctly to
// Anthropic roles: model→assistant, user→user.
func TestAnthropicModel_RoleMapping(t *testing.T) {
	var captured anthropicRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &captured)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(makeAnthropicResponse("ok")))
	}))
	defer srv.Close()

	m := NewAnthropicModel(srv.URL, "sk-ant-test", "claude-sonnet-4-5")
	req := &model.LLMRequest{
		Contents: []*genai.Content{
			{Role: "user", Parts: []*genai.Part{{Text: "Hello"}}},
			{Role: "model", Parts: []*genai.Part{{Text: "Hi there!"}}},
			{Role: "user", Parts: []*genai.Part{{Text: "How are you?"}}},
		},
	}

	_, err := collectAnthropicResponse(m.GenerateContent(context.Background(), req, false))
	if err != nil {
		t.Fatalf("GenerateContent() error = %v", err)
	}

	if len(captured.Messages) != 3 {
		t.Fatalf("messages count = %d, want 3", len(captured.Messages))
	}
	cases := []struct{ role, content string }{
		{"user", "Hello"},
		{"assistant", "Hi there!"},
		{"user", "How are you?"},
	}
	for i, c := range cases {
		if captured.Messages[i].Role != c.role {
			t.Errorf("messages[%d].role = %q, want %q", i, captured.Messages[i].Role, c.role)
		}
		if captured.Messages[i].Content[0].Text != c.content {
			t.Errorf("messages[%d].content = %q, want %q", i, captured.Messages[i].Content[0].Text, c.content)
		}
	}
}

// TestAnthropicModel_MultiPartConcatenation verifies that multiple text parts in
// a single message are joined with newlines.
func TestAnthropicModel_MultiPartConcatenation(t *testing.T) {
	var captured anthropicRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &captured)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(makeAnthropicResponse("ok")))
	}))
	defer srv.Close()

	m := NewAnthropicModel(srv.URL, "sk-ant-test", "claude-sonnet-4-5")
	req := &model.LLMRequest{
		Contents: []*genai.Content{
			{
				Role: "user",
				Parts: []*genai.Part{
					{Text: "Part one."},
					{Text: "Part two."},
					{Text: "Part three."},
				},
			},
		},
	}

	_, err := collectAnthropicResponse(m.GenerateContent(context.Background(), req, false))
	if err != nil {
		t.Fatalf("GenerateContent() error = %v", err)
	}

	if len(captured.Messages) != 1 {
		t.Fatalf("messages count = %d, want 1", len(captured.Messages))
	}
	want := "Part one.\nPart two.\nPart three."
	if captured.Messages[0].Content[0].Text != want {
		t.Errorf("content = %q, want %q", captured.Messages[0].Content[0].Text, want)
	}
}

// TestAnthropicModel_ToolUse verifies genai.FunctionCall and
// genai.FunctionResponse parts are converted to tool_use / tool_result blocks,
// and that FunctionDeclarations are converted to Anthropic tools.
func TestAnthropicModel_ToolUse(t *testing.T) {
	var captured anthropicRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &captured)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(makeAnthropicResponse("ok")))
	}))
	defer srv.Close()

	m := NewAnthropicModel(srv.URL, "sk-ant-test", "claude-sonnet-4-5")
	req := &model.LLMRequest{
		Contents: []*genai.Content{
			{
				Role: "model",
				Parts: []*genai.Part{
					{
						FunctionCall: &genai.FunctionCall{
							ID:   "toolu_01",
							Name: "get_weather",
							Args: map[string]any{"city": "Paris"},
						},
					},
				},
			},
			{
				Role: "user",
				Parts: []*genai.Part{
					{
						FunctionResponse: &genai.FunctionResponse{
							ID:       "toolu_01",
							Name:     "get_weather",
							Response: map[string]any{"temp": "20C"},
						},
					},
				},
			},
		},
		Config: &genai.GenerateContentConfig{
			Tools: []*genai.Tool{
				{
					FunctionDeclarations: []*genai.FunctionDeclaration{
						{
							Name:        "get_weather",
							Description: "Get the weather",
							ParametersJsonSchema: map[string]any{
								"type":       "object",
								"properties": map[string]any{"city": map[string]any{"type": "string"}},
							},
						},
					},
				},
			},
		},
	}

	_, err := collectAnthropicResponse(m.GenerateContent(context.Background(), req, false))
	if err != nil {
		t.Fatalf("GenerateContent() error = %v", err)
	}

	// Assistant turn: tool_use block.
	if len(captured.Messages) != 2 {
		t.Fatalf("messages count = %d, want 2", len(captured.Messages))
	}
	assistant := captured.Messages[0]
	if assistant.Role != "assistant" {
		t.Errorf("messages[0].role = %q, want assistant", assistant.Role)
	}
	if len(assistant.Content) != 1 || assistant.Content[0].Type != "tool_use" {
		t.Fatalf("messages[0] content = %+v, want single tool_use block", assistant.Content)
	}
	if assistant.Content[0].Name != "get_weather" {
		t.Errorf("tool_use name = %q, want get_weather", assistant.Content[0].Name)
	}
	if assistant.Content[0].ID != "toolu_01" {
		t.Errorf("tool_use id = %q, want toolu_01", assistant.Content[0].ID)
	}

	// User turn: tool_result block.
	user := captured.Messages[1]
	if user.Role != "user" {
		t.Errorf("messages[1].role = %q, want user", user.Role)
	}
	if len(user.Content) != 1 || user.Content[0].Type != "tool_result" {
		t.Fatalf("messages[1] content = %+v, want single tool_result block", user.Content)
	}
	if user.Content[0].ToolUseID != "toolu_01" {
		t.Errorf("tool_result tool_use_id = %q, want toolu_01", user.Content[0].ToolUseID)
	}
	if user.Content[0].Content != `{"temp":"20C"}` {
		t.Errorf("tool_result content = %v, want %q", user.Content[0].Content, `{"temp":"20C"}`)
	}

	// Tools schema.
	if len(captured.Tools) != 1 {
		t.Fatalf("tools count = %d, want 1", len(captured.Tools))
	}
	if captured.Tools[0].Name != "get_weather" {
		t.Errorf("tools[0].name = %q, want get_weather", captured.Tools[0].Name)
	}
	if captured.Tools[0].Description != "Get the weather" {
		t.Errorf("tools[0].description = %q, want Get the weather", captured.Tools[0].Description)
	}
	if captured.Tools[0].InputSchema == nil {
		t.Error("tools[0].input_schema is nil, want a JSON schema object")
	}
}

// TestAnthropicModel_Response verifies the canned response is parsed into text
// and function-call parts with correct role and usage metadata.
func TestAnthropicModel_Response(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(makeAnthropicResponse("The weather in Paris is 20C.")))
	}))
	defer srv.Close()

	m := NewAnthropicModel(srv.URL, "sk-ant-test", "claude-sonnet-4-5")
	req := &model.LLMRequest{
		Contents: []*genai.Content{
			{Role: "user", Parts: []*genai.Part{{Text: "What's the weather?"}}},
		},
	}

	resp, err := collectAnthropicResponse(m.GenerateContent(context.Background(), req, false))
	if err != nil {
		t.Fatalf("GenerateContent() error = %v", err)
	}
	if resp == nil || resp.Content == nil {
		t.Fatal("response or content is nil")
	}
	if resp.Content.Role != "model" {
		t.Errorf("response role = %q, want %q", resp.Content.Role, "model")
	}
	if len(resp.Content.Parts) != 2 {
		t.Fatalf("parts count = %d, want 2", len(resp.Content.Parts))
	}
	if resp.Content.Parts[0].Text != "The weather in Paris is 20C." {
		t.Errorf("parts[0].text = %q, want %q", resp.Content.Parts[0].Text, "The weather in Paris is 20C.")
	}
	fc := resp.Content.Parts[1].FunctionCall
	if fc == nil {
		t.Fatal("parts[1] function call is nil")
	}
	if fc.Name != "get_weather" {
		t.Errorf("function call name = %q, want get_weather", fc.Name)
	}
	if fc.ID != "toolu_01" {
		t.Errorf("function call id = %q, want toolu_01", fc.ID)
	}
	if fc.Args["city"] != "Paris" {
		t.Errorf("function call args = %+v, want city=Paris", fc.Args)
	}

	if resp.UsageMetadata == nil {
		t.Fatal("usage metadata is nil")
	}
	if resp.UsageMetadata.PromptTokenCount != 12 {
		t.Errorf("prompt token count = %d, want 12", resp.UsageMetadata.PromptTokenCount)
	}
	if resp.UsageMetadata.CandidatesTokenCount != 34 {
		t.Errorf("candidates token count = %d, want 34", resp.UsageMetadata.CandidatesTokenCount)
	}
	if resp.UsageMetadata.TotalTokenCount != 46 {
		t.Errorf("total token count = %d, want 46", resp.UsageMetadata.TotalTokenCount)
	}
}

// TestAnthropicModel_Non2xxError verifies a descriptive error is returned for
// non-2xx HTTP responses, including the status code and body.
func TestAnthropicModel_Non2xxError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"type":"authentication_error","message":"invalid x-api-key"}}`))
	}))
	defer srv.Close()

	m := NewAnthropicModel(srv.URL, "bad-key", "claude-sonnet-4-5")
	req := &model.LLMRequest{
		Contents: []*genai.Content{
			{Role: "user", Parts: []*genai.Part{{Text: "hi"}}},
		},
	}

	_, err := collectAnthropicResponse(m.GenerateContent(context.Background(), req, false))
	if err == nil {
		t.Fatal("expected error for 401 response, got nil")
	}
	if !strings.Contains(err.Error(), "anthropic") {
		t.Errorf("error %q should be prefixed with anthropic", err.Error())
	}
	if !strings.Contains(err.Error(), "401") {
		t.Errorf("error %q should contain status code 401", err.Error())
	}
	if !strings.Contains(err.Error(), "invalid x-api-key") {
		t.Errorf("error %q should contain response body", err.Error())
	}
}

// TestAnthropicModel_EmptyContent verifies a descriptive error is returned when
// the response has no content blocks.
func TestAnthropicModel_EmptyContent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"content":[],"usage":{"input_tokens":1,"output_tokens":0}}`))
	}))
	defer srv.Close()

	m := NewAnthropicModel(srv.URL, "sk-ant-test", "claude-sonnet-4-5")
	req := &model.LLMRequest{
		Contents: []*genai.Content{
			{Role: "user", Parts: []*genai.Part{{Text: "hi"}}},
		},
	}

	_, err := collectAnthropicResponse(m.GenerateContent(context.Background(), req, false))
	if err == nil {
		t.Fatal("expected error for empty content, got nil")
	}
	if !strings.Contains(err.Error(), "no content or tool use") {
		t.Errorf("error %q should mention empty content", err.Error())
	}
}
