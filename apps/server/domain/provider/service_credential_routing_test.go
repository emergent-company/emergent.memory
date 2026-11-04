package provider

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/emergent-company/emergent.memory/internal/config"
	"github.com/emergent-company/emergent.memory/pkg/crypto"
)

// TestDecryptProjectConfig_RoutesNonLegacyCredential verifies that a vendor
// outside the legacy four has its decrypted credential routed onto the
// resolved credential (API key for api-key vendors), with protocol/auth/base
// URL populated from its registry definition. Regression test for new vendors
// losing their decrypted credential.
func TestDecryptProjectConfig_RoutesNonLegacyCredential(t *testing.T) {
	hexKey, err := crypto.GenerateKey()
	if err != nil {
		t.Fatalf("failed to generate key: %v", err)
	}
	enc, _ := crypto.NewEncryptor(hexKey)
	const secret = "sk-aliyun-secret"
	ciphertext, nonce, _ := enc.Encrypt([]byte(secret))

	cfg := &config.Config{
		LLMProvider: config.LLMProviderConfig{EncryptionKey: hexKey},
	}
	svc := NewCredentialService(nil, NewRegistry(), nil, cfg, slog.Default())

	projCfg := &ProjectProviderConfig{
		Provider:            ProviderAliyun,
		EncryptedCredential: ciphertext,
		EncryptionNonce:     nonce,
		GenerativeModel:     "aliyun/qwen-max",
	}

	resolved, err := svc.decryptProjectConfig(projCfg)
	if err != nil {
		t.Fatalf("decryptProjectConfig failed: %v", err)
	}
	if resolved.APIKey != secret {
		t.Errorf("APIKey = %q, want %q (non-legacy credential must be routed)", resolved.APIKey, secret)
	}
	if resolved.Protocol != ProtocolOpenAIChat {
		t.Errorf("Protocol = %q, want %q", resolved.Protocol, ProtocolOpenAIChat)
	}
	if resolved.Auth != AuthBearer {
		t.Errorf("Auth = %q, want %q", resolved.Auth, AuthBearer)
	}
	if resolved.BaseURL == "" {
		t.Error("BaseURL is empty, want the Aliyun default from the definition")
	}
	if resolved.GenerativeModel != "qwen-max" {
		t.Errorf("GenerativeModel = %q, want %q", resolved.GenerativeModel, "qwen-max")
	}
}

// TestBuildTempResolvedCred_NonLegacyKeyUsedByTestConnection verifies that a
// non-legacy vendor's configured API key survives buildTempResolvedCred and is
// sent on the test-connection generate call. Regression test for new vendors
// losing their credential on the configure/test path.
func TestBuildTempResolvedCred_NonLegacyKeyUsedByTestConnection(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{
				{"message": map[string]any{"content": "hello"}},
			},
		})
	}))
	defer srv.Close()

	cfg := &config.Config{}
	svc := newTestCredentialService(cfg)

	cred := svc.buildTempResolvedCred(ProviderAliyun, UpsertProviderConfigRequest{
		APIKey:          "sk-aliyun-configure",
		BaseURL:         srv.URL,
		GenerativeModel: "qwen-max",
	})
	if cred.APIKey != "sk-aliyun-configure" {
		t.Fatalf("temp cred APIKey = %q, want %q", cred.APIKey, "sk-aliyun-configure")
	}

	catalog := &ModelCatalogService{
		log: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	reply, err := catalog.generateContentForModel(context.Background(), ProviderAliyun, cred, "qwen-max")
	if err != nil {
		t.Fatalf("generateContentForModel failed: %v", err)
	}
	if reply != "hello" {
		t.Errorf("reply = %q, want %q", reply, "hello")
	}
	if gotAuth != "Bearer sk-aliyun-configure" {
		t.Errorf("Authorization header = %q, want %q (configured key must be used by test-connection)", gotAuth, "Bearer sk-aliyun-configure")
	}
}
