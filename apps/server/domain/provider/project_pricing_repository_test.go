package provider_test

import (
	"context"
	"log/slog"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/domain/provider"
	"github.com/emergent-company/emergent.memory/internal/testutil"
)

// TestProjectCustomPricingRepository exercises the four project custom pricing
// repository methods against the DB test harness: upsert, get, list (ordered),
// upsert-replace, and delete.
func TestProjectCustomPricingRepository(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	ctx := context.Background()
	testDB := testutil.SetupTestDBOrFail(t, ctx, "pcustompricing")
	defer testDB.Close()

	// Create an org + project so the FK from project_custom_pricing to
	// kb.projects is satisfiable.
	orgID := uuid.New().String()
	projectID := uuid.New().String()
	require.NoError(t, testutil.CreateTestOrganization(ctx, testDB.GetDB(), orgID, "Pricing Test Org"))
	require.NoError(t, testutil.CreateTestProject(ctx, testDB.GetDB(), testutil.TestProject{
		ID:    projectID,
		OrgID: orgID,
		Name:  "Pricing Test Project",
	}, testutil.AdminUser.ID))

	repo := provider.NewRepository(testDB.GetDB(), slog.Default())

	t.Run("get missing returns nil", func(t *testing.T) {
		got, err := repo.GetProjectCustomPricing(ctx, projectID, provider.ProviderOpenAI, "gpt-4o")
		require.NoError(t, err)
		assert.Nil(t, got)
	})

	t.Run("upsert then get", func(t *testing.T) {
		entry := &provider.ProjectCustomPricing{
			ProjectID:       projectID,
			Provider:        provider.ProviderOpenAI,
			Model:           "gpt-4o",
			TextInputPrice:  2.50,
			ImageInputPrice: 5.00,
			OutputPrice:     10.00,
		}
		require.NoError(t, repo.UpsertProjectCustomPricing(ctx, entry))

		got, err := repo.GetProjectCustomPricing(ctx, projectID, provider.ProviderOpenAI, "gpt-4o")
		require.NoError(t, err)
		require.NotNil(t, got)
		assert.Equal(t, projectID, got.ProjectID)
		assert.Equal(t, provider.ProviderOpenAI, got.Provider)
		assert.Equal(t, "gpt-4o", got.Model)
		assert.Equal(t, 2.50, got.TextInputPrice)
		assert.Equal(t, 5.00, got.ImageInputPrice)
		assert.Equal(t, 0.0, got.VideoInputPrice)
		assert.Equal(t, 0.0, got.AudioInputPrice)
		assert.Equal(t, 10.00, got.OutputPrice)
		assert.NotEmpty(t, got.ID)
	})

	t.Run("upsert replaces on same key", func(t *testing.T) {
		entry := &provider.ProjectCustomPricing{
			ProjectID:   projectID,
			Provider:    provider.ProviderOpenAI,
			Model:       "gpt-4o",
			OutputPrice: 12.50,
		}
		require.NoError(t, repo.UpsertProjectCustomPricing(ctx, entry))

		got, err := repo.GetProjectCustomPricing(ctx, projectID, provider.ProviderOpenAI, "gpt-4o")
		require.NoError(t, err)
		require.NotNil(t, got)
		assert.Equal(t, 12.50, got.OutputPrice, "upsert must replace the previous price")
		assert.Equal(t, 0.0, got.TextInputPrice, "replaced entry must not carry stale prices")
	})

	t.Run("list is ordered by provider then model", func(t *testing.T) {
		// Two more overrides for the same project.
		require.NoError(t, repo.UpsertProjectCustomPricing(ctx, &provider.ProjectCustomPricing{
			ProjectID:   projectID,
			Provider:    provider.ProviderDeepSeek,
			Model:       "deepseek-v4-pro",
			OutputPrice: 3.48,
		}))
		require.NoError(t, repo.UpsertProjectCustomPricing(ctx, &provider.ProjectCustomPricing{
			ProjectID:   projectID,
			Provider:    provider.ProviderOpenAI,
			Model:       "gpt-4o-mini",
			OutputPrice: 0.60,
		}))

		entries, err := repo.ListProjectCustomPricing(ctx, projectID)
		require.NoError(t, err)
		require.Len(t, entries, 3)

		// deepseek < openai, and gpt-4o < gpt-4o-mini within openai.
		assert.Equal(t, provider.ProviderDeepSeek, entries[0].Provider)
		assert.Equal(t, provider.ProviderOpenAI, entries[1].Provider)
		assert.Equal(t, "gpt-4o", entries[1].Model)
		assert.Equal(t, provider.ProviderOpenAI, entries[2].Provider)
		assert.Equal(t, "gpt-4o-mini", entries[2].Model)
	})

	t.Run("list for other project is empty", func(t *testing.T) {
		otherOrg := uuid.New().String()
		otherProject := uuid.New().String()
		require.NoError(t, testutil.CreateTestOrganization(ctx, testDB.GetDB(), otherOrg, "Other Org"))
		require.NoError(t, testutil.CreateTestProject(ctx, testDB.GetDB(), testutil.TestProject{
			ID:    otherProject,
			OrgID: otherOrg,
			Name:  "Other Project",
		}, testutil.AdminUser.ID))

		entries, err := repo.ListProjectCustomPricing(ctx, otherProject)
		require.NoError(t, err)
		assert.Empty(t, entries, "overrides are project-scoped")
	})

	t.Run("delete removes override", func(t *testing.T) {
		require.NoError(t, repo.DeleteProjectCustomPricing(ctx, projectID, provider.ProviderDeepSeek, "deepseek-v4-pro"))

		got, err := repo.GetProjectCustomPricing(ctx, projectID, provider.ProviderDeepSeek, "deepseek-v4-pro")
		require.NoError(t, err)
		assert.Nil(t, got)

		// Deleting again is a no-op, not an error.
		require.NoError(t, repo.DeleteProjectCustomPricing(ctx, projectID, provider.ProviderDeepSeek, "deepseek-v4-pro"))
	})
}
