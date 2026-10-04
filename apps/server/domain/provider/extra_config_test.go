package provider

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/emergent-company/emergent.memory/internal/config"
	"github.com/emergent-company/emergent.memory/internal/testdb"
	"github.com/emergent-company/emergent.memory/pkg/crypto"
)

// TestProjectProviderConfig_ExtraConfigRoundTrip is the persist+read round-trip
// for vendor extra config (issue #1451). It asserts that a declared secret field
// (LKEAP secret_key) is stored as ciphertext at rest — never in the jsonb
// column or the plaintext bytes — and decrypted back on read, while a non-secret
// field (api_version) is stored in the plaintext jsonb column. It also pins the
// omit-on-update and per-key merge semantics.
func TestProjectProviderConfig_ExtraConfigRoundTrip(t *testing.T) {
	if testing.Short() {
		testdb.SkipOrFatal(t, "skipping database integration test in short mode")
	}
	ctx := context.Background()
	tdb := testdb.SetupTestDBOrFail(t, ctx, "provextra")
	t.Cleanup(tdb.Close)
	db := tdb.DB
	log := slog.New(slog.NewTextHandler(io.Discard, nil))

	orgID := uuid.NewString()
	projectID := uuid.NewString()
	if _, err := db.ExecContext(ctx, `INSERT INTO kb.orgs (id, name) VALUES (?, ?)`, orgID, "org-"+orgID); err != nil {
		t.Fatalf("seed org: %v", err)
	}
	if _, err := db.ExecContext(ctx,
		`INSERT INTO kb.projects (id, organization_id, name) VALUES (?, ?, ?)`,
		projectID, orgID, "proj-"+projectID); err != nil {
		t.Fatalf("seed project: %v", err)
	}

	hexKey, err := crypto.GenerateKey()
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	cfg := &config.Config{LLMProvider: config.LLMProviderConfig{EncryptionKey: hexKey}}
	repo := NewRepository(db, log)
	svc := NewCredentialService(repo, NewRegistry(), nil, cfg, log)

	const secretPlaintext = "sk-lkeap-super-secret"
	ciphertext, nonce, err := svc.EncryptCredential([]byte("sk-primary"))
	if err != nil {
		t.Fatalf("encrypt primary credential: %v", err)
	}

	// LKEAP declares secret_key as a secret extra field; api_version is stored
	// as a plain non-secret field for the round-trip assertion.
	nonSecret, secret, err := svc.mergeExtraConfig(ProviderLKEAP, nil, map[string]string{
		"api_version": "2024-10-21",
		"secret_key":  secretPlaintext,
	})
	if err != nil {
		t.Fatalf("mergeExtraConfig() error = %v", err)
	}
	if _, leaked := nonSecret["secret_key"]; leaked {
		t.Fatal("secret field leaked into the non-secret extra map")
	}
	if secret["secret_key"] != secretPlaintext {
		t.Fatalf("secret map secret_key = %q, want %q", secret["secret_key"], secretPlaintext)
	}
	secretCT, secretNonce, err := svc.encryptExtraSecrets(secret)
	if err != nil {
		t.Fatalf("encryptExtraSecrets() error = %v", err)
	}

	row := &ProjectProviderConfig{
		ProjectID:            projectID,
		Provider:             ProviderLKEAP,
		Slug:                 ProviderSlug(ProviderLKEAP),
		EncryptedCredential:  ciphertext,
		EncryptionNonce:      nonce,
		GenerativeModel:      "deepseek-v3",
		ExtraConfig:          nonSecret,
		ExtraSecretEncrypted: secretCT,
		ExtraSecretNonce:     secretNonce,
	}
	if err := repo.UpsertProjectProviderConfig(ctx, row); err != nil {
		t.Fatalf("UpsertProjectProviderConfig() error = %v", err)
	}

	stored, err := repo.GetProjectProviderConfigBySlug(ctx, projectID, ProviderSlug(ProviderLKEAP))
	if err != nil {
		t.Fatalf("GetProjectProviderConfigBySlug() error = %v", err)
	}
	if stored == nil {
		t.Fatal("stored config = nil")
	}

	// Ciphertext-at-rest: the secret blob must be non-empty, must not equal the
	// plaintext, and must not contain it.
	if len(stored.ExtraSecretEncrypted) == 0 {
		t.Fatal("stored extra_secret_encrypted is empty, want ciphertext")
	}
	if bytes.Equal(stored.ExtraSecretEncrypted, []byte(secretPlaintext)) {
		t.Fatal("stored extra_secret_encrypted equals plaintext")
	}
	if bytes.Contains(stored.ExtraSecretEncrypted, []byte(secretPlaintext)) {
		t.Fatal("stored extra_secret_encrypted contains the plaintext secret")
	}

	// The non-secret jsonb map must carry api_version and must never carry the
	// secret, both in the scanned entity and in the raw column text.
	if stored.ExtraConfig["api_version"] != "2024-10-21" {
		t.Fatalf("stored ExtraConfig[api_version] = %q, want 2024-10-21", stored.ExtraConfig["api_version"])
	}
	if _, leaked := stored.ExtraConfig["secret_key"]; leaked {
		t.Fatal("secret_key present in the plaintext jsonb extra_config")
	}
	var rawExtra string
	if err := db.QueryRowContext(ctx,
		`SELECT extra_config::text FROM kb.project_provider_configs WHERE id = ?`, stored.ID).Scan(&rawExtra); err != nil {
		t.Fatalf("select raw extra_config: %v", err)
	}
	if strings.Contains(rawExtra, secretPlaintext) || strings.Contains(rawExtra, "secret_key") {
		t.Fatalf("raw jsonb extra_config contains the secret: %s", rawExtra)
	}

	// Plaintext-on-read: the resolver decrypts the secret back.
	resolved, err := svc.decryptProjectConfig(stored)
	if err != nil {
		t.Fatalf("decryptProjectConfig() error = %v", err)
	}
	if resolved.Extra["secret_key"] != secretPlaintext {
		t.Fatalf("resolved Extra[secret_key] = %q, want %q", resolved.Extra["secret_key"], secretPlaintext)
	}
	if resolved.Extra["api_version"] != "2024-10-21" {
		t.Fatalf("resolved Extra[api_version] = %q, want 2024-10-21", resolved.Extra["api_version"])
	}

	// Omit-on-update: a nil submitted map preserves every stored extra.
	nonSecret2, secret2, err := svc.mergeExtraConfig(ProviderLKEAP, stored, nil)
	if err != nil {
		t.Fatalf("mergeExtraConfig(nil) error = %v", err)
	}
	if nonSecret2["api_version"] != "2024-10-21" || secret2["secret_key"] != secretPlaintext {
		t.Fatalf("nil merge did not preserve extras: non-secret=%v secret=%v", nonSecret2, secret2)
	}

	// Per-key merge: a blank secret preserves the stored secret, while a blank
	// non-secret clears it.
	nonSecret3, secret3, err := svc.mergeExtraConfig(ProviderLKEAP, stored, map[string]string{
		"secret_key":  "",
		"api_version": "",
	})
	if err != nil {
		t.Fatalf("mergeExtraConfig(blank) error = %v", err)
	}
	if secret3["secret_key"] != secretPlaintext {
		t.Fatalf("blank secret cleared the stored secret: %q", secret3["secret_key"])
	}
	if _, ok := nonSecret3["api_version"]; ok {
		t.Fatalf("blank non-secret did not clear api_version: %v", nonSecret3)
	}
}

// TestBuildTempResolvedCredWithExtra_CarriesMergedExtras pins the upsert-path
// fix: the temp credential used for catalog sync and connection tests must be
// built from the merged stored+submitted extras, so an update that omits
// extraConfig still validates against the stored api_version/secret header.
func TestBuildTempResolvedCredWithExtra_CarriesMergedExtras(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	svc := NewCredentialService(nil, NewRegistry(), nil, &config.Config{}, log)

	merged := map[string]string{"api_version": "2024-10-21", "secret_key": "sk-stored"}
	cred := svc.buildTempResolvedCredWithExtra(ProviderLKEAP, UpsertProviderConfigRequest{APIKey: "sk-primary"}, merged)
	if cred.Extra["api_version"] != "2024-10-21" {
		t.Errorf("temp cred dropped stored api_version: %v", cred.Extra)
	}
	if cred.ExtraHeaders["X-Secret-Key"] != "sk-stored" {
		t.Errorf("temp cred dropped stored secret header: %v", cred.ExtraHeaders)
	}
}

// TestApplyExtraConfig_DecryptFailureSurfaces pins that a corrupted extra-secret
// blob is returned as an error rather than silently resolving without it.
func TestApplyExtraConfig_DecryptFailureSurfaces(t *testing.T) {
	hexKey, err := crypto.GenerateKey()
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	cfg := &config.Config{LLMProvider: config.LLMProviderConfig{EncryptionKey: hexKey}}
	svc := NewCredentialService(nil, NewRegistry(), nil, cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))

	tampered := &ProjectProviderConfig{
		Provider:             ProviderLKEAP,
		ExtraSecretEncrypted: []byte("not-a-valid-ciphertext"),
		ExtraSecretNonce:     []byte("0123456789ab"),
	}
	if err := svc.applyExtraConfig(&ResolvedCredential{}, tampered); err == nil {
		t.Fatal("applyExtraConfig() = nil error for a corrupt secret blob")
	}
}

// TestApplyDefinition_PopulatesExtra pins that applyDefinition materialises
// vendor extra fields (declared defaults), derives HTTP headers, and rewrites
// the Volcengine Ark endpoint for the selected region.
func TestApplyDefinition_PopulatesExtra(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	svc := NewCredentialService(nil, NewRegistry(), nil, &config.Config{}, log)

	// Volcengine default region: extra populated from the declared default and
	// the default base URL kept.
	def := &ResolvedCredential{Provider: ProviderVolcengine}
	svc.applyDefinition(def)
	if def.Extra["region"] != "cn-beijing" {
		t.Errorf("volcengine default Extra[region] = %q, want cn-beijing", def.Extra["region"])
	}
	if def.BaseURL != "https://ark.cn-beijing.volces.com/api/v3" {
		t.Errorf("volcengine default BaseURL = %q, want https://ark.cn-beijing.volces.com/api/v3", def.BaseURL)
	}

	// Volcengine region substitution (the rerank region requirement).
	shanghai := &ResolvedCredential{Provider: ProviderVolcengine, Extra: map[string]string{"region": "cn-shanghai"}}
	svc.applyDefinition(shanghai)
	if shanghai.BaseURL != "https://ark.cn-shanghai.volces.com/api/v3" {
		t.Errorf("volcengine shanghai BaseURL = %q, want https://ark.cn-shanghai.volces.com/api/v3", shanghai.BaseURL)
	}

	// LKEAP secret_key is declared with a header; applyDefinition derives it.
	lkeap := &ResolvedCredential{Provider: ProviderLKEAP, Extra: map[string]string{"secret_key": "sk-lkeap"}}
	svc.applyDefinition(lkeap)
	if lkeap.ExtraHeaders["X-Secret-Key"] != "sk-lkeap" {
		t.Errorf("lkeap ExtraHeaders[X-Secret-Key] = %q, want sk-lkeap", lkeap.ExtraHeaders["X-Secret-Key"])
	}
	// A vendor without a declared header must not invent one.
	if len(def.ExtraHeaders) != 0 {
		t.Errorf("volcengine ExtraHeaders = %v, want empty", def.ExtraHeaders)
	}
}

// TestApplyExtraBaseURL_VolcengineRegion is a focused table test for the region
// host rewrite.
func TestApplyExtraBaseURL_VolcengineRegion(t *testing.T) {
	def := definitionFor(ProviderVolcengine)
	tests := []struct {
		name    string
		baseURL string
		region  string
		want    string
	}{
		{"default", "https://ark.cn-beijing.volces.com/api/v3", "", "https://ark.cn-beijing.volces.com/api/v3"},
		{"shanghai", "https://ark.cn-beijing.volces.com/api/v3", "cn-shanghai", "https://ark.cn-shanghai.volces.com/api/v3"},
		{"embedding path", "https://ark.cn-beijing.volces.com/api/v3/embeddings/multimodal", "cn-shanghai", "https://ark.cn-shanghai.volces.com/api/v3/embeddings/multimodal"},
		{"custom host untouched", "https://my-proxy.example.com/v1", "cn-shanghai", "https://my-proxy.example.com/v1"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := applyExtraBaseURL(def, tc.baseURL, map[string]string{"region": tc.region})
			if got != tc.want {
				t.Errorf("applyExtraBaseURL(%q, region=%q) = %q, want %q", tc.baseURL, tc.region, got, tc.want)
			}
		})
	}
}
