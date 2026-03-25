package adk

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/emergent-company/emergent.memory/internal/config"
	"google.golang.org/adk/model"
	"google.golang.org/genai"
)

// makeOpenAIResponse builds a minimal OpenAI Chat Completions JSON response.
func makeOpenAIResponse(content string) string {
	return `{"choices":[{"message":{"content":"` + content + `"}}]}`
}

// collectResponse drains the iterator returned by GenerateContent and returns
// the first response and first error encountered.
func collectResponse(seq func(yield func(*model.LLMResponse, error) bool)) (*model.LLMResponse, error) {
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

// TestOpenAICompatibleModel_Name verifies Name() returns the configured model name.
func TestOpenAICompatibleModel_Name(t *testing.T) {
	m := NewOpenAICompatibleModel("http://localhost:11434/v1", "", "llama3")
	if m.Name() != "llama3" {
		t.Errorf("Name() = %q, want %q", m.Name(), "llama3")
	}
}

// TestOpenAICompatibleModel_RequestFormat verifies the HTTP request sent to the
// endpoint has the correct model, messages, and max_tokens fields (task 17.1).
func TestOpenAICompatibleModel_RequestFormat(t *testing.T) {
	var captured openaiRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &captured)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(makeOpenAIResponse("hello")))
	}))
	defer srv.Close()

	m := NewOpenAICompatibleModel(srv.URL, "", "llama3")
	req := &model.LLMRequest{
		Contents: []*genai.Content{
			{Role: "user", Parts: []*genai.Part{{Text: "Say hello"}}},
		},
		Config: &genai.GenerateContentConfig{MaxOutputTokens: 64},
	}

	resp, err := collectResponse(m.GenerateContent(context.Background(), req, false))
	if err != nil {
		t.Fatalf("GenerateContent() error = %v", err)
	}
	if resp == nil {
		t.Fatal("GenerateContent() returned nil response")
	}

	if captured.Model != "llama3" {
		t.Errorf("request model = %q, want %q", captured.Model, "llama3")
	}
	if len(captured.Messages) != 1 {
		t.Fatalf("request messages count = %d, want 1", len(captured.Messages))
	}
	if captured.Messages[0].Role != "user" {
		t.Errorf("message role = %q, want %q", captured.Messages[0].Role, "user")
	}
	if captured.Messages[0].Content != "Say hello" {
		t.Errorf("message content = %q, want %q", captured.Messages[0].Content, "Say hello")
	}
	if captured.MaxTokens != 64 {
		t.Errorf("max_tokens = %d, want 64", captured.MaxTokens)
	}
}

// TestOpenAICompatibleModel_AuthHeader verifies the Authorization header is set
// when an API key is provided (task 17.1).
func TestOpenAICompatibleModel_AuthHeader(t *testing.T) {
	var authHeader string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(makeOpenAIResponse("ok")))
	}))
	defer srv.Close()

	m := NewOpenAICompatibleModel(srv.URL, "sk-test-key", "llama3")
	req := &model.LLMRequest{
		Contents: []*genai.Content{
			{Role: "user", Parts: []*genai.Part{{Text: "hi"}}},
		},
	}

	_, err := collectResponse(m.GenerateContent(context.Background(), req, false))
	if err != nil {
		t.Fatalf("GenerateContent() error = %v", err)
	}

	want := "Bearer sk-test-key"
	if authHeader != want {
		t.Errorf("Authorization header = %q, want %q", authHeader, want)
	}
}

// TestOpenAICompatibleModel_NoAuthHeaderWhenKeyEmpty verifies no Authorization
// header is sent when the API key is empty (keyless local servers).
func TestOpenAICompatibleModel_NoAuthHeaderWhenKeyEmpty(t *testing.T) {
	var authHeader string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(makeOpenAIResponse("ok")))
	}))
	defer srv.Close()

	m := NewOpenAICompatibleModel(srv.URL, "", "llama3")
	req := &model.LLMRequest{
		Contents: []*genai.Content{
			{Role: "user", Parts: []*genai.Part{{Text: "hi"}}},
		},
	}

	_, err := collectResponse(m.GenerateContent(context.Background(), req, false))
	if err != nil {
		t.Fatalf("GenerateContent() error = %v", err)
	}

	if authHeader != "" {
		t.Errorf("Authorization header = %q, want empty", authHeader)
	}
}

// TestOpenAICompatibleModel_JSONMode verifies response_format is included when
// ResponseMIMEType is "application/json" (task 17.2).
func TestOpenAICompatibleModel_JSONMode(t *testing.T) {
	var captured openaiRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &captured)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(makeOpenAIResponse("result ok")))
	}))
	defer srv.Close()

	m := NewOpenAICompatibleModel(srv.URL, "", "llama3")
	req := &model.LLMRequest{
		Contents: []*genai.Content{
			{Role: "user", Parts: []*genai.Part{{Text: "Return JSON"}}},
		},
		Config: &genai.GenerateContentConfig{
			ResponseMIMEType: "application/json",
		},
	}

	_, err := collectResponse(m.GenerateContent(context.Background(), req, false))
	if err != nil {
		t.Fatalf("GenerateContent() error = %v", err)
	}

	if captured.ResponseFormat == nil {
		t.Fatal("response_format is nil, want {type: json_object}")
	}
	if captured.ResponseFormat.Type != "json_object" {
		t.Errorf("response_format.type = %q, want %q", captured.ResponseFormat.Type, "json_object")
	}
}

// TestOpenAICompatibleModel_NoJSONModeByDefault verifies response_format is
// omitted when ResponseMIMEType is not set.
func TestOpenAICompatibleModel_NoJSONModeByDefault(t *testing.T) {
	var captured openaiRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &captured)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(makeOpenAIResponse("hello")))
	}))
	defer srv.Close()

	m := NewOpenAICompatibleModel(srv.URL, "", "llama3")
	req := &model.LLMRequest{
		Contents: []*genai.Content{
			{Role: "user", Parts: []*genai.Part{{Text: "hi"}}},
		},
	}

	_, err := collectResponse(m.GenerateContent(context.Background(), req, false))
	if err != nil {
		t.Fatalf("GenerateContent() error = %v", err)
	}

	if captured.ResponseFormat != nil {
		t.Errorf("response_format = %+v, want nil", captured.ResponseFormat)
	}
}

// TestOpenAICompatibleModel_RoleMapping verifies ADK roles are mapped correctly
// to OpenAI roles: model→assistant, system→system, user→user (task 17.3).
func TestOpenAICompatibleModel_RoleMapping(t *testing.T) {
	var captured openaiRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &captured)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(makeOpenAIResponse("ok")))
	}))
	defer srv.Close()

	m := NewOpenAICompatibleModel(srv.URL, "", "llama3")
	req := &model.LLMRequest{
		Contents: []*genai.Content{
			{Role: "system", Parts: []*genai.Part{{Text: "You are helpful."}}},
			{Role: "user", Parts: []*genai.Part{{Text: "Hello"}}},
			{Role: "model", Parts: []*genai.Part{{Text: "Hi there!"}}},
			{Role: "user", Parts: []*genai.Part{{Text: "How are you?"}}},
		},
	}

	_, err := collectResponse(m.GenerateContent(context.Background(), req, false))
	if err != nil {
		t.Fatalf("GenerateContent() error = %v", err)
	}

	if len(captured.Messages) != 4 {
		t.Fatalf("messages count = %d, want 4", len(captured.Messages))
	}

	cases := []struct{ role, content string }{
		{"system", "You are helpful."},
		{"user", "Hello"},
		{"assistant", "Hi there!"},
		{"user", "How are you?"},
	}
	for i, c := range cases {
		if captured.Messages[i].Role != c.role {
			t.Errorf("messages[%d].role = %q, want %q", i, captured.Messages[i].Role, c.role)
		}
		if captured.Messages[i].Content != c.content {
			t.Errorf("messages[%d].content = %q, want %q", i, captured.Messages[i].Content, c.content)
		}
	}
}

// TestOpenAICompatibleModel_MultiPartConcatenation verifies that multiple text
// parts in a single message are joined with newlines (task 17.4).
func TestOpenAICompatibleModel_MultiPartConcatenation(t *testing.T) {
	var captured openaiRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &captured)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(makeOpenAIResponse("ok")))
	}))
	defer srv.Close()

	m := NewOpenAICompatibleModel(srv.URL, "", "llama3")
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

	_, err := collectResponse(m.GenerateContent(context.Background(), req, false))
	if err != nil {
		t.Fatalf("GenerateContent() error = %v", err)
	}

	if len(captured.Messages) != 1 {
		t.Fatalf("messages count = %d, want 1", len(captured.Messages))
	}
	want := "Part one.\nPart two.\nPart three."
	if captured.Messages[0].Content != want {
		t.Errorf("content = %q, want %q", captured.Messages[0].Content, want)
	}
}

// TestOpenAICompatibleModel_Non2xxError verifies a descriptive error is returned
// for non-2xx HTTP responses (task 17.5).
func TestOpenAICompatibleModel_Non2xxError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"invalid api key"}`))
	}))
	defer srv.Close()

	m := NewOpenAICompatibleModel(srv.URL, "bad-key", "llama3")
	req := &model.LLMRequest{
		Contents: []*genai.Content{
			{Role: "user", Parts: []*genai.Part{{Text: "hi"}}},
		},
	}

	_, err := collectResponse(m.GenerateContent(context.Background(), req, false))
	if err == nil {
		t.Fatal("expected error for 401 response, got nil")
	}
	if !strings.Contains(err.Error(), "401") {
		t.Errorf("error %q should contain status code 401", err.Error())
	}
	if !strings.Contains(err.Error(), "invalid api key") {
		t.Errorf("error %q should contain response body", err.Error())
	}
}

// TestOpenAICompatibleModel_NetworkError verifies a descriptive error is returned
// for network failures (task 17.5 / 17.8).
func TestOpenAICompatibleModel_NetworkError(t *testing.T) {
	// Use a server that immediately closes the connection.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Hijack and close to simulate connection refused.
		hj, ok := w.(http.Hijacker)
		if !ok {
			http.Error(w, "no hijack", 500)
			return
		}
		conn, _, _ := hj.Hijack()
		conn.Close()
	}))
	defer srv.Close()

	m := NewOpenAICompatibleModel(srv.URL, "", "llama3")
	req := &model.LLMRequest{
		Contents: []*genai.Content{
			{Role: "user", Parts: []*genai.Part{{Text: "hi"}}},
		},
	}

	_, err := collectResponse(m.GenerateContent(context.Background(), req, false))
	if err == nil {
		t.Fatal("expected error for network failure, got nil")
	}
	if !strings.Contains(err.Error(), "openai-compatible") {
		t.Errorf("error %q should mention openai-compatible", err.Error())
	}
}

// TestOpenAICompatibleModel_ResponseText verifies the response text is correctly
// extracted from the choices array.
func TestOpenAICompatibleModel_ResponseText(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(makeOpenAIResponse("Hello, world!")))
	}))
	defer srv.Close()

	m := NewOpenAICompatibleModel(srv.URL, "", "llama3")
	req := &model.LLMRequest{
		Contents: []*genai.Content{
			{Role: "user", Parts: []*genai.Part{{Text: "Say hello"}}},
		},
	}

	resp, err := collectResponse(m.GenerateContent(context.Background(), req, false))
	if err != nil {
		t.Fatalf("GenerateContent() error = %v", err)
	}
	if resp == nil || resp.Content == nil {
		t.Fatal("response or content is nil")
	}
	if len(resp.Content.Parts) == 0 {
		t.Fatal("response has no parts")
	}
	if resp.Content.Parts[0].Text != "Hello, world!" {
		t.Errorf("response text = %q, want %q", resp.Content.Parts[0].Text, "Hello, world!")
	}
	if resp.Content.Role != "model" {
		t.Errorf("response role = %q, want %q", resp.Content.Role, "model")
	}
}

// TestModelFactory_OpenAICompatibleDBCred verifies that CreateModelWithName
// creates an openaiCompatibleModel when the DB resolver returns an
// IsOpenAICompatible credential (task 17.6).
func TestModelFactory_OpenAICompatibleDBCred(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(makeOpenAIResponse("hi")))
	}))
	defer srv.Close()

	log := slog.New(slog.NewTextHandler(os.Stdout, nil))
	cfg := &config.LLMConfig{Model: "default-model"}

	resolver := &staticResolver{cred: &ResolvedCredential{
		IsOpenAICompatible: true,
		OpenAIBaseURL:      srv.URL,
		APIKey:             "",
		GenerativeModel:    "llama3",
		Source:             "test",
	}}

	factory := NewModelFactory(cfg, log, resolver, nil)
	llm, err := factory.CreateModelWithName(context.Background(), "ignored")
	if err != nil {
		t.Fatalf("CreateModelWithName() error = %v", err)
	}
	if llm == nil {
		t.Fatal("CreateModelWithName() returned nil")
	}
	if llm.Name() != "llama3" {
		t.Errorf("model name = %q, want %q", llm.Name(), "llama3")
	}
}

// TestModelFactory_OpenAICompatibleEnvFallback verifies that CreateModelWithName
// creates an openaiCompatibleModel from env-var config when no DB resolver is
// present (task 17.7).
func TestModelFactory_OpenAICompatibleEnvFallback(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(makeOpenAIResponse("hi")))
	}))
	defer srv.Close()

	log := slog.New(slog.NewTextHandler(os.Stdout, nil))
	cfg := &config.LLMConfig{
		OpenAIBaseURL: srv.URL,
		OpenAIAPIKey:  "",
		OpenAIModel:   "mistral",
	}

	factory := NewModelFactory(cfg, log, nil, nil)
	llm, err := factory.CreateModelWithName(context.Background(), "ignored")
	if err != nil {
		t.Fatalf("CreateModelWithName() error = %v", err)
	}
	if llm == nil {
		t.Fatal("CreateModelWithName() returned nil")
	}
	if llm.Name() != "mistral" {
		t.Errorf("model name = %q, want %q", llm.Name(), "mistral")
	}
}

// TestModelFactory_OpenAICompatibleEnvFallback_MissingModel verifies that an
// error is returned when OPENAI_BASE_URL is set but LLM_MODEL is empty.
func TestModelFactory_OpenAICompatibleEnvFallback_MissingModel(t *testing.T) {
	log := slog.New(slog.NewTextHandler(os.Stdout, nil))
	cfg := &config.LLMConfig{
		OpenAIBaseURL: "http://localhost:11434/v1",
		OpenAIModel:   "", // missing
	}

	factory := NewModelFactory(cfg, log, nil, nil)
	// Pass a non-empty modelName so we reach the OpenAI branch (the top-level
	// "model name is required" guard fires before the OpenAI path when empty).
	_, err := factory.CreateModelWithName(context.Background(), "some-model")
	if err == nil {
		t.Fatal("expected error when LLM_MODEL is empty, got nil")
	}
	if !strings.Contains(err.Error(), "LLM_MODEL") {
		t.Errorf("error %q should mention LLM_MODEL", err.Error())
	}
}

// TestLLMConfig_IsEnabled_OpenAIBaseURL verifies IsEnabled returns true when
// OpenAIBaseURL is set (task 17.8).
func TestLLMConfig_IsEnabled_OpenAIBaseURL(t *testing.T) {
	cfg := &config.LLMConfig{
		OpenAIBaseURL: "http://localhost:11434/v1",
	}
	if !cfg.IsEnabled() {
		t.Error("IsEnabled() = false, want true when OpenAIBaseURL is set")
	}
}

// TestLLMConfig_IsEnabled_OpenAIBaseURL_NetworkDisabled verifies IsEnabled
// returns false when NetworkDisabled is set even if OpenAIBaseURL is set.
func TestLLMConfig_IsEnabled_OpenAIBaseURL_NetworkDisabled(t *testing.T) {
	cfg := &config.LLMConfig{
		OpenAIBaseURL:   "http://localhost:11434/v1",
		NetworkDisabled: true,
	}
	if cfg.IsEnabled() {
		t.Error("IsEnabled() = true, want false when NetworkDisabled is set")
	}
}

// TestOpenAICompatibleModel_TrailingSlashStripped verifies that a trailing slash
// in baseURL is stripped so the path is correct.
func TestOpenAICompatibleModel_TrailingSlashStripped(t *testing.T) {
	var path string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(makeOpenAIResponse("ok")))
	}))
	defer srv.Close()

	// Pass URL with trailing slash
	m := NewOpenAICompatibleModel(srv.URL+"/", "", "llama3")
	req := &model.LLMRequest{
		Contents: []*genai.Content{
			{Role: "user", Parts: []*genai.Part{{Text: "hi"}}},
		},
	}

	_, err := collectResponse(m.GenerateContent(context.Background(), req, false))
	if err != nil {
		t.Fatalf("GenerateContent() error = %v", err)
	}

	if path != "/chat/completions" {
		t.Errorf("request path = %q, want %q", path, "/chat/completions")
	}
}

// staticResolver is a test CredentialResolver that always returns a fixed credential.
type staticResolver struct {
	cred *ResolvedCredential
	err  error
}

func (r *staticResolver) ResolveAny(_ context.Context) (*ResolvedCredential, error) {
	return r.cred, r.err
}
