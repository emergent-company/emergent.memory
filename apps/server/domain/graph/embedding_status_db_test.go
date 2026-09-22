package graph_test

import (
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"

	"github.com/emergent-company/emergent.memory/domain/graph"
	"github.com/emergent-company/emergent.memory/internal/config"
	"github.com/emergent-company/emergent.memory/internal/testutil"
)

func setupEmbeddingStatusTest(t *testing.T) (context.Context, bun.IDB, string, *config.Config) {
	t.Helper()
	if testing.Short() {
		t.Skip("skipping database integration test in short mode")
	}
	ctx := context.Background()
	testDB := testutil.SetupTestDBOrFail(t, ctx, "embedstatus")
	t.Cleanup(testDB.Close)

	db := testDB.GetDB()
	orgID := uuid.NewString()
	if err := testutil.CreateTestOrganization(ctx, db, orgID, "Embedding Status Org"); err != nil {
		t.Fatalf("create org: %v", err)
	}
	projectID := uuid.NewString()
	if err := testutil.CreateTestProject(ctx, db, testutil.TestProject{
		ID:    projectID,
		OrgID: orgID,
		Name:  "Embedding Status Project",
	}, testutil.AdminUser.ID); err != nil {
		t.Fatalf("create project: %v", err)
	}
	cfg := *testDB.Config
	return ctx, db, projectID, &cfg
}

// vec768 returns a 768-dimension pgvector literal.
func vec768() string {
	parts := make([]string, 768)
	for i := range parts {
		parts[i] = "0.0"
	}
	return "[" + strings.Join(parts, ",") + "]"
}

// insertEmbeddingStatusObject inserts a HEAD graph object, optionally with an
// embedding_v2 vector and an embedding_updated_at timestamp.
func insertEmbeddingStatusObject(t *testing.T, ctx context.Context, db bun.IDB, projectID, typ string, vector *string, embeddingUpdatedAt *time.Time) uuid.UUID {
	t.Helper()
	id := uuid.New()
	_, err := db.NewRaw(`
		INSERT INTO kb.graph_objects
			(id, project_id, branch_id, canonical_id, supersedes_id, version, type, status,
			 properties, labels, created_at, updated_at, embedding_v2, embedding_updated_at)
		VALUES (?, uuid(?), NULL, ?, NULL, 1, ?, 'active', '{}'::jsonb, '{}'::text[], now(), now(), ?::vector, ?)
	`, id.String(), projectID, id.String(), typ, vector, embeddingUpdatedAt).Exec(ctx)
	if err != nil {
		t.Fatalf("insert object %s: %v", typ, err)
	}
	return id
}

// insertEmbeddingJob inserts a graph embedding job row for an object.
func insertEmbeddingJob(t *testing.T, ctx context.Context, db bun.IDB, objectID uuid.UUID, status string, createdAt time.Time) {
	t.Helper()
	_, err := db.NewRaw(`
		INSERT INTO kb.graph_embedding_jobs (object_id, status, created_at)
		VALUES (?, ?, ?)
	`, objectID.String(), status, createdAt).Exec(ctx)
	if err != nil {
		t.Fatalf("insert job %s: %v", status, err)
	}
}

// TestEmbeddingStatus_Repository verifies the computed embedding_status across
// List and GetByID for every derivation rule.
func TestEmbeddingStatus_Repository(t *testing.T) {
	ctx, db, projectID, cfg := setupEmbeddingStatusTest(t)
	repo := graph.NewRepository(db, slog.Default(), cfg)
	pid := uuid.MustParse(projectID)

	base := time.Now().UTC().Truncate(time.Millisecond)

	vec := vec768()

	// Embedded: vector present + a failed job row -> still embedded.
	embeddedAt := base.Add(-time.Hour)
	embeddedID := insertEmbeddingStatusObject(t, ctx, db, projectID, "Embedded", &vec, &embeddedAt)
	insertEmbeddingJob(t, ctx, db, embeddedID, "failed", base.Add(-2*time.Hour))

	// Pending / processing / failed / dead_letter (no vector).
	pendingID := insertEmbeddingStatusObject(t, ctx, db, projectID, "Pending", nil, nil)
	insertEmbeddingJob(t, ctx, db, pendingID, "pending", base)

	processingID := insertEmbeddingStatusObject(t, ctx, db, projectID, "Processing", nil, nil)
	insertEmbeddingJob(t, ctx, db, processingID, "processing", base)

	failedID := insertEmbeddingStatusObject(t, ctx, db, projectID, "Failed", nil, nil)
	insertEmbeddingJob(t, ctx, db, failedID, "failed", base)

	deadLetterID := insertEmbeddingStatusObject(t, ctx, db, projectID, "DeadLetter", nil, nil)
	insertEmbeddingJob(t, ctx, db, deadLetterID, "dead_letter", base)

	// Completed/cancelled with no vector -> missing.
	completedID := insertEmbeddingStatusObject(t, ctx, db, projectID, "Completed", nil, nil)
	insertEmbeddingJob(t, ctx, db, completedID, "completed", base)

	cancelledID := insertEmbeddingStatusObject(t, ctx, db, projectID, "Cancelled", nil, nil)
	insertEmbeddingJob(t, ctx, db, cancelledID, "cancelled", base)

	// No vector, no job -> missing.
	missingID := insertEmbeddingStatusObject(t, ctx, db, projectID, "Missing", nil, nil)

	// Retried: older failed + newer processing -> processing.
	retriedID := insertEmbeddingStatusObject(t, ctx, db, projectID, "Retried", nil, nil)
	insertEmbeddingJob(t, ctx, db, retriedID, "failed", base.Add(-time.Hour))
	insertEmbeddingJob(t, ctx, db, retriedID, "processing", base)

	want := map[uuid.UUID]string{
		embeddedID:   "embedded",
		pendingID:    "pending",
		processingID: "processing",
		failedID:     "failed",
		deadLetterID: "dead_letter",
		completedID:  "missing",
		cancelledID:  "missing",
		missingID:    "missing",
		retriedID:    "processing",
	}

	// --- List path ---
	objs, err := repo.List(ctx, graph.ListParams{ProjectID: pid, Limit: 100})
	require.NoError(t, err)

	byID := make(map[uuid.UUID]*graph.GraphObject, len(objs))
	for _, o := range objs {
		byID[o.ID] = o
	}

	for id, status := range want {
		obj, ok := byID[id]
		require.Truef(t, ok, "List must return object %s", id)
		assert.Equalf(t, status, obj.EmbeddingStatus, "List embedding_status for %s", id)
	}

	// embedding_updated_at is populated only for the embedded object.
	assert.NotNil(t, byID[embeddedID].EmbeddingUpdatedAt, "embedded object must carry embedding_updated_at")
	// Compare instants, not time.Time struct equality: the driver may hand back
	// the timestamp in a different location than the UTC value inserted here,
	// and reflect.DeepEqual (used by assert.Equal) would then fail even though
	// the instant is identical.
	assert.True(t, embeddedAt.Equal(*byID[embeddedID].EmbeddingUpdatedAt),
		"embedded object embedding_updated_at = %v, want %v", byID[embeddedID].EmbeddingUpdatedAt, embeddedAt)
	assert.Nil(t, byID[missingID].EmbeddingUpdatedAt, "non-embedded object must not carry embedding_updated_at")

	// --- GetByID path ---
	for id, status := range want {
		obj, err := repo.GetByID(ctx, pid, id)
		require.NoErrorf(t, err, "GetByID %s", id)
		assert.Equalf(t, id, obj.ID, "GetByID must return the object's real id (not a zero value)")
		assert.Equalf(t, status, obj.EmbeddingStatus, "GetByID embedding_status for %s", id)
	}

	// --- GetHeadByCanonicalID path ---
	// GetHeadByCanonicalID backs PATCH, merge, restore, and other transactional
	// flows, so it must return populated IDs and the computed EmbeddingStatus too
	// — a select-list regression here would pass the GetByID assertions above
	// while breaking those callers.
	for id, status := range want {
		obj, err := repo.GetHeadByCanonicalID(ctx, db, pid, id, nil)
		require.NoErrorf(t, err, "GetHeadByCanonicalID %s", id)
		assert.Equalf(t, id, obj.ID, "GetHeadByCanonicalID must return the object's real id (not a zero value)")
		assert.Equalf(t, id, obj.CanonicalID, "GetHeadByCanonicalID must return the canonical id")
		assert.Equalf(t, status, obj.EmbeddingStatus, "GetHeadByCanonicalID embedding_status for %s", id)
	}
}

// TestGetHeadByCanonicalID_ScansEmbeddingV2 proves GetHeadByCanonicalID returns
// the HEAD object for a canonical id without failing the scan when the row
// carries an embedding_v2 vector. Regression guard for the explicit-column
// projection: `go.*` selected embedding_v2 (a pgvector column with no
// GraphObject field) and Bun aborted with "does not have column embedding_v2".
func TestGetHeadByCanonicalID_ScansEmbeddingV2(t *testing.T) {
	ctx, db, projectID, cfg := setupEmbeddingStatusTest(t)
	repo := graph.NewRepository(db, slog.Default(), cfg)
	pid := uuid.MustParse(projectID)

	at := time.Now().UTC().Truncate(time.Millisecond)
	vec := vec768()
	embeddedID := insertEmbeddingStatusObject(t, ctx, db, projectID, "HeadCanonicalEmbedded", &vec, &at)
	plainID := insertEmbeddingStatusObject(t, ctx, db, projectID, "HeadCanonicalPlain", nil, nil)

	embedded, err := repo.GetHeadByCanonicalID(ctx, db, pid, embeddedID, nil)
	require.NoError(t, err)
	require.NotNil(t, embedded)
	assert.Equal(t, embeddedID, embedded.ID)
	assert.Equal(t, "embedded", embedded.EmbeddingStatus)
	require.NotNil(t, embedded.EmbeddingUpdatedAt)
	// Equal compares the instant, ignoring the timestamptz location the driver
	// attaches on scan (time.Local) versus the UTC value we inserted.
	assert.True(t, embedded.EmbeddingUpdatedAt.Equal(at),
		"embedding_updated_at mismatch: got %v want %v", embedded.EmbeddingUpdatedAt, at)

	plain, err := repo.GetHeadByCanonicalID(ctx, db, pid, plainID, nil)
	require.NoError(t, err)
	require.NotNil(t, plain)
	assert.Equal(t, plainID, plain.ID)
	assert.Equal(t, "missing", plain.EmbeddingStatus)
	assert.Nil(t, plain.EmbeddingUpdatedAt)
}
