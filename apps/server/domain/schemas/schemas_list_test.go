package schemas_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/domain/schemas"
	"github.com/emergent-company/emergent.memory/pkg/apperror"
)

// TestListPacksMissingProjectID verifies the ListPacks handler rejects requests
// without a projectId path param before it touches the service layer.
func TestListPacksMissingProjectID(t *testing.T) {
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("projectId")
	c.SetParamValues("")

	h := schemas.NewHandler(nil) // svc never reached for a missing param
	err := h.ListPacks(c)
	require.Error(t, err)

	var aerr *apperror.Error
	require.ErrorAs(t, err, &aerr)
	assert.Equal(t, http.StatusBadRequest, aerr.HTTPStatus)
	assert.Equal(t, "bad_request", aerr.Code)
	assert.Equal(t, "projectId is required", aerr.Message)
}
