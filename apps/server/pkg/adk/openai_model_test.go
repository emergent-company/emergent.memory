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
	"github.com/google/jsonschema-go/jsonschema"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/adk/model"
	"google.golang.org/adk/tool"
	"google.golang.org/adk/tool/functiontool"
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
		func(ctx tool.Context, args map[string]any) (map[string]any, error) {
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
		func(ctx tool.Context, args map[string]any) (map[string]any, error) {
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
