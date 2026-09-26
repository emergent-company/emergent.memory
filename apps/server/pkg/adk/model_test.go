package adk

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/emergent-company/emergent.memory/internal/config"
	"google.golang.org/adk/model"
	"google.golang.org/genai"
)

func TestNewModelFactory(t *testing.T) {
	cfg := &config.LLMConfig{
		GCPProjectID:     "test-project",
		VertexAILocation: "us-central1",
		Model:            "gemini-1.5-pro",
		Temperature:      0.1,
		MaxOutputTokens:  8192,
	}
	log := slog.New(slog.NewTextHandler(os.Stdout, nil))

	factory := NewModelFactory(cfg, log, nil, nil, nil)

	if factory == nil {
		t.Fatal("NewModelFactory returned nil")
	}
	if factory.cfg != cfg {
		t.Error("NewModelFactory didn't set config")
	}
	if factory.log != log {
		t.Error("NewModelFactory didn't set logger")
	}
}

func TestModelFactoryCreateModelWithName_ValidationErrors(t *testing.T) {
	log := slog.New(slog.NewTextHandler(os.Stdout, nil))

	tests := []struct {
		name      string
		cfg       *config.LLMConfig
		modelName string
		wantErr   string
	}{
		{
			name: "missing model name",
			cfg: &config.LLMConfig{
				GCPProjectID:     "test-project",
				VertexAILocation: "us-central1",
			},
			modelName: "",
			wantErr:   "model name is required",
		},
		{
			name: "bare model name without provider prefix",
			cfg: &config.LLMConfig{
				GCPProjectID:     "test-project",
				VertexAILocation: "us-central1",
			},
			modelName: "gemini-1.5-pro",
			wantErr:   `model name "gemini-1.5-pro" must include a provider prefix (e.g. deepseek/deepseek-v4-flash, openai/gpt-4o, google/gemini-2.5-flash)`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			factory := NewModelFactory(tt.cfg, log, nil, nil, nil)
			_, err := factory.CreateModelWithName(context.Background(), tt.modelName)

			if err == nil {
				t.Error("CreateModelWithName() expected error, got nil")
			} else if err.Error() != tt.wantErr {
				t.Errorf("CreateModelWithName() error = %q, want %q", err.Error(), tt.wantErr)
			}
		})
	}
}

func TestModelFactoryDefaultGenerateConfig(t *testing.T) {
	cfg := &config.LLMConfig{
		Temperature:     0.5,
		MaxOutputTokens: 4096,
	}
	log := slog.New(slog.NewTextHandler(os.Stdout, nil))
	factory := NewModelFactory(cfg, log, nil, nil, nil)

	config := factory.DefaultGenerateConfig()

	if config == nil {
		t.Fatal("DefaultGenerateConfig returned nil")
	}
	if config.Temperature == nil {
		t.Fatal("DefaultGenerateConfig Temperature is nil")
	}
	if *config.Temperature != 0.5 {
		t.Errorf("DefaultGenerateConfig Temperature = %f, want 0.5", *config.Temperature)
	}
	if config.MaxOutputTokens != 4096 {
		t.Errorf("DefaultGenerateConfig MaxOutputTokens = %d, want 4096", config.MaxOutputTokens)
	}
}

func TestModelFactoryExtractionGenerateConfig(t *testing.T) {
	cfg := &config.LLMConfig{
		Temperature:     0.5, // Should be ignored for extraction
		MaxOutputTokens: 8192,
	}
	log := slog.New(slog.NewTextHandler(os.Stdout, nil))
	factory := NewModelFactory(cfg, log, nil, nil, nil)

	config := factory.ExtractionGenerateConfig()

	if config == nil {
		t.Fatal("ExtractionGenerateConfig returned nil")
	}
	if config.Temperature == nil {
		t.Fatal("ExtractionGenerateConfig Temperature is nil")
	}
	// Extraction should always use temperature 0
	if *config.Temperature != 0.0 {
		t.Errorf("ExtractionGenerateConfig Temperature = %f, want 0.0", *config.Temperature)
	}
	if config.MaxOutputTokens != 8192 {
		t.Errorf("ExtractionGenerateConfig MaxOutputTokens = %d, want 8192", config.MaxOutputTokens)
	}
}

func TestModelFactoryIsEnabled(t *testing.T) {
	log := slog.New(slog.NewTextHandler(os.Stdout, nil))

	tests := []struct {
		name string
		cfg  *config.LLMConfig
		want bool
	}{
		{
			name: "enabled with all fields",
			cfg: &config.LLMConfig{
				GCPProjectID:     "test-project",
				VertexAILocation: "us-central1",
				Model:            "gemini-1.5-pro",
			},
			want: true,
		},
		{
			name: "disabled without project",
			cfg: &config.LLMConfig{
				GCPProjectID:     "",
				VertexAILocation: "us-central1",
				Model:            "gemini-1.5-pro",
			},
			want: false,
		},
		{
			name: "disabled without location",
			cfg: &config.LLMConfig{
				GCPProjectID:     "test-project",
				VertexAILocation: "",
				Model:            "gemini-1.5-pro",
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			factory := NewModelFactory(tt.cfg, log, nil, nil, nil)
			got := factory.IsEnabled()
			if got != tt.want {
				t.Errorf("IsEnabled() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestModelFactoryModelName(t *testing.T) {
	cfg := &config.LLMConfig{
		Model: "gemini-1.5-flash",
	}
	log := slog.New(slog.NewTextHandler(os.Stdout, nil))
	factory := NewModelFactory(cfg, log, nil, nil, nil)

	got := factory.ModelName()
	if got != "gemini-1.5-flash" {
		t.Errorf("ModelName() = %q, want %q", got, "gemini-1.5-flash")
	}
}

func TestPtrFloat32(t *testing.T) {
	tests := []struct {
		name  string
		value float32
	}{
		{"zero", 0.0},
		{"positive", 0.5},
		{"negative", -0.5},
		{"one", 1.0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ptr := ptrFloat32(tt.value)
			if ptr == nil {
				t.Fatal("ptrFloat32 returned nil")
			}
			if *ptr != tt.value {
				t.Errorf("ptrFloat32(%f) = %f, want %f", tt.value, *ptr, tt.value)
			}
		})
	}
}

func TestProvideModelFactory(t *testing.T) {
	cfg := &config.Config{
		LLM: config.LLMConfig{
			GCPProjectID:     "test-project",
			VertexAILocation: "us-central1",
			Model:            "gemini-1.5-pro",
			Temperature:      0.1,
			MaxOutputTokens:  8192,
		},
	}
	log := slog.New(slog.NewTextHandler(os.Stdout, nil))

	factory := provideModelFactory(modelFactoryParams{Cfg: cfg, Log: log})

	if factory == nil {
		t.Fatal("provideModelFactory returned nil")
	}
	if factory.cfg.GCPProjectID != "test-project" {
		t.Errorf("provideModelFactory cfg.GCPProjectID = %q, want %q", factory.cfg.GCPProjectID, "test-project")
	}
	if factory.cfg.Model != "gemini-1.5-pro" {
		t.Errorf("provideModelFactory cfg.Model = %q, want %q", factory.cfg.Model, "gemini-1.5-pro")
	}
}

func TestModelFactoryCreateModel_ValidationErrors(t *testing.T) {
	log := slog.New(slog.NewTextHandler(os.Stdout, nil))

	tests := []struct {
		name    string
		cfg     *config.LLMConfig
		wantErr string
	}{
		{
			name: "missing model name (uses config's empty model)",
			cfg: &config.LLMConfig{
				GCPProjectID:     "test-project",
				VertexAILocation: "us-central1",
				Model:            "",
			},
			wantErr: "no generative model configured: set DEEPSEEK_MODEL, OPENAI_MODEL, or VERTEX_AI_MODEL",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			factory := NewModelFactory(tt.cfg, log, nil, nil, nil)
			_, err := factory.CreateModel(context.Background())

			if err == nil {
				t.Error("CreateModel() expected error, got nil")
			} else if err.Error() != tt.wantErr {
				t.Errorf("CreateModel() error = %q, want %q", err.Error(), tt.wantErr)
			}
		})
	}
}

func TestModelFactoryExtractionGenerateConfigWithSchema(t *testing.T) {
	cfg := &config.LLMConfig{
		Temperature:     0.5,
		MaxOutputTokens: 8192,
	}
	log := slog.New(slog.NewTextHandler(os.Stdout, nil))
	factory := NewModelFactory(cfg, log, nil, nil, nil)

	schema := &genai.Schema{
		Type:        genai.TypeObject,
		Description: "Test schema",
		Required:    []string{"entities"},
		Properties: map[string]*genai.Schema{
			"entities": {
				Type:        genai.TypeArray,
				Description: "Array of entities",
				Items: &genai.Schema{
					Type:     genai.TypeObject,
					Required: []string{"name", "type"},
					Properties: map[string]*genai.Schema{
						"name": {Type: genai.TypeString},
						"type": {Type: genai.TypeString, Enum: []string{"Person", "Organization"}},
					},
				},
			},
		},
	}

	config := factory.ExtractionGenerateConfigWithSchema(schema)

	if config == nil {
		t.Fatal("ExtractionGenerateConfigWithSchema returned nil")
	}
	if config.Temperature == nil {
		t.Fatal("ExtractionGenerateConfigWithSchema Temperature is nil")
	}
	if *config.Temperature != 0.0 {
		t.Errorf("ExtractionGenerateConfigWithSchema Temperature = %f, want 0.0", *config.Temperature)
	}
	if config.MaxOutputTokens != 8192 {
		t.Errorf("ExtractionGenerateConfigWithSchema MaxOutputTokens = %d, want 8192", config.MaxOutputTokens)
	}
	if config.ResponseMIMEType != "application/json" {
		t.Errorf("ExtractionGenerateConfigWithSchema ResponseMIMEType = %q, want %q", config.ResponseMIMEType, "application/json")
	}
	if config.ResponseSchema == nil {
		t.Fatal("ExtractionGenerateConfigWithSchema ResponseSchema is nil")
	}
	if config.ResponseSchema != schema {
		t.Error("ExtractionGenerateConfigWithSchema ResponseSchema doesn't match input schema")
	}
}

func TestModelFactoryExtractionGenerateConfigWithSchema_NilSchema(t *testing.T) {
	cfg := &config.LLMConfig{
		Temperature:     0.5,
		MaxOutputTokens: 4096,
	}
	log := slog.New(slog.NewTextHandler(os.Stdout, nil))
	factory := NewModelFactory(cfg, log, nil, nil, nil)

	config := factory.ExtractionGenerateConfigWithSchema(nil)

	if config == nil {
		t.Fatal("ExtractionGenerateConfigWithSchema returned nil for nil schema")
	}
	if *config.Temperature != 0.0 {
		t.Errorf("Temperature = %f, want 0.0", *config.Temperature)
	}
	if config.ResponseMIMEType != "application/json" {
		t.Errorf("ResponseMIMEType = %q, want %q", config.ResponseMIMEType, "application/json")
	}
	if config.ResponseSchema != nil {
		t.Error("ResponseSchema should be nil when nil schema passed")
	}
}

func TestModelFactoryExtractionGenerateConfigWithSchema_SchemaWithEnumConstraint(t *testing.T) {
	cfg := &config.LLMConfig{
		MaxOutputTokens: 8192,
	}
	log := slog.New(slog.NewTextHandler(os.Stdout, nil))
	factory := NewModelFactory(cfg, log, nil, nil, nil)

	schema := &genai.Schema{
		Type:     genai.TypeObject,
		Required: []string{"relationships"},
		Properties: map[string]*genai.Schema{
			"relationships": {
				Type: genai.TypeArray,
				Items: &genai.Schema{
					Type:     genai.TypeObject,
					Required: []string{"type"},
					Properties: map[string]*genai.Schema{
						"type": {
							Type: genai.TypeString,
							Enum: []string{"WORKS_AT", "LOCATED_IN", "PARENT_OF"},
						},
					},
				},
			},
		},
	}

	config := factory.ExtractionGenerateConfigWithSchema(schema)

	if config.ResponseSchema == nil {
		t.Fatal("ResponseSchema is nil")
	}

	relSchema := config.ResponseSchema.Properties["relationships"]
	if relSchema == nil {
		t.Fatal("relationships property is nil")
	}

	typeSchema := relSchema.Items.Properties["type"]
	if typeSchema == nil {
		t.Fatal("type property is nil")
	}

	if len(typeSchema.Enum) != 3 {
		t.Errorf("type.Enum length = %d, want 3", len(typeSchema.Enum))
	}
}

// stubCredentialResolver is a minimal adk.CredentialResolver for tests.
type stubCredentialResolver struct {
	cred *ResolvedCredential
}

func (s *stubCredentialResolver) ResolveAny(_ context.Context) (*ResolvedCredential, error) {
	return s.cred, nil
}

func (s *stubCredentialResolver) ResolveFor(_ context.Context, _ string) (*ResolvedCredential, error) {
	return s.cred, nil
}

func (s *stubCredentialResolver) ResolveBySlug(_ context.Context, _ string) (*ResolvedCredential, error) {
	return s.cred, nil
}

// stubModelResolver returns a fixed (model, source) pair.
type stubModelResolver struct {
	model  string
	source string
}

func (s *stubModelResolver) ResolveGenerativeModelByID(_ context.Context, _ string) (string, string, error) {
	return s.model, s.source, nil
}

func TestModelFactoryCreateModel_FallsBackToProviderConfig(t *testing.T) {
	log := slog.New(slog.NewTextHandler(os.Stdout, nil))
	cfg := &config.LLMConfig{}

	cred := &ResolvedCredential{
		Provider:        "deepseek",
		GenerativeModel: "deepseek-v4-flash",
		BaseURL:         "https://api.deepseek.com/v1",
		APIKey:          "test-key",
		Source:          "project",
	}

	factory := NewModelFactory(cfg, log,
		&stubCredentialResolver{cred: cred},
		nil,
		&stubModelResolver{model: "", source: ""},
	)

	ctx := WithProjectID(context.Background(), "00000000-0000-0000-0000-000000000000")

	llm, err := factory.CreateModel(ctx)
	if err != nil {
		t.Fatalf("CreateModel() unexpected error: %v", err)
	}
	if llm == nil {
		t.Fatal("CreateModel() returned nil model")
	}
}

func TestIsReasonerModel(t *testing.T) {
	for _, tc := range []struct {
		name string
		want bool
	}{
		{"deepseek-v4-flash", true},
		{"deepseek-v4-pro", true},        // was previously missed — v4-pro rejects tool_choice too
		{"openai/deepseek-v4-pro", true}, // provider-prefixed form
		{"deepseek-reasoner", true},
		{"kvasir", true},
		{"deepseek-chat", false}, // v3, non-reasoner — supports tool_choice
		{"openai/gpt-4o", false},
		{"google/gemini-2.5-flash", false},
	} {
		if got := isReasonerModel(tc.name); got != tc.want {
			t.Errorf("isReasonerModel(%q) = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestIsDeepSeekModel(t *testing.T) {
	for _, tc := range []struct {
		name string
		want bool
	}{
		{"deepseek-v4-pro", true},
		{"deepseek-v4-flash", true},
		{"openai/deepseek-v4-pro", true},
		{"deepseek-reasoner", true},
		{"qwen3-32b", false},
		{"openai/gpt-4o", false},
		{"google/gemini-2.5-flash", false},
	} {
		if got := isDeepSeekModel(tc.name); got != tc.want {
			t.Errorf("isDeepSeekModel(%q) = %v, want %v", tc.name, got, tc.want)
		}
	}
}

// TestBuildMessages_DeepSeekReasoningEcho verifies that DeepSeek assistant
// messages always carry reasoning_content (even empty), while non-DeepSeek
// models omit it — matching opencode's unconditional-echo invariant.
func TestBuildMessages_DeepSeekReasoningEcho(t *testing.T) {
	toolCall := func(id string) *genai.Part {
		return &genai.Part{
			FunctionCall: &genai.FunctionCall{ID: id, Name: "web-fetch", Args: map[string]any{"url": "https://example.com"}},
		}
	}
	thought := func(text string) *genai.Part {
		return &genai.Part{Text: text, Thought: true}
	}

	// DeepSeek: tool-call turn with no reasoning must still carry empty reasoning_content.
	// (ensureToolCallResponsePairs appends a synthetic tool response, so there are 2 messages.)
	msgs := buildMessages([]*genai.Content{{Role: "model", Parts: []*genai.Part{toolCall("call_1")}}}, true)
	if len(msgs) < 1 {
		t.Fatal("got no messages, want at least the assistant message")
	}
	if msgs[0].Role != "assistant" {
		t.Fatalf("first message role = %q, want assistant", msgs[0].Role)
	}
	if msgs[0].ReasoningContent == nil {
		t.Fatal("deepseek assistant tool turn: ReasoningContent is nil, want empty string")
	}
	if *msgs[0].ReasoningContent != "" {
		t.Errorf("deepseek assistant tool turn: reasoning_content = %q, want empty", *msgs[0].ReasoningContent)
	}

	// DeepSeek: reasoning turn must carry the reasoning text.
	msgs = buildMessages([]*genai.Content{{Role: "model", Parts: []*genai.Part{thought("thinking here"), {Text: "final answer"}}}}, true)
	if len(msgs) != 1 {
		t.Fatalf("got %d messages, want 1", len(msgs))
	}
	if msgs[0].ReasoningContent == nil || *msgs[0].ReasoningContent != "thinking here" {
		t.Errorf("deepseek assistant reasoning turn: reasoning_content = %v, want %q", msgs[0].ReasoningContent, "thinking here")
	}

	// Non-DeepSeek: tool-call turn omits reasoning_content.
	msgs = buildMessages([]*genai.Content{{Role: "model", Parts: []*genai.Part{toolCall("call_1")}}}, false)
	if len(msgs) < 1 {
		t.Fatal("got no messages, want at least the assistant message")
	}
	if msgs[0].ReasoningContent != nil {
		t.Errorf("non-deepseek assistant tool turn: ReasoningContent = %v, want nil", msgs[0].ReasoningContent)
	}
}

// TestGenerateContent_DeepSeekDisablesThinking verifies that a DeepSeek model
// with tools present sends `thinking: {"type": "disabled"}` (not the vLLM-only
// chat_template_kwargs knob) so DeepSeek doesn't enter thinking mode and then
// reject follow-up turns for missing reasoning_content.
func TestGenerateContent_DeepSeekDisablesThinking(t *testing.T) {
	var captured map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &captured)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"ok"},"finish_reason":"stop"}]}`))
	}))
	defer srv.Close()

	m := NewOpenAICompatibleModel(srv.URL, "test-key", "deepseek-v4-pro")

	req := &model.LLMRequest{
		Contents: []*genai.Content{genai.NewContentFromText("hi", genai.RoleUser)},
		Config: &genai.GenerateContentConfig{
			Tools: []*genai.Tool{
				{FunctionDeclarations: []*genai.FunctionDeclaration{{Name: "web-fetch"}}},
			},
		},
	}

	for _, err := range m.GenerateContent(context.Background(), req, false) {
		if err != nil {
			t.Fatalf("GenerateContent returned error: %v", err)
		}
	}

	thinking, ok := captured["thinking"].(map[string]any)
	if !ok {
		t.Fatalf("request body missing thinking field; body=%v", captured)
	}
	if thinking["type"] != "disabled" {
		t.Errorf("thinking.type = %v, want disabled", thinking["type"])
	}
	if _, hasKwargs := captured["chat_template_kwargs"]; hasKwargs {
		t.Errorf("deepseek request should not set chat_template_kwargs; body=%v", captured)
	}
}
