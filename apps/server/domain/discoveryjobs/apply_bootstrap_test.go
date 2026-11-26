package discoveryjobs

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/domain/pipeline"
	"github.com/emergent-company/emergent.memory/internal/testdb"
)

type fakePolicyReader struct {
	pol    pipeline.Policy
	bumped int
}

func (f *fakePolicyReader) Policy(ctx context.Context, projectID uuid.UUID) (pipeline.Policy, error) {
	return f.pol, nil
}

func (f *fakePolicyReader) BumpSchemaGeneration(ctx context.Context, projectID uuid.UUID) error {
	f.bumped++
	return nil
}

func quietLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// TestMaybeAutoApplyFirstApplyInstallsOverCap proves the bootstrap fix: on the
// first apply (no installed discovered pack) the new-type-per-run cap must not
// block the install, and declined types must be filtered out of the pack.
func TestMaybeAutoApplyFirstApplyInstallsOverCap(t *testing.T) {
	ctx := context.Background()
	testDB := testdb.SetupTestDBOrFail(t, ctx, "autoapply_first")
	defer testDB.Close()
	db := testDB.DB

	orgID := uuid.NewString()
	projectID := uuid.NewString()
	_, err := db.NewRaw(`INSERT INTO kb.orgs (id, name, created_at, updated_at) VALUES (?, 'T', now(), now())`, orgID).Exec(ctx)
	require.NoError(t, err)
	_, err = db.NewRaw(`INSERT INTO kb.projects (id, name, organization_id, created_at, updated_at) VALUES (?, 'T', ?, now(), now())`, projectID, orgID).Exec(ctx)
	require.NoError(t, err)
	projectUUID := uuid.MustParse(projectID)

	repo := NewRepository(db, quietLogger())
	reader := &fakePolicyReader{pol: pipeline.DefaultPolicy(pipeline.ModeAuto)}

	// Seven types (exceeds the default max_new_types_per_run of 5) plus one
	// relationship-like type that the gate must decline.
	typeNames := []string{"A", "B", "C", "D", "E", "F", "Relationship"}
	objSchemas := JSONMap{}
	for _, n := range typeNames {
		objSchemas[n] = map[string]any{"type": "object", "properties": map[string]any{}}
	}
	packID, _, err := repo.UpsertDiscoveredMemorySchema(ctx, CreateMemorySchemaParams{
		Name:              "Discovered Types - t",
		Version:           "1.0.0",
		Description:       "test",
		Author:            "t",
		ObjectTypeSchemas: objSchemas,
		UIConfigs:         JSONMap{},
		Source:            "discovered",
		PendingReview:     true,
		ProjectID:         &projectUUID,
	})
	require.NoError(t, err)

	job := &DiscoveryJob{ID: uuid.New(), OrganizationID: uuid.MustParse(orgID), ProjectID: projectUUID, Status: StatusCompleted}
	require.NoError(t, repo.Create(ctx, job))

	refined := make([]DiscoveredType, 0, len(typeNames))
	for _, n := range typeNames {
		refined = append(refined, DiscoveredType{TypeName: n, Confidence: 0.9, Frequency: 2})
	}

	svc := &Service{repo: repo, log: quietLogger(), policyReader: reader}
	svc.maybeAutoApply(ctx, projectUUID, job.ID, packID, refined, nil)

	require.Equal(t, 1, reader.bumped, "first apply must bump schema_generation")

	installed, err := repo.InstalledDiscoveredSchema(ctx, projectUUID)
	require.NoError(t, err)
	require.NotNil(t, installed)
	require.Len(t, installed.ObjectTypeSchemas, 6)
	_, hasRel := installed.ObjectTypeSchemas["Relationship"]
	require.False(t, hasRel, "relationship-like type must be filtered")
}

func TestRelationshipUsesOnly(t *testing.T) {
	keep := map[string]bool{"A": true, "B": true}
	require.True(t, relationshipUsesOnly(map[string]any{"sourceType": "A", "targetType": "B"}, keep))
	require.True(t, relationshipUsesOnly(map[string]any{"sourceTypes": []any{"A", "B"}, "targetTypes": []any{"B"}}, keep))
	require.False(t, relationshipUsesOnly(map[string]any{"sourceType": "A", "targetType": "C"}, keep))
	require.False(t, relationshipUsesOnly("nonsense", keep))
}
