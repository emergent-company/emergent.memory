package provider

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/google/uuid"

	"github.com/emergent-company/emergent.memory/internal/config"
	"github.com/emergent-company/emergent.memory/internal/testdb"
	"github.com/emergent-company/emergent.memory/pkg/auth"
	"github.com/emergent-company/emergent.memory/pkg/crypto"
)

// TestResolveAny_NewVendorOnly_Resolves is a regression test for auto-selection
// ignoring every vendor outside the legacy four: a project configured solely
// with a non-legacy registry vendor (Anthropic) must still resolve a default
// credential and generative model.
func TestResolveAny_NewVendorOnly_Resolves(t *testing.T) {
	if testing.Short() {
		testdb.SkipOrFatal(t, "skipping database integration test in short mode")
	}
	ctx := context.Background()
	tdb := testdb.SetupTestDBOrFail(t, ctx, "provresolvenew")
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

	const secret = "sk-anthropic-resolve"
	ciphertext, nonce, err := svc.EncryptCredential([]byte(secret))
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	if err := repo.UpsertProjectProviderConfig(ctx, &ProjectProviderConfig{
		ProjectID:           projectID,
		Provider:            ProviderAnthropic,
		Slug:                ProviderSlug(ProviderAnthropic),
		EncryptedCredential: ciphertext,
		EncryptionNonce:     nonce,
		GenerativeModel:     "claude-sonnet-4-5",
	}); err != nil {
		t.Fatalf("upsert config: %v", err)
	}

	rctx := auth.ContextWithProjectID(ctx, projectID)

	cred, err := svc.ResolveAny(rctx)
	if err != nil {
		t.Fatalf("ResolveAny() error = %v", err)
	}
	if cred == nil {
		t.Fatal("ResolveAny() = nil, want a resolved credential for a non-legacy vendor")
	}
	if cred.Provider != ProviderAnthropic {
		t.Errorf("resolved provider = %q, want %q", cred.Provider, ProviderAnthropic)
	}
	if cred.APIKey != secret {
		t.Errorf("resolved APIKey = %q, want %q", cred.APIKey, secret)
	}

	model, err := svc.DefaultGenerativeModel(rctx, projectID)
	if err != nil {
		t.Fatalf("DefaultGenerativeModel() error = %v", err)
	}
	if model != "anthropic/claude-sonnet-4-5" {
		t.Errorf("DefaultGenerativeModel() = %q, want %q", model, "anthropic/claude-sonnet-4-5")
	}
}
