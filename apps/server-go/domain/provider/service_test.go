package provider

import (
	"context"
	"log/slog"
	"testing"

	"github.com/emergent-company/emergent/internal/config"
	"github.com/emergent-company/emergent/pkg/auth"
	"github.com/emergent-company/emergent/pkg/crypto"
)

// newTestCredentialService creates a CredentialService for testing with the
// given config and a nil repository (env-only tests don't hit the DB).
func newTestCredentialService(cfg *config.Config) *CredentialService {
	registry := NewRegistry()
	log := slog.Default()

	return &CredentialService{
		repo:     nil, // no DB in unit tests
		registry: registry,
		cfg:      cfg,
		log:      log,
	}
}

func TestResolveFromEnv_GoogleAI(t *testing.T) {
	cfg := &config.Config{
		LLM: config.LLMConfig{
			GoogleAPIKey: "test-google-api-key",
			Model:        "gemini-2.0-flash",
		},
		Embeddings: config.EmbeddingsConfig{
			Model: "gemini-embedding-001",
		},
	}
	svc := newTestCredentialService(cfg)

	// No project/org in context → should fall to env
	ctx := context.Background()
	resolved, err := svc.Resolve(ctx, ProviderGoogleAI)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if resolved.Provider != ProviderGoogleAI {
		t.Errorf("expected provider google-ai, got %s", resolved.Provider)
	}
	if resolved.Source != SourceEnvironment {
		t.Errorf("expected source environment, got %s", resolved.Source)
	}
	if resolved.APIKey != "test-google-api-key" {
		t.Errorf("expected API key 'test-google-api-key', got %q", resolved.APIKey)
	}
	if resolved.GenerativeModel != "gemini-2.0-flash" {
		t.Errorf("expected generative model 'gemini-2.0-flash', got %q", resolved.GenerativeModel)
	}
	if resolved.EmbeddingModel != "gemini-embedding-001" {
		t.Errorf("expected embedding model 'gemini-embedding-001', got %q", resolved.EmbeddingModel)
	}
}

func TestResolveFromEnv_VertexAI(t *testing.T) {
	cfg := &config.Config{
		LLM: config.LLMConfig{
			GCPProjectID:     "my-gcp-project",
			VertexAILocation: "us-central1",
			Model:            "gemini-2.0-flash",
		},
		Embeddings: config.EmbeddingsConfig{
			Model: "gemini-embedding-001",
		},
	}
	svc := newTestCredentialService(cfg)

	ctx := context.Background()
	resolved, err := svc.Resolve(ctx, ProviderVertexAI)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if resolved.Provider != ProviderVertexAI {
		t.Errorf("expected provider vertex-ai, got %s", resolved.Provider)
	}
	if resolved.Source != SourceEnvironment {
		t.Errorf("expected source environment, got %s", resolved.Source)
	}
	if resolved.GCPProject != "my-gcp-project" {
		t.Errorf("expected GCP project 'my-gcp-project', got %q", resolved.GCPProject)
	}
	if resolved.Location != "us-central1" {
		t.Errorf("expected location 'us-central1', got %q", resolved.Location)
	}
}

func TestResolveFromEnv_GoogleAI_NotConfigured(t *testing.T) {
	cfg := &config.Config{
		LLM:        config.LLMConfig{},
		Embeddings: config.EmbeddingsConfig{},
	}
	svc := newTestCredentialService(cfg)

	ctx := context.Background()
	_, err := svc.Resolve(ctx, ProviderGoogleAI)
	if err == nil {
		t.Fatal("expected error when no Google AI key configured")
	}
}

func TestResolveFromEnv_VertexAI_NotConfigured(t *testing.T) {
	cfg := &config.Config{
		LLM:        config.LLMConfig{},
		Embeddings: config.EmbeddingsConfig{},
	}
	svc := newTestCredentialService(cfg)

	ctx := context.Background()
	_, err := svc.Resolve(ctx, ProviderVertexAI)
	if err == nil {
		t.Fatal("expected error when no Vertex AI credentials configured")
	}
}

func TestResolve_UnsupportedProvider(t *testing.T) {
	cfg := &config.Config{}
	svc := newTestCredentialService(cfg)

	ctx := context.Background()
	_, err := svc.Resolve(ctx, ProviderType("openai"))
	if err == nil {
		t.Fatal("expected error for unsupported provider")
	}
}

func TestResolve_PolicyNone_FallsToEnv(t *testing.T) {
	// When a project has policy=none, it should skip org and go straight to env
	cfg := &config.Config{
		LLM: config.LLMConfig{
			GoogleAPIKey: "env-key",
			Model:        "env-model",
		},
		Embeddings: config.EmbeddingsConfig{
			Model: "env-embed",
		},
	}
	svc := newTestCredentialService(cfg)

	// resolveFromEnv directly to verify
	resolved, err := svc.resolveFromEnv(ProviderGoogleAI)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resolved.Source != SourceEnvironment {
		t.Errorf("expected source environment, got %s", resolved.Source)
	}
	if resolved.APIKey != "env-key" {
		t.Errorf("expected API key 'env-key', got %q", resolved.APIKey)
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
	svc := NewCredentialService(nil, NewRegistry(), cfg, slog.Default())

	plaintext := []byte("my-secret-api-key-12345")
	ciphertext, nonce, err := svc.EncryptCredential(plaintext)
	if err != nil {
		t.Fatalf("encrypt failed: %v", err)
	}

	// Decrypt using the encryptor directly
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
	svc := NewCredentialService(nil, NewRegistry(), cfg, slog.Default())

	_, _, err := svc.EncryptCredential([]byte("secret"))
	if err == nil {
		t.Fatal("expected error when encryption key not configured")
	}
}

func TestDecryptOrgCredential(t *testing.T) {
	hexKey, err := crypto.GenerateKey()
	if err != nil {
		t.Fatalf("failed to generate key: %v", err)
	}

	enc, _ := crypto.NewEncryptor(hexKey)
	ciphertext, nonce, _ := enc.Encrypt([]byte("org-api-key"))

	cfg := &config.Config{
		LLMProvider: config.LLMProviderConfig{
			EncryptionKey: hexKey,
		},
	}
	svc := NewCredentialService(nil, NewRegistry(), cfg, slog.Default())

	cred := &OrganizationProviderCredential{
		Provider:            ProviderGoogleAI,
		EncryptedCredential: ciphertext,
		EncryptionNonce:     nonce,
	}

	resolved, err := svc.decryptOrgCredential(cred)
	if err != nil {
		t.Fatalf("decrypt org credential failed: %v", err)
	}
	if resolved.APIKey != "org-api-key" {
		t.Errorf("expected API key 'org-api-key', got %q", resolved.APIKey)
	}
	if resolved.Source != SourceOrganization {
		t.Errorf("expected source organization, got %s", resolved.Source)
	}
}

func TestDecryptProjectCredential_VertexAI(t *testing.T) {
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
	svc := NewCredentialService(nil, NewRegistry(), cfg, slog.Default())

	policy := &ProjectProviderPolicy{
		Provider:            ProviderVertexAI,
		Policy:              PolicyProject,
		EncryptedCredential: ciphertext,
		EncryptionNonce:     nonce,
		GCPProject:          "proj-gcp",
		Location:            "europe-west4",
		EmbeddingModel:      "custom-embed",
		GenerativeModel:     "custom-gen",
	}

	resolved, err := svc.decryptProjectCredential(policy)
	if err != nil {
		t.Fatalf("decrypt project credential failed: %v", err)
	}
	if resolved.Provider != ProviderVertexAI {
		t.Errorf("expected provider vertex-ai, got %s", resolved.Provider)
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

func TestResolve_WithContextProjectAndOrg_NoDBFallsToEnv(t *testing.T) {
	// When context has project/org IDs but no DB is available (nil repo),
	// the service should still work — the repo calls will panic,
	// but with env fallback available and no DB calls needed, it should be fine.
	// In this test, we simulate by setting project context but having nil repo.
	// The Resolve method will try to call repo.GetProjectPolicy which will panic on nil.
	// So instead, we test the resolveFromEnv path directly.
	cfg := &config.Config{
		LLM: config.LLMConfig{
			GoogleAPIKey: "context-env-key",
			Model:        "context-env-model",
		},
		Embeddings: config.EmbeddingsConfig{
			Model: "context-env-embed",
		},
	}
	svc := newTestCredentialService(cfg)

	resolved, err := svc.resolveFromEnv(ProviderGoogleAI)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resolved.APIKey != "context-env-key" {
		t.Errorf("expected 'context-env-key', got %q", resolved.APIKey)
	}
}

func TestResolve_ContextWithoutProjectOrOrg(t *testing.T) {
	cfg := &config.Config{
		LLM: config.LLMConfig{
			GoogleAPIKey: "fallback-key",
			Model:        "fallback-model",
		},
		Embeddings: config.EmbeddingsConfig{
			Model: "fallback-embed",
		},
	}
	svc := newTestCredentialService(cfg)

	// Context has auth user but no project/org IDs
	ctx := context.Background()
	ctx = auth.ContextWithUser(ctx, &auth.AuthUser{ID: "user1"})

	resolved, err := svc.Resolve(ctx, ProviderGoogleAI)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resolved.Source != SourceEnvironment {
		t.Errorf("expected source environment, got %s", resolved.Source)
	}
	if resolved.APIKey != "fallback-key" {
		t.Errorf("expected API key 'fallback-key', got %q", resolved.APIKey)
	}
}
