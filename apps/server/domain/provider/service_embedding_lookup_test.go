package provider

import (
	"context"
	"database/sql"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/google/uuid"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/pgdialect"

	"github.com/emergent-company/emergent.memory/internal/config"
	"github.com/emergent-company/emergent.memory/internal/testdb"
	"github.com/emergent-company/emergent.memory/pkg/auth"
)

// TestNoProjectProviderConfigErrorReadsOnce pins the user-visible text of a
// missing project provider credential: the ErrNoProjectProviderConfig sentinel
// already says "no provider config found for project", so the detail clause
// must not repeat "for project" (#690 item 2). The sentinel prefix stays intact
// so callers can keep classifying the gap with errors.Is.
func TestNoProjectProviderConfigErrorReadsOnce(t *testing.T) {
	err := noProjectProviderConfigError("google", "1a3d9c", "run 'memory provider configure-project google' to set them")
	if !errors.Is(err, ErrNoProjectProviderConfig) {
		t.Fatalf("error = %v, want it to wrap ErrNoProjectProviderConfig", err)
	}
	const want = "no provider config found for project: google has no credentials for 1a3d9c — run 'memory provider configure-project google' to set them"
	if err.Error() != want {
		t.Errorf("error = %q, want %q", err.Error(), want)
	}
	if got := strings.Count(err.Error(), "for project"); got != 1 {
		t.Errorf("error text repeats \"for project\" %d times, want 1: %q", got, err.Error())
	}
}

// TestResolveAnyEmbeddingPropagatesLookupError pins that a repository failure on
// the "no explicit project model" path surfaces instead of being swallowed and
// reported as "no embedding model configured" (#690 item 4). The repository is
// backed by a closed database handle, so every lookup fails immediately — no
// Postgres is required.
func TestResolveAnyEmbeddingPropagatesLookupError(t *testing.T) {
	sqldb, err := sql.Open("pgx", "postgres://localhost:5432/none")
	if err != nil {
		t.Fatalf("open sql db: %v", err)
	}
	// Closing the handle makes every query fail with "sql: database is closed".
	if err := sqldb.Close(); err != nil {
		t.Fatalf("close sql db: %v", err)
	}
	db := bun.NewDB(sqldb, pgdialect.New())
	t.Cleanup(func() { _ = db.Close() })

	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	svc := NewCredentialService(NewRepository(db, log), NewRegistry(), nil, &config.Config{}, log)

	ctx := auth.ContextWithProjectID(context.Background(), uuid.NewString())
	cred, err := svc.ResolveAnyEmbedding(ctx)
	if err == nil {
		t.Fatal("ResolveAnyEmbedding() error = nil, want the repository error to propagate")
	}
	if cred != nil {
		t.Errorf("ResolveAnyEmbedding() credential = %+v, want nil on error", cred)
	}
	if !strings.Contains(err.Error(), "embedding credential lookup failed") {
		t.Errorf("error = %q, want it to name the embedding credential lookup", err.Error())
	}
	if errors.Is(err, ErrNoProjectProviderConfig) {
		t.Errorf("error = %q, must not be classified as a missing provider config", err.Error())
	}
}

// TestModelProviderKnown_ResolvesDialectAndProjectSlug pins the write-time
// provider validator: a supported dialect is known without any config, an
// arbitrary name is unknown, and a project provider instance slug is known once
// its config row exists — without requiring a usable credential.
func TestModelProviderKnown_ResolvesDialectAndProjectSlug(t *testing.T) {
	if testing.Short() {
		testdb.SkipOrFatal(t, "skipping database integration test in short mode")
	}
	ctx := context.Background()
	tdb := testdb.SetupTestDBOrFail(t, ctx, "provknown")
	t.Cleanup(tdb.Close)

	orgID := uuid.NewString()
	projectID := uuid.NewString()
	if _, err := tdb.DB.ExecContext(ctx, `INSERT INTO kb.orgs (id, name) VALUES (?, ?)`, orgID, "org-"+orgID); err != nil {
		t.Fatalf("seed org: %v", err)
	}
	if _, err := tdb.DB.ExecContext(ctx,
		`INSERT INTO kb.projects (id, organization_id, name) VALUES (?, ?, ?)`,
		projectID, orgID, "proj-"+projectID); err != nil {
		t.Fatalf("seed project: %v", err)
	}

	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	repo := NewRepository(tdb.DB, log)
	svc := NewCredentialService(repo, NewRegistry(), nil, &config.Config{}, log)

	if known, err := svc.ModelProviderKnown(ctx, projectID, "google"); err != nil || !known {
		t.Errorf("ModelProviderKnown(google) = (%v, %v), want (true, nil)", known, err)
	}
	if known, err := svc.ModelProviderKnown(ctx, projectID, "typo"); err != nil || known {
		t.Errorf("ModelProviderKnown(typo) = (%v, %v), want (false, nil)", known, err)
	}

	if err := repo.UpsertProjectProviderConfig(ctx, &ProjectProviderConfig{
		ProjectID:           projectID,
		Provider:            ProviderGoogleAI,
		Slug:                "google-eu",
		EncryptedCredential: []byte("ciphertext"),
		EncryptionNonce:     []byte("nonce"),
	}); err != nil {
		t.Fatalf("seed provider instance: %v", err)
	}
	if known, err := svc.ModelProviderKnown(ctx, projectID, "google-eu"); err != nil || !known {
		t.Errorf("ModelProviderKnown(google-eu) = (%v, %v), want (true, nil)", known, err)
	}
}
