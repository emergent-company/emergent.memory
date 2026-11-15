package blueprints

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func writeMarketBlueprintDir(t *testing.T, name, skillName string) string {
	t.Helper()
	dir := t.TempDir()
	manifest := BlueprintManifest{
		Skills: []SkillManifest{{
			Name:        skillName,
			Description: "Installed from the market",
			Content:     "# market skill",
		}},
	}
	raw, err := json.Marshal(blueprintDescriptor{
		Name:              name,
		Version:           "1.0.0",
		Description:       "market blueprint",
		Author:            "Acme",
		BlueprintManifest: manifest,
	})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "blueprint.json"), raw, 0o644))
	return dir
}

func TestImportFromLocalDirRecordsMarketOriginAndSkillProvenance(t *testing.T) {
	db := connectTestDB(t)
	svc, _, _ := newMigrationService(t, db)
	ctx := context.Background()
	_, projectID := seedProject(t, db)

	name := uniqueName("market-local")
	skillName := "market-skill-" + uuid.NewString()[:8]
	dir := writeMarketBlueprintDir(t, name, skillName)
	sourceURL := "https://market.example/listings/demo"

	bp, err := svc.ImportFromLocalDir(ctx, projectID, &LocalImportRequest{
		Root:            dir,
		Apply:           true,
		Origin:          OriginMarket,
		SourceKind:      "market",
		SourceURL:       sourceURL,
		SkillProvenance: &SkillProvenance{Source: "marketplace", SourceURL: sourceURL},
	})
	require.NoError(t, err)
	assert.Equal(t, OriginMarket, bp.Origin)
	assert.Equal(t, sourceURL, bp.SourceURL)

	got, err := svc.GetBlueprint(ctx, projectID, bp.ID)
	require.NoError(t, err)
	assert.Equal(t, OriginMarket, got.Origin)

	skillsList, err := svc.skillsRepo.FindAll(ctx, nil, nil)
	require.NoError(t, err)
	var found bool
	for _, sk := range skillsList {
		if sk.Name != skillName {
			continue
		}
		found = true
		require.NotNil(t, sk.Metadata)
		assert.Equal(t, "marketplace", sk.Metadata.Source)
		assert.Equal(t, sourceURL, sk.Metadata.SourceURL)
	}
	assert.True(t, found, "created skill not found")

	// A repeat import of the same content is idempotent: same blueprint row,
	// no duplicate.
	again, err := svc.ImportFromLocalDir(ctx, projectID, &LocalImportRequest{
		Root:            dir,
		Apply:           true,
		Origin:          OriginMarket,
		SourceKind:      "market",
		SourceURL:       sourceURL,
		SkillProvenance: &SkillProvenance{Source: "marketplace", SourceURL: sourceURL},
	})
	require.NoError(t, err)
	assert.Equal(t, bp.ID, again.ID)
}
