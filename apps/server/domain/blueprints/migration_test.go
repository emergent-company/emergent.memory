package blueprints

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"

	"github.com/emergent-company/emergent.memory/domain/agents"
	"github.com/emergent-company/emergent.memory/domain/graph"
	"github.com/emergent-company/emergent.memory/domain/schemas"
	"github.com/emergent-company/emergent.memory/domain/skills"
	"github.com/emergent-company/emergent.memory/internal/config"
	"github.com/google/uuid"
)

// newMigrationService wires a full blueprints + schemas + graph stack so the
// apply → AssignPack → migration path can be exercised end-to-end. The graph
// service uses the real schema/inverse-type providers but nil embedding/enqueue
// deps (unused by the migration path).
func newMigrationService(t *testing.T, db *bun.DB) (*Service, *schemas.Service, *schemas.Repository) {
	t.Helper()
	log := testLogger()

	cfg := &config.Config{}
	cfg.Graph.MaxListLimit = 1000

	graphRepo := graph.NewRepository(db, log, cfg)
	graphSvc := graph.NewService(graphRepo, log,
		graph.ProvideSchemaProvider(db, log),
		graph.ProvideInverseTypeProvider(db, log),
		nil, nil, nil, nil, nil, nil)

	schemasRepo := schemas.NewRepository(db, log)
	schemasSvc := schemas.NewService(schemasRepo, graphSvc, log, cfg)

	repo := NewRepository(db, log)
	skillsRepo := skills.NewRepository(db, log)
	svc := NewService(ServiceParams{Repo: repo, SchemasSvc: schemasSvc, SchemasRepo: schemasRepo, SkillsRepo: skillsRepo, GraphSvc: graphSvc, Log: log})
	svc.agentRepo = agents.NewRepository(db)
	return svc, schemasSvc, schemasRepo
}

// createDraftBlueprint creates a draft blueprint row with the given manifest.
func createDraftBlueprint(t *testing.T, db *bun.DB, name, version string, manifest json.RawMessage) *Blueprint {
	t.Helper()
	bp := testBlueprint(name, version)
	bp.Manifest = manifest
	require.NoError(t, NewRepository(db, testLogger()).Create(context.Background(), bp))
	return bp
}

// TestApplyUpgradeMigration verifies the full blueprint upgrade path through
// the API (not the UI): apply v1 (type "person" + template-hint description),
// then apply v2 (type "Person" + migration hints + clean description), and
// assert the migration runs and deactivates v1 so the installed list shows only
// v2 with the clean description.
func TestApplyUpgradeMigration(t *testing.T) {
	db := connectTestDB(t)
	svc, schemasSvc, schemasRepo := newMigrationService(t, db)
	ctx := context.Background()

	_, projectID := seedProject(t, db)
	name := uniqueName("upgrade")

	v1 := json.RawMessage(mustJSON(t, BlueprintManifest{
		Packs: []PackManifest{{
			Name:    name,
			Version: "1.0.0",
			ObjectTypes: []ObjectTypeDef{{
				Name:        "person",
				Label:       "Person",
				Description: "A person. Key: {source}:{source_id}. Labels: source:{src}, email:{email}.",
				Properties:  map[string]any{},
			}},
		}},
	}))
	v2 := json.RawMessage(mustJSON(t, BlueprintManifest{
		Packs: []PackManifest{{
			Name:        name,
			Version:     "2.0.0",
			Description: "clean",
			ObjectTypes: []ObjectTypeDef{{
				Name:        "Person",
				Label:       "Person",
				Description: "A person.",
				Properties:  map[string]any{},
			}},
			Migrations: &schemas.SchemaMigrationHints{
				FromVersion: "1.0.0",
				TypeRenames: []schemas.TypeRename{{From: "person", To: "Person"}},
			},
		}},
	}))

	bp1 := createDraftBlueprint(t, db, name, "1.0.0", v1)
	bp2 := createDraftBlueprint(t, db, name, "2.0.0", v2)
	userID := uuid.NewString()

	// Apply v1.
	if _, err := svc.Apply(ctx, bp1.ID, projectID, userID, ApplyOptions{}); err != nil {
		t.Fatalf("apply v1: %v", err)
	}

	// Apply v2 (upgrade). This syncs the pack definition and enqueues an async
	// migration job.
	if _, err := svc.Apply(ctx, bp2.ID, projectID, userID, ApplyOptions{}); err != nil {
		t.Fatalf("apply v2: %v", err)
	}

	// v2's pack must carry the clean description (no template hints).
	v2pack, err := schemasRepo.GetPackByNameVersion(ctx, name, "2.0.0")
	require.NoError(t, err)
	assert.NotContains(t, string(v2pack.ObjectTypeSchemas), "Key: {source}",
		"v2 pack description must not contain template hints")

	// Start the migration worker so it processes the pending job (migrate +
	// auto-uninstall the from_version).
	worker := schemas.NewSchemaMigrationJobWorker(schemasSvc, testLogger())
	require.NoError(t, worker.Start(ctx))
	defer func() { _ = worker.Stop(ctx) }()

	// Poll until v1's assignment is deactivated (removed from the installed list).
	deadline := time.Now().Add(20 * time.Second)
	for {
		installed, err := schemasRepo.GetInstalledPacks(ctx, projectID)
		require.NoError(t, err)
		v1Active := false
		v2Active := false
		for _, it := range installed {
			if it.Name == name {
				if it.Version == "1.0.0" {
					v1Active = true
				}
				if it.Version == "2.0.0" {
					v2Active = true
				}
			}
		}
		if v2Active && !v1Active {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("migration did not deactivate v1: installed=%+v", installed)
		}
		time.Sleep(200 * time.Millisecond)
	}
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	require.NoError(t, err)
	return string(b)
}
