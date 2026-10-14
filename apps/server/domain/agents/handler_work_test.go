package agents

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/pkg/apperror"
)

// TestUpdateWorkItemStatusHandler_MalformedBody pins the bind/route sliver of
// the set-status endpoint: a malformed JSON body is rejected with a 400 before
// the work-action service is ever reached (the handler has no workActions).
func TestUpdateWorkItemStatusHandler_MalformedBody(t *testing.T) {
	h := &Handler{}

	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"status":`))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("projectId", "canonicalId")
	c.SetParamValues("p1", "w1")

	err := h.UpdateWorkItemStatus(c)
	require.Error(t, err)
	var appErr *apperror.Error
	require.ErrorAs(t, err, &appErr)
	require.Equal(t, http.StatusBadRequest, appErr.HTTPStatus)
}

// TestUpdateWorkItemStatusHandler_SetsStatus pins the happy path of the
// set-status endpoint: a valid body is forwarded to the work-action service and
// the item transitions to the posted status with a 200 response.
func TestUpdateWorkItemStatusHandler_SetsStatus(t *testing.T) {
	ctx, _, repo, gSvc, projectID := setupWorkActions(t)

	canonicalID := createWorkObject(t, ctx, gSvc, projectID, "k-handler-status", "in_progress", "researcher")

	h := &Handler{}
	h.WithWorkActionService(newWorkActions(repo, gSvc))

	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"status":"done"}`))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("projectId", "canonicalId")
	c.SetParamValues(projectID, canonicalID)

	require.NoError(t, h.UpdateWorkItemStatus(c))
	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "done", workHeadStatus(t, ctx, gSvc, projectID, canonicalID))
}
