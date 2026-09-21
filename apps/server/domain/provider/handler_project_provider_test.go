package provider_test

import (
	"net/http"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/domain/provider"
	"github.com/emergent-company/emergent.memory/pkg/apperror"
)

// TestProjectProvider_RejectsCrossOrgProject verifies that a caller in org A
// cannot trigger an outbound provider test against a project owned by org B:
// the handler must reject with 403 before resolving (and spending) org B's
// stored credentials.
func TestProjectProvider_RejectsCrossOrgProject(t *testing.T) {
	h, testDB, orgA, _, projectB := newPricingOverrideHandler(t)
	defer testDB.Close()

	e := echo.New()
	c, _ := newOverrideContext(t, e, http.MethodPost, "/api/v1/projects/"+projectB+"/providers/deepseek/test", orgA, nil)
	c.SetParamNames("projectId", "provider")
	c.SetParamValues(projectB, "deepseek")

	err := h.TestProjectProvider(c)
	require.Error(t, err)
	appErr, ok := err.(*apperror.Error)
	require.True(t, ok, "expected *apperror.Error, got %T", err)
	assert.Equal(t, http.StatusForbidden, appErr.HTTPStatus)
}

// TestProjectProvider_AllowsOwnProject verifies that a caller in org A testing
// its own project is not blocked by the ownership guard (it proceeds to
// credential resolution, which reports 400 here because no credentials are
// configured for the test project).
func TestProjectProvider_AllowsOwnProject(t *testing.T) {
	h, testDB, orgA, projectA, _ := newPricingOverrideHandler(t)
	defer testDB.Close()

	e := echo.New()
	c, _ := newOverrideContext(t, e, http.MethodPost, "/api/v1/projects/"+projectA+"/providers/deepseek/test", orgA, nil)
	c.SetParamNames("projectId", "provider")
	c.SetParamValues(projectA, "deepseek")

	err := h.TestProjectProvider(c)
	require.Error(t, err)
	appErr, ok := err.(*apperror.Error)
	require.True(t, ok, "expected *apperror.Error, got %T", err)
	assert.Equal(t, http.StatusBadRequest, appErr.HTTPStatus,
		"own project must pass ownership check and fail later at credential resolution")
}

// TestProvider_RejectsCrossOrgProject verifies the org-level provider test
// endpoint also enforces project ownership when a projectId query param is
// supplied, so a caller cannot spend another org's credentials via query params.
func TestProvider_RejectsCrossOrgProject(t *testing.T) {
	h, testDB, orgA, _, projectB := newPricingOverrideHandler(t)
	defer testDB.Close()

	e := echo.New()
	c, _ := newOverrideContext(t, e, http.MethodPost, "/api/v1/providers/deepseek/test?projectId="+projectB, orgA, nil)
	c.SetParamNames("provider")
	c.SetParamValues(string(provider.ProviderDeepSeek))

	err := h.TestProvider(c)
	require.Error(t, err)
	appErr, ok := err.(*apperror.Error)
	require.True(t, ok, "expected *apperror.Error, got %T", err)
	assert.Equal(t, http.StatusForbidden, appErr.HTTPStatus)
}
