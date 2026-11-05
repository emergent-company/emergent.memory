package blueprints

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"

	"github.com/emergent-company/emergent.memory/domain/schemas"
	"github.com/emergent-company/emergent.memory/pkg/apperror"
	"github.com/emergent-company/emergent.memory/pkg/auth"
)

// widgetTypes builds an object-type array for a Widget conflict test.
func widgetTypes(props map[string]any) []map[string]any {
	return []map[string]any{{
		"name":       "Widget",
		"properties": props,
	}}
}

// seedRegisteredPack creates a project-scoped schema pack and assigns it, so the
// project object-type registry owns the pack's types under packName.
func seedRegisteredPack(t *testing.T, ctx context.Context, schemasSvc *schemas.Service, projectID, packName string, objectTypes []map[string]any) *schemas.GraphMemorySchema {
	t.Helper()
	raw, err := json.Marshal(objectTypes)
	require.NoError(t, err)
	pack, err := schemasSvc.CreatePack(ctx, projectID, &schemas.CreatePackRequest{
		Name:                   packName,
		Version:                "1.0.0",
		ObjectTypeSchemasSnake: raw,
	})
	require.NoError(t, err)
	_, err = schemasSvc.AssignPack(ctx, projectID, uuid.NewString(), &schemas.AssignPackRequest{
		SchemaID: pack.ID,
		Merge:    true,
	})
	require.NoError(t, err)
	return pack
}

// createPublishedMergeBlueprint creates + publishes a project-private blueprint
// carrying manifest.
func createPublishedMergeBlueprint(t *testing.T, ctx context.Context, svc *Service, projectID, name string, manifest BlueprintManifest) *Blueprint {
	t.Helper()
	bp, err := svc.CreateBlueprint(ctx, &CreateBlueprintRequest{
		Name:      name,
		Version:   "1.0.0",
		ProjectID: &projectID,
		Manifest:  json.RawMessage(mustJSON(t, manifest)),
	})
	require.NoError(t, err)
	published, err := svc.PublishBlueprint(ctx, projectID, bp.ID)
	require.NoError(t, err)
	return published
}

// mergedPackManifest is the incoming manifest used across the conflict tests.
func mergedPackManifest(packName string, version string) BlueprintManifest {
	return BlueprintManifest{
		Packs: []PackManifest{{
			Name:    packName,
			Version: version,
			ObjectTypes: []ObjectTypeDef{{
				Name: "Widget",
				Properties: map[string]any{
					"name":  map[string]any{"type": "number"},
					"email": map[string]any{"type": "string"},
				},
			}},
		}},
		Agents: []AgentManifest{{
			Name:         "merge-agent-" + packName,
			Description:  "merge test agent",
			SystemPrompt: "test",
			Tools:        []string{"skill-list"},
		}},
		Skills: []SkillManifest{{
			Name:        "merge-skill-test",
			Description: "merge test skill",
			Content:     "test content",
		}},
		Seed: &SeedManifest{Objects: []SeedObjectRecord{{
			Type: "Widget", Key: "merge-seed-" + packName,
			Properties: map[string]any{"name": "seed"},
		}}},
	}
}

func countRows(t *testing.T, ctx context.Context, db bun.IDB, table string) int {
	t.Helper()
	var n int
	require.NoError(t, db.NewRaw("SELECT COUNT(*) FROM "+table).Scan(ctx, &n))
	return n
}

func registrySchemaJSON(t *testing.T, ctx context.Context, db bun.IDB, projectID, typeName string) map[string]any {
	t.Helper()
	var raw string
	require.NoError(t, db.NewRaw(`
		SELECT json_schema::text FROM kb.project_object_schema_registry
		WHERE project_id = ? AND type_name = ?
	`, projectID, typeName).Scan(ctx, &raw))
	var out map[string]any
	require.NoError(t, json.Unmarshal([]byte(raw), &out))
	return out
}

func TestApplyMergeConflict(t *testing.T) {
	db := connectTestDB(t)
	svc, schemasSvc, _ := newMigrationService(t, db)
	ctx := context.Background()
	_, projectID := seedProject(t, db)

	seedRegisteredPack(t, ctx, schemasSvc, projectID, "Existing Pack", widgetTypes(map[string]any{
		"name": map[string]any{"type": "string"},
		"age":  map[string]any{"type": "integer"},
	}))

	manifest := mergedPackManifest("Incoming Pack", "1.0.0")
	bp := createPublishedMergeBlueprint(t, ctx, svc, projectID, uniqueName("merge-conflict"), manifest)

	t.Run("preview reports conflict and property diff", func(t *testing.T) {
		preview, err := svc.PreviewApply(ctx, bp.ID, projectID)
		require.NoError(t, err)
		require.True(t, preview.RequiresMerge)
		require.Len(t, preview.Conflicts, 1)

		c := preview.Conflicts[0]
		assert.Equal(t, "Widget", c.TypeName)
		assert.Equal(t, "Incoming Pack", c.IncomingPackName)
		assert.Equal(t, "Existing Pack", c.ExistingSchemaName)
		assert.Equal(t, []string{"email"}, c.AddedProperties)
		require.Len(t, c.ConflictingProperties, 1)
		assert.Equal(t, "name", c.ConflictingProperties[0].Property)
	})

	t.Run("apply blocks before any mutation", func(t *testing.T) {
		tables := []string{
			"kb.project_object_schema_registry",
			"kb.project_schemas",
			"kb.graph_schemas",
			"kb.agent_definitions",
			"kb.skills",
			"kb.graph_objects",
			"kb.blueprint_applications",
		}
		before := make(map[string]int, len(tables))
		for _, table := range tables {
			before[table] = countRows(t, ctx, db, table)
		}

		_, err := svc.Apply(ctx, bp.ID, projectID, uuid.NewString(), ApplyOptions{})
		require.Error(t, err)
		var apErr *apperror.Error
		require.True(t, errors.As(err, &apErr))
		assert.Equal(t, apperror.ErrConflict.Code, apErr.Code)
		assert.Equal(t, http.StatusConflict, apErr.HTTPStatus)
		require.NotNil(t, apErr.Details)
		assert.Contains(t, apErr.Details, "conflicts")

		for _, table := range tables {
			assert.Equal(t, before[table], countRows(t, ctx, db, table), "table %s changed on blocked apply", table)
		}
	})

	t.Run("apply with merge succeeds and existing property wins", func(t *testing.T) {
		result, err := svc.Apply(ctx, bp.ID, projectID, uuid.NewString(), ApplyOptions{MergeTypeConflicts: true})
		require.NoError(t, err)
		require.Len(t, result.TypeConflicts, 1)
		assert.Equal(t, "Widget", result.TypeConflicts[0].TypeName)

		schema := registrySchemaJSON(t, ctx, db, projectID, "Widget")
		props, ok := schema["properties"].(map[string]any)
		require.True(t, ok)
		nameDef, ok := props["name"].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, "string", nameDef["type"], "existing property definition wins")
		_, hasEmail := props["email"]
		assert.True(t, hasEmail, "incoming new property is added")
		_, hasAge := props["age"]
		assert.True(t, hasAge, "unrelated existing property is preserved")
	})
}

func TestApplySamePackNameIgnoresConflict(t *testing.T) {
	db := connectTestDB(t)
	svc, schemasSvc, _ := newMigrationService(t, db)
	ctx := context.Background()
	_, projectID := seedProject(t, db)

	seedRegisteredPack(t, ctx, schemasSvc, projectID, "Same Pack", widgetTypes(map[string]any{
		"name": map[string]any{"type": "string"},
	}))
	bp := createPublishedMergeBlueprint(t, ctx, svc, projectID, uniqueName("same-pack"), mergedPackManifest("Same Pack", "2.0.0"))

	preview, err := svc.PreviewApply(ctx, bp.ID, projectID)
	require.NoError(t, err)
	assert.False(t, preview.RequiresMerge)
	assert.Empty(t, preview.Conflicts)

	result, err := svc.Apply(ctx, bp.ID, projectID, uuid.NewString(), ApplyOptions{})
	require.NoError(t, err)
	assert.Empty(t, result.TypeConflicts)
}

// importBlueprintJSON builds a repo-root blueprint.json with manifest fields.
func importBlueprintJSON(t *testing.T, name, version string, manifest BlueprintManifest) string {
	t.Helper()
	var m map[string]any
	require.NoError(t, json.Unmarshal([]byte(mustJSON(t, manifest)), &m))
	m["name"] = name
	m["version"] = version
	return mustJSON(t, m)
}

func TestImportMergeConflict(t *testing.T) {
	db := connectTestDB(t)
	svc, schemasSvc, _ := newMigrationService(t, db)
	ctx := context.Background()
	_, projectID := seedProject(t, db)

	seedRegisteredPack(t, ctx, schemasSvc, projectID, "Existing Pack", widgetTypes(map[string]any{
		"name": map[string]any{"type": "string"},
	}))

	packName := "Incoming Pack"

	t.Run("preview populates type conflicts", func(t *testing.T) {
		name := uniqueName("import-preview")
		archive := buildTarGz(t, []tarEntry{{
			name: "repo-HEAD/blueprint.json",
			body: importBlueprintJSON(t, name, "1.0.0", mergedPackManifest(packName, "1.0.0")),
		}})
		ts := servingTarball(t, archive)
		svc.fetcher = testGitHubFetcher(t, ts.URL, 1<<20, 1<<20, 100)

		h := NewHandler(svc)
		body, err := json.Marshal(ImportGitHubRequest{URL: "https://github.com/acme/repo"})
		require.NoError(t, err)
		c, rec := newAuthzCtx(t, http.MethodPost, "/api/blueprints/import/preview", body,
			&auth.AuthUser{ID: uuid.NewString(), ProjectID: projectID})
		require.NoError(t, h.PreviewImport(c))
		require.Equal(t, http.StatusOK, rec.Code)

		var preview BlueprintPreview
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &preview))
		require.Len(t, preview.TypeConflicts, 1)
		assert.Equal(t, "Widget", preview.TypeConflicts[0].TypeName)
		assert.Equal(t, []string{"email"}, preview.TypeConflicts[0].AddedProperties)
	})

	t.Run("apply import blocked leaves no blueprint row", func(t *testing.T) {
		name := uniqueName("import-blocked")
		archive := buildTarGz(t, []tarEntry{{
			name: "repo-HEAD/blueprint.json",
			body: importBlueprintJSON(t, name, "1.0.0", mergedPackManifest(packName, "1.0.0")),
		}})
		ts := servingTarball(t, archive)
		svc.fetcher = testGitHubFetcher(t, ts.URL, 1<<20, 1<<20, 100)

		_, err := svc.ImportFromGitHub(ctx, projectID, &ImportGitHubRequest{
			URL:   "https://github.com/acme/repo",
			Apply: true,
		})
		require.Error(t, err)
		var apErr *apperror.Error
		require.True(t, errors.As(err, &apErr))
		assert.Equal(t, apperror.ErrConflict.Code, apErr.Code)

		_, getErr := svc.repo.GetByNameVersion(ctx, projectID, name, "1.0.0")
		require.Error(t, getErr, "failed atomic import must leave no blueprint row")
	})

	t.Run("apply import with merge succeeds", func(t *testing.T) {
		name := uniqueName("import-merge")
		archive := buildTarGz(t, []tarEntry{{
			name: "repo-HEAD/blueprint.json",
			body: importBlueprintJSON(t, name, "1.0.0", mergedPackManifest(packName, "1.0.0")),
		}})
		ts := servingTarball(t, archive)
		svc.fetcher = testGitHubFetcher(t, ts.URL, 1<<20, 1<<20, 100)

		bp, err := svc.ImportFromGitHub(ctx, projectID, &ImportGitHubRequest{
			URL:                "https://github.com/acme/repo",
			Apply:              true,
			MergeTypeConflicts: true,
		})
		require.NoError(t, err)
		require.Equal(t, name, bp.Name)
	})
}

func TestBlueprintUpdateMergeConflict(t *testing.T) {
	db := connectTestDB(t)
	svc, schemasSvc, _ := newMigrationService(t, db)
	ctx := context.Background()

	newProject := func() string {
		_, projectID := seedProject(t, db)
		seedRegisteredPack(t, ctx, schemasSvc, projectID, "Existing Pack", widgetTypes(map[string]any{
			"name": map[string]any{"type": "string"},
		}))
		return projectID
	}

	upstream := func(name string) *httptest.Server {
		archive := buildTarGz(t, []tarEntry{{
			name: "repo-HEAD/blueprint.json",
			body: importBlueprintJSON(t, name, "2.0.0", mergedPackManifest("Incoming Pack", "1.0.0")),
		}})
		return servingTarball(t, archive)
	}

	t.Run("preview reports conflicts without creating a version", func(t *testing.T) {
		projectID := newProject()
		name := uniqueName("update-preview")
		seedAppliedGitHubBlueprint(t, svc, projectID, name, "1.0.0", "hi")
		ts := upstream(name)
		svc.fetcher = testGitHubFetcher(t, ts.URL, 1<<20, 1<<20, 100)

		preview, err := svc.PreviewBlueprintUpdate(ctx, projectID, name)
		require.NoError(t, err)
		assert.Equal(t, name, preview.BlueprintName)
		assert.Equal(t, "1.0.0", preview.CurrentVersion)
		assert.Equal(t, "2.0.0", preview.LatestVersion)
		assert.True(t, preview.RequiresMerge)
		require.Len(t, preview.Conflicts, 1)
		assert.Equal(t, "Widget", preview.Conflicts[0].TypeName)

		_, getErr := svc.repo.GetByNameVersion(ctx, projectID, name, "2.0.0")
		require.Error(t, getErr, "preview must not create a new blueprint version")
	})

	t.Run("update job fails immediately with conflict details", func(t *testing.T) {
		projectID := newProject()
		name := uniqueName("update-blocked")
		seedAppliedGitHubBlueprint(t, svc, projectID, name, "1.0.0", "hi")
		ts := upstream(name)
		svc.fetcher = testGitHubFetcher(t, ts.URL, 1<<20, 1<<20, 100)

		job, err := svc.EnqueueBlueprintUpdateWithOptions(ctx, projectID, name, UpdateJobOptions{})
		require.NoError(t, err)
		require.NoError(t, svc.RunUpdateJobs(ctx))

		got, err := svc.repo.GetUpdateJob(ctx, job.ID)
		require.NoError(t, err)
		assert.Equal(t, UpdateJobStatusFailed, got.Status)
		assert.Equal(t, 1, got.Attempts, "conflict must fail immediately, not consume retries")
		assert.Contains(t, got.LastError, "object type name conflicts require merge confirmation")
		assert.Contains(t, string(got.Result), `"conflicts"`)
		assert.Contains(t, string(got.Result), `"Widget"`)

		applied, err := svc.repo.ListApplied(ctx, projectID)
		require.NoError(t, err)
		require.Len(t, applied, 1)
		assert.Equal(t, "1.0.0", applied[0].Version, "applied version must be unchanged")

		// A blocked update is a true no-op: the rejected upstream version must
		// not have been created or published.
		_, getErr := svc.repo.GetByNameVersion(ctx, projectID, name, "2.0.0")
		require.Error(t, getErr, "blocked update must not create the 2.0.0 blueprint version")
	})

	t.Run("merge-flagged update job applies", func(t *testing.T) {
		projectID := newProject()
		name := uniqueName("update-merge")
		seedAppliedGitHubBlueprint(t, svc, projectID, name, "1.0.0", "hi")
		ts := upstream(name)
		svc.fetcher = testGitHubFetcher(t, ts.URL, 1<<20, 1<<20, 100)

		job, err := svc.EnqueueBlueprintUpdateWithOptions(ctx, projectID, name, UpdateJobOptions{MergeTypeConflicts: true})
		require.NoError(t, err)
		require.True(t, job.MergeTypeConflicts)
		require.NoError(t, svc.RunUpdateJobs(ctx))

		got, err := svc.repo.GetUpdateJob(ctx, job.ID)
		require.NoError(t, err)
		assert.Equal(t, UpdateJobStatusSucceeded, got.Status)
		var outcome struct {
			Applied bool   `json:"applied"`
			Version string `json:"version"`
		}
		require.NoError(t, json.Unmarshal(got.Result, &outcome))
		assert.True(t, outcome.Applied)
		assert.Equal(t, "2.0.0", outcome.Version)

		applied, err := svc.repo.ListApplied(ctx, projectID)
		require.NoError(t, err)
		require.Len(t, applied, 1)
		assert.Equal(t, "2.0.0", applied[0].Version)
	})
}

func TestHandler_PreviewBlueprintUpdates(t *testing.T) {
	db := connectTestDB(t)
	svc, schemasSvc, _ := newMigrationService(t, db)
	ctx := context.Background()
	_, projectID := seedProject(t, db)

	seedRegisteredPack(t, ctx, schemasSvc, projectID, "Existing Pack", widgetTypes(map[string]any{
		"name": map[string]any{"type": "string"},
	}))
	name := uniqueName("handler-update-preview")
	seedAppliedGitHubBlueprint(t, svc, projectID, name, "1.0.0", "hi")

	archive := buildTarGz(t, []tarEntry{{
		name: "repo-HEAD/blueprint.json",
		body: importBlueprintJSON(t, name, "2.0.0", mergedPackManifest("Incoming Pack", "1.0.0")),
	}})
	ts := servingTarball(t, archive)
	svc.fetcher = testGitHubFetcher(t, ts.URL, 1<<20, 1<<20, 100)

	h := NewHandler(svc)
	c, rec := newAuthzCtx(t, http.MethodPost, "/api/blueprints/"+name+"/updates/preview", nil,
		&auth.AuthUser{ID: uuid.NewString(), ProjectID: projectID})
	c.SetParamNames("name")
	c.SetParamValues(name)

	require.NoError(t, h.PreviewBlueprintUpdates(c))
	require.Equal(t, http.StatusOK, rec.Code)

	var preview BlueprintUpdatePreview
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &preview))
	assert.Equal(t, "1.0.0", preview.CurrentVersion)
	assert.Equal(t, "2.0.0", preview.LatestVersion)
	assert.True(t, preview.RequiresMerge)
	require.Len(t, preview.Conflicts, 1)
}
