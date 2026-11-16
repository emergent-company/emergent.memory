package modelconfig

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/emergent-company/emergent.memory/internal/testdb"
)

// seedModelConfigProject inserts the org+project rows the config FK needs.
func seedModelConfigProject(t *testing.T, ctx context.Context, tdb *testdb.TestDB, name string) uuid.UUID {
	t.Helper()
	orgID := uuid.NewString()
	if _, err := tdb.DB.ExecContext(ctx, `INSERT INTO kb.orgs (id, name) VALUES (?, ?)`, orgID, "org-"+orgID); err != nil {
		t.Fatalf("seed org: %v", err)
	}
	projectID := uuid.New()
	if _, err := tdb.DB.ExecContext(ctx,
		`INSERT INTO kb.projects (id, organization_id, name) VALUES (?, ?, ?)`,
		projectID, orgID, name+"-"+projectID.String()); err != nil {
		t.Fatalf("seed project: %v", err)
	}
	return projectID
}

func loadModelConfig(t *testing.T, ctx context.Context, store *Store, projectID uuid.UUID) *ProjectModelConfig {
	t.Helper()
	cfg, err := store.GetProjectModelConfig(ctx, projectID)
	if err != nil {
		t.Fatalf("GetProjectModelConfig: %v", err)
	}
	if cfg == nil {
		t.Fatal("GetProjectModelConfig = nil, want the row created by the partial upsert")
	}
	return cfg
}

// TestUpsertProjectModelConfigPatchSemantics pins the store-level partial-update
// contract that #708 needs: a field the patch does not name is never written,
// so setting one model leaves the other untouched, and clearing is explicit.
func TestUpsertProjectModelConfigPatchSemantics(t *testing.T) {
	if testing.Short() {
		testdb.SkipOrFatal(t, "skipping database integration test in short mode")
	}
	ctx := context.Background()
	tdb := testdb.SetupTestDBOrFail(t, ctx, "mc_patch")
	t.Cleanup(tdb.Close)
	store := NewStore(tdb.DB, slog.New(slog.NewTextHandler(io.Discard, nil)))
	projectID := seedModelConfigProject(t, ctx, tdb, "patch")

	str := func(s string) *string { return &s }
	genModel, genSlug := "gemini-embedding-001", "google-vertex"
	embModel, embSlug := "gemini-embedding-001", "google"

	// First write creates the row from the patch alone; the store leans on the
	// column defaults for the fields it does not name.
	saved, err := store.UpsertProjectModelConfig(ctx, projectID, ProjectModelConfigPatch{
		GenerativeModel:        &genModel,
		GenerativeProviderSlug: &genSlug,
	})
	if err != nil {
		t.Fatalf("initial upsert: %v", err)
	}
	if saved.EmbeddingModel != "" || saved.EmbeddingProviderSlug != "" {
		t.Errorf("created row embedding = (%q, %q), want both empty", saved.EmbeddingModel, saved.EmbeddingProviderSlug)
	}
	if saved.CreatedAt.IsZero() || saved.UpdatedAt.IsZero() {
		t.Errorf("created row timestamps = (%v, %v), want the column defaults", saved.CreatedAt, saved.UpdatedAt)
	}

	// A generative-only patch preserves the (still empty) embedding columns and
	// resolves the row it wrote.
	if saved, err = store.UpsertProjectModelConfig(ctx, projectID, ProjectModelConfigPatch{
		GenerativeModel: &genModel,
	}); err != nil {
		t.Fatalf("generative-only upsert: %v", err)
	}
	if saved.GenerativeModel != genModel || saved.GenerativeProviderSlug != genSlug {
		t.Errorf("generative = (%q, %q), want (%q, %q) preserved", saved.GenerativeModel, saved.GenerativeProviderSlug, genModel, genSlug)
	}

	// An embedding-only patch preserves the generative pair.
	if saved, err = store.UpsertProjectModelConfig(ctx, projectID, ProjectModelConfigPatch{
		EmbeddingModel:        &embModel,
		EmbeddingProviderSlug: &embSlug,
	}); err != nil {
		t.Fatalf("embedding-only upsert: %v", err)
	}
	if saved.GenerativeModel != genModel || saved.GenerativeProviderSlug != genSlug {
		t.Errorf("generative = (%q, %q), want (%q, %q) preserved", saved.GenerativeModel, saved.GenerativeProviderSlug, genModel, genSlug)
	}
	if saved.EmbeddingModel != embModel || saved.EmbeddingProviderSlug != embSlug {
		t.Errorf("embedding = (%q, %q), want (%q, %q)", saved.EmbeddingModel, saved.EmbeddingProviderSlug, embModel, embSlug)
	}

	// A pointer to "" clears only the named field.
	if saved, err = store.UpsertProjectModelConfig(ctx, projectID, ProjectModelConfigPatch{
		GenerativeModel:        str(""),
		GenerativeProviderSlug: str(""),
	}); err != nil {
		t.Fatalf("clear generative upsert: %v", err)
	}
	if saved.GenerativeModel != "" || saved.GenerativeProviderSlug != "" {
		t.Errorf("generative = (%q, %q), want cleared", saved.GenerativeModel, saved.GenerativeProviderSlug)
	}
	if saved.EmbeddingModel != embModel {
		t.Errorf("embedding = %q, want the stored value preserved", saved.EmbeddingModel)
	}
}

// TestUpsertProjectModelConfigCreatesEmptySlugs pins #730 §1: a row created by a
// partial patch stores the empty string (not NULL) in the model columns it does
// not name. The
// two provider-slug columns added by migration 00189 are nullable with no
// default, so an INSERT that named only project_id left the unnamed pair NULL —
// a second representation of "unset" that migration 00189 also reads as "legacy
// row needing slug resolution". Reading the columns back as NULL-ness rather
// than as Go strings is what makes the assertion sharp: bun decodes NULL into ""
// for a non-pointer string, which is why the existing assertions could not see it.
func TestUpsertProjectModelConfigCreatesEmptySlugs(t *testing.T) {
	if testing.Short() {
		testdb.SkipOrFatal(t, "skipping database integration test in short mode")
	}
	ctx := context.Background()
	tdb := testdb.SetupTestDBOrFail(t, ctx, "mc_empty_slugs")
	t.Cleanup(tdb.Close)
	store := NewStore(tdb.DB, slog.New(slog.NewTextHandler(io.Discard, nil)))
	projectID := seedModelConfigProject(t, ctx, tdb, "empty_slugs")

	genSlug := "google-vertex"
	if _, err := store.UpsertProjectModelConfig(ctx, projectID, ProjectModelConfigPatch{
		GenerativeModel:        strPtr("gemini-2.5-flash"),
		GenerativeProviderSlug: &genSlug,
	}); err != nil {
		t.Fatalf("generative-only upsert: %v", err)
	}

	var row struct {
		EmbeddingModelNull    bool `bun:"embedding_model_null"`
		EmbeddingSlugNull     bool `bun:"embedding_slug_null"`
		GenerativeSlugNull    bool `bun:"generative_slug_null"`
		EmbeddingModelIsEmpty bool `bun:"embedding_model_is_empty"`
		EmbeddingSlugIsEmpty  bool `bun:"embedding_slug_is_empty"`
	}
	if err := tdb.DB.NewRaw(`SELECT
			embedding_model IS NULL               AS embedding_model_null,
			embedding_provider_slug IS NULL       AS embedding_slug_null,
			generative_provider_slug IS NULL      AS generative_slug_null,
			embedding_model = ''                  AS embedding_model_is_empty,
			embedding_provider_slug = ''          AS embedding_slug_is_empty
		FROM kb.project_model_config WHERE project_id = ?`, projectID).Scan(ctx, &row); err != nil {
		t.Fatalf("read raw columns: %v", err)
	}
	if row.EmbeddingModelNull || row.EmbeddingSlugNull || row.GenerativeSlugNull {
		t.Errorf("created row has a NULL model column (embedding_model=%t, embedding_provider_slug=%t, generative_provider_slug=%t), want '' for every unnamed column",
			row.EmbeddingModelNull, row.EmbeddingSlugNull, row.GenerativeSlugNull)
	}
	if !row.EmbeddingModelIsEmpty || !row.EmbeddingSlugIsEmpty {
		t.Errorf("unnamed embedding columns are not '' (embedding_model=%t, embedding_provider_slug=%t)",
			row.EmbeddingModelIsEmpty, row.EmbeddingSlugIsEmpty)
	}
}

// TestUpsertProjectModelConfigEmptyPatchKeepsUpdatedAt pins the documented no-op
// from #730 §2: an empty patch must not move updated_at. The timestamp is pinned
// with a real write rather than a sleep, so the test is deterministic and also
// catches an implementation that always bumps the column.
func TestUpsertProjectModelConfigEmptyPatchKeepsUpdatedAt(t *testing.T) {
	if testing.Short() {
		testdb.SkipOrFatal(t, "skipping database integration test in short mode")
	}
	ctx := context.Background()
	tdb := testdb.SetupTestDBOrFail(t, ctx, "mc_empty_patch")
	t.Cleanup(tdb.Close)
	store := NewStore(tdb.DB, slog.New(slog.NewTextHandler(io.Discard, nil)))
	projectID := seedModelConfigProject(t, ctx, tdb, "empty_patch")

	if _, err := store.UpsertProjectModelConfig(ctx, projectID, ProjectModelConfigPatch{
		GenerativeModel: strPtr("deepseek-v4-flash"),
	}); err != nil {
		t.Fatalf("seed config: %v", err)
	}

	// Pin updated_at in the past so any write at NOW() is unmistakable.
	pinned := time.Now().UTC().Add(-42 * time.Minute).Truncate(time.Microsecond)
	if _, err := tdb.DB.ExecContext(ctx,
		`UPDATE kb.project_model_config SET updated_at = ? WHERE project_id = ?`,
		pinned, projectID); err != nil {
		t.Fatalf("pin updated_at: %v", err)
	}

	saved, err := store.UpsertProjectModelConfig(ctx, projectID, ProjectModelConfigPatch{})
	if err != nil {
		t.Fatalf("empty-patch upsert: %v", err)
	}
	if !saved.UpdatedAt.Equal(pinned) {
		t.Errorf("empty patch moved updated_at: returned %v, want the pinned %v", saved.UpdatedAt, pinned)
	}
	stored := loadModelConfig(t, ctx, store, projectID)
	if !stored.UpdatedAt.Equal(pinned) {
		t.Errorf("empty patch persisted a new updated_at: stored %v, want the pinned %v", stored.UpdatedAt, pinned)
	}
	if stored.GenerativeModel != "deepseek-v4-flash" {
		t.Errorf("empty patch changed generative_model to %q, want it preserved", stored.GenerativeModel)
	}
}

func strPtr(s string) *string { return &s }

// TestUpsertProjectModelConfigConcurrentPartialUpdates is the regression test
// for #708. Two concurrent partial updates — one naming generative, one naming
// embedding — must both survive. The pre-fix implementation read the row and
// wrote every column back, so the later writer resurrected its stale copy of
// the field it did not touch and one model silently disappeared.
//
// Each iteration starts both writers behind one barrier and asserts both latest
// values afterwards, so an interleaving that loses an update fails the test.
func TestUpsertProjectModelConfigConcurrentPartialUpdates(t *testing.T) {
	if testing.Short() {
		testdb.SkipOrFatal(t, "skipping database integration test in short mode")
	}
	ctx := context.Background()
	tdb := testdb.SetupTestDBOrFail(t, ctx, "mc_patch_race")
	t.Cleanup(tdb.Close)
	store := NewStore(tdb.DB, slog.New(slog.NewTextHandler(io.Discard, nil)))
	projectID := seedModelConfigProject(t, ctx, tdb, "race")

	str := func(s string) *string { return &s }
	genSlug, embSlug := "deepseek", "google"
	if _, err := store.UpsertProjectModelConfig(ctx, projectID, ProjectModelConfigPatch{
		GenerativeModel:        str("deepseek-v4-flash"),
		GenerativeProviderSlug: &genSlug,
		EmbeddingModel:         str("gemini-embedding-001"),
		EmbeddingProviderSlug:  &embSlug,
	}); err != nil {
		t.Fatalf("seed config: %v", err)
	}

	const iterations = 50
	for i := 0; i < iterations; i++ {
		gen := fmt.Sprintf("gen-%d", i)
		emb := fmt.Sprintf("emb-%d", i)

		start := make(chan struct{})
		errs := make(chan error, 2)
		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			<-start
			_, err := store.UpsertProjectModelConfig(ctx, projectID, ProjectModelConfigPatch{
				GenerativeModel:        &gen,
				GenerativeProviderSlug: &genSlug,
			})
			errs <- err
		}()
		go func() {
			defer wg.Done()
			<-start
			_, err := store.UpsertProjectModelConfig(ctx, projectID, ProjectModelConfigPatch{
				EmbeddingModel:        &emb,
				EmbeddingProviderSlug: &embSlug,
			})
			errs <- err
		}()
		close(start)
		wg.Wait()
		close(errs)
		for err := range errs {
			if err != nil {
				t.Fatalf("iteration %d: concurrent upsert error: %v", i, err)
			}
		}

		got := loadModelConfig(t, ctx, store, projectID)
		if got.GenerativeModel != gen || got.EmbeddingModel != emb {
			t.Fatalf("iteration %d: stored = (%q, %q), want (%q, %q) — a concurrent partial update was lost",
				i, got.GenerativeModel, got.EmbeddingModel, gen, emb)
		}
	}
}
