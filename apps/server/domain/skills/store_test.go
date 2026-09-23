package skills

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"log/slog"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"

	"github.com/emergent-company/emergent.memory/internal/testdb"
)

// connectTestDB opens a throwaway test database owned by this test, so each
// test runs against a uniquely-named database it drops on cleanup. Skips when
// unavailable or in short mode; fails when REQUIRE_DB is set.
func connectTestDB(t *testing.T) *bun.DB {
	t.Helper()
	if testing.Short() {
		testdb.SkipOrFatal(t, "Skipping database integration test in short mode")
	}
	tdb := testdb.SetupTestDBOrFail(t, context.Background(), "skills")
	t.Cleanup(tdb.Close)
	return tdb.DB
}

// fakeEmbedder implements Embedder for tests.
type fakeEmbedder struct {
	vec []float32
	err error
}

func (f *fakeEmbedder) EmbedQuery(ctx context.Context, query string) ([]float32, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.vec, nil
}

// testVector returns a 768-dim vector matching the description_embedding column.
func testVector() []float32 {
	v := make([]float32, 768)
	for i := range v {
		v[i] = float32(i%7) / 10
	}
	return v
}

func testSkill(name string, projectID *string) *Skill {
	return &Skill{
		Name:        name,
		Description: "description of " + name,
		Content:     "content of " + name,
		ProjectID:   projectID,
	}
}

// seedProject creates a real org + project row (kb.skills has FK constraints on
// project_id and org_id) and returns their IDs.
func seedProject(t *testing.T, db bun.IDB) (orgID, projectID string) {
	t.Helper()
	ctx := context.Background()
	orgID = uuid.NewString()
	projectID = uuid.NewString()
	_, err := db.ExecContext(ctx, `INSERT INTO kb.orgs (id, name) VALUES (?, ?)`, orgID, "test-org-"+orgID)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `INSERT INTO kb.projects (id, organization_id, name) VALUES (?, ?, ?)`,
		projectID, orgID, "test-project-"+projectID)
	require.NoError(t, err)
	return orgID, projectID
}

func TestComputeContentHash(t *testing.T) {
	sum := sha256.Sum256([]byte("hello"))
	assert.Equal(t, hex.EncodeToString(sum[:]), computeContentHash("hello"))
}

func TestRepository_Create_RecordsProvenanceAndHash(t *testing.T) {
	db := connectTestDB(t)
	repo := NewRepository(db, slog.Default())
	ctx := context.Background()

	_, pid := seedProject(t, db)
	name := "provenance-" + uuid.NewString()
	content := "# How to review code\n\nSteps..."
	meta := &SkillMetadata{
		Location:    "/opt/skills/SKILL.md",
		Source:      SourceCLI,
		License:     "MIT",
		Version:     "1.2.3",
		SourceURL:   "https://example.com/skills/code-review",
		OriginID:    "origin-42",
		ContentHash: "caller-supplied-bogus-hash",
	}
	s := testSkill(name, &pid)
	s.Content = content
	s.Metadata = meta

	require.NoError(t, repo.Create(ctx, s))

	got, err := repo.FindByID(ctx, s.ID)
	require.NoError(t, err)
	require.NotNil(t, got.Metadata)

	// content_hash is always server-computed; caller-provided value never trusted.
	wantHash := computeContentHash(content)
	assert.Equal(t, wantHash, got.Metadata.ContentHash, "content_hash must be server-computed sha256 of content")

	// All other provenance fields preserved verbatim.
	assert.Equal(t, meta.Location, got.Metadata.Location)
	assert.Equal(t, meta.Source, got.Metadata.Source)
	assert.Equal(t, meta.License, got.Metadata.License)
	assert.Equal(t, meta.Version, got.Metadata.Version)
	assert.Equal(t, meta.SourceURL, got.Metadata.SourceURL)
	assert.Equal(t, meta.OriginID, got.Metadata.OriginID)
}

func TestRepository_Create_EmbeddingOnEveryWritePath(t *testing.T) {
	db := connectTestDB(t)
	ctx := context.Background()

	t.Run("embedding generated when embedder succeeds", func(t *testing.T) {
		repo := NewRepository(db, slog.Default(), WithEmbedder(&fakeEmbedder{vec: testVector()}))
		_, pid := seedProject(t, db)
		s := testSkill("embed-ok-"+uuid.NewString(), &pid)
		require.NoError(t, repo.Create(ctx, s))

		got, err := repo.FindByID(ctx, s.ID)
		require.NoError(t, err)
		assert.NotEmpty(t, got.DescriptionEmbedding, "description_embedding must be non-null when embedding succeeds")
	})

	t.Run("embedding failure is non-fatal, skill still created with null embedding", func(t *testing.T) {
		repo := NewRepository(db, slog.Default(), WithEmbedder(&fakeEmbedder{err: errors.New("embeddings down")}))
		_, pid := seedProject(t, db)
		s := testSkill("embed-fail-"+uuid.NewString(), &pid)
		require.NoError(t, repo.Create(ctx, s), "create must succeed even when embedding generation fails")

		got, err := repo.FindByID(ctx, s.ID)
		require.NoError(t, err)
		assert.Empty(t, got.DescriptionEmbedding, "description_embedding must be null when embedding fails")
	})

	t.Run("no embedder configured leaves null embedding", func(t *testing.T) {
		repo := NewRepository(db, slog.Default())
		_, pid := seedProject(t, db)
		s := testSkill("embed-none-"+uuid.NewString(), &pid)
		require.NoError(t, repo.Create(ctx, s))

		got, err := repo.FindByID(ctx, s.ID)
		require.NoError(t, err)
		assert.Empty(t, got.DescriptionEmbedding)
	})
}

func TestRepository_Update_RecomputesHashAndPreservesProvenance(t *testing.T) {
	db := connectTestDB(t)
	repo := NewRepository(db, slog.Default(), WithEmbedder(&fakeEmbedder{vec: testVector()}))
	ctx := context.Background()

	_, pid := seedProject(t, db)
	meta := &SkillMetadata{
		Source:    SourceManual,
		License:   "Apache-2.0",
		Version:   "0.9.0",
		SourceURL: "https://example.com/skill",
		OriginID:  "origin-7",
	}
	s := testSkill("update-"+uuid.NewString(), &pid)
	s.Metadata = meta
	require.NoError(t, repo.Create(ctx, s))

	t.Run("content change recomputes hash, preserves other provenance", func(t *testing.T) {
		newContent := "completely different content v2"
		content := newContent
		updated, err := repo.Update(ctx, s.ID, &UpdateSkillDTO{Content: &content})
		require.NoError(t, err)

		assert.Equal(t, newContent, updated.Content)
		require.NotNil(t, updated.Metadata)
		assert.Equal(t, computeContentHash(newContent), updated.Metadata.ContentHash, "hash must be recomputed on content change")
		assert.Equal(t, meta.Source, updated.Metadata.Source)
		assert.Equal(t, meta.License, updated.Metadata.License)
		assert.Equal(t, meta.Version, updated.Metadata.Version)
		assert.Equal(t, meta.SourceURL, updated.Metadata.SourceURL)
		assert.Equal(t, meta.OriginID, updated.Metadata.OriginID)
	})

	t.Run("non-content update preserves provenance and hash", func(t *testing.T) {
		before, err := repo.FindByID(ctx, s.ID)
		require.NoError(t, err)

		desc := "brand new description"
		updated, err := repo.Update(ctx, s.ID, &UpdateSkillDTO{Description: &desc})
		require.NoError(t, err)

		require.NotNil(t, updated.Metadata)
		assert.Equal(t, before.Metadata.ContentHash, updated.Metadata.ContentHash, "hash must not change when content is unchanged")
		assert.Equal(t, before.Metadata.Source, updated.Metadata.Source)
		assert.Equal(t, before.Metadata.License, updated.Metadata.License)
		assert.Equal(t, before.Metadata.Version, updated.Metadata.Version)
		assert.Equal(t, before.Metadata.SourceURL, updated.Metadata.SourceURL)
		assert.Equal(t, before.Metadata.OriginID, updated.Metadata.OriginID)
		// Description change regenerates the embedding (reload from DB to avoid
		// the in-memory struct carrying the pre-update vector).
		reloaded, err := repo.FindByID(ctx, s.ID)
		require.NoError(t, err)
		assert.NotEmpty(t, reloaded.DescriptionEmbedding)
	})

	t.Run("metadata replace keeps server-computed hash", func(t *testing.T) {
		newMeta := &SkillMetadata{
			Source:      SourceMarketplace,
			License:     "MIT",
			ContentHash: "bogus-hash-from-caller",
		}
		updated, err := repo.Update(ctx, s.ID, &UpdateSkillDTO{Metadata: newMeta})
		require.NoError(t, err)

		require.NotNil(t, updated.Metadata)
		// Hash recomputed from existing (unchanged) content; other fields verbatim.
		assert.Equal(t, computeContentHash(updated.Content), updated.Metadata.ContentHash)
		assert.Equal(t, SourceMarketplace, updated.Metadata.Source)
		assert.Equal(t, "MIT", updated.Metadata.License)
		assert.Equal(t, "", updated.Metadata.Version, "unset fields must be empty, not inherited")
	})

	t.Run("description change with failing embedder clears stale embedding", func(t *testing.T) {
		failing := NewRepository(db, slog.Default(), WithEmbedder(&fakeEmbedder{err: errors.New("down")}))
		desc := "description after embedder failure"
		updated, err := failing.Update(ctx, s.ID, &UpdateSkillDTO{Description: &desc})
		require.NoError(t, err)
		assert.Equal(t, desc, updated.Description)
		// The stale embedding must be NULL in the DB (the returned struct still
		// carries the pre-update vector it was loaded with).
		reloaded, err := failing.FindByID(ctx, s.ID)
		require.NoError(t, err)
		assert.Empty(t, reloaded.DescriptionEmbedding, "stale embedding must be cleared when regeneration fails")
	})
}
