package provider_test

import (
	"context"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/domain/provider"
	"github.com/emergent-company/emergent.memory/internal/testutil"
)

// TestProviderPricing_ListPricing exercises Repository.ListPricing against the
// DB test harness: empty table returns an empty non-nil slice, populated rows
// come back ordered by provider then model.
func TestProviderPricing_ListPricing(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	ctx := context.Background()
	testDB := testutil.SetupTestDBOrFail(t, ctx, "listpricing")
	defer testDB.Close()

	repo := provider.NewRepository(testDB.GetDB(), slog.Default())

	t.Run("empty table returns empty non-nil slice", func(t *testing.T) {
		rows, err := repo.ListPricing(ctx)
		require.NoError(t, err)
		assert.NotNil(t, rows)
		assert.Empty(t, rows)
	})

	t.Run("populated table returns rows ordered by provider then model", func(t *testing.T) {
		// Deliberately inserted out of order.
		require.NoError(t, repo.UpsertPricing(ctx, []provider.ProviderPricing{
			{Provider: provider.ProviderGoogleAI, Model: "gemini-2.5-flash", TextInputPrice: 0.15, OutputPrice: 0.60},
			{Provider: provider.ProviderVertexAI, Model: "gemini-2.5-flash", TextInputPrice: 0.15, OutputPrice: 0.60},
			{Provider: provider.ProviderDeepSeek, Model: "deepseek-v4-pro", TextInputPrice: 1.74, OutputPrice: 3.48},
			{Provider: provider.ProviderGoogleAI, Model: "gemini-1.5-flash", TextInputPrice: 0.075, OutputPrice: 0.30},
			{Provider: provider.ProviderDeepSeek, Model: "deepseek-chat", TextInputPrice: 0.28, OutputPrice: 0.42},
		}))

		rows, err := repo.ListPricing(ctx)
		require.NoError(t, err)
		require.Len(t, rows, 5)

		// Order: deepseek < google < google-vertex (provider), then model ASC.
		wantOrder := []struct {
			provider provider.ProviderType
			model    string
		}{
			{provider.ProviderDeepSeek, "deepseek-chat"},
			{provider.ProviderDeepSeek, "deepseek-v4-pro"},
			{provider.ProviderGoogleAI, "gemini-1.5-flash"},
			{provider.ProviderGoogleAI, "gemini-2.5-flash"},
			{provider.ProviderVertexAI, "gemini-2.5-flash"},
		}
		for i, want := range wantOrder {
			assert.Equalf(t, want.provider, rows[i].Provider, "rows[%d].Provider", i)
			assert.Equalf(t, want.model, rows[i].Model, "rows[%d].Model", i)
		}
		assert.Equal(t, 1.74, rows[1].TextInputPrice)
		assert.Equal(t, 3.48, rows[1].OutputPrice)
		assert.False(t, rows[0].LastSynced.IsZero(), "last_synced should be populated")
	})
}
