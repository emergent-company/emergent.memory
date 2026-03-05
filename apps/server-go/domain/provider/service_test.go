package provider

import (
	"context"
	"log/slog"
	"testing"

	"github.com/emergent-company/emergent/internal/config"
	"github.com/emergent-company/emergent/pkg/crypto"
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
	_, err := svc.Resolve(ctx, ProviderType("openai"))
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

// TestDecryptOrgConfig verifies that decryptOrgConfig correctly decrypts a
// Google AI org config and populates the resolved credential fields.
func TestDecryptOrgConfig(t *testing.T) {
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
	svc := NewCredentialService(nil, NewRegistry(), nil, cfg, slog.Default())

	orgCfg := &OrgProviderConfig{
		Provider:            ProviderGoogleAI,
		EncryptedCredential: ciphertext,
		EncryptionNonce:     nonce,
		GenerativeModel:     "gemini-2.5-flash",
		EmbeddingModel:      "text-embedding-004",
	}

	resolved, err := svc.decryptOrgConfig(orgCfg)
	if err != nil {
		t.Fatalf("decrypt org config failed: %v", err)
	}
	if resolved.APIKey != "org-api-key" {
		t.Errorf("expected API key 'org-api-key', got %q", resolved.APIKey)
	}
	if resolved.Source != SourceOrganization {
		t.Errorf("expected source organization, got %s", resolved.Source)
	}
	if resolved.GenerativeModel != "gemini-2.5-flash" {
		t.Errorf("expected generative model 'gemini-2.5-flash', got %q", resolved.GenerativeModel)
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
		EmbeddingModel:      "custom-embed",
		GenerativeModel:     "custom-gen",
	}

	resolved, err := svc.decryptProjectConfig(projCfg)
	if err != nil {
		t.Fatalf("decrypt project config failed: %v", err)
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
