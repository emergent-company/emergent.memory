package adk

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/emergent-company/emergent.memory/internal/config"
	"github.com/google/jsonschema-go/jsonschema"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/model"
	"google.golang.org/adk/v2/tool"
	"google.golang.org/adk/v2/tool/functiontool"
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

// TestOpenAICompatibleModel_RetriesRateLimit verifies a provider 429 is answered
// inside the call instead of being surfaced to the job above it (#237). The
// header asks for a zero-second wait so the test does not sleep.
func TestOpenAICompatibleModel_RetriesRateLimit(t *testing.T) {
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"error":"No deployments available for selected model, Try again in 5 seconds"}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(makeOpenAIResponse("recovered")))
	}))
	defer srv.Close()

	m := NewOpenAICompatibleModel(srv.URL, "key", "deepseek")
	req := &model.LLMRequest{
		Contents: []*genai.Content{
			{Role: "user", Parts: []*genai.Part{{Text: "extract"}}},
		},
	}

	resp, err := collectResponse(m.GenerateContent(context.Background(), req, false))
	if err != nil {
		t.Fatalf("GenerateContent() error = %v, want the retry to succeed", err)
	}
	if calls != 2 {
		t.Errorf("provider calls = %d, want 2 (one 429 then one success)", calls)
	}
	if resp == nil || resp.Content == nil || len(resp.Content.Parts) == 0 || resp.Content.Parts[0].Text != "recovered" {
		t.Fatalf("response = %+v, want the recovered body", resp)
	}
}

// TestOpenAICompatibleModel_RateLimitExhaustsRetries verifies the retry is
// bounded: a provider that keeps rate limiting still produces one error naming
// the status and body, after maxProviderRetries extra attempts.
func TestOpenAICompatibleModel_RateLimitExhaustsRetries(t *testing.T) {
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Retry-After", "0")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error":"rate limited"}`))
	}))
	defer srv.Close()

	m := NewOpenAICompatibleModel(srv.URL, "key", "deepseek")
	req := &model.LLMRequest{
		Contents: []*genai.Content{
			{Role: "user", Parts: []*genai.Part{{Text: "extract"}}},
		},
	}

	_, err := collectResponse(m.GenerateContent(context.Background(), req, false))
	if err == nil {
		t.Fatal("expected an error once the retries are spent")
	}
	if !strings.Contains(err.Error(), "429") {
		t.Errorf("error %q should contain the status code", err.Error())
	}
	if !strings.Contains(err.Error(), "rate limited") {
		t.Errorf("error %q should contain the provider body", err.Error())
	}
	if want := maxProviderRetries + 1; calls != want {
		t.Errorf("provider calls = %d, want %d", calls, want)
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

// TestModelFactory_OpenAIDBCred verifies that CreateModelWithName creates an
// openaiCompatibleModel when the DB resolver returns an openai provider credential.
func TestModelFactory_OpenAIDBCred(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(makeOpenAIResponse("hi")))
	}))
	defer srv.Close()

	log := slog.New(slog.NewTextHandler(os.Stdout, nil))
	cfg := &config.LLMConfig{Model: "default-model"}

	resolver := &staticResolver{cred: &ResolvedCredential{
		Provider:        "openai",
		BaseURL:         srv.URL,
		APIKey:          "sk-test",
		GenerativeModel: "openai/gpt-4o",
		Source:          "test",
	}}

	factory := NewModelFactory(cfg, log, resolver, nil, nil)
	llm, err := factory.CreateModelWithName(context.Background(), "openai/gpt-4o")
	if err != nil {
		t.Fatalf("CreateModelWithName() error = %v", err)
	}
	if llm == nil {
		t.Fatal("CreateModelWithName() returned nil")
	}
}

// TestModelFactory_OpenAIEnvFallback verifies that CreateModelWithName
// creates an openaiCompatibleModel from env-var config when no DB resolver is present.
func TestModelFactory_OpenAIEnvFallback(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(makeOpenAIResponse("hi")))
	}))
	defer srv.Close()

	log := slog.New(slog.NewTextHandler(os.Stdout, nil))
	cfg := &config.LLMConfig{
		OpenAIAPIKey:  "sk-test",
		OpenAIBaseURL: srv.URL,
		OpenAIModel:   "openai/gpt-4o",
	}

	factory := NewModelFactory(cfg, log, nil, nil, nil)
	llm, err := factory.CreateModelWithName(context.Background(), "openai/gpt-4o")
	if err != nil {
		t.Fatalf("CreateModelWithName() error = %v", err)
	}
	if llm == nil {
		t.Fatal("CreateModelWithName() returned nil")
	}
}

// TestModelFactory_OpenAIEnvFallback_MissingModel verifies that an error is
// returned when a bare model name (no provider prefix) is passed.
func TestModelFactory_OpenAIEnvFallback_MissingModel(t *testing.T) {
	log := slog.New(slog.NewTextHandler(os.Stdout, nil))
	cfg := &config.LLMConfig{
		OpenAIAPIKey:  "sk-test",
		OpenAIBaseURL: "http://localhost:11434/v1",
		OpenAIModel:   "",
	}

	factory := NewModelFactory(cfg, log, nil, nil, nil)
	_, err := factory.CreateModelWithName(context.Background(), "bare-model-no-prefix")
	if err == nil {
		t.Fatal("expected error for bare model name without provider prefix, got nil")
	}
}

// TestLLMConfig_IsEnabled_OpenAIAPIKey verifies IsEnabled returns true when
// OpenAIAPIKey is set.
func TestLLMConfig_IsEnabled_OpenAIAPIKey(t *testing.T) {
	cfg := &config.LLMConfig{
		OpenAIAPIKey: "sk-test",
	}
	if !cfg.IsEnabled() {
		t.Error("IsEnabled() = false, want true when OpenAIAPIKey is set")
	}
}

// TestLLMConfig_IsEnabled_OpenAIAPIKey_NetworkDisabled verifies IsEnabled
// returns false when NetworkDisabled is set even if OpenAIAPIKey is set.
func TestLLMConfig_IsEnabled_OpenAIAPIKey_NetworkDisabled(t *testing.T) {
	cfg := &config.LLMConfig{
		OpenAIAPIKey:    "sk-test",
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

func (r *staticResolver) ResolveFor(_ context.Context, _ string) (*ResolvedCredential, error) {
	return r.cred, r.err
}

func (r *staticResolver) ResolveBySlug(_ context.Context, _ string) (*ResolvedCredential, error) {
	return r.cred, r.err
}

// TestOpenAICompatibleModel_ToolSchema_ContainsResolvedPrefixedName is the schema
// half of the bare-tool-name fix (agent-tools-in-chat-schema). The tool pool
// resolves a bare whitelist entry (web_fetch_exa) to its prefixed pool key
// (ts_web_fetch_exa) and wraps it as an ADK functiontool — see
// domain/agents/toolpool_test.go TestResolveTools_BareExternalName_YieldsPrefixedADKTool.
// This test feeds an identically-constructed functiontool through the real model
// request path (buildOpenAITools) and asserts the OpenAI tool schema the model
// receives carries the prefixed pool key, never the bare whitelist name.
func TestOpenAICompatibleModel_ToolSchema_ContainsResolvedPrefixedName(t *testing.T) {
	// Mirrors toolpool.wrapSingleTool's external-tool wrapper construction:
	// a functiontool whose Name is the prefixed ServerName_ToolName pool key.
	poolTool, err := functiontool.New(
		functiontool.Config{
			Name:        "ts_web_fetch_exa",
			Description: "Fetch a URL via the Exa MCP server",
			InputSchema: &jsonschema.Schema{Type: "object"},
		},
		func(ctx agent.Context, args map[string]any) (map[string]any, error) {
			return map[string]any{"ok": true, "result": "fetched"}, nil
		},
	)
	require.NoError(t, err)
	require.Equal(t, "ts_web_fetch_exa", poolTool.Name())

	// Pack the resolved tool into genai declarations the way the ADK runner
	// does before invoking the model (internal/toolinternal/toolutils.PackTool).
	declarer, ok := poolTool.(interface {
		Declaration() *genai.FunctionDeclaration
	})
	require.True(t, ok, "functiontool must expose Declaration()")
	decl := declarer.Declaration()
	require.NotNil(t, decl)
	require.Equal(t, "ts_web_fetch_exa", decl.Name)

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
			{Role: "user", Parts: []*genai.Part{{Text: "fetch it"}}},
		},
		Config: &genai.GenerateContentConfig{
			Tools: []*genai.Tool{{FunctionDeclarations: []*genai.FunctionDeclaration{decl}}},
		},
	}

	_, err = collectResponse(m.GenerateContent(context.Background(), req, false))
	require.NoError(t, err)

	require.Len(t, captured.Tools, 1, "exactly one tool declaration reaches the model")
	require.NotNil(t, captured.Tools[0].Function)
	assert.Equal(t, "ts_web_fetch_exa", captured.Tools[0].Function.Name,
		"the LLM schema must carry the resolved prefixed pool key")
	assert.NotEqual(t, "web_fetch_exa", captured.Tools[0].Function.Name,
		"the bare whitelist name must not leak into the LLM schema")
	assert.Equal(t, "Fetch a URL via the Exa MCP server", captured.Tools[0].Function.Description)
}

// captureOpenAIRequestForTool runs a single-tool GenerateContent against an
// httptest backend and returns the OpenAI request body the server captured,
// so tests can assert exactly what tool schema reached the wire.
func captureOpenAIRequestForTool(t *testing.T, poolTool tool.Tool) openaiRequest {
	t.Helper()
	// Pack the resolved tool into genai declarations the way the ADK runner
	// does before invoking the model (internal/toolinternal/toolutils.PackTool).
	declarer, ok := poolTool.(interface {
		Declaration() *genai.FunctionDeclaration
	})
	require.True(t, ok, "functiontool must expose Declaration()")
	decl := declarer.Declaration()
	require.NotNil(t, decl)

	var captured openaiRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &captured)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(makeOpenAIResponse("ok")))
	}))
	t.Cleanup(srv.Close)

	m := NewOpenAICompatibleModel(srv.URL, "", "llama3")
	req := &model.LLMRequest{
		Contents: []*genai.Content{
			{Role: "user", Parts: []*genai.Part{{Text: "fetch it"}}},
		},
		Config: &genai.GenerateContentConfig{
			Tools: []*genai.Tool{{FunctionDeclarations: []*genai.FunctionDeclaration{decl}}},
		},
	}
	_, err := collectResponse(m.GenerateContent(context.Background(), req, false))
	require.NoError(t, err)
	return captured
}

// TestOpenAICompatibleModel_ToolSchema_SpaceyServerName_ValidFunctionName covers
// the slugify half of the fix: a server named "E2E MCP 123" exposes its tools
// under the slugged pool key e2e_mcp_123_web_fetch_exa (domain/agents
// externalToolKey), which must reach the OpenAI wire schema as a legal function
// name matching ^[A-Za-z0-9_-]{1,64}$ — the raw spacey key would be dropped or
// refused by tool-calling providers.
func TestOpenAICompatibleModel_ToolSchema_SpaceyServerName_ValidFunctionName(t *testing.T) {
	poolTool, err := functiontool.New(
		functiontool.Config{
			Name:        "e2e_mcp_123_web_fetch_exa", // slugged pool key for server "E2E MCP 123"
			Description: "Fetch a URL via the Exa MCP server",
			InputSchema: &jsonschema.Schema{Type: "object"},
		},
		func(ctx agent.Context, args map[string]any) (map[string]any, error) {
			return map[string]any{"ok": true, "result": "fetched"}, nil
		},
	)
	require.NoError(t, err)

	captured := captureOpenAIRequestForTool(t, poolTool)

	require.Len(t, captured.Tools, 1, "exactly one tool declaration reaches the model")
	require.NotNil(t, captured.Tools[0].Function)
	assert.Equal(t, "e2e_mcp_123_web_fetch_exa", captured.Tools[0].Function.Name)
	assert.Regexp(t, `^[A-Za-z0-9_-]{1,64}$`, captured.Tools[0].Function.Name,
		"the slugged key must satisfy the LLM function-name contract on the wire")
	assert.NotEqual(t, "E2E MCP 123_web_fetch_exa", captured.Tools[0].Function.Name,
		"the raw spacey server name must never reach the schema")
}

// captureOpenAIRequestWithCompat runs a single-tool GenerateContent through an
// openaiCompatibleModel configured with the given vendor compat and returns the
// request body the backend captured, so compat-shaping tests can assert exactly
// what reached the wire.
func captureOpenAIRequestWithCompat(t *testing.T, compat VendorCompat, modelName string) openaiRequest {
	t.Helper()
	var captured openaiRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &captured)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(makeOpenAIResponse("ok")))
	}))
	t.Cleanup(srv.Close)

	m := NewOpenAICompatibleModelWithCompat(srv.URL, "", modelName, AuthBearer, compat, nil, nil)
	req := &model.LLMRequest{
		Contents: []*genai.Content{
			{Role: "user", Parts: []*genai.Part{{Text: "do it"}}},
		},
		Config: &genai.GenerateContentConfig{
			Tools: []*genai.Tool{{
				FunctionDeclarations: []*genai.FunctionDeclaration{
					{Name: "do_thing", Description: "do the thing"},
				},
			}},
		},
	}
	_, err := collectResponse(m.GenerateContent(context.Background(), req, false))
	require.NoError(t, err)
	return captured
}

// TestOpenAICompatibleModel_CompatMaxCompletionTokens verifies that a vendor
// with MaxTokensField == "max_completion_tokens" gets the output cap sent via
// the max_completion_tokens field and never via max_tokens.
func TestOpenAICompatibleModel_CompatMaxCompletionTokens(t *testing.T) {
	var raw []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(makeOpenAIResponse("ok")))
	}))
	defer srv.Close()

	m := NewOpenAICompatibleModelWithCompat(srv.URL, "", "gpt-4o", AuthBearer,
		VendorCompat{MaxTokensField: "max_completion_tokens"}, nil, nil)
	req := &model.LLMRequest{
		Contents: []*genai.Content{{Role: "user", Parts: []*genai.Part{{Text: "hi"}}}},
		Config:   &genai.GenerateContentConfig{MaxOutputTokens: 128},
	}

	_, err := collectResponse(m.GenerateContent(context.Background(), req, false))
	require.NoError(t, err)

	body := string(raw)
	if !strings.Contains(body, `"max_completion_tokens":128`) {
		t.Errorf("request body missing max_completion_tokens: %s", body)
	}
	if strings.Contains(body, `"max_tokens"`) {
		t.Errorf("request body should not contain max_tokens: %s", body)
	}
}

// TestOpenAICompatibleModel_CompatNoToolChoice verifies that NoToolChoice
// suppresses the tool_choice field even when tools are present.
func TestOpenAICompatibleModel_CompatNoToolChoice(t *testing.T) {
	captured := captureOpenAIRequestWithCompat(t, VendorCompat{NoToolChoice: true}, "gpt-4o")

	require.Len(t, captured.Tools, 1, "tools must still be sent")
	if captured.ToolChoice != "" {
		t.Errorf("tool_choice = %q, want empty (NoToolChoice)", captured.ToolChoice)
	}
}

// TestOpenAICompatibleModel_CompatThinkingObject verifies that a vendor with
// ThinkingStyle "thinking" gets the top-level thinking object when thinking is
// disabled alongside tools, and no chat_template_kwargs.
func TestOpenAICompatibleModel_CompatThinkingObject(t *testing.T) {
	captured := captureOpenAIRequestWithCompat(t, VendorCompat{ThinkingStyle: "thinking"}, "qwen3")

	require.NotNil(t, captured.Thinking, "expected top-level thinking object")
	tm, ok := captured.Thinking.(map[string]any)
	require.True(t, ok, "thinking should unmarshal to an object, got %T", captured.Thinking)
	assert.Equal(t, "disabled", tm["type"])
	assert.Nil(t, captured.ChatTemplateKwargs, "chat_template_kwargs must not be set")
}

// TestOpenAICompatibleModel_CompatChatTemplateKwargs verifies that a vendor with
// ThinkingStyle "chat_template_kwargs" disables thinking via the kwargs field
// and leaves the top-level thinking field unset.
func TestOpenAICompatibleModel_CompatChatTemplateKwargs(t *testing.T) {
	captured := captureOpenAIRequestWithCompat(t, VendorCompat{ThinkingStyle: "chat_template_kwargs"}, "qwen3")

	require.NotNil(t, captured.ChatTemplateKwargs, "expected chat_template_kwargs")
	assert.Equal(t, false, captured.ChatTemplateKwargs["enable_thinking"])
	assert.Nil(t, captured.Thinking, "top-level thinking must not be set")
}

// TestOpenAICompatibleModel_ZeroCompatLegacyKwargs is a regression guard: a zero
// VendorCompat on a non-deepseek model must still take the legacy
// chat_template_kwargs path when thinking is disabled with tools present.
func TestOpenAICompatibleModel_ZeroCompatLegacyKwargs(t *testing.T) {
	captured := captureOpenAIRequestWithCompat(t, VendorCompat{}, "llama3")

	require.NotNil(t, captured.ChatTemplateKwargs, "expected legacy chat_template_kwargs")
	assert.Equal(t, false, captured.ChatTemplateKwargs["enable_thinking"])
	assert.Nil(t, captured.Thinking, "legacy non-deepseek path must not set top-level thinking")
	if captured.ToolChoice != "required" {
		t.Errorf("tool_choice = %q, want %q (legacy tool_choice still sent)", captured.ToolChoice, "required")
	}
}

// TestOpenAICompatibleModel_CancelDuringBackoffSurfacesCancellation verifies a
// run cancelled while the client waits out a retryable response reports the
// cancellation, not the provider's 429 (#254).
func TestOpenAICompatibleModel_CancelDuringBackoffSurfacesCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	tr := &retryableResponseTransport{
		status: http.StatusTooManyRequests,
		body:   `{"error":"rate limited"}`,
		cancel: cancel,
	}
	m := NewOpenAICompatibleModel("http://provider.invalid/v1", "key", "deepseek")
	m.(*openaiCompatibleModel).client = &http.Client{Transport: tr}

	_, err := collectResponse(m.GenerateContent(ctx, &model.LLMRequest{
		Contents: []*genai.Content{{Role: "user", Parts: []*genai.Part{{Text: "extract"}}}},
	}, false))
	if err == nil {
		t.Fatal("expected a cancellation error, got nil")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("errors.Is(%v, context.Canceled) = false, want true", err)
	}
	if tr.calls != 1 {
		t.Errorf("provider calls = %d, want 1 (no retry once the context ended)", tr.calls)
	}
}

// TestOpenAICompatibleModel_CachedPromptTokens verifies prompt-cache hits are
// surfaced in usage metadata (OpenAI-compatible prompt_tokens_details).
func TestOpenAICompatibleModel_CachedPromptTokens(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"ok"}}],"usage":{"prompt_tokens":100,"completion_tokens":5,"total_tokens":105,"prompt_tokens_details":{"cached_tokens":80}}}`))
	}))
	defer srv.Close()

	m := NewOpenAICompatibleModel(srv.URL, "", "m")
	req := &model.LLMRequest{Contents: []*genai.Content{{Role: "user", Parts: []*genai.Part{{Text: "hi"}}}}}

	resp, err := collectResponse(m.GenerateContent(context.Background(), req, false))
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.NotNil(t, resp.UsageMetadata)
	assert.EqualValues(t, 100, resp.UsageMetadata.PromptTokenCount)
	assert.EqualValues(t, 5, resp.UsageMetadata.CandidatesTokenCount)
	assert.EqualValues(t, 80, resp.UsageMetadata.CachedContentTokenCount)
}

// TestOpenAICompatibleModel_ReasoningEffort verifies the graded thinking level
// is sent as reasoning_effort and that an explicit effort does not also send
// the contradictory thinking:disabled toggle when tools are present.
func TestOpenAICompatibleModel_ReasoningEffort(t *testing.T) {
	makeReq := func() *model.LLMRequest {
		return &model.LLMRequest{
			Contents: []*genai.Content{{Role: "user", Parts: []*genai.Part{{Text: "hi"}}}},
			Config: &genai.GenerateContentConfig{
				Tools: []*genai.Tool{{FunctionDeclarations: []*genai.FunctionDeclaration{{Name: "foo"}}}},
			},
		}
	}

	t.Run("effort is sent and thinking is not disabled", func(t *testing.T) {
		var captured openaiRequest
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			body, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(body, &captured)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(makeOpenAIResponse("ok")))
		}))
		defer srv.Close()

		m := NewOpenAICompatibleModel(srv.URL, "", "deepseek-v4-flash")
		eff, ok := m.(ReasoningEffortConfigurator)
		require.True(t, ok, "openai-compatible adapter must implement ReasoningEffortConfigurator")
		eff.SetReasoningEffort("high")

		_, err := collectResponse(m.GenerateContent(context.Background(), makeReq(), false))
		require.NoError(t, err)
		assert.Equal(t, "high", captured.ReasoningEffort)
		assert.Nil(t, captured.Thinking, "explicit effort must not also send thinking:disabled")
	})

	t.Run("empty effort omits the field and keeps the tool default", func(t *testing.T) {
		var captured openaiRequest
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			body, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(body, &captured)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(makeOpenAIResponse("ok")))
		}))
		defer srv.Close()

		m := NewOpenAICompatibleModel(srv.URL, "", "deepseek-v4-flash")
		_, err := collectResponse(m.GenerateContent(context.Background(), makeReq(), false))
		require.NoError(t, err)
		assert.Empty(t, captured.ReasoningEffort)
		// DeepSeek default with tools present: thinking disabled.
		assert.NotNil(t, captured.Thinking)
	})
}
