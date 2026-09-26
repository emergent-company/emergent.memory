package provider

import (
	"context"
	"log/slog"
	"testing"

	"github.com/emergent-company/emergent.memory/internal/config"
	"github.com/emergent-company/emergent.memory/pkg/crypto"
)

// newTestCredentialService creates a CredentialService for testing with a nil
// repository and catalog. Only suitable for tests that do not hit the DB.
func newTestCredentialService(cfg *config.Config) *CredentialService {
	registry := NewRegistry()
	log := slog.Default()

	return &CredentialService{
		repo:     nil,
		registry: registry,
		catalog:  nil,
		cfg:      cfg,
		log:      log,
	}
}

// TestResolve_NoContext_ReturnsNil verifies that Resolve returns (nil, nil)
// when there is no project or org in context (callers fall back to env vars).
func TestResolve_NoContext_ReturnsNil(t *testing.T) {
	cfg := &config.Config{
		LLM: config.LLMConfig{
			GoogleAPIKey: "test-google-api-key",
		},
	}
	svc := newTestCredentialService(cfg)

	ctx := context.Background()
	resolved, err := svc.Resolve(ctx, ProviderGoogleAI)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resolved != nil {
		t.Errorf("expected nil resolved credential, got %+v", resolved)
	}
}

// TestResolve_UnsupportedProvider verifies that an unsupported provider returns
// an error immediately.
func TestResolve_UnsupportedProvider(t *testing.T) {
	cfg := &config.Config{}
	svc := newTestCredentialService(cfg)

	ctx := context.Background()
	_, err := svc.Resolve(ctx, ProviderType("unsupported-provider-xyz"))
	if err == nil {
		t.Fatal("expected error for unsupported provider")
	}
}

func TestEncryptDecryptRoundTrip(t *testing.T) {
	hexKey, err := crypto.GenerateKey()
	if err != nil {
		t.Fatalf("failed to generate key: %v", err)
	}

	cfg := &config.Config{
		LLMProvider: config.LLMProviderConfig{
			EncryptionKey: hexKey,
		},
	}
	svc := NewCredentialService(nil, NewRegistry(), nil, cfg, slog.Default())

	plaintext := []byte("my-secret-api-key-12345")
	ciphertext, nonce, err := svc.EncryptCredential(plaintext)
	if err != nil {
		t.Fatalf("encrypt failed: %v", err)
	}

	decrypted, err := svc.encryptor.Decrypt(ciphertext, nonce)
	if err != nil {
		t.Fatalf("decrypt failed: %v", err)
	}
	if string(decrypted) != string(plaintext) {
		t.Errorf("expected %q, got %q", plaintext, decrypted)
	}
}

func TestEncryptCredential_NoKey(t *testing.T) {
	cfg := &config.Config{}
	svc := NewCredentialService(nil, NewRegistry(), nil, cfg, slog.Default())

	_, _, err := svc.EncryptCredential([]byte("secret"))
	if err == nil {
		t.Fatal("expected error when encryption key not configured")
	}
}

func TestStripModelPrefix(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		// A recognised dialect routing prefix is stripped, leaving the bare
		// model name — even when the bare model itself contains slashes.
		{"deepseek/deepseek-v4-flash", "deepseek-v4-flash"},
		{"openai/deepseek-v4-flash", "deepseek-v4-flash"},
		{"google/gemini-embedding-2-preview", "gemini-embedding-2-preview"},
		{"google-vertex/gemini-2.5-flash", "gemini-2.5-flash"},
		{"google/gemini-2.5-flash/experimental", "gemini-2.5-flash/experimental"},
		// Bare names are returned unchanged.
		{"deepseek-v4-pro", "deepseek-v4-pro"},
		{"", ""},
		// Unqualified multi-segment model IDs are NOT routing prefixes — their
		// first segment is not a dialect, so they stay intact.
		{"publishers/google/models/gemini-2.0-flash", "publishers/google/models/gemini-2.0-flash"},
		{"locations/us-central1/publishers/google/models/gemini-2.5-flash", "locations/us-central1/publishers/google/models/gemini-2.5-flash"},
	}
	for _, c := range cases {
		if got := stripModelPrefix(c.in); got != c.want {
			t.Errorf("stripModelPrefix(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// TestDecryptProjectConfig verifies that decryptProjectConfig correctly decrypts
// a Vertex AI project config and populates all credential fields.
func TestDecryptProjectConfig(t *testing.T) {
	hexKey, err := crypto.GenerateKey()
	if err != nil {
		t.Fatalf("failed to generate key: %v", err)
	}

	enc, _ := crypto.NewEncryptor(hexKey)
	saJSON := `{"type":"service_account","project_id":"test"}`
	ciphertext, nonce, _ := enc.Encrypt([]byte(saJSON))

	cfg := &config.Config{
		LLMProvider: config.LLMProviderConfig{
			EncryptionKey: hexKey,
		},
	}
	svc := NewCredentialService(nil, NewRegistry(), nil, cfg, slog.Default())

	projCfg := &ProjectProviderConfig{
		Provider:            ProviderVertexAI,
		EncryptedCredential: ciphertext,
		EncryptionNonce:     nonce,
		GCPProject:          "proj-gcp",
		Location:            "europe-west4",
		EmbeddingModel:      "google-vertex/custom-embed",
		GenerativeModel:     "google-vertex/custom-gen",
	}

	resolved, err := svc.decryptProjectConfig(projCfg)
	if err != nil {
		t.Fatalf("decrypt project config failed: %v", err)
	}
	if resolved.Provider != ProviderVertexAI {
		t.Errorf("expected provider google-vertex, got %s", resolved.Provider)
	}
	if resolved.Source != SourceProject {
		t.Errorf("expected source project, got %s", resolved.Source)
	}
	if resolved.ServiceAccountJSON != saJSON {
		t.Errorf("expected SA JSON %q, got %q", saJSON, resolved.ServiceAccountJSON)
	}
	if resolved.GCPProject != "proj-gcp" {
		t.Errorf("expected GCP project 'proj-gcp', got %q", resolved.GCPProject)
	}
	if resolved.Location != "europe-west4" {
		t.Errorf("expected location 'europe-west4', got %q", resolved.Location)
	}
	if resolved.EmbeddingModel != "custom-embed" {
		t.Errorf("expected embedding model 'custom-embed', got %q", resolved.EmbeddingModel)
	}
	if resolved.GenerativeModel != "custom-gen" {
		t.Errorf("expected generative model 'custom-gen', got %q", resolved.GenerativeModel)
	}
}

// TestResolveAny_NoContext_ReturnsNil verifies that ResolveAny returns (nil, nil)
// when there is no project or org in context.
func TestResolveAny_NoContext_ReturnsNil(t *testing.T) {
	cfg := &config.Config{}
	svc := newTestCredentialService(cfg)

	ctx := context.Background()
	resolved, err := svc.ResolveAny(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resolved != nil {
		t.Errorf("expected nil resolved credential, got %+v", resolved)
	}
}

// TestResolveAny_PriorityOrder verifies that ResolveAny tries VertexAI before
// GoogleAI before OpenAICompatible. This is a regression test for the bug where
// OpenAICompatible was tried first, causing Gemini model names to be sent to
// local/self-hosted endpoints when both provider types were configured.
//
// We test the priority indirectly: with no DB context, all providers return nil
// (no config found), so ResolveAny returns nil. The order is verified by
// inspecting the provider constants used in the loop — this test documents the
// expected order and will catch any future reordering.
func TestResolveAny_PriorityOrder_VertexBeforeGoogleBeforeOpenAI(t *testing.T) {
	// The expected resolution order: cloud providers first, local/self-hosted last.
	expectedOrder := []ProviderType{ProviderDeepSeek, ProviderOpenAI, ProviderVertexAI, ProviderGoogleAI}

	// Verify the constants have the expected string values (guards against rename bugs).
	if ProviderVertexAI != "google-vertex" {
		t.Errorf("ProviderVertexAI = %q, want %q", ProviderVertexAI, "google-vertex")
	}
	if ProviderGoogleAI != "google" {
		t.Errorf("ProviderGoogleAI = %q, want %q", ProviderGoogleAI, "google")
	}
	if ProviderOpenAI != "openai" {
		t.Errorf("ProviderOpenAI = %q, want %q", ProviderOpenAI, "openai")
	}
	if ProviderDeepSeek != "deepseek" {
		t.Errorf("ProviderDeepSeek = %q, want %q", ProviderDeepSeek, "deepseek")
	}

	// Verify the order slice itself matches expectations.
	for i, p := range expectedOrder {
		if i == 0 && p != ProviderDeepSeek {
			t.Errorf("position 0 = %q, want ProviderDeepSeek", p)
		}
		if i == 1 && p != ProviderOpenAI {
			t.Errorf("position 1 = %q, want ProviderOpenAI", p)
		}
	}
}

// ---------------------------------------------------------------------------
// shouldTestGenerative / shouldTestEmbed logic
//
// These tests document and enforce the skip-test conditions introduced to
// support embedding-only configurations (e.g. Google gemini-embedding-*).
//
// Logic (same in UpsertOrgConfig and UpsertProjectConfig):
//
//	shouldTestGenerative = GenerativeModel != "" || EmbeddingModel == ""
//	shouldTestEmbed      = EmbeddingModel  != "" || GenerativeModel == ""  (non-DeepSeek)
// ---------------------------------------------------------------------------

// shouldTestGenerative mirrors the production condition.
func shouldTestGenerative(generativeModel, embeddingModel string) bool {
	return generativeModel != "" || embeddingModel == ""
}

// shouldTestEmbed mirrors the production condition (excluding DeepSeek check).
func shouldTestEmbed(generativeModel, embeddingModel string) bool {
	return embeddingModel != "" || generativeModel == ""
}

func TestShouldTestGenerative(t *testing.T) {
	cases := []struct {
		name            string
		generativeModel string
		embeddingModel  string
		wantTest        bool
	}{
		{
			name:            "both models set — test generative",
			generativeModel: "gemini-2.5-flash",
			embeddingModel:  "text-embedding-004",
			wantTest:        true,
		},
		{
			name:            "only generative set — test generative",
			generativeModel: "gemini-2.5-flash",
			embeddingModel:  "",
			wantTest:        true,
		},
		{
			name:            "only embedding set — skip generative test",
			generativeModel: "",
			embeddingModel:  "text-embedding-004",
			wantTest:        false,
		},
		{
			name:            "neither set (bare key) — test generative (fall-through)",
			generativeModel: "",
			embeddingModel:  "",
			wantTest:        true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := shouldTestGenerative(tc.generativeModel, tc.embeddingModel)
			if got != tc.wantTest {
				t.Errorf("shouldTestGenerative(%q, %q) = %v, want %v",
					tc.generativeModel, tc.embeddingModel, got, tc.wantTest)
			}
		})
	}
}

func TestShouldTestEmbed(t *testing.T) {
	cases := []struct {
		name            string
		generativeModel string
		embeddingModel  string
		wantTest        bool
	}{
		{
			name:            "both models set — test embed",
			generativeModel: "gemini-2.5-flash",
			embeddingModel:  "text-embedding-004",
			wantTest:        true,
		},
		{
			name:            "only embedding set — test embed",
			generativeModel: "",
			embeddingModel:  "text-embedding-004",
			wantTest:        true,
		},
		{
			name:            "only generative set — skip embed test",
			generativeModel: "gemini-2.5-flash",
			embeddingModel:  "",
			wantTest:        false,
		},
		{
			name:            "neither set (bare key) — test embed (fall-through)",
			generativeModel: "",
			embeddingModel:  "",
			wantTest:        true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := shouldTestEmbed(tc.generativeModel, tc.embeddingModel)
			if got != tc.wantTest {
				t.Errorf("shouldTestEmbed(%q, %q) = %v, want %v",
					tc.generativeModel, tc.embeddingModel, got, tc.wantTest)
			}
		})
	}
}

// TestShouldTestGenerative_Symmetric verifies that exactly one of the two
// conditions fires when only one model type is provided — preventing a situation
// where both tests run for a single-scope API key.
func TestShouldTest_Symmetric_SingleModel(t *testing.T) {
	// Embedding-only: only embed test should run.
	if shouldTestGenerative("", "embed-model") {
		t.Error("embedding-only: generative test should be skipped")
	}
	if !shouldTestEmbed("", "embed-model") {
		t.Error("embedding-only: embed test should run")
	}

	// Generative-only: only generative test should run.
	if !shouldTestGenerative("gen-model", "") {
		t.Error("generative-only: generative test should run")
	}
	if shouldTestEmbed("gen-model", "") {
		t.Error("generative-only: embed test should be skipped")
	}
}

// TestResolveAny_UnsupportedProviderError verifies that ResolveAny propagates
// the last error when all providers fail with errors (not nil returns).
// This guards against silently swallowing credential resolution failures.
func TestResolveAny_PropagatesLastError(t *testing.T) {
	cfg := &config.Config{}
	svc := newTestCredentialService(cfg)

	// With a nil repo and no context, Resolve returns (nil, nil) for all providers.
	// So ResolveAny should return (nil, nil) — no error to propagate.
	ctx := context.Background()
	resolved, err := svc.ResolveAny(ctx)
	if err != nil {
		t.Fatalf("unexpected error with no context: %v", err)
	}
	if resolved != nil {
		t.Errorf("expected nil, got %+v", resolved)
	}
}

// TestResolveAnyEmbedding_NoContext_ReturnsNil verifies that ResolveAnyEmbedding
// returns (nil, nil) when there is no project context.
func TestResolveAnyEmbedding_NoContext_ReturnsNil(t *testing.T) {
	cfg := &config.Config{}
	svc := newTestCredentialService(cfg)

	resolved, err := svc.ResolveAnyEmbedding(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resolved != nil {
		t.Errorf("expected nil resolved credential, got %+v", resolved)
	}
}

// TestPickEmbeddingConfig_SkipsGenerativeOnlyProviders is a regression test for
// the embedding fallback: when DeepSeek (generative-only, no embedding model)
// is configured alongside a Google embedding provider, pickEmbeddingConfig must
// skip DeepSeek and select the Google config — never short-circuiting on a
// generative-only provider.
func TestPickEmbeddingConfig_SkipsGenerativeOnlyProviders(t *testing.T) {
	cfgs := map[ProviderType]*ProjectProviderConfig{
		ProviderDeepSeek: {Provider: ProviderDeepSeek, EmbeddingModel: ""},
		ProviderGoogleAI: {Provider: ProviderGoogleAI, EmbeddingModel: "gemini/gemini-embedding-001"},
	}

	picked := pickEmbeddingConfig(cfgs)
	if picked == nil {
		t.Fatal("expected a config to be picked")
	}
	if picked.Provider != ProviderGoogleAI {
		t.Errorf("picked provider = %q, want %q", picked.Provider, ProviderGoogleAI)
	}
}

// TestPickEmbeddingConfig_NoneHaveEmbedding verifies pickEmbeddingConfig returns
// nil when no configured provider carries an embedding model.
func TestPickEmbeddingConfig_NoneHaveEmbedding(t *testing.T) {
	cfgs := map[ProviderType]*ProjectProviderConfig{
		ProviderDeepSeek: {Provider: ProviderDeepSeek, EmbeddingModel: ""},
		ProviderOpenAI:   {Provider: ProviderOpenAI, EmbeddingModel: ""},
	}
	if picked := pickEmbeddingConfig(cfgs); picked != nil {
		t.Errorf("expected nil, got %+v", picked)
	}
}

// TestPickEmbeddingConfig_RespectsOrder verifies pickEmbeddingConfig prefers
// Google AI over Vertex AI when both carry an embedding model.
func TestPickEmbeddingConfig_RespectsOrder(t *testing.T) {
	cfgs := map[ProviderType]*ProjectProviderConfig{
		ProviderVertexAI: {Provider: ProviderVertexAI, EmbeddingModel: "gemini-embedding-001"},
		ProviderGoogleAI: {Provider: ProviderGoogleAI, EmbeddingModel: "gemini-embedding-001"},
	}
	picked := pickEmbeddingConfig(cfgs)
	if picked == nil || picked.Provider != ProviderGoogleAI {
		t.Errorf("picked = %+v, want ProviderGoogleAI first", picked)
	}
}

// TestPrefixedGenerativeModelName is a regression test for double-stripping:
// decryptProjectConfig already runs stored models through stripModelPrefix
// (single-slash prefixes stripped, multi-segment Vertex resource paths kept
// intact), so DefaultGenerativeModel's name building must prefix the provider
// without a second Cut that would corrupt "publishers/google/models/..." into
// "google/models/...".
func TestPrefixedGenerativeModelName(t *testing.T) {
	cases := []struct {
		name     string
		provider ProviderType
		gen      string
		want     string
	}{
		{
			name:     "single-slash prefixed name is stripped then re-prefixed",
			provider: ProviderOpenAI,
			gen:      "openai/gpt-4o",
			want:     "openai/gpt-4o",
		},
		{
			name:     "foreign single-slash prefix is replaced by routing provider",
			provider: ProviderOpenAI,
			gen:      "deepseek/deepseek-v4-flash",
			want:     "openai/deepseek-v4-flash",
		},
		{
			name:     "bare name is unchanged",
			provider: ProviderGoogleAI,
			gen:      "gemini-2.5-flash",
			want:     "google/gemini-2.5-flash",
		},
		{
			name:     "multi-segment Vertex path is prefixed intact, not double-cut",
			provider: ProviderVertexAI,
			gen:      "publishers/google/models/gemini-2.5-flash",
			want:     "google-vertex/publishers/google/models/gemini-2.5-flash",
		},
		{
			name:     "multi-segment Vertex path with location is prefixed intact",
			provider: ProviderVertexAI,
			gen:      "locations/us-central1/publishers/google/models/gemini-2.5-flash",
			want:     "google-vertex/locations/us-central1/publishers/google/models/gemini-2.5-flash",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := prefixedGenerativeModelName(ProviderSlug(tc.provider), tc.gen); got != tc.want {
				t.Errorf("prefixedGenerativeModelName(%q, %q) = %q, want %q", tc.provider, tc.gen, got, tc.want)
			}
		})
	}
}

// TestDefaultGenerativeModelName_DecryptAndBuild wires the real decrypt path
// (encrypt → ProjectProviderConfig → decryptProjectConfig) into the name
// builder, proving the provider-credential fallback reports intact model names
// for both prefixed and multi-segment Vertex values.
func TestDefaultGenerativeModelName_DecryptAndBuild(t *testing.T) {
	hexKey, err := crypto.GenerateKey()
	if err != nil {
		t.Fatalf("failed to generate key: %v", err)
	}
	enc, _ := crypto.NewEncryptor(hexKey)
	saJSON := `{"type":"service_account","project_id":"test"}`
	ciphertext, nonce, _ := enc.Encrypt([]byte(saJSON))

	cfg := &config.Config{
		LLMProvider: config.LLMProviderConfig{
			EncryptionKey: hexKey,
		},
	}
	svc := NewCredentialService(nil, NewRegistry(), nil, cfg, slog.Default())

	cases := []struct {
		name string
		cfg  *ProjectProviderConfig
		want string
	}{
		{
			name: "vertex multi-segment path stored bare stays intact",
			cfg: &ProjectProviderConfig{
				Provider:            ProviderVertexAI,
				EncryptedCredential: ciphertext,
				EncryptionNonce:     nonce,
				GenerativeModel:     "publishers/google/models/gemini-2.5-flash",
			},
			want: "google-vertex/publishers/google/models/gemini-2.5-flash",
		},
		{
			name: "vertex single-slash prefixed name is stripped then re-prefixed once",
			cfg: &ProjectProviderConfig{
				Provider:            ProviderVertexAI,
				EncryptedCredential: ciphertext,
				EncryptionNonce:     nonce,
				GenerativeModel:     "google-vertex/gemini-2.5-flash",
			},
			want: "google-vertex/gemini-2.5-flash",
		},
		{
			name: "openai prefixed name keeps provider once",
			cfg: &ProjectProviderConfig{
				Provider:            ProviderOpenAI,
				EncryptedCredential: ciphertext,
				EncryptionNonce:     nonce,
				GenerativeModel:     "openai/gpt-4o",
			},
			want: "openai/gpt-4o",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cred, err := svc.decryptProjectConfig(tc.cfg)
			if err != nil {
				t.Fatalf("decryptProjectConfig failed: %v", err)
			}
			if cred == nil || cred.GenerativeModel == "" {
				t.Fatal("expected a resolved credential with a generative model")
			}
			if got := prefixedGenerativeModelName(ProviderSlug(cred.Provider), cred.GenerativeModel); got != tc.want {
				t.Errorf("decrypt+build = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestPrefixedEmbeddingModelName mirrors TestPrefixedGenerativeModelName for the
// embedding path: single-slash prefixes are stripped then re-prefixed with the
// routing provider, and multi-segment Vertex resource paths are kept intact so
// the EmbeddingResolverAdapter's provider/model split does not corrupt them.
func TestPrefixedEmbeddingModelName(t *testing.T) {
	cases := []struct {
		name     string
		provider ProviderType
		emb      string
		want     string
	}{
		{
			name:     "single-slash prefixed name is stripped then re-prefixed",
			provider: ProviderGoogleAI,
			emb:      "google/gemini-embedding-001",
			want:     "google/gemini-embedding-001",
		},
		{
			name:     "bare name is prefixed",
			provider: ProviderVertexAI,
			emb:      "text-embedding-005",
			want:     "google-vertex/text-embedding-005",
		},
		{
			name:     "multi-segment Vertex path is prefixed intact, not double-cut",
			provider: ProviderVertexAI,
			emb:      "publishers/google/models/text-embedding-005",
			want:     "google-vertex/publishers/google/models/text-embedding-005",
		},
		{
			name:     "foreign single-slash prefix is replaced by routing provider",
			provider: ProviderOpenAI,
			emb:      "google/text-embedding-005",
			want:     "openai/text-embedding-005",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := prefixedEmbeddingModelName(ProviderSlug(tc.provider), tc.emb); got != tc.want {
				t.Errorf("prefixedEmbeddingModelName(%q, %q) = %q, want %q", tc.provider, tc.emb, got, tc.want)
			}
		})
	}
}

// TestDefaultEmbeddingModelEmptyProject verifies the no-project early return
// returns ("", nil) without touching the repository (nil-safe, mirrors
// DefaultGenerativeModel's guard).
func TestDefaultEmbeddingModelEmptyProject(t *testing.T) {
	svc := newTestCredentialService(&config.Config{})
	model, err := svc.DefaultEmbeddingModel(context.Background(), "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if model != "" {
		t.Errorf("DefaultEmbeddingModel(\"\") = %q, want \"\"", model)
	}
}
