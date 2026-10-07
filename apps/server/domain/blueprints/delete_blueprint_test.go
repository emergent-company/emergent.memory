package blueprints

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"

	"github.com/emergent-company/emergent.memory/pkg/apperror"
)

// TestService_DeleteBlueprint verifies the relaxed delete rules:
//   - a global (project_id NULL, origin='builtin') blueprint is immutable (409);
//   - an applied project-private blueprint is rejected with 409 until unapplied;
//   - an unapplied project-private blueprint deletes and cleans up its pack
//     claims plus any orphaned project-owned schema packs;
//   - the draft-only restriction is relaxed (a published private blueprint
//     deletes too).
func TestService_DeleteBlueprint(t *testing.T) {
	db := connectTestDB(t)
	svc := newServiceFromBun(t, db)
	ctx := context.Background()

	t.Run("global builtin immutable", func(t *testing.T) {
		bp, err := svc.CreateBlueprint(ctx, &CreateBlueprintRequest{
			Name: uniqueName("del-global"), Version: "1.0.0", Manifest: []byte(`{}`), Origin: OriginBuiltin,
		})
		require.NoError(t, err)
		err = svc.DeleteBlueprint(ctx, uuid.NewString(), bp.ID)
		assertAppErrorCode(t, err, apperror.ErrConflict.Code)
	})

	t.Run("applied private rejected until unapply", func(t *testing.T) {
		proj := uuid.NewString()
		bp, err := svc.CreateBlueprint(ctx, &CreateBlueprintRequest{
			Name: uniqueName("del-applied"), Version: "1.0.0", Manifest: []byte(`{}`), ProjectID: &proj,
		})
		require.NoError(t, err)
		insertApplication(t, db, bp.ID, proj, bp.Version, "applied")

		err = svc.DeleteBlueprint(ctx, proj, bp.ID)
		assertAppErrorCode(t, err, apperror.ErrConflict.Code)
		assert.Contains(t, err.Error(), "uninstall the blueprint before removing it from the registry")
	})

	t.Run("unapplied private deletes and cleans orphan pack + claims", func(t *testing.T) {
		proj := uuid.NewString()
		bp, err := svc.CreateBlueprint(ctx, &CreateBlueprintRequest{
			Name: uniqueName("del-private"), Version: "1.0.0", Manifest: []byte(`{}`), ProjectID: &proj,
		})
		require.NoError(t, err)

		packID := insertSchemaPack(t, db, proj, uniqueName("del-pack"), "1.0.0")
		insertPackClaim(t, db, bp.ID, proj, packID)

		require.NoError(t, svc.DeleteBlueprint(ctx, proj, bp.ID))

		_, err = svc.repo.GetByID(ctx, proj, bp.ID)
		assertAppErrorCode(t, err, apperror.ErrNotFound.Code)
		assertPackClaimCount(t, db, bp.ID, 0)
		assertSchemaPackExists(t, db, packID, false)
	})

	t.Run("published private deletes (draft-only relaxed)", func(t *testing.T) {
		proj := uuid.NewString()
		bp, err := svc.CreateBlueprint(ctx, &CreateBlueprintRequest{
			Name: uniqueName("del-published"), Version: "1.0.0", Manifest: []byte(`{}`), ProjectID: &proj,
		})
		require.NoError(t, err)
		_, err = svc.PublishBlueprint(ctx, proj, bp.ID)
		require.NoError(t, err)

		require.NoError(t, svc.DeleteBlueprint(ctx, proj, bp.ID))
		_, err = svc.repo.GetByID(ctx, proj, bp.ID)
		assertAppErrorCode(t, err, apperror.ErrNotFound.Code)
	})
}

func insertApplication(t *testing.T, db bun.IDB, blueprintID, projectID, version, status string) {
	t.Helper()
	_, err := db.ExecContext(context.Background(), `
		INSERT INTO kb.blueprint_applications (blueprint_id, project_id, version, status, applied_at, created_at, updated_at)
		VALUES (?, ?, ?, ?, now(), now(), now())
	`, blueprintID, projectID, version, status)
	require.NoError(t, err)
}

func insertSchemaPack(t *testing.T, db bun.IDB, projectID, name, version string) string {
	t.Helper()
	id := uuid.NewString()
	_, err := db.ExecContext(context.Background(), `
		INSERT INTO kb.graph_schemas (id, name, version, description, source, object_type_schemas, relationship_type_schemas, project_id, published_at, created_at, updated_at)
		VALUES (?, ?, ?, '', 'manual', '[{"name":"Thing","properties":{}}]'::jsonb, '[]'::jsonb, ?, now(), now(), now())
	`, id, name, version, projectID)
	require.NoError(t, err)
	return id
}

func insertPackClaim(t *testing.T, db bun.IDB, blueprintID, projectID, schemaID string) {
	t.Helper()
	_, err := db.ExecContext(context.Background(), `
		INSERT INTO kb.blueprint_pack_claims (blueprint_id, project_id, schema_id) VALUES (?, ?, ?)
	`, blueprintID, projectID, schemaID)
	require.NoError(t, err)
}

func assertPackClaimCount(t *testing.T, db bun.IDB, blueprintID string, want int) {
	t.Helper()
	var n int
	err := db.NewRaw(`SELECT COUNT(*) FROM kb.blueprint_pack_claims WHERE blueprint_id = ?`, blueprintID).
		Scan(context.Background(), &n)
	require.NoError(t, err)
	assert.Equal(t, want, n)
}

func assertSchemaPackExists(t *testing.T, db bun.IDB, packID string, want bool) {
	t.Helper()
	var n int
	err := db.NewRaw(`SELECT COUNT(*) FROM kb.graph_schemas WHERE id = ?`, packID).
		Scan(context.Background(), &n)
	require.NoError(t, err)
	assert.Equal(t, want, n > 0)
}
