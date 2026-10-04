package provider

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/internal/testdb"
)

// TestPruneStalePricing_DB exercises the real DELETE against the DB test
// harness, since service tests only exercise a fake and the ListPricing test
// never runs the delete path. It guards the data-deleting semantics:
//
//   - a managed-vendor row absent from keep is deleted;
//   - a baseline row present in keep survives;
//   - a non-managed vendor's row is untouched even when absent from keep.
//
// Runs under REQUIRE_DB=1 (CI's DB-backed job), where a missing database fails
// rather than silently skipping.
func TestPruneStalePricing_DB(t *testing.T) {
	if testing.Short() {
		testdb.SkipOrFatal(t, "skipping database integration test in short mode")
	}

	ctx := context.Background()
	tdb := testdb.SetupTestDBOrFail(t, ctx, "prunepricing")
	t.Cleanup(tdb.Close)

	repo := NewRepository(tdb.DB, slog.New(slog.NewTextHandler(io.Discard, nil)))

	managed := managedPricingProviders()
	require.Contains(t, managed, ProviderDeepSeek, "managed set must include deepseek")
	require.NotContains(t, managed, ProviderJina, "jina is intentionally unmanaged")

	// Seed three rows: a managed stale row (absent from keep), a baseline row
	// (present in keep), and a non-managed vendor row (absent from keep).
	require.NoError(t, repo.UpsertPricing(ctx, []ProviderPricing{
		{Provider: ProviderDeepSeek, Model: "stale-model", TextInputPrice: 9.99, OutputPrice: 9.99},
		{Provider: ProviderGoogleAI, Model: "gemini-2.5-flash", TextInputPrice: 0.15, OutputPrice: 0.60},
		{Provider: ProviderJina, Model: "jina-embeddings-v3", TextInputPrice: 0.02},
	}))

	// keep mirrors what Sync passes: the merged baseline+feed entries. The
	// Google AI baseline row is present; the stale DeepSeek model is not.
	pruned, err := repo.PruneStalePricing(ctx, managed, staticPricing)
	require.NoError(t, err)
	assert.Equal(t, int64(1), pruned, "only the absent managed row should be pruned")

	rows, err := repo.ListPricing(ctx)
	require.NoError(t, err)

	got := make(map[ProviderType][]string, len(rows))
	for _, r := range rows {
		got[r.Provider] = append(got[r.Provider], r.Model)
	}

	assert.NotContains(t, got[ProviderDeepSeek], "stale-model", "managed row absent from keep must be deleted")
	assert.Contains(t, got[ProviderGoogleAI], "gemini-2.5-flash", "baseline row present in keep must survive")
	assert.Contains(t, got[ProviderJina], "jina-embeddings-v3", "non-managed vendor row must be untouched")
}
