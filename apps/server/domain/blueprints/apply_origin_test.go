package blueprints

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestApply_OriginBuiltin_UsesGlobalPack verifies that applying an
// origin='builtin' blueprint materializes (or reuses) the GLOBAL
// source='builtin' schema pack (project_id NULL), not a project-scoped copy.
func TestApply_OriginBuiltin_UsesGlobalPack(t *testing.T) {
	db := connectTestDB(t)
	svc, _, schemasRepo := newMigrationService(t, db)
	ctx := context.Background()

	_, projectID := seedProject(t, db)
	name := uniqueName("builtin")
	version := "1.0.0"

	manifest := json.RawMessage(mustJSON(t, BlueprintManifest{
		Packs: []PackManifest{{
			Name:    name,
			Version: version,
			ObjectTypes: []ObjectTypeDef{{
				Name: "Thing", Label: "Thing", Properties: map[string]any{},
			}},
		}},
	}))

	// Built-ins are seeded globally by the server and are NOT creatable through
	// the client-facing CreateBlueprint path (origin is server-derived), so
	// insert the global builtin row directly, as the seeder does.
	bp := testBlueprint(name, version)
	bp.Manifest = manifest
	bp.Checksum = manifestChecksum(manifest)
	bp.Status = StatusPublished
	bp.Origin = OriginBuiltin
	require.NoError(t, svc.repo.Create(ctx, bp))
	assert.Equal(t, "global", bp.Scope())

	_, err := svc.Apply(ctx, bp.ID, projectID, uuid.NewString(), ApplyOptions{})
	require.NoError(t, err)

	pack, err := schemasRepo.GetBuiltinPackByNameVersion(ctx, name, version)
	require.NoError(t, err)
	assert.Nil(t, pack.ProjectID, "builtin pack must be global (project_id NULL)")
	assert.NotNil(t, pack.Source)
	assert.Equal(t, "builtin", *pack.Source)

	// A project-scoped pack of the same name must NOT have been created.
	_, err = schemasRepo.GetProjectPackByNameVersion(ctx, projectID, name, version)
	require.Error(t, err)
}

// TestApply_OriginBuiltin_RefreshesStaleGlobalPack verifies that re-applying a
// built-in blueprint syncs the global pack row from the embedded manifest.
// Built-in schema packs are created on first apply and are never reseeded on
// boot, so without the refresh a pack whose content changed under the same
// version (a property label, a description, a type) would keep serving its
// first-published definitions forever — while the re-apply reported success.
func TestApply_OriginBuiltin_RefreshesStaleGlobalPack(t *testing.T) {
	db := connectTestDB(t)
	svc, _, schemasRepo := newMigrationService(t, db)
	ctx := context.Background()

	_, projectID := seedProject(t, db)
	name := uniqueName("builtin-refresh")
	version := "1.0.0"

	// The pack row as a deployment created it before the property was labelled.
	_, err := upsertBuiltinSchemaPack(ctx, schemasRepo.DB(), PackManifest{
		Name: name, Version: version,
		ObjectTypes: []ObjectTypeDef{{
			Name: "Thing", Label: "Thing",
			Properties: map[string]any{"event_time": map[string]any{"type": "string"}},
		}},
	})
	require.NoError(t, err)

	// The shipped manifest now carries the label.
	manifest := json.RawMessage(mustJSON(t, BlueprintManifest{
		Packs: []PackManifest{{
			Name: name, Version: version,
			ObjectTypes: []ObjectTypeDef{{
				Name: "Thing", Label: "Thing",
				Properties: map[string]any{"event_time": map[string]any{"type": "string", "title": "Event time"}},
			}},
		}},
	}))
	bp := testBlueprint(name, version)
	bp.Manifest = manifest
	bp.Checksum = manifestChecksum(manifest)
	bp.Status = StatusPublished
	bp.Origin = OriginBuiltin
	require.NoError(t, svc.repo.Create(ctx, bp))

	_, err = svc.Apply(ctx, bp.ID, projectID, uuid.NewString(), ApplyOptions{})
	require.NoError(t, err)

	pack, err := schemasRepo.GetBuiltinPackByNameVersion(ctx, name, version)
	require.NoError(t, err)

	var stored []struct {
		Name       string                    `json:"name"`
		Properties map[string]map[string]any `json:"properties"`
	}
	require.NoError(t, json.Unmarshal(pack.ObjectTypeSchemas, &stored))
	require.Len(t, stored, 1)
	assert.Equal(t, "Event time", stored[0].Properties["event_time"]["title"],
		"re-applying a built-in blueprint must refresh the global pack from the manifest")
}

// TestApply_OriginUser_ProjectScopedPack_NoCrossProjectReuse verifies that
// origin='user'/'import' blueprints materialize a PROJECT-SCOPED pack only, and
// that two projects applying the same (name, version) never share one pack (the
// bug behind "appears/succeeds in one project, fails in others").
func TestApply_OriginUser_ProjectScopedPack_NoCrossProjectReuse(t *testing.T) {
	db := connectTestDB(t)
	svc, _, schemasRepo := newMigrationService(t, db)
	ctx := context.Background()

	_, projA := seedProject(t, db)
	_, projB := seedProject(t, db)
	name := uniqueName("projpack")
	version := "1.0.0"

	manifest := json.RawMessage(mustJSON(t, BlueprintManifest{
		Packs: []PackManifest{{
			Name: name, Version: version,
			ObjectTypes: []ObjectTypeDef{{Name: "Thing", Properties: map[string]any{}}},
		}},
	}))

	// Project A applies a user-origin blueprint.
	bpA, err := svc.CreateBlueprint(ctx, &CreateBlueprintRequest{
		Name: name, Version: version, Manifest: manifest, ProjectID: &projA, Origin: OriginUser,
	})
	require.NoError(t, err)
	_, err = svc.Apply(ctx, bpA.ID, projA, uuid.NewString(), ApplyOptions{})
	require.NoError(t, err)

	// Project B applies a DIFFERENT blueprint of the same (name, version).
	bpB, err := svc.CreateBlueprint(ctx, &CreateBlueprintRequest{
		Name: name, Version: version, Manifest: manifest, ProjectID: &projB, Origin: OriginImport,
	})
	require.NoError(t, err)
	_, err = svc.Apply(ctx, bpB.ID, projB, uuid.NewString(), ApplyOptions{})
	require.NoError(t, err)

	packA, err := schemasRepo.GetProjectPackByNameVersion(ctx, projA, name, version)
	require.NoError(t, err)
	packB, err := schemasRepo.GetProjectPackByNameVersion(ctx, projB, name, version)
	require.NoError(t, err)

	assert.NotEqual(t, packA.ID, packB.ID, "projects must not share a project-scoped pack")
	assert.Equal(t, projA, *packA.ProjectID)
	assert.Equal(t, projB, *packB.ProjectID)
}
