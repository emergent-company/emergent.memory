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
	"google.golang.org/adk/v2/model"
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
			wantErr: "no generative model configured: set DEEPSEEK_MODEL, OPENAI_MODEL, VERTEX_AI_MODEL, or <PROVIDER>_MODEL (e.g. ANTHROPIC_MODEL)",
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

// TestModelFactory_NetworkDisabled_HardGate verifies that LLM_NETWORK_DISABLED
// is a hard gate at model-creation time: when the kill switch is on, no
// provider-backed model is constructed from either env vars or the DB
// credential hierarchy.
func TestModelFactory_NetworkDisabled_HardGate(t *testing.T) {
	log := slog.New(slog.NewTextHandler(os.Stdout, nil))

	envCfg := &config.LLMConfig{
		NetworkDisabled: true,
		DeepSeekAPIKey:  "test-key",
		DeepSeekModel:   "deepseek/deepseek-v4-flash",
	}
	dbCred := &ResolvedCredential{
		Provider:        "deepseek",
		GenerativeModel: "deepseek-v4-flash",
		BaseURL:         "https://api.deepseek.com/v1",
		APIKey:          "test-key",
		Source:          "project",
	}

	t.Run("CreateModel env path errors", func(t *testing.T) {
		f := NewModelFactory(envCfg, log, nil, nil, nil)
		llm, err := f.CreateModel(context.Background())
		if !errors.Is(err, ErrNetworkDisabled) {
			t.Fatalf("CreateModel() error = %v, want ErrNetworkDisabled", err)
		}
		if llm != nil {
			t.Fatal("CreateModel() built a model despite LLM_NETWORK_DISABLED")
		}
	})

	t.Run("CreateModelWithName env path errors", func(t *testing.T) {
		f := NewModelFactory(envCfg, log, nil, nil, nil)
		llm, err := f.CreateModelWithName(context.Background(), "deepseek/deepseek-v4-flash")
		if !errors.Is(err, ErrNetworkDisabled) {
			t.Fatalf("CreateModelWithName() error = %v, want ErrNetworkDisabled", err)
		}
		if llm != nil {
			t.Fatal("CreateModelWithName() built a model despite LLM_NETWORK_DISABLED")
		}
	})

	t.Run("CreateModel DB path errors", func(t *testing.T) {
		f := NewModelFactory(&config.LLMConfig{NetworkDisabled: true}, log,
			&stubCredentialResolver{cred: dbCred},
			nil,
			&stubModelResolver{model: "deepseek/deepseek-v4-flash", source: "project"},
		)
		ctx := WithProjectID(context.Background(), "00000000-0000-0000-0000-000000000000")
		llm, err := f.CreateModel(ctx)
		if !errors.Is(err, ErrNetworkDisabled) {
			t.Fatalf("CreateModel() error = %v, want ErrNetworkDisabled", err)
		}
		if llm != nil {
			t.Fatal("CreateModel() built a DB-backed model despite LLM_NETWORK_DISABLED")
		}
	})

	t.Run("CreateModelWithName DB path errors", func(t *testing.T) {
		f := NewModelFactory(&config.LLMConfig{NetworkDisabled: true}, log,
			&stubCredentialResolver{cred: dbCred}, nil, nil)
		llm, err := f.CreateModelWithName(context.Background(), "deepseek/deepseek-v4-flash")
		if !errors.Is(err, ErrNetworkDisabled) {
			t.Fatalf("CreateModelWithName() error = %v, want ErrNetworkDisabled", err)
		}
		if llm != nil {
			t.Fatal("CreateModelWithName() built a DB-backed model despite LLM_NETWORK_DISABLED")
		}
	})
}

// TestModelFactory_NetworkEnabled_Unchanged verifies that with the kill switch
// off (the default), env-var and DB model construction still succeed.
func TestModelFactory_NetworkEnabled_Unchanged(t *testing.T) {
	log := slog.New(slog.NewTextHandler(os.Stdout, nil))

	t.Run("env path builds model", func(t *testing.T) {
		cfg := &config.LLMConfig{
			DeepSeekAPIKey: "test-key",
			DeepSeekModel:  "deepseek/deepseek-v4-flash",
		}
		f := NewModelFactory(cfg, log, nil, nil, nil)
		llm, err := f.CreateModel(context.Background())
		if err != nil {
			t.Fatalf("CreateModel() unexpected error: %v", err)
		}
		if llm == nil {
			t.Fatal("CreateModel() returned nil model")
		}
	})

	t.Run("env path builds named model", func(t *testing.T) {
		cfg := &config.LLMConfig{DeepSeekAPIKey: "test-key"}
		f := NewModelFactory(cfg, log, nil, nil, nil)
		llm, err := f.CreateModelWithName(context.Background(), "deepseek/deepseek-v4-flash")
		if err != nil {
			t.Fatalf("CreateModelWithName() unexpected error: %v", err)
		}
		if llm == nil {
			t.Fatal("CreateModelWithName() returned nil model")
		}
	})

	t.Run("DB path builds model", func(t *testing.T) {
		cred := &ResolvedCredential{
			Provider:        "deepseek",
			GenerativeModel: "deepseek-v4-flash",
			BaseURL:         "https://api.deepseek.com/v1",
			APIKey:          "test-key",
			Source:          "project",
		}
		f := NewModelFactory(&config.LLMConfig{}, log,
			&stubCredentialResolver{cred: cred}, nil,
			&stubModelResolver{model: "deepseek/deepseek-v4-flash", source: "project"},
		)
		ctx := WithProjectID(context.Background(), "00000000-0000-0000-0000-000000000000")
		llm, err := f.CreateModel(ctx)
		if err != nil {
			t.Fatalf("CreateModel() unexpected error: %v", err)
		}
		if llm == nil {
			t.Fatal("CreateModel() returned nil model")
		}
	})
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

// TestCreateModel_EnvFallbackWhenResolverWired pins the #1456 reachability fix:
// the production model resolver is always wired, so a DB resolver that yields no
// model must still fall through to env configuration. Before the fix this path
// returned the "no generative model configured" error without ever consulting
// ModelName(), killing env-only self-host deployments.
func TestCreateModel_EnvFallbackWhenResolverWired(t *testing.T) {
	f := NewModelFactory(&config.LLMConfig{}, testLogger(), nil, nil,
		&stubModelResolver{model: "", source: ""})
	f.WithEnvVendorResolver(registryStub())
	f.WithEnvLookup(envMap(map[string]string{
		"ANTHROPIC_API_KEY": "ak",
		"ANTHROPIC_MODEL":   "claude-sonnet-4-5",
	}))

	ctx := WithProjectID(context.Background(), "00000000-0000-0000-0000-000000000000")
	llm, err := f.CreateModel(ctx)
	if err != nil {
		t.Fatalf("CreateModel() error = %v, want env fallback when resolver yields no model", err)
	}
	m, ok := llm.(*anthropicModel)
	if !ok {
		t.Fatalf("model type = %T, want *anthropicModel", llm)
	}
	if m.modelName != "claude-sonnet-4-5" {
		t.Errorf("modelName = %q, want claude-sonnet-4-5", m.modelName)
	}
}

// TestCreateModel_NoDBNoEnv_Errors verifies that when neither the DB resolver nor
// env yields a model, CreateModel still returns the clear configuration error
// rather than a nil model or a panic.
func TestCreateModel_NoDBNoEnv_Errors(t *testing.T) {
	f := NewModelFactory(&config.LLMConfig{}, testLogger(), nil, nil,
		&stubModelResolver{model: "", source: ""})

	ctx := WithProjectID(context.Background(), "00000000-0000-0000-0000-000000000000")
	_, err := f.CreateModel(ctx)
	if err == nil {
		t.Fatal("CreateModel() error = nil, want no-generative-model error")
	}
	if !strings.Contains(err.Error(), "no generative model configured") {
		t.Errorf("error = %q, want mention of no generative model configured", err.Error())
	}
}

// TestCreateModel_DBModelBeatsEnv verifies DB precedence survives the env
// fallback: when the resolver yields a model, env is never consulted even though
// a (different) env model is configured.
func TestCreateModel_DBModelBeatsEnv(t *testing.T) {
	cfg := &config.LLMConfig{DeepSeekAPIKey: "dk", DeepSeekModel: "deepseek/deepseek-env"}
	f := NewModelFactory(cfg, testLogger(), nil, nil,
		&stubModelResolver{model: "anthropic/db-model", source: "project"})
	f.WithEnvVendorResolver(registryStub())
	f.WithEnvLookup(envMap(map[string]string{
		"ANTHROPIC_API_KEY": "ak",
	}))
	w := &captureWrapper{}
	f.wrapper = w

	// The env default (deepseek) is a different provider than the DB model
	// (anthropic); if env leaked through we would build a deepseek model.
	if got := f.ModelName(); got != "deepseek/deepseek-env" {
		t.Fatalf("ModelName() = %q, want deepseek/deepseek-env (env default)", got)
	}

	ctx := WithProjectID(context.Background(), "00000000-0000-0000-0000-000000000000")
	llm, err := f.CreateModel(ctx)
	if err != nil {
		t.Fatalf("CreateModel() error = %v", err)
	}
	m, ok := llm.(*anthropicModel)
	if !ok {
		t.Fatalf("model type = %T, want *anthropicModel (DB model wins over env)", llm)
	}
	if m.modelName != "db-model" {
		t.Errorf("modelName = %q, want db-model", m.modelName)
	}
	if !w.called || w.slug != "anthropic" {
		t.Errorf("wrapper slug = %q (called=%v), want anthropic — DB model must win over env", w.slug, w.called)
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

// countingCredentialResolver counts resolver calls so tests can assert the
// network-disabled gate returns before any credential resolution happens.
type countingCredentialResolver struct {
	cred     *ResolvedCredential
	anyCalls int
	forCalls int
}

func (s *countingCredentialResolver) ResolveAny(_ context.Context) (*ResolvedCredential, error) {
	s.anyCalls++
	return s.cred, nil
}

func (s *countingCredentialResolver) ResolveFor(_ context.Context, _ string) (*ResolvedCredential, error) {
	s.forCalls++
	return s.cred, nil
}

func (s *countingCredentialResolver) ResolveBySlug(_ context.Context, _ string) (*ResolvedCredential, error) {
	return s.cred, nil
}

// countingModelResolver counts ResolveGenerativeModelByID calls.
type countingModelResolver struct {
	model  string
	source string
	calls  int
}

func (s *countingModelResolver) ResolveGenerativeModelByID(_ context.Context, _ string) (string, string, error) {
	s.calls++
	return s.model, s.source, nil
}

// stubTestLLMChecker returns a fixed test-LLM flag value.
type stubTestLLMChecker struct {
	enabled bool
}

func (s *stubTestLLMChecker) IsTestLLM(_ context.Context, _ string) bool { return s.enabled }

// TestModelFactory_NetworkDisabled_ShortCircuitsResolution locks the ordering
// guarantee that the kill-switch gate fires before any credential or model
// resolution: with LLM_NETWORK_DISABLED on the resolvers must never be called,
// so credentials are not even looked up.
func TestModelFactory_NetworkDisabled_ShortCircuitsResolution(t *testing.T) {
	log := slog.New(slog.NewTextHandler(os.Stdout, nil))
	ctx := WithProjectID(context.Background(), "00000000-0000-0000-0000-000000000000")

	t.Run("CreateModel does not call model resolver", func(t *testing.T) {
		mr := &countingModelResolver{model: "deepseek/deepseek-v4-flash", source: "project"}
		cr := &countingCredentialResolver{cred: &ResolvedCredential{Provider: "deepseek", GenerativeModel: "deepseek-v4-flash"}}
		f := NewModelFactory(&config.LLMConfig{NetworkDisabled: true}, log, cr, nil, mr)
		if _, err := f.CreateModel(ctx); !errors.Is(err, ErrNetworkDisabled) {
			t.Fatalf("CreateModel() error = %v, want ErrNetworkDisabled", err)
		}
		if mr.calls != 0 {
			t.Errorf("model resolver called %d times while network disabled, want 0", mr.calls)
		}
		if cr.anyCalls != 0 {
			t.Errorf("credential resolver called %d times while network disabled, want 0", cr.anyCalls)
		}
	})

	t.Run("CreateModelWithName does not call credential resolver", func(t *testing.T) {
		cr := &countingCredentialResolver{cred: &ResolvedCredential{Provider: "deepseek", GenerativeModel: "deepseek-v4-flash"}}
		f := NewModelFactory(&config.LLMConfig{NetworkDisabled: true}, log, cr, nil, nil)
		if _, err := f.CreateModelWithName(ctx, "deepseek/deepseek-v4-flash"); !errors.Is(err, ErrNetworkDisabled) {
			t.Fatalf("CreateModelWithName() error = %v, want ErrNetworkDisabled", err)
		}
		if cr.forCalls != 0 {
			t.Errorf("credential resolver called %d times while network disabled, want 0", cr.forCalls)
		}
	})
}

// TestModelFactory_NetworkDisabled_TestLLMTakesPrecedence locks the documented
// ordering: a project with test-LLM enabled still gets the deterministic
// no-network canned model even when the kill switch is on. A project without the
// flag gets ErrNetworkDisabled — the test-LLM feature can never route to a live
// provider model while the switch is on.
func TestModelFactory_NetworkDisabled_TestLLMTakesPrecedence(t *testing.T) {
	log := slog.New(slog.NewTextHandler(os.Stdout, nil))
	ctx := WithProjectID(context.Background(), "00000000-0000-0000-0000-000000000000")
	cfg := &config.LLMConfig{
		NetworkDisabled: true,
		DeepSeekAPIKey:  "test-key",
		DeepSeekModel:   "deepseek/deepseek-v4-flash",
	}

	t.Run("canned model on both entry points", func(t *testing.T) {
		f := NewModelFactory(cfg, log, nil, nil, nil)
		f.WithTestLLMChecker(&stubTestLLMChecker{enabled: true})

		llm, err := f.CreateModel(ctx)
		if err != nil {
			t.Fatalf("CreateModel() error = %v, want nil (canned model)", err)
		}
		if llm == nil {
			t.Fatal("CreateModel() returned nil model")
		}
		if llm.Name() != "test-llm" {
			t.Errorf("CreateModel() model name = %q, want test-llm", llm.Name())
		}

		named, err := f.CreateModelWithName(ctx, "deepseek/deepseek-v4-flash")
		if err != nil {
			t.Fatalf("CreateModelWithName() error = %v, want nil (canned model)", err)
		}
		if named == nil {
			t.Fatal("CreateModelWithName() returned nil model")
		}
		if named.Name() != "test-llm" {
			t.Errorf("CreateModelWithName() model name = %q, want test-llm", named.Name())
		}
	})

	t.Run("flag off still gated", func(t *testing.T) {
		f := NewModelFactory(cfg, log, nil, nil, nil)
		f.WithTestLLMChecker(&stubTestLLMChecker{enabled: false})
		if _, err := f.CreateModel(ctx); !errors.Is(err, ErrNetworkDisabled) {
			t.Fatalf("CreateModel() error = %v, want ErrNetworkDisabled", err)
		}
	})
}
