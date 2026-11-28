package discoveryjobs

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/domain/pipeline"
	"github.com/emergent-company/emergent.memory/internal/testdb"
)

// TestMaybeAutoApplyIsAdditiveAcrossExtends is the regression test for #1215:
// a later discovery run whose refined set omits an earlier episode's type must
// not deactivate that type. The installed pack has to be the union of the
// currently-active discovered types and this run's accepted types, otherwise
// deactivateOtherDiscoveredAssignments drops a type that still has objects on
// main.
func TestMaybeAutoApplyIsAdditiveAcrossExtends(t *testing.T) {
	ctx := context.Background()
	testDB := testdb.SetupTestDBOrFail(t, ctx, "autoapply_additive")
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
	svc := &Service{repo: repo, log: quietLogger(), policyReader: reader}

	apply := func(typeNames ...string) {
		t.Helper()
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
		svc.maybeAutoApply(ctx, projectUUID, job.ID, packID, refined, nil)
	}

	// First run installs Person + Animal.
	apply("Person", "Animal")

	installed, err := repo.InstalledDiscoveredSchema(ctx, projectUUID)
	require.NoError(t, err)
	require.NotNil(t, installed)
	require.Len(t, installed.ObjectTypeSchemas, 2)
	require.Contains(t, installed.ObjectTypeSchemas, "Animal")

	// Second run discovers Person + FoodDish only. The earlier Animal type must
	// survive the apply even though this run's refined set omits it.
	apply("Person", "FoodDish")

	installed, err = repo.InstalledDiscoveredSchema(ctx, projectUUID)
	require.NoError(t, err)
	require.NotNil(t, installed)
	require.Len(t, installed.ObjectTypeSchemas, 3, "installed pack must be the union of active discovered types + this run's accepted types")
	require.Contains(t, installed.ObjectTypeSchemas, "Person")
	require.Contains(t, installed.ObjectTypeSchemas, "FoodDish")
	require.Contains(t, installed.ObjectTypeSchemas, "Animal", "an earlier episode's type must not be deactivated by a later apply")

	// Exactly one active discovered assignment remains, and it is the one that
	// carries Animal.
	var activeAnimal int
	require.NoError(t, db.NewRaw(`
		SELECT count(*)
		FROM kb.project_schemas ps
		JOIN kb.graph_schemas gs ON gs.id = ps.schema_id
		WHERE ps.project_id = ? AND ps.active = true
		  AND gs.source = 'discovered'
		  AND gs.object_type_schemas ? 'Animal'`,
		projectUUID,
	).Scan(ctx, &activeAnimal))
	require.Equal(t, 1, activeAnimal, "Animal must remain active on exactly one discovered assignment")
}
