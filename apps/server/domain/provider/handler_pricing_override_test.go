package provider_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/domain/provider"
	"github.com/emergent-company/emergent.memory/internal/config"
	"github.com/emergent-company/emergent.memory/internal/testutil"
	"github.com/emergent-company/emergent.memory/pkg/apperror"
	"github.com/emergent-company/emergent.memory/pkg/auth"
)

// newPricingOverrideHandler builds a Handler wired to a real repository and
// credential service backed by the test database.
func newPricingOverrideHandler(t *testing.T) (*provider.Handler, *testutil.TestDB, string, string, string) {
	t.Helper()

	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	ctx := context.Background()
	testDB := testutil.SetupTestDBOrFail(t, ctx, "pricingoverride")

	orgA := uuid.New().String()
	projectA := uuid.New().String()
	orgB := uuid.New().String()
	projectB := uuid.New().String()

	require.NoError(t, testutil.CreateTestOrganization(ctx, testDB.GetDB(), orgA, "Org A"))
	require.NoError(t, testutil.CreateTestProject(ctx, testDB.GetDB(), testutil.TestProject{
		ID:    projectA,
		OrgID: orgA,
		Name:  "Project A",
	}, testutil.AdminUser.ID))
	require.NoError(t, testutil.CreateTestOrganization(ctx, testDB.GetDB(), orgB, "Org B"))
	require.NoError(t, testutil.CreateTestProject(ctx, testDB.GetDB(), testutil.TestProject{
		ID:    projectB,
		OrgID: orgB,
		Name:  "Project B",
	}, testutil.AdminUser.ID))

	repo := provider.NewRepository(testDB.GetDB(), slog.Default())
	credSvc := provider.NewCredentialService(repo, provider.NewRegistry(), nil, &config.Config{}, slog.Default())
	h := provider.NewHandler(credSvc, nil, repo)

	return h, testDB, orgA, projectA, projectB
}

// newOverrideContext builds an echo context for a caller scoped to orgID.
func newOverrideContext(t *testing.T, e *echo.Echo, method, target, orgID string, body any) (echo.Context, *httptest.ResponseRecorder) {
	t.Helper()

	var reader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		require.NoError(t, err)
		reader = bytes.NewReader(b)
	}
	req := httptest.NewRequest(method, target, reader)
	if body != nil {
		req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	}
	req = req.WithContext(auth.ContextWithOrgID(req.Context(), orgID))
	rec := httptest.NewRecorder()
	return e.NewContext(req, rec), rec
}

func TestPricingOverrides_UpsertReturnsEntry(t *testing.T) {
	h, testDB, orgA, projectA, _ := newPricingOverrideHandler(t)
	defer testDB.Close()

	e := echo.New()
	c, rec := newOverrideContext(t, e, http.MethodPut, "/api/v1/projects/"+projectA+"/pricing-overrides", orgA,
		provider.UpsertProjectPricingOverridesRequest{
			Provider:        provider.ProviderDeepSeek,
			Model:           "deepseek-v4-pro",
			TextInputPrice:  1.74,
			ImageInputPrice: 2.00,
			VideoInputPrice: 3.00,
			AudioInputPrice: 4.00,
			OutputPrice:     3.48,
		})
	c.SetParamNames("projectId")
	c.SetParamValues(projectA)

	require.NoError(t, h.UpsertProjectPricingOverrides(c))
	assert.Equal(t, http.StatusOK, rec.Code)

	var got provider.ProjectCustomPricing
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	assert.Equal(t, projectA, got.ProjectID)
	assert.Equal(t, provider.ProviderDeepSeek, got.Provider)
	assert.Equal(t, "deepseek-v4-pro", got.Model)
	assert.Equal(t, 1.74, got.TextInputPrice)
	assert.Equal(t, 2.00, got.ImageInputPrice)
	assert.Equal(t, 3.00, got.VideoInputPrice)
	assert.Equal(t, 4.00, got.AudioInputPrice)
	assert.Equal(t, 3.48, got.OutputPrice)
	assert.NotEmpty(t, got.ID)
}

func TestPricingOverrides_UpsertMissingProjectIDBadRequest(t *testing.T) {
	h, testDB, orgA, _, _ := newPricingOverrideHandler(t)
	defer testDB.Close()

	e := echo.New()
	c, _ := newOverrideContext(t, e, http.MethodPut, "/api/v1/projects//pricing-overrides", orgA,
		provider.UpsertProjectPricingOverridesRequest{Provider: provider.ProviderOpenAI, Model: "gpt-4o"})
	c.SetParamNames("projectId")
	c.SetParamValues("")

	err := h.UpsertProjectPricingOverrides(c)
	require.Error(t, err)
	appErr, ok := err.(*apperror.Error)
	require.True(t, ok, "expected *apperror.Error, got %T", err)
	assert.Equal(t, http.StatusBadRequest, appErr.HTTPStatus)
}

func TestPricingOverrides_ListReturnsOverrides(t *testing.T) {
	h, testDB, orgA, projectA, _ := newPricingOverrideHandler(t)
	defer testDB.Close()

	// Seed two overrides directly via the repository.
	repo := provider.NewRepository(testDB.GetDB(), slog.Default())
	ctx := context.Background()
	require.NoError(t, repo.UpsertProjectCustomPricing(ctx, &provider.ProjectCustomPricing{
		ProjectID:   projectA,
		Provider:    provider.ProviderDeepSeek,
		Model:       "deepseek-v4-pro",
		OutputPrice: 3.48,
	}))
	require.NoError(t, repo.UpsertProjectCustomPricing(ctx, &provider.ProjectCustomPricing{
		ProjectID:   projectA,
		Provider:    provider.ProviderOpenAI,
		Model:       "gpt-4o",
		OutputPrice: 10.00,
	}))

	e := echo.New()
	c, rec := newOverrideContext(t, e, http.MethodGet, "/api/v1/projects/"+projectA+"/pricing-overrides", orgA, nil)
	c.SetParamNames("projectId")
	c.SetParamValues(projectA)

	require.NoError(t, h.ListProjectPricingOverrides(c))
	assert.Equal(t, http.StatusOK, rec.Code)

	var got []provider.ProjectCustomPricing
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	require.Len(t, got, 2)
	assert.Equal(t, provider.ProviderDeepSeek, got[0].Provider)
	assert.Equal(t, provider.ProviderOpenAI, got[1].Provider)
}

func TestPricingOverrides_DeleteRemovesOverride(t *testing.T) {
	h, testDB, orgA, projectA, _ := newPricingOverrideHandler(t)
	defer testDB.Close()

	repo := provider.NewRepository(testDB.GetDB(), slog.Default())
	ctx := context.Background()
	require.NoError(t, repo.UpsertProjectCustomPricing(ctx, &provider.ProjectCustomPricing{
		ProjectID:   projectA,
		Provider:    provider.ProviderDeepSeek,
		Model:       "deepseek-v4-pro",
		OutputPrice: 3.48,
	}))

	e := echo.New()
	c, rec := newOverrideContext(t, e, http.MethodDelete,
		"/api/v1/projects/"+projectA+"/pricing-overrides/deepseek/deepseek-v4-pro", orgA, nil)
	c.SetParamNames("projectId", "provider", "model")
	c.SetParamValues(projectA, "deepseek", "deepseek-v4-pro")

	require.NoError(t, h.DeleteProjectPricingOverride(c))
	assert.Equal(t, http.StatusOK, rec.Code)

	got, err := repo.GetProjectCustomPricing(ctx, projectA, provider.ProviderDeepSeek, "deepseek-v4-pro")
	require.NoError(t, err)
	assert.Nil(t, got, "override must be removed after delete")
}

func TestPricingOverrides_CrossProjectForbidden(t *testing.T) {
	h, testDB, orgA, _, projectB := newPricingOverrideHandler(t)
	defer testDB.Close()

	e := echo.New()
	// Caller is scoped to orgA but targets projectB (orgB) — must be forbidden.
	c, _ := newOverrideContext(t, e, http.MethodPut,
		"/api/v1/projects/"+projectB+"/pricing-overrides", orgA,
		provider.UpsertProjectPricingOverridesRequest{Provider: provider.ProviderOpenAI, Model: "gpt-4o", OutputPrice: 10.00})
	c.SetParamNames("projectId")
	c.SetParamValues(projectB)

	err := h.UpsertProjectPricingOverrides(c)
	require.Error(t, err)
	appErr, ok := err.(*apperror.Error)
	require.True(t, ok, "expected *apperror.Error, got %T", err)
	assert.Equal(t, http.StatusForbidden, appErr.HTTPStatus)

	// The override must NOT have been persisted.
	repo := provider.NewRepository(testDB.GetDB(), slog.Default())
	got, getErr := repo.GetProjectCustomPricing(context.Background(), projectB, provider.ProviderOpenAI, "gpt-4o")
	require.NoError(t, getErr)
	assert.Nil(t, got, "cross-project upsert must not modify pricing")
}

func TestPricingOverrides_UnknownProjectNotFound(t *testing.T) {
	h, testDB, orgA, _, _ := newPricingOverrideHandler(t)
	defer testDB.Close()

	e := echo.New()
	unknownProject := uuid.New().String()
	c, _ := newOverrideContext(t, e, http.MethodGet,
		"/api/v1/projects/"+unknownProject+"/pricing-overrides", orgA, nil)
	c.SetParamNames("projectId")
	c.SetParamValues(unknownProject)

	err := h.ListProjectPricingOverrides(c)
	require.Error(t, err)
	appErr, ok := err.(*apperror.Error)
	require.True(t, ok, "expected *apperror.Error, got %T", err)
	assert.Equal(t, http.StatusNotFound, appErr.HTTPStatus)
}
