package adk

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"strings"
	"testing"

	"google.golang.org/adk/v2/model"

	"github.com/emergent-company/emergent.memory/internal/config"
)

// errResolver is a CredentialResolver that always fails, used to exercise the
// env-fallback decision on the resolver-error path.
type errResolver struct{}

func (errResolver) ResolveAny(context.Context) (*ResolvedCredential, error) {
	return nil, errors.New("db unavailable")
}
func (errResolver) ResolveFor(context.Context, string) (*ResolvedCredential, error) {
	return nil, errors.New("db unavailable")
}
func (errResolver) ResolveBySlug(context.Context, string) (*ResolvedCredential, error) {
	return nil, errors.New("db unavailable")
}

// stubEnvVendors is an in-memory EnvVendorResolver for tests.
type stubEnvVendors struct {
	vendors map[string]EnvVendor
	order   []string
}

func (s *stubEnvVendors) EnvVendor(provider string) (EnvVendor, bool) {
	v, ok := s.vendors[provider]
	return v, ok
}

func (s *stubEnvVendors) EnvVendorOrder() []string { return s.order }

// envMap returns an env getter backed by a map, so tests never touch the
// process environment.
func envMap(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
}

// registryStub returns a resolver mirroring two non-legacy registry vendors and
// honoring the default protocol/auth the real adapter would supply.
func registryStub() *stubEnvVendors {
	return &stubEnvVendors{
		order: []string{"anthropic", "openrouter"},
		vendors: map[string]EnvVendor{
			"anthropic": {
				Prefix:         "ANTHROPIC",
				DefaultBaseURL: "https://api.anthropic.com/v1",
				Protocol:       protocolAnthropicMessages,
				Auth:           string(AuthXAPIKey),
			},
			"openrouter": {
				Prefix:         "OPENROUTER",
				DefaultBaseURL: "https://openrouter.ai/api/v1",
				Protocol:       protocolOpenAIChat,
				Auth:           string(AuthBearer),
			},
		},
	}
}

func TestEnvPrefix(t *testing.T) {
	tests := map[string]string{
		"openai":        "OPENAI",
		"deepseek":      "DEEPSEEK",
		"google":        "GOOGLE",
		"google-vertex": "GOOGLE_VERTEX",
		"azure-openai":  "AZURE_OPENAI",
		"openrouter":    "OPENROUTER",
		"siliconflow":   "SILICONFLOW",
	}
	for provider, want := range tests {
		if got := EnvPrefix(provider); got != want {
			t.Errorf("EnvPrefix(%q) = %q, want %q", provider, got, want)
		}
	}
}

// TestResolveEnvVendor_LegacyBackCompat pins the four legacy providers to their
// historical env vars, defaults, and protocols.
func TestResolveEnvVendor_LegacyBackCompat(t *testing.T) {
	t.Run("deepseek", func(t *testing.T) {
		f := NewModelFactory(&config.LLMConfig{DeepSeekAPIKey: "dk", DeepSeekModel: "deepseek/deepseek-v4-flash"}, testLogger(), nil, nil, nil)
		cfg, err := f.ResolveEnvVendor("deepseek", "")
		if err != nil {
			t.Fatalf("ResolveEnvVendor() error = %v", err)
		}
		if cfg.APIKey != "dk" {
			t.Errorf("APIKey = %q, want dk", cfg.APIKey)
		}
		if cfg.BaseURL != legacyDeepSeekBaseURL {
			t.Errorf("BaseURL = %q, want %q", cfg.BaseURL, legacyDeepSeekBaseURL)
		}
		if cfg.Model != "deepseek-v4-flash" {
			t.Errorf("Model = %q, want deepseek-v4-flash (routing prefix stripped)", cfg.Model)
		}
		if cfg.Protocol != protocolOpenAIChat {
			t.Errorf("Protocol = %q, want %q", cfg.Protocol, protocolOpenAIChat)
		}
	})

	t.Run("openai default base promotes to responses", func(t *testing.T) {
		f := NewModelFactory(&config.LLMConfig{OpenAIAPIKey: "ok", OpenAIModel: "openai/gpt-4o"}, testLogger(), nil, nil, nil)
		cfg, err := f.ResolveEnvVendor("openai", "")
		if err != nil {
			t.Fatalf("ResolveEnvVendor() error = %v", err)
		}
		if cfg.BaseURL != legacyOpenAIBaseURL {
			t.Errorf("BaseURL = %q, want %q", cfg.BaseURL, legacyOpenAIBaseURL)
		}
		if cfg.Model != "gpt-4o" {
			t.Errorf("Model = %q, want gpt-4o", cfg.Model)
		}
		if cfg.Protocol != protocolOpenAIResponses {
			t.Errorf("Protocol = %q, want %q", cfg.Protocol, protocolOpenAIResponses)
		}
	})

	t.Run("openai custom base stays chat", func(t *testing.T) {
		f := NewModelFactory(&config.LLMConfig{OpenAIAPIKey: "ok", OpenAIBaseURL: "http://localhost:11434/v1"}, testLogger(), nil, nil, nil)
		cfg, err := f.ResolveEnvVendor("openai", "gpt-4o")
		if err != nil {
			t.Fatalf("ResolveEnvVendor() error = %v", err)
		}
		if cfg.Protocol != protocolOpenAIChat {
			t.Errorf("Protocol = %q, want %q", cfg.Protocol, protocolOpenAIChat)
		}
	})

	t.Run("google", func(t *testing.T) {
		f := NewModelFactory(&config.LLMConfig{GoogleAPIKey: "gk"}, testLogger(), nil, nil, nil)
		cfg, err := f.ResolveEnvVendor("google", "gemini-2.5-flash")
		if err != nil {
			t.Fatalf("ResolveEnvVendor() error = %v", err)
		}
		if !cfg.IsGoogleAI || cfg.APIKey != "gk" || cfg.Protocol != protocolGoogleGenAI {
			t.Errorf("google config = %+v, want IsGoogleAI with key gk and google-genai", cfg)
		}
	})

	t.Run("vertex", func(t *testing.T) {
		f := NewModelFactory(&config.LLMConfig{GCPProjectID: "proj", VertexAILocation: "us-central1"}, testLogger(), nil, nil, nil)
		cfg, err := f.ResolveEnvVendor("google-vertex", "gemini-2.5-flash")
		if err != nil {
			t.Fatalf("ResolveEnvVendor() error = %v", err)
		}
		if !cfg.IsVertexAI || cfg.GCPProject != "proj" || cfg.Location != "us-central1" {
			t.Errorf("vertex config = %+v, want IsVertexAI proj/us-central1", cfg)
		}
	})
}

// TestCreateEnvModel_LegacyResolvesModel verifies each legacy env var still
// builds a concrete model with the historical base URL and model defaults.
func TestCreateEnvModel_LegacyResolvesModel(t *testing.T) {
	ctx := context.Background()

	t.Run("deepseek", func(t *testing.T) {
		f := NewModelFactory(&config.LLMConfig{DeepSeekAPIKey: "dk"}, testLogger(), nil, nil, nil)
		llm, err := f.CreateModelWithName(ctx, "deepseek/deepseek-v4-flash")
		if err != nil {
			t.Fatalf("CreateModelWithName() error = %v", err)
		}
		m, ok := llm.(*openaiCompatibleModel)
		if !ok {
			t.Fatalf("model type = %T, want *openaiCompatibleModel", llm)
		}
		if m.baseURL != legacyDeepSeekBaseURL || m.modelName != "deepseek-v4-flash" || m.apiKey != "dk" {
			t.Errorf("deepseek model = %+v, want baseURL %q model deepseek-v4-flash key dk", m, legacyDeepSeekBaseURL)
		}
	})

	t.Run("openai default base uses responses", func(t *testing.T) {
		f := NewModelFactory(&config.LLMConfig{OpenAIAPIKey: "ok"}, testLogger(), nil, nil, nil)
		llm, err := f.CreateModelWithName(ctx, "openai/gpt-4o")
		if err != nil {
			t.Fatalf("CreateModelWithName() error = %v", err)
		}
		m, ok := llm.(*openaiResponsesModel)
		if !ok {
			t.Fatalf("model type = %T, want *openaiResponsesModel", llm)
		}
		if m.baseURL != legacyOpenAIBaseURL || m.modelName != "gpt-4o" || m.apiKey != "ok" {
			t.Errorf("openai model = %+v, want baseURL %q model gpt-4o key ok", m, legacyOpenAIBaseURL)
		}
	})

	t.Run("openai custom base uses chat", func(t *testing.T) {
		f := NewModelFactory(&config.LLMConfig{OpenAIAPIKey: "ok", OpenAIBaseURL: "http://localhost:11434/v1"}, testLogger(), nil, nil, nil)
		llm, err := f.CreateModelWithName(ctx, "openai/llama3")
		if err != nil {
			t.Fatalf("CreateModelWithName() error = %v", err)
		}
		m, ok := llm.(*openaiCompatibleModel)
		if !ok {
			t.Fatalf("model type = %T, want *openaiCompatibleModel", llm)
		}
		if m.baseURL != "http://localhost:11434/v1" {
			t.Errorf("baseURL = %q, want custom local URL", m.baseURL)
		}
	})

	t.Run("google", func(t *testing.T) {
		f := NewModelFactory(&config.LLMConfig{GoogleAPIKey: "gk"}, testLogger(), nil, nil, nil)
		llm, err := f.CreateModelWithName(ctx, "google/gemini-2.5-flash")
		if err != nil {
			t.Fatalf("CreateModelWithName() error = %v", err)
		}
		if llm == nil || llm.Name() != "gemini-2.5-flash" {
			t.Errorf("google model = %v, want gemini-2.5-flash", llm)
		}
	})
}

// TestCreateEnvModel_GenericVendor verifies a non-legacy registered vendor now
// resolves through the generic <PREFIX>_* convention.
func TestCreateEnvModel_GenericVendor(t *testing.T) {
	ctx := context.Background()

	t.Run("anthropic native protocol", func(t *testing.T) {
		f := NewModelFactory(&config.LLMConfig{}, testLogger(), nil, nil, nil)
		f.WithEnvVendorResolver(registryStub())
		f.WithEnvLookup(envMap(map[string]string{
			"ANTHROPIC_API_KEY": "ak",
			"ANTHROPIC_MODEL":   "claude-sonnet-4-5",
		}))

		llm, err := f.CreateModelWithName(ctx, "anthropic/claude-sonnet-4-5")
		if err != nil {
			t.Fatalf("CreateModelWithName() error = %v", err)
		}
		m, ok := llm.(*anthropicModel)
		if !ok {
			t.Fatalf("model type = %T, want *anthropicModel", llm)
		}
		if m.baseURL != "https://api.anthropic.com/v1" || m.modelName != "claude-sonnet-4-5" || m.apiKey != "ak" {
			t.Errorf("anthropic model = %+v, want default baseURL, model, key", m)
		}
	})

	t.Run("openrouter openai-chat protocol", func(t *testing.T) {
		f := NewModelFactory(&config.LLMConfig{}, testLogger(), nil, nil, nil)
		f.WithEnvVendorResolver(registryStub())
		f.WithEnvLookup(envMap(map[string]string{
			"OPENROUTER_API_KEY": "rk",
			"OPENROUTER_MODEL":   "openai/gpt-4o", // namespaced model id must NOT be stripped
		}))

		llm, err := f.CreateModelWithName(ctx, "openrouter/openai/gpt-4o")
		if err != nil {
			t.Fatalf("CreateModelWithName() error = %v", err)
		}
		m, ok := llm.(*openaiCompatibleModel)
		if !ok {
			t.Fatalf("model type = %T, want *openaiCompatibleModel", llm)
		}
		if m.baseURL != "https://openrouter.ai/api/v1" || m.modelName != "openai/gpt-4o" {
			t.Errorf("openrouter model = %+v, want default baseURL and namespaced model preserved", m)
		}
	})
}

// TestCreateEnvModel_MissingKeyNotEnabled verifies a vendor with no env API key
// is not enabled and never silently built.
func TestCreateEnvModel_MissingKeyNotEnabled(t *testing.T) {
	ctx := context.Background()

	f := NewModelFactory(&config.LLMConfig{}, testLogger(), nil, nil, nil)
	f.WithEnvVendorResolver(registryStub())
	f.WithEnvLookup(envMap(map[string]string{
		"ANTHROPIC_MODEL": "claude-sonnet-4-5", // key deliberately absent
	}))

	_, err := f.CreateModelWithName(ctx, "anthropic/claude-sonnet-4-5")
	if err == nil {
		t.Fatal("expected error when ANTHROPIC_API_KEY is unset, got nil")
	}
	if !strings.Contains(err.Error(), "ANTHROPIC_API_KEY is not set") {
		t.Errorf("error = %q, want mention of ANTHROPIC_API_KEY is not set", err.Error())
	}
}

// TestCreateEnvModel_UnregisteredVendor verifies an unregistered vendor errors
// rather than falling through to an accidental default endpoint.
func TestCreateEnvModel_UnregisteredVendor(t *testing.T) {
	f := NewModelFactory(&config.LLMConfig{}, testLogger(), nil, nil, nil)
	f.WithEnvVendorResolver(registryStub())

	_, err := f.CreateModelWithName(context.Background(), "mystery/model-1")
	if err == nil {
		t.Fatal("expected error for unregistered vendor, got nil")
	}
	if !strings.Contains(err.Error(), "unsupported provider") {
		t.Errorf("error = %q, want unsupported provider", err.Error())
	}
}

// TestResolveEnvVendor_BaseURLAndModelOverrides verifies the generic
// <PREFIX>_BASE_URL and <PREFIX>_MODEL overrides apply and that a routed model
// naming the vendor itself is stripped while a foreign namespace is preserved.
func TestResolveEnvVendor_BaseURLAndModelOverrides(t *testing.T) {
	f := NewModelFactory(&config.LLMConfig{}, testLogger(), nil, nil, nil)
	f.WithEnvVendorResolver(registryStub())
	f.WithEnvLookup(envMap(map[string]string{
		"OPENROUTER_API_KEY":  "rk",
		"OPENROUTER_BASE_URL": "https://relay.example.com/v1",
		"OPENROUTER_MODEL":    "openrouter/vendor/model",
	}))

	cfg, err := f.ResolveEnvVendor("openrouter", "")
	if err != nil {
		t.Fatalf("ResolveEnvVendor() error = %v", err)
	}
	if cfg.BaseURL != "https://relay.example.com/v1" {
		t.Errorf("BaseURL = %q, want relay override", cfg.BaseURL)
	}
	if cfg.Model != "vendor/model" {
		t.Errorf("Model = %q, want vendor/model (own prefix stripped)", cfg.Model)
	}

	// A foreign namespace must survive: openrouter namespaces the upstream
	// vendor ("openai/gpt-4o"); stripping it would corrupt the model id.
	f.WithEnvLookup(envMap(map[string]string{
		"OPENROUTER_API_KEY": "rk",
		"OPENROUTER_MODEL":   "openai/gpt-4o",
	}))
	cfg, err = f.ResolveEnvVendor("openrouter", "")
	if err != nil {
		t.Fatalf("ResolveEnvVendor() error = %v", err)
	}
	if cfg.Model != "openai/gpt-4o" {
		t.Errorf("Model = %q, want openai/gpt-4o (foreign namespace preserved)", cfg.Model)
	}
}

// TestModelNameAndIsEnabled_Generic verifies generic env discovery drives the
// no-DB-resolver CreateModel path, and that a key without a model does not
// enable the factory (no accidental enable).
func TestModelNameAndIsEnabled_Generic(t *testing.T) {
	t.Run("configured", func(t *testing.T) {
		f := NewModelFactory(&config.LLMConfig{}, testLogger(), nil, nil, nil)
		f.WithEnvVendorResolver(registryStub())
		f.WithEnvLookup(envMap(map[string]string{
			"ANTHROPIC_API_KEY": "ak",
			"ANTHROPIC_MODEL":   "claude-sonnet-4-5",
		}))

		if got := f.ModelName(); got != "anthropic/claude-sonnet-4-5" {
			t.Errorf("ModelName() = %q, want anthropic/claude-sonnet-4-5", got)
		}
		if !f.IsEnabled() {
			t.Error("IsEnabled() = false, want true when a generic vendor has key + model")
		}
	})

	t.Run("key without model is not enabled", func(t *testing.T) {
		f := NewModelFactory(&config.LLMConfig{}, testLogger(), nil, nil, nil)
		f.WithEnvVendorResolver(registryStub())
		f.WithEnvLookup(envMap(map[string]string{"ANTHROPIC_API_KEY": "ak"}))

		if got := f.ModelName(); got != "" {
			t.Errorf("ModelName() = %q, want empty without a model", got)
		}
		if f.IsEnabled() {
			t.Error("IsEnabled() = true, want false on a key alone")
		}
	})
}

// TestModelName_GenericNamespacedModelRoutesToOwningVendor verifies a namespaced
// model value (e.g. OpenRouter's "openai/gpt-4o") is re-prefixed with the owning
// vendor rather than mistaken for a routed reference to another dialect.
func TestModelName_GenericNamespacedModelRoutesToOwningVendor(t *testing.T) {
	f := NewModelFactory(&config.LLMConfig{}, testLogger(), nil, nil, nil)
	f.WithEnvVendorResolver(registryStub())
	f.WithEnvLookup(envMap(map[string]string{
		"OPENROUTER_API_KEY": "rk",
		"OPENROUTER_MODEL":   "openai/gpt-4o",
	}))

	if got := f.ModelName(); got != "openrouter/openai/gpt-4o" {
		t.Fatalf("ModelName() = %q, want openrouter/openai/gpt-4o", got)
	}
	// CreateModel (no DB resolver) must build the OpenRouter model, not OpenAI's.
	llm, err := f.CreateModel(context.Background())
	if err != nil {
		t.Fatalf("CreateModel() error = %v", err)
	}
	m, ok := llm.(*openaiCompatibleModel)
	if !ok {
		t.Fatalf("model type = %T, want *openaiCompatibleModel", llm)
	}
	if m.baseURL != "https://openrouter.ai/api/v1" || m.modelName != "openai/gpt-4o" {
		t.Errorf("model = %+v, want OpenRouter default baseURL and namespaced model", m)
	}
}

// TestIsEnabled_NetworkDisabled_GenericVendor verifies LLM_NETWORK_DISABLED is a
// hard kill switch: a fully configured generic vendor still reports disabled.
func TestIsEnabled_NetworkDisabled_GenericVendor(t *testing.T) {
	f := NewModelFactory(&config.LLMConfig{NetworkDisabled: true}, testLogger(), nil, nil, nil)
	f.WithEnvVendorResolver(registryStub())
	f.WithEnvLookup(envMap(map[string]string{
		"ANTHROPIC_API_KEY": "ak",
		"ANTHROPIC_MODEL":   "claude-sonnet-4-5",
	}))

	if f.IsEnabled() {
		t.Error("IsEnabled() = true, want false when NetworkDisabled is set")
	}
}

// TestResolveEnvVendor_UsesDefinitionResponsesHosts verifies the env path selects
// openai-responses only when the *vendor definition itself* declares the
// resolved base URL's host — never for every openai-chat vendor whose URL happens
// to be a first-party OpenAI host. This keeps env and DB protocol selection
// identical.
func TestResolveEnvVendor_UsesDefinitionResponsesHosts(t *testing.T) {
	vendors := &stubEnvVendors{
		order: []string{"openrouter", "relay"},
		vendors: map[string]EnvVendor{
			// Mirrors OpenRouter: openai-chat but declares no Responses host.
			"openrouter": {Prefix: "OPENROUTER", Protocol: protocolOpenAIChat, Auth: string(AuthBearer)},
			// A vendor that explicitly declares the first-party host.
			"relay": {Prefix: "RELAY", Protocol: protocolOpenAIChat, ResponsesHosts: []string{"api.openai.com"}, Auth: string(AuthBearer)},
		},
	}
	f := NewModelFactory(&config.LLMConfig{}, testLogger(), nil, nil, nil)
	f.WithEnvVendorResolver(vendors)

	t.Run("undeclared host is not promoted", func(t *testing.T) {
		f.WithEnvLookup(envMap(map[string]string{
			"OPENROUTER_API_KEY":  "rk",
			"OPENROUTER_BASE_URL": "https://api.openai.com/v1",
		}))
		cfg, err := f.ResolveEnvVendor("openrouter", "gpt-4o")
		if err != nil {
			t.Fatalf("ResolveEnvVendor() error = %v", err)
		}
		if cfg.Protocol != protocolOpenAIChat {
			t.Errorf("Protocol = %q, want %q (definition declares no Responses host)", cfg.Protocol, protocolOpenAIChat)
		}
	})

	t.Run("declared host is promoted", func(t *testing.T) {
		f.WithEnvLookup(envMap(map[string]string{
			"RELAY_API_KEY":  "rk",
			"RELAY_BASE_URL": "https://api.openai.com/v1",
		}))
		cfg, err := f.ResolveEnvVendor("relay", "gpt-4o")
		if err != nil {
			t.Fatalf("ResolveEnvVendor() error = %v", err)
		}
		if cfg.Protocol != protocolOpenAIResponses {
			t.Errorf("Protocol = %q, want %q (definition declares api.openai.com)", cfg.Protocol, protocolOpenAIResponses)
		}
	})

	t.Run("declared host not matched keeps chat", func(t *testing.T) {
		f.WithEnvLookup(envMap(map[string]string{
			"RELAY_API_KEY":  "rk",
			"RELAY_BASE_URL": "https://relay.example.com/v1",
		}))
		cfg, err := f.ResolveEnvVendor("relay", "gpt-4o")
		if err != nil {
			t.Fatalf("ResolveEnvVendor() error = %v", err)
		}
		if cfg.Protocol != protocolOpenAIChat {
			t.Errorf("Protocol = %q, want %q (relay host not declared)", cfg.Protocol, protocolOpenAIChat)
		}
	})
}

// TestCreateEnvModel_WhitespaceKeyNotEnabled verifies a whitespace-only API key
// is treated as absent and never enables a vendor.
func TestCreateEnvModel_WhitespaceKeyNotEnabled(t *testing.T) {
	f := NewModelFactory(&config.LLMConfig{}, testLogger(), nil, nil, nil)
	f.WithEnvVendorResolver(registryStub())
	f.WithEnvLookup(envMap(map[string]string{
		"ANTHROPIC_API_KEY": "   ",
		"ANTHROPIC_MODEL":   "claude-sonnet-4-5",
	}))

	if got := f.ModelName(); got != "" {
		t.Errorf("ModelName() = %q, want empty for whitespace-only key", got)
	}
	if f.IsEnabled() {
		t.Error("IsEnabled() = true, want false for whitespace-only key")
	}
	_, err := f.CreateModelWithName(context.Background(), "anthropic/claude-sonnet-4-5")
	if err == nil || !strings.Contains(err.Error(), "ANTHROPIC_API_KEY is not set") {
		t.Errorf("error = %v, want ANTHROPIC_API_KEY is not set", err)
	}
}

// TestCreateModelWithName_ResolverErrorFallsBackToGenericEnv verifies a
// credential-resolver error falls through to env when a generic vendor is
// configured, and is surfaced (not masked) when no env credential exists.
func TestCreateModelWithName_ResolverErrorFallsBackToGenericEnv(t *testing.T) {
	ctx := context.Background()

	t.Run("falls back when generic env is configured", func(t *testing.T) {
		f := NewModelFactory(&config.LLMConfig{}, testLogger(), errResolver{}, nil, nil)
		f.WithEnvVendorResolver(registryStub())
		f.WithEnvLookup(envMap(map[string]string{"ANTHROPIC_API_KEY": "ak"}))

		llm, err := f.CreateModelWithName(ctx, "anthropic/claude-sonnet-4-5")
		if err != nil {
			t.Fatalf("CreateModelWithName() error = %v, want env fallback", err)
		}
		if _, ok := llm.(*anthropicModel); !ok {
			t.Fatalf("model type = %T, want *anthropicModel", llm)
		}
	})

	t.Run("surfaces error when no env credential", func(t *testing.T) {
		f := NewModelFactory(&config.LLMConfig{}, testLogger(), errResolver{}, nil, nil)
		f.WithEnvVendorResolver(registryStub())
		f.WithEnvLookup(envMap(nil))

		_, err := f.CreateModelWithName(ctx, "anthropic/claude-sonnet-4-5")
		if err == nil {
			t.Fatal("expected credential-resolution error, got nil")
		}
		if !strings.Contains(err.Error(), "LLM credential resolution failed") {
			t.Errorf("error = %q, want LLM credential resolution failed", err.Error())
		}
	})
}

// captureWrapper records the slug/dialect the factory attributed to a model.
type captureWrapper struct {
	called  bool
	slug    string
	dialect string
}

func (w *captureWrapper) WrapModel(inner model.LLM, slug, dialect string) model.LLM {
	w.called = true
	w.slug = slug
	w.dialect = dialect
	return inner
}

// TestCreateModelWithName_DBConfigBeatsEnv verifies the DB credential path wins
// over env when a credential resolver returns a credential, even when the same
// vendor is also configured from env.
func TestCreateModelWithName_DBConfigBeatsEnv(t *testing.T) {
	f := NewModelFactory(&config.LLMConfig{}, testLogger(),
		&staticResolver{cred: &ResolvedCredential{
			Provider:        "anthropic",
			Slug:            "anthropic-db",
			BaseURL:         "https://db.example.com/v1",
			APIKey:          "db-key",
			GenerativeModel: "claude-db",
			Protocol:        protocolAnthropicMessages,
			Source:          "project",
		}}, nil, nil)
	f.WithEnvVendorResolver(registryStub())
	f.WithEnvLookup(envMap(map[string]string{
		"ANTHROPIC_API_KEY":  "env-key",
		"ANTHROPIC_BASE_URL": "https://env.example.com/v1",
		"ANTHROPIC_MODEL":    "claude-env",
	}))
	w := &captureWrapper{}
	f.wrapper = w

	llm, err := f.CreateModelWithName(context.Background(), "anthropic/claude-db")
	if err != nil {
		t.Fatalf("CreateModelWithName() error = %v", err)
	}
	m, ok := llm.(*anthropicModel)
	if !ok {
		t.Fatalf("model type = %T, want *anthropicModel", llm)
	}
	if m.baseURL != "https://db.example.com/v1" || m.apiKey != "db-key" {
		t.Errorf("model = %+v, want DB credential (baseURL db.example.com, key db-key)", m)
	}
	if !w.called || w.slug != "anthropic-db" {
		t.Errorf("wrapper slug = %q (called=%v), want anthropic-db — DB credential must win over env", w.slug, w.called)
	}
}
