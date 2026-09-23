package provider_test

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/domain/provider"
	"github.com/emergent-company/emergent.memory/internal/config"
	"github.com/emergent-company/emergent.memory/internal/testutil"
)

// newPricingHandler builds a Handler wired to a real repository backed by a
// fresh test database.
func newPricingHandler(t *testing.T) (*provider.Handler, *testutil.TestDB) {
	t.Helper()

	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	ctx := context.Background()
	testDB := testutil.SetupTestDBOrFail(t, ctx, "listpricinghandler")

	repo := provider.NewRepository(testDB.GetDB(), slog.Default())
	credSvc := provider.NewCredentialService(repo, provider.NewRegistry(), nil, &config.Config{}, slog.Default())
	h := provider.NewHandler(credSvc, nil, repo)

	return h, testDB
}

func TestPricing_ListEmpty(t *testing.T) {
	h, testDB := newPricingHandler(t)
	defer testDB.Close()

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/pricing", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	require.NoError(t, h.ListPricing(c))
	assert.Equal(t, http.StatusOK, rec.Code)

	var rows []provider.ProviderPricing
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &rows))
	assert.NotNil(t, rows, "empty pricing table must serialize as [] not null")
	assert.Empty(t, rows)
}

func TestPricing_ListReturnsRows(t *testing.T) {
	h, testDB := newPricingHandler(t)
	defer testDB.Close()

	ctx := context.Background()
	repo := provider.NewRepository(testDB.GetDB(), slog.Default())
	require.NoError(t, repo.UpsertPricing(ctx, []provider.ProviderPricing{
		{Provider: provider.ProviderDeepSeek, Model: "deepseek-v4-pro", TextInputPrice: 1.74, OutputPrice: 3.48},
		{Provider: provider.ProviderOpenAI, Model: "gpt-4o", TextInputPrice: 2.50, OutputPrice: 10.00},
	}))

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/pricing", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	require.NoError(t, h.ListPricing(c))
	assert.Equal(t, http.StatusOK, rec.Code)

	var rows []provider.ProviderPricing
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &rows))
	require.Len(t, rows, 2)
	assert.Equal(t, provider.ProviderDeepSeek, rows[0].Provider)
	assert.Equal(t, "deepseek-v4-pro", rows[0].Model)
	assert.Equal(t, 1.74, rows[0].TextInputPrice)
	assert.Equal(t, 3.48, rows[0].OutputPrice)
	assert.Equal(t, provider.ProviderOpenAI, rows[1].Provider)
	assert.Equal(t, "gpt-4o", rows[1].Model)
	assert.Equal(t, 10.00, rows[1].OutputPrice)
}
