package provider

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/emergent-company/emergent.memory/internal/config"
	"github.com/emergent-company/emergent.memory/internal/testdb"
	"github.com/emergent-company/emergent.memory/pkg/auth"
	"github.com/emergent-company/emergent.memory/pkg/crypto"
)

// TestResolve_SlugFirstOverDialectAlias pins #709: Resolve/ResolveFor must be
// slug-first like ResolveByRef. A project instance whose slug is literally an
// alias name ("gemini") is otherwise unreachable — the alias was rewritten to
// its dialect before any project lookup, so resolution returned the google
// dialect's default instance (or told the operator to configure "google") while
// the instance they actually created was never consulted.
func TestResolve_SlugFirstOverDialectAlias(t *testing.T) {
	if testing.Short() {
		testdb.SkipOrFatal(t, "skipping database integration test in short mode")
	}
	ctx := context.Background()
	tdb := testdb.SetupTestDBOrFail(t, ctx, "provslugfirst")
	t.Cleanup(tdb.Close)
	db := tdb.DB
	log := slog.New(slog.NewTextHandler(io.Discard, nil))

	hexKey, err := crypto.GenerateKey()
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	cfg := &config.Config{LLMProvider: config.LLMProviderConfig{EncryptionKey: hexKey}}
	repo := NewRepository(db, log)
	svc := NewCredentialService(repo, NewRegistry(), nil, cfg, log)

	orgID := uuid.NewString()
	if _, err := db.ExecContext(ctx, `INSERT INTO kb.orgs (id, name) VALUES (?, ?)`, orgID, "org-"+orgID); err != nil {
		t.Fatalf("seed org: %v", err)
	}
	seedProject := func(name string) string {
		t.Helper()
		projectID := uuid.NewString()
		if _, err := db.ExecContext(ctx,
			`INSERT INTO kb.projects (id, organization_id, name) VALUES (?, ?, ?)`,
			projectID, orgID, name+"-"+projectID); err != nil {
			t.Fatalf("seed project: %v", err)
		}
		return projectID
	}
	seedInstance := func(projectID string, dialect ProviderType, slug, secret string) {
		t.Helper()
		ciphertext, nonce, err := svc.EncryptCredential([]byte(secret))
		if err != nil {
			t.Fatalf("encrypt credential for %q: %v", slug, err)
		}
		if err := repo.UpsertProjectProviderConfig(ctx, &ProjectProviderConfig{
			ProjectID:           projectID,
			Provider:            dialect,
			Slug:                ProviderSlug(slug),
			EncryptedCredential: ciphertext,
			EncryptionNonce:     nonce,
		}); err != nil {
			t.Fatalf("seed provider instance %q: %v", slug, err)
		}
	}

	// Project with an instance slugged "gemini" (an OpenAI-protocol proxy)
	// alongside the "google" dialect instance.
	projectID := seedProject("both")
	seedInstance(projectID, ProviderOpenAI, "gemini", "sk-gemini-proxy")
	seedInstance(projectID, ProviderGoogleAI, "google", "sk-google")

	// Project with only a google instance: the alias fallback must still work.
	aliasOnlyProjectID := seedProject("alias-only")
	seedInstance(aliasOnlyProjectID, ProviderGoogleAI, "google", "sk-google-alias")

	rctx := auth.ContextWithProjectID(ctx, projectID)

	cred, err := svc.Resolve(rctx, "gemini")
	if err != nil {
		t.Fatalf("Resolve(gemini) error = %v", err)
	}
	if cred.Slug != "gemini" {
		t.Errorf("Resolve(gemini) slug = %q, want the instance slug gemini", cred.Slug)
	}
	if cred.Provider != ProviderOpenAI {
		t.Errorf("Resolve(gemini) dialect = %q, want %q", cred.Provider, ProviderOpenAI)
	}
	if cred.APIKey != "sk-gemini-proxy" {
		t.Errorf("Resolve(gemini) APIKey = %q, want the gemini instance's key", cred.APIKey)
	}

	// ResolveFor is the string surface and delegates to Resolve.
	cred, err = svc.ResolveFor(rctx, "gemini")
	if err != nil {
		t.Fatalf("ResolveFor(gemini) error = %v", err)
	}
	if cred.Slug != "gemini" || cred.APIKey != "sk-gemini-proxy" {
		t.Errorf("ResolveFor(gemini) = (slug %q, key %q), want the gemini instance", cred.Slug, cred.APIKey)
	}

	// A dialect name still resolves its own instance.
	cred, err = svc.Resolve(rctx, "google")
	if err != nil {
		t.Fatalf("Resolve(google) error = %v", err)
	}
	if cred.Slug != "google" || cred.APIKey != "sk-google" {
		t.Errorf("Resolve(google) = (slug %q, key %q), want the google instance", cred.Slug, cred.APIKey)
	}

	// The alias fallback is preserved where no instance claims the alias.
	actx := auth.ContextWithProjectID(ctx, aliasOnlyProjectID)
	cred, err = svc.Resolve(actx, "gemini")
	if err != nil {
		t.Fatalf("Resolve(gemini) with no gemini instance error = %v", err)
	}
	if cred.Provider != ProviderGoogleAI || cred.APIKey != "sk-google-alias" {
		t.Errorf("alias fallback = (dialect %q, key %q), want the google instance", cred.Provider, cred.APIKey)
	}

	// Unknown names still fail as unsupported providers rather than being
	// reported as a missing instance.
	if _, err := svc.Resolve(rctx, "gogle"); err == nil || !strings.Contains(err.Error(), "unsupported provider") {
		t.Errorf("Resolve(gogle) error = %v, want an unsupported-provider error", err)
	}
}
