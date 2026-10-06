package blueprints

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"

	"github.com/emergent-company/emergent.memory/pkg/apperror"
)

// newServiceFromBun builds a Service with a real blueprints Repository and nil
// cross-domain deps. CRUD/versioning/status paths only use s.repo, and the
// Apply paths under test (deprecated / invalid manifest) return before any
// cross-domain dep is touched, so nil is safe here.
func newServiceFromBun(t *testing.T, db *bun.DB) *Service {
	t.Helper()
	repo := NewRepository(db, testLogger())
	return NewService(ServiceParams{Repo: repo, Log: testLogger()})
}

// TestService_CreateBlueprint_Success verifies a created blueprint is a draft
// with the manifest round-tripping through the jsonb column. A request without
// a project is global.
func TestService_CreateBlueprint_Success(t *testing.T) {
	db := connectTestDB(t)
	svc := newServiceFromBun(t, db)
	ctx := context.Background()

	bp, err := svc.CreateBlueprint(ctx, &CreateBlueprintRequest{
		Name:        uniqueName("svc-create"),
		Version:     "1.0.0",
		Description: "a test blueprint",
		Author:      "tester",
		Manifest:    json.RawMessage(`{"kind":"service"}`),
	})
	require.NoError(t, err)
	assert.NotEmpty(t, bp.ID)
	assert.Equal(t, StatusDraft, bp.Status)
	assert.Equal(t, "", bp.Checksum)
	assert.Equal(t, "global", bp.Scope(), "no project in request => global blueprint")

	got, err := svc.repo.GetByID(ctx, "", bp.ID)
	require.NoError(t, err)
	assert.JSONEq(t, `{"kind":"service"}`, string(got.Manifest), "manifest must round-trip")
	assert.Equal(t, "a test blueprint", got.Description)
}

// TestService_CreateBlueprint_Conflict verifies duplicate (name, version) is
// rejected with a conflict error.
func TestService_CreateBlueprint_Conflict(t *testing.T) {
	db := connectTestDB(t)
	svc := newServiceFromBun(t, db)
	ctx := context.Background()

	req := &CreateBlueprintRequest{Name: uniqueName("svc-dup"), Version: "1.0.0", Manifest: json.RawMessage(`{}`)}
	_, err := svc.CreateBlueprint(ctx, req)
	require.NoError(t, err)

	_, err = svc.CreateBlueprint(ctx, req)
	assertAppErrorCode(t, err, apperror.ErrConflict.Code)
}

// TestService_CreateBlueprint_Validation verifies empty name/version are
// rejected with bad_request.
func TestService_CreateBlueprint_Validation(t *testing.T) {
	db := connectTestDB(t)
	svc := newServiceFromBun(t, db)
	ctx := context.Background()

	_, err := svc.CreateBlueprint(ctx, &CreateBlueprintRequest{Version: "1.0.0", Manifest: json.RawMessage(`{}`)})
	assertAppErrorCode(t, err, apperror.ErrBadRequest.Code)

	_, err = svc.CreateBlueprint(ctx, &CreateBlueprintRequest{Name: uniqueName("no-version"), Manifest: json.RawMessage(`{}`)})
	assertAppErrorCode(t, err, apperror.ErrBadRequest.Code)
}

// TestService_UpdateBlueprint verifies draft updates apply and non-draft
// updates are rejected. Only private (project-scoped) drafts can be updated.
func TestService_UpdateBlueprint(t *testing.T) {
	db := connectTestDB(t)
	svc := newServiceFromBun(t, db)
	ctx := context.Background()

	proj := uuid.NewString()
	bp, err := svc.CreateBlueprint(ctx, &CreateBlueprintRequest{
		Name: uniqueName("svc-update"), Version: "1.0.0", Manifest: json.RawMessage(`{"v":1}`),
		ProjectID: &proj,
	})
	require.NoError(t, err)

	t.Run("draft update applies", func(t *testing.T) {
		desc := "updated"
		author := "author2"
		updated, err := svc.UpdateBlueprint(ctx, proj, bp.ID, &UpdateBlueprintRequest{
			Description: &desc,
			Author:      &author,
			Manifest:    json.RawMessage(`{"v":2}`),
		})
		require.NoError(t, err)
		assert.Equal(t, "updated", updated.Description)
		assert.Equal(t, "author2", updated.Author)
		assert.JSONEq(t, `{"v":2}`, string(updated.Manifest))
		assert.Equal(t, StatusDraft, updated.Status, "status must remain draft")
	})

	t.Run("published blueprint cannot be updated", func(t *testing.T) {
		_, err := svc.PublishBlueprint(ctx, proj, bp.ID)
		require.NoError(t, err)

		desc := "nope"
		_, err = svc.UpdateBlueprint(ctx, proj, bp.ID, &UpdateBlueprintRequest{Description: &desc})
		assertAppErrorCode(t, err, apperror.ErrConflict.Code)
	})

	t.Run("another project cannot update a private blueprint", func(t *testing.T) {
		other := uuid.NewString()
		desc := "sneaky"
		_, err := svc.UpdateBlueprint(ctx, other, bp.ID, &UpdateBlueprintRequest{Description: &desc})
		assertAppErrorCode(t, err, apperror.ErrNotFound.Code)
	})
}

// TestService_PublishBlueprint_ComputesSha256Checksum verifies draft→published
// with a non-empty sha256 checksum of the manifest.
func TestService_PublishBlueprint_ComputesSha256Checksum(t *testing.T) {
	db := connectTestDB(t)
	svc := newServiceFromBun(t, db)
	ctx := context.Background()

	proj := uuid.NewString()
	bp, err := svc.CreateBlueprint(ctx, &CreateBlueprintRequest{
		Name: uniqueName("svc-publish"), Version: "1.0.0",
		Manifest:  json.RawMessage(`{"kind":"publish-me"}`),
		ProjectID: &proj,
	})
	require.NoError(t, err)

	// Checksum must be sha256 of the ROUND-TRIPPED manifest (jsonb normalizes
	// whitespace, so compute against what the DB actually returns).
	stored, err := svc.repo.GetByID(ctx, proj, bp.ID)
	require.NoError(t, err)

	pub, err := svc.PublishBlueprint(ctx, proj, bp.ID)
	require.NoError(t, err)
	assert.Equal(t, StatusPublished, pub.Status)
	require.NotEmpty(t, pub.Checksum)

	sum := sha256.Sum256(stored.Manifest)
	assert.Equal(t, hex.EncodeToString(sum[:]), pub.Checksum)
}

// TestService_PublishBlueprint_AlreadyPublished_Conflict verifies publishing a
// published blueprint is rejected.
func TestService_PublishBlueprint_AlreadyPublished_Conflict(t *testing.T) {
	db := connectTestDB(t)
	svc := newServiceFromBun(t, db)
	ctx := context.Background()

	proj := uuid.NewString()
	bp, err := svc.CreateBlueprint(ctx, &CreateBlueprintRequest{
		Name: uniqueName("svc-republish"), Version: "1.0.0", Manifest: json.RawMessage(`{}`),
		ProjectID: &proj,
	})
	require.NoError(t, err)
	_, err = svc.PublishBlueprint(ctx, proj, bp.ID)
	require.NoError(t, err)

	_, err = svc.PublishBlueprint(ctx, proj, bp.ID)
	assertAppErrorCode(t, err, apperror.ErrConflict.Code)
}

// TestService_NewVersion verifies cloning into a new draft version, and that
// an existing (name, version) is rejected.
func TestService_NewVersion(t *testing.T) {
	db := connectTestDB(t)
	svc := newServiceFromBun(t, db)
	ctx := context.Background()

	proj := uuid.NewString()
	bp, err := svc.CreateBlueprint(ctx, &CreateBlueprintRequest{
		Name:        uniqueName("svc-version"),
		Version:     "1.0.0",
		Description: "orig desc",
		Author:      "orig author",
		Manifest:    json.RawMessage(`{"kind":"clone-me"}`),
		ProjectID:   &proj,
	})
	require.NoError(t, err)

	clone, err := svc.NewVersion(ctx, proj, bp.ID, "2.0.0")
	require.NoError(t, err)
	assert.NotEqual(t, bp.ID, clone.ID, "clone must be a NEW row")
	assert.Equal(t, bp.Name, clone.Name)
	assert.Equal(t, "2.0.0", clone.Version)
	assert.Equal(t, StatusDraft, clone.Status)
	assert.Equal(t, "orig desc", clone.Description)
	assert.Equal(t, "orig author", clone.Author)
	assert.JSONEq(t, `{"kind":"clone-me"}`, string(clone.Manifest), "manifest must be copied")
	require.NotNil(t, clone.ProjectID, "clone of a private blueprint must stay private")
	assert.Equal(t, proj, *clone.ProjectID)

	versions, err := svc.ListVersions(ctx, proj, bp.Name)
	require.NoError(t, err)
	require.Len(t, versions, 2)

	t.Run("existing version conflicts", func(t *testing.T) {
		_, err := svc.NewVersion(ctx, proj, bp.ID, "1.0.0")
		assertAppErrorCode(t, err, apperror.ErrConflict.Code)
	})
}

// TestService_DeprecateBlueprint_IsIdempotent verifies any-status→deprecated
// and that deprecating an already-deprecated blueprint is a no-op success.
func TestService_DeprecateBlueprint_IsIdempotent(t *testing.T) {
	db := connectTestDB(t)
	svc := newServiceFromBun(t, db)
	ctx := context.Background()

	proj := uuid.NewString()
	bp, err := svc.CreateBlueprint(ctx, &CreateBlueprintRequest{
		Name: uniqueName("svc-deprecate"), Version: "1.0.0", Manifest: json.RawMessage(`{}`),
		ProjectID: &proj,
	})
	require.NoError(t, err)

	// draft → deprecated directly.
	dep, err := svc.DeprecateBlueprint(ctx, proj, bp.ID)
	require.NoError(t, err)
	assert.Equal(t, StatusDeprecated, dep.Status)

	// deprecated → deprecated is idempotent (no error, same row).
	again, err := svc.DeprecateBlueprint(ctx, proj, bp.ID)
	require.NoError(t, err)
	assert.Equal(t, StatusDeprecated, again.Status)
	assert.Equal(t, dep.ID, again.ID)
}

// TestService_DeleteBlueprint_DraftOnly verifies draft deletion succeeds and
// published deletion is rejected.
func TestService_DeleteBlueprint_DraftOnly(t *testing.T) {
	db := connectTestDB(t)
	svc := newServiceFromBun(t, db)
	ctx := context.Background()

	proj := uuid.NewString()
	req := &CreateBlueprintRequest{Name: uniqueName("svc-delete"), Version: "1.0.0", Manifest: json.RawMessage(`{}`), ProjectID: &proj}

	t.Run("draft deletes", func(t *testing.T) {
		bp, err := svc.CreateBlueprint(ctx, req)
		require.NoError(t, err)
		require.NoError(t, svc.DeleteBlueprint(ctx, proj, bp.ID))
		_, err = svc.repo.GetByID(ctx, proj, bp.ID)
		assertAppErrorCode(t, err, apperror.ErrNotFound.Code)
	})

	t.Run("published cannot be deleted", func(t *testing.T) {
		bp, err := svc.CreateBlueprint(ctx, req)
		require.NoError(t, err)
		_, err = svc.PublishBlueprint(ctx, proj, bp.ID)
		require.NoError(t, err)
		err = svc.DeleteBlueprint(ctx, proj, bp.ID)
		assertAppErrorCode(t, err, apperror.ErrConflict.Code)
	})

	t.Run("another project cannot delete a private blueprint", func(t *testing.T) {
		bp, err := svc.CreateBlueprint(ctx, &CreateBlueprintRequest{
			Name: uniqueName("svc-delete-other"), Version: "1.0.0", Manifest: json.RawMessage(`{}`),
			ProjectID: &proj,
		})
		require.NoError(t, err)
		err = svc.DeleteBlueprint(ctx, uuid.NewString(), bp.ID)
		assertAppErrorCode(t, err, apperror.ErrNotFound.Code)
	})
}

// TestService_Apply_Deprecated_Conflict verifies Apply rejects deprecated
// blueprints before touching any cross-domain dep.
func TestService_Apply_Deprecated_Conflict(t *testing.T) {
	db := connectTestDB(t)
	svc := newServiceFromBun(t, db)
	ctx := context.Background()

	proj := uuid.NewString()
	bp, err := svc.CreateBlueprint(ctx, &CreateBlueprintRequest{
		Name: uniqueName("svc-apply-dep"), Version: "1.0.0", Manifest: json.RawMessage(`{}`),
		ProjectID: &proj,
	})
	require.NoError(t, err)
	_, err = svc.DeprecateBlueprint(ctx, proj, bp.ID)
	require.NoError(t, err)

	_, err = svc.Apply(ctx, bp.ID, proj, uuid.NewString(), ApplyOptions{})
	assertAppErrorCode(t, err, apperror.ErrConflict.Code)
}

// TestService_Apply_InvalidManifest_BadRequest verifies Apply rejects a
// manifest that is valid JSONB but not a BlueprintManifest, before touching
// any cross-domain dep.
func TestService_Apply_InvalidManifest_BadRequest(t *testing.T) {
	db := connectTestDB(t)
	svc := newServiceFromBun(t, db)
	ctx := context.Background()

	// `[]` is valid JSON (storable in the jsonb column) but cannot unmarshal
	// into BlueprintManifest.
	proj := uuid.NewString()
	bp, err := svc.CreateBlueprint(ctx, &CreateBlueprintRequest{
		Name: uniqueName("svc-apply-invalid"), Version: "1.0.0", Manifest: json.RawMessage(`[]`),
		ProjectID: &proj,
	})
	require.NoError(t, err)

	_, err = svc.Apply(ctx, bp.ID, proj, uuid.NewString(), ApplyOptions{})
	assertAppErrorCode(t, err, apperror.ErrBadRequest.Code)
}

// TestService_Apply_PreValidation_BadModel verifies the validate-before-
// materialize contract at the Apply level: a manifest whose agent carries a
// bare model name (missing the provider prefix) returns 400 BEFORE any pack is
// materialized. The manifest carries a pack that would otherwise reach
// applyPacks; because newServiceFromBun wires no schemas service, reaching
// applyPacks would panic — a clean 400 proves validation ran first and zero
// packs were created.
func TestService_Apply_PreValidation_BadModel(t *testing.T) {
	db := connectTestDB(t)
	svc := newServiceFromBun(t, db)
	ctx := context.Background()

	proj := uuid.NewString()
	manifest := json.RawMessage(`{
		"packs": [{"name": "p", "version": "1.0.0", "objectTypes": [{"name": "T"}]}],
		"agents": [{"name": "a", "model": {"name": "gemini-2.5-flash"}}]
	}`)

	bp, err := svc.CreateBlueprint(ctx, &CreateBlueprintRequest{
		Name: uniqueName("svc-apply-badmodel"), Version: "1.0.0", Manifest: manifest, ProjectID: &proj,
	})
	require.NoError(t, err)
	_, err = svc.PublishBlueprint(ctx, proj, bp.ID)
	require.NoError(t, err)

	_, err = svc.Apply(ctx, bp.ID, proj, uuid.NewString(), ApplyOptions{})
	assertAppErrorCode(t, err, apperror.ErrBadRequest.Code)
}

// TestService_GlobalBlueprint_VisibleFromAnyProject verifies a global blueprint
// is listable and gettable from any project.
func TestService_GlobalBlueprint_VisibleFromAnyProject(t *testing.T) {
	db := connectTestDB(t)
	svc := newServiceFromBun(t, db)
	ctx := context.Background()

	proj := uuid.NewString()
	bp, err := svc.CreateBlueprint(ctx, &CreateBlueprintRequest{
		Name: uniqueName("svc-global-vis"), Version: "1.0.0", Manifest: json.RawMessage(`{}`),
	})
	require.NoError(t, err)
	assert.Equal(t, "global", bp.Scope())

	// Visible from a project context...
	got, err := svc.GetBlueprint(ctx, proj, bp.ID)
	require.NoError(t, err)
	assert.Equal(t, bp.ID, got.ID)

	// ...and from no project context.
	got, err = svc.GetBlueprint(ctx, "", bp.ID)
	require.NoError(t, err)
	assert.Equal(t, bp.ID, got.ID)

	// Listable (filtered and unfiltered) from any project.
	list, err := svc.ListBlueprints(ctx, proj, bp.Name)
	require.NoError(t, err)
	require.Len(t, list, 1)
	assert.Equal(t, bp.ID, list[0].ID)

	versions, err := svc.ListVersions(ctx, proj, bp.Name)
	require.NoError(t, err)
	require.Len(t, versions, 1)
}

// TestService_PrivateBlueprint_HiddenFromOtherProjects verifies a private
// blueprint is only visible to its owning project.
func TestService_PrivateBlueprint_HiddenFromOtherProjects(t *testing.T) {
	db := connectTestDB(t)
	svc := newServiceFromBun(t, db)
	ctx := context.Background()

	owner := uuid.NewString()
	other := uuid.NewString()
	bp, err := svc.CreateBlueprint(ctx, &CreateBlueprintRequest{
		Name: uniqueName("svc-private-vis"), Version: "1.0.0", Manifest: json.RawMessage(`{}`),
		ProjectID: &owner,
	})
	require.NoError(t, err)
	assert.Equal(t, "project", bp.Scope())

	// Owner sees it.
	got, err := svc.GetBlueprint(ctx, owner, bp.ID)
	require.NoError(t, err)
	assert.Equal(t, bp.ID, got.ID)

	// Another project: not found.
	_, err = svc.GetBlueprint(ctx, other, bp.ID)
	assertAppErrorCode(t, err, apperror.ErrNotFound.Code)

	// No project context: not found (global-only scope).
	_, err = svc.GetBlueprint(ctx, "", bp.ID)
	assertAppErrorCode(t, err, apperror.ErrNotFound.Code)

	// Not listed for another project, even with the exact name filter.
	list, err := svc.ListBlueprints(ctx, other, bp.Name)
	require.NoError(t, err)
	require.Empty(t, list)
}

// TestService_GlobalBlueprint_Immutability verifies content mutations
// (Update/Delete) on a global blueprint are rejected, while lifecycle
// transitions (Publish/Deprecate) are allowed.
func TestService_GlobalBlueprint_Immutability(t *testing.T) {
	db := connectTestDB(t)
	svc := newServiceFromBun(t, db)
	ctx := context.Background()

	proj := uuid.NewString()
	bp, err := svc.CreateBlueprint(ctx, &CreateBlueprintRequest{
		Name: uniqueName("svc-global-immutable"), Version: "1.0.0", Manifest: json.RawMessage(`{}`),
	})
	require.NoError(t, err)

	desc := "nope"
	_, err = svc.UpdateBlueprint(ctx, proj, bp.ID, &UpdateBlueprintRequest{Description: &desc})
	assertAppErrorCode(t, err, apperror.ErrConflict.Code)

	err = svc.DeleteBlueprint(ctx, proj, bp.ID)
	assertAppErrorCode(t, err, apperror.ErrConflict.Code)

	// Lifecycle transitions are allowed on global blueprints.
	pub, err := svc.PublishBlueprint(ctx, proj, bp.ID)
	require.NoError(t, err)
	assert.Equal(t, StatusPublished, pub.Status)

	dep, err := svc.DeprecateBlueprint(ctx, proj, bp.ID)
	require.NoError(t, err)
	assert.Equal(t, StatusDeprecated, dep.Status)

	// The blueprint is still readable (immutable, not hidden).
	_, err = svc.GetBlueprint(ctx, proj, bp.ID)
	require.NoError(t, err)
}

// TestService_GlobalBlueprint_Publishable verifies a global draft can be
// published (draft → published with a manifest checksum) — the gateway seed
// prerequisite for the create → publish → apply install flow.
func TestService_GlobalBlueprint_Publishable(t *testing.T) {
	db := connectTestDB(t)
	svc := newServiceFromBun(t, db)
	ctx := context.Background()

	proj := uuid.NewString()
	bp, err := svc.CreateBlueprint(ctx, &CreateBlueprintRequest{
		Name: uniqueName("svc-global-publish"), Version: "1.0.0", Manifest: json.RawMessage(`{"kind":"seed"}`),
	})
	require.NoError(t, err)
	assert.Equal(t, "global", bp.Scope())
	assert.Equal(t, StatusDraft, bp.Status)

	pub, err := svc.PublishBlueprint(ctx, proj, bp.ID)
	require.NoError(t, err)
	assert.Equal(t, StatusPublished, pub.Status)
	require.NotEmpty(t, pub.Checksum, "published global blueprint must carry a manifest checksum")

	// Published global remains visible (and thus applyable) from the caller's
	// read scope.
	got, err := svc.GetBlueprint(ctx, proj, bp.ID)
	require.NoError(t, err)
	assert.Equal(t, StatusPublished, got.Status)
}

// TestService_NewVersion_OfGlobal_ProducesPrivateClone verifies forking a
// global blueprint yields a clone scoped to the caller's project.
func TestService_NewVersion_OfGlobal_ProducesPrivateClone(t *testing.T) {
	db := connectTestDB(t)
	svc := newServiceFromBun(t, db)
	ctx := context.Background()

	proj := uuid.NewString()
	bp, err := svc.CreateBlueprint(ctx, &CreateBlueprintRequest{
		Name: uniqueName("svc-global-fork"), Version: "1.0.0", Manifest: json.RawMessage(`{"kind":"fork"}`),
	})
	require.NoError(t, err)
	assert.Equal(t, "global", bp.Scope())

	clone, err := svc.NewVersion(ctx, proj, bp.ID, "1.1.0")
	require.NoError(t, err)
	require.NotNil(t, clone.ProjectID, "fork of a global blueprint must become private")
	assert.Equal(t, proj, *clone.ProjectID)
	assert.Equal(t, bp.Name, clone.Name)
	assert.JSONEq(t, `{"kind":"fork"}`, string(clone.Manifest), "manifest must be copied")

	// The clone is invisible to other projects.
	_, err = svc.GetBlueprint(ctx, uuid.NewString(), clone.ID)
	assertAppErrorCode(t, err, apperror.ErrNotFound.Code)

	// The global source and the private clone can coexist on the same name.
	versions, err := svc.ListVersions(ctx, proj, bp.Name)
	require.NoError(t, err)
	require.Len(t, versions, 2)

	// Forking with no project context yields a global clone.
	globalClone, err := svc.NewVersion(ctx, "", bp.ID, "2.0.0")
	require.NoError(t, err)
	assert.Nil(t, globalClone.ProjectID, "fork without project context stays global")
}
