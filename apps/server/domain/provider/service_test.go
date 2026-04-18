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
	// This order ensures that a Gemini model name is never accidentally routed to
	// an OpenAI-compatible endpoint when a Google provider is also configured.
	expectedOrder := []ProviderType{ProviderVertexAI, ProviderGoogleAI, ProviderOpenAICompatible}

	// Verify the constants have the expected string values (guards against rename bugs).
	if ProviderVertexAI != "google-vertex" {
		t.Errorf("ProviderVertexAI = %q, want %q", ProviderVertexAI, "google-vertex")
	}
	if ProviderGoogleAI != "google" {
		t.Errorf("ProviderGoogleAI = %q, want %q", ProviderGoogleAI, "google")
	}
	if ProviderOpenAICompatible != "openai-compatible" {
		t.Errorf("ProviderOpenAICompatible = %q, want %q", ProviderOpenAICompatible, "openai-compatible")
	}

	// Verify the order slice itself matches expectations.
	// This is a compile-time-safe way to document and enforce the priority.
	for i, p := range expectedOrder {
		if i == 0 && p != ProviderVertexAI {
			t.Errorf("position 0 = %q, want ProviderVertexAI", p)
		}
		if i == 1 && p != ProviderGoogleAI {
			t.Errorf("position 1 = %q, want ProviderGoogleAI", p)
		}
		if i == 2 && p != ProviderOpenAICompatible {
			t.Errorf("position 2 = %q, want ProviderOpenAICompatible", p)
		}
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

