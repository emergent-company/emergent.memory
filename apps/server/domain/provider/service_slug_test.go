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
	"github.com/emergent-company/emergent.memory/pkg/modelref"
)

func TestValidateProviderSlug(t *testing.T) {
	cases := []struct {
		name    string
		slug    ProviderSlug
		dialect ProviderDialect
		wantErr bool
	}{
		{name: "simple", slug: "openai", dialect: ProviderOpenAI},
		{name: "with dash", slug: "openai-main", dialect: ProviderOpenAI},
		{name: "starts digit", slug: "1openai", dialect: ProviderOpenAI},
		{name: "own dialect allowed", slug: "openai", dialect: ProviderOpenAI},
		{name: "dialect name allowed for other dialect", slug: "deepseek", dialect: ProviderOpenAI},
		{name: "uppercase", slug: "OpenAI", dialect: ProviderOpenAI, wantErr: true},
		{name: "underscore", slug: "openai_main", dialect: ProviderOpenAI, wantErr: true},
		{name: "leading dash", slug: "-openai", dialect: ProviderOpenAI, wantErr: true},
		{name: "trailing space", slug: "openai ", dialect: ProviderOpenAI, wantErr: true},
		{name: "empty", slug: "", dialect: ProviderOpenAI, wantErr: true},
		{name: "too long", slug: ProviderSlug(strings.Repeat("a", 64)), dialect: ProviderOpenAI, wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateProviderSlug(tc.slug, tc.dialect)
			if tc.wantErr && err == nil {
				t.Fatalf("validateProviderSlug(%q, %q) = nil, want error", tc.slug, tc.dialect)
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("validateProviderSlug(%q, %q) unexpected error: %v", tc.slug, tc.dialect, err)
			}
		})
	}
}

func TestResolveSlug(t *testing.T) {
	cases := []struct {
		name      string
		requested string
		dialect   ProviderDialect
		want      ProviderSlug
		wantErr   bool
	}{
		{name: "empty defaults to dialect", requested: "", dialect: ProviderOpenAI, want: "openai"},
		{name: "whitespace defaults to dialect", requested: "   ", dialect: ProviderDeepSeek, want: "deepseek"},
		{name: "explicit slug", requested: "openai-prod", dialect: ProviderOpenAI, want: "openai-prod"},
		{name: "explicit trimmed", requested: "  azure  ", dialect: ProviderOpenAI, want: "azure"},
		{name: "invalid explicit", requested: "Bad Slug", dialect: ProviderOpenAI, wantErr: true},
		{name: "dialect name explicit", requested: "google", dialect: ProviderOpenAI, want: "google"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := resolveSlug(tc.requested, tc.dialect)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("resolveSlug(%q) = %q, want error", tc.requested, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("resolveSlug(%q) unexpected error: %v", tc.requested, err)
			}
			if got != tc.want {
				t.Fatalf("resolveSlug(%q) = %q, want %q", tc.requested, got, tc.want)
			}
		})
	}
}

func TestResolveByRef_NoContext(t *testing.T) {
	svc := newTestCredentialService(&config.Config{})
	ctx := context.Background()

	if cred, err := svc.ResolveByRef(ctx, modelref.Ref{Provider: "openai", Model: "gpt-4o"}); err != nil || cred != nil {
		t.Fatalf("ResolveByRef with no project context = (%v, %v), want (nil, nil)", cred, err)
	}
	if cred, err := svc.ResolveBySlug(ctx, "openai"); err != nil || cred != nil {
		t.Fatalf("ResolveBySlug with no project context = (%v, %v), want (nil, nil)", cred, err)
	}
}

func TestResolveByRef_EmptyProvider(t *testing.T) {
	svc := newTestCredentialService(&config.Config{})
	if _, err := svc.ResolveByRef(context.Background(), modelref.Ref{Model: "gpt-4o"}); err == nil {
		t.Fatal("ResolveByRef with empty provider should error")
	}
}

// TestResolveByRef_DialectNameSlug_Wins is a regression test for allowing an
// instance slug to reuse another dialect's name: a project that has an
// openai-dialect instance whose slug equals "deepseek" must resolve a
// "deepseek" model reference to that instance (slug-first), never to the
// deepseek dialect default.
func TestResolveByRef_DialectNameSlug_Wins(t *testing.T) {
	if testing.Short() {
		testdb.SkipOrFatal(t, "skipping database integration test in short mode")
	}
	ctx := context.Background()
	tdb := testdb.SetupTestDBOrFail(t, ctx, "provslugdialect")
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

	const secret = "sk-openai-named-deepseek"
	ciphertext, nonce, err := svc.EncryptCredential([]byte(secret))
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	// An openai-dialect instance whose slug equals the "deepseek" dialect name.
	if err := repo.UpsertProjectProviderConfig(ctx, &ProjectProviderConfig{
		ProjectID:           projectID,
		Provider:            ProviderOpenAI,
		Slug:                ProviderSlug("deepseek"),
		EncryptedCredential: ciphertext,
		EncryptionNonce:     nonce,
		GenerativeModel:     "gpt-4o",
	}); err != nil {
		t.Fatalf("upsert config: %v", err)
	}

	rctx := auth.ContextWithProjectID(ctx, projectID)
	cred, err := svc.ResolveByRef(rctx, modelref.Ref{Provider: "deepseek", Model: "deepseek-chat"})
	if err != nil {
		t.Fatalf("ResolveByRef() error = %v", err)
	}
	if cred == nil {
		t.Fatal("ResolveByRef() = nil, want the slug-first instance")
	}
	if cred.Provider != ProviderOpenAI {
		t.Errorf("resolved provider = %q, want %q (slug-first must win over the dialect fallback)", cred.Provider, ProviderOpenAI)
	}
	if cred.Slug != ProviderSlug("deepseek") {
		t.Errorf("resolved slug = %q, want %q", cred.Slug, ProviderSlug("deepseek"))
	}
	if cred.APIKey != secret {
		t.Errorf("resolved APIKey = %q, want %q", cred.APIKey, secret)
	}
}
