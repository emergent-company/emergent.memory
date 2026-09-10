package orgs

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/pkg/apperror"
)

// runUpdateHandler invokes the Update handler directly with the given org id and
// JSON body, returning the recorded response and the handler's error.
func runUpdateHandler(t *testing.T, repo orgRepository, id, body string) (*httptest.ResponseRecorder, error) {
	t.Helper()

	e := echo.New()
	req := httptest.NewRequest(http.MethodPatch, "/api/orgs/"+id, bytes.NewReader([]byte(body)))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("id")
	c.SetParamValues(id)

	h := NewHandler(testOrgService(repo))
	return rec, h.Update(c)
}

func TestHandlerUpdate_Success(t *testing.T) {
	repo := &fakeOrgRepo{org: &Org{ID: "org-1", Name: "Renamed"}}
	rec, err := runUpdateHandler(t, repo, "org-1", `{"name":"Renamed"}`)

	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)

	var got OrgDTO
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	assert.Equal(t, OrgDTO{ID: "org-1", Name: "Renamed"}, got)
	assert.Equal(t, 1, repo.updateCalls)
}

func TestHandlerUpdate_InvalidName(t *testing.T) {
	repo := &fakeOrgRepo{}
	_, err := runUpdateHandler(t, repo, "org-1", `{"name":""}`)

	require.Error(t, err)
	var appErr *apperror.Error
	require.ErrorAs(t, err, &appErr)
	assert.Equal(t, 400, appErr.HTTPStatus)
	assert.Zero(t, repo.updateCalls)
}

func TestHandlerUpdate_UnknownID(t *testing.T) {
	repo := &fakeOrgRepo{err: apperror.ErrNotFound.WithMessage("Organization not found")}
	_, err := runUpdateHandler(t, repo, "missing", `{"name":"New name"}`)

	require.Error(t, err)
	var appErr *apperror.Error
	require.ErrorAs(t, err, &appErr)
	assert.Equal(t, 404, appErr.HTTPStatus)
	assert.Equal(t, "missing", repo.updatedID)
}

func TestHandlerUpdate_MalformedBody(t *testing.T) {
	repo := &fakeOrgRepo{}
	_, err := runUpdateHandler(t, repo, "org-1", `{`)

	require.Error(t, err)
	var appErr *apperror.Error
	require.ErrorAs(t, err, &appErr)
	assert.Equal(t, 400, appErr.HTTPStatus)
}

// TestUpdateOrgRequest_Fields pins the request contract (name-only rename).
func TestUpdateOrgRequest_Fields(t *testing.T) {
	req := UpdateOrgRequest{Name: "New Name"}
	assert.Equal(t, "New Name", req.Name)
}
