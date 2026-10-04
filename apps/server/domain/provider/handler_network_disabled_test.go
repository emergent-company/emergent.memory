package provider_test

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/domain/provider"
	"github.com/emergent-company/emergent.memory/pkg/adk"
	"github.com/emergent-company/emergent.memory/pkg/apperror"
)

// TestTestProvider_NetworkDisabled_ReturnsBadRequest verifies the handler path
// surfaces LLM_NETWORK_DISABLED as a normal 400 before resolving credentials.
//
// creds and repo are deliberately nil: the kill-switch guard must short-circuit
// before either is touched, so a nil dereference or timeout would fail the test
// if the guard were mutated away. That is itself the "no credentials resolved,
// no outbound request issued" assertion.
func TestTestProvider_NetworkDisabled_ReturnsBadRequest(t *testing.T) {
	catalog := provider.NewModelCatalogService(
		nil,
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		provider.WithNetworkDisabled(true),
	)
	h := provider.NewHandler(nil, catalog, nil, nil)

	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/providers/deepseek/test", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("provider")
	c.SetParamValues(string(provider.ProviderDeepSeek))

	err := h.TestProvider(c)
	require.Error(t, err)
	appErr, ok := err.(*apperror.Error)
	require.True(t, ok, "expected *apperror.Error, got %T", err)
	assert.Equal(t, http.StatusBadRequest, appErr.HTTPStatus)
	assert.Contains(t, appErr.Message, adk.ErrNetworkDisabled.Error())
}
