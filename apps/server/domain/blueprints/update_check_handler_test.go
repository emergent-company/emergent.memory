package blueprints

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/internal/config"
	"github.com/emergent-company/emergent.memory/pkg/apperror"
	"github.com/emergent-company/emergent.memory/pkg/auth"
	"github.com/emergent-company/emergent.memory/pkg/crypto"
)

// newUpdateHandler builds a Handler whose Service has a configured encryptor.
func newUpdateHandler(t *testing.T) *Handler {
	t.Helper()
	db := connectTestDB(t)
	repo := NewRepository(db, testLogger())
	hexKey, err := crypto.GenerateKey()
	require.NoError(t, err)
	svc := NewService(ServiceParams{Repo: repo, Cfg: &config.Config{GitHubTokenEncryptionKey: hexKey}, Log: testLogger()})
	return NewHandler(svc)
}

func TestHandler_CheckBlueprintUpdates_RequiresProject(t *testing.T) {
	h, _ := newAuthzHandler(t)
	c, _ := newAuthzCtx(t, http.MethodPost, "/api/blueprints/foo/updates/check", nil, &auth.AuthUser{ID: uuid.NewString()})
	c.SetParamNames("name")
	c.SetParamValues("foo")
	err := h.CheckBlueprintUpdates(c)
	assertAppErrorCode(t, err, apperror.ErrBadRequest.Code)
}

func TestHandler_CheckBlueprintUpdates_EnqueuesJob(t *testing.T) {
	h, _ := newAuthzHandler(t)
	proj := uuid.NewString()
	c, rec := newAuthzCtx(t, http.MethodPost, "/api/blueprints/foo/updates/check", nil,
		&auth.AuthUser{ID: uuid.NewString(), ProjectID: proj})
	c.SetParamNames("name")
	c.SetParamValues("foo")

	require.NoError(t, h.CheckBlueprintUpdates(c))
	assert.Equal(t, http.StatusAccepted, rec.Code)

	var job BlueprintUpdateJob
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &job))
	assert.Equal(t, UpdateActionCheck, job.Action)
	assert.Equal(t, "foo", job.BlueprintName)
	assert.Equal(t, proj, job.ProjectID)
	assert.Equal(t, UpdateJobStatusPending, job.Status)
}

func TestHandler_GetBlueprintUpdates_NotFound(t *testing.T) {
	h, _ := newAuthzHandler(t)
	proj := uuid.NewString()
	c, _ := newAuthzCtx(t, http.MethodGet, "/api/blueprints/foo/updates", nil,
		&auth.AuthUser{ID: uuid.NewString(), ProjectID: proj})
	c.SetParamNames("name")
	c.SetParamValues("foo")

	err := h.GetBlueprintUpdates(c)
	assertAppErrorCode(t, err, apperror.ErrNotFound.Code)
}

func TestHandler_GetBlueprintUpdates_ReturnsStatus(t *testing.T) {
	h, db := newAuthzHandler(t)
	ctx := context.Background()
	proj := uuid.NewString()

	now := time.Now()
	_, err := db.NewInsert().Model(&BlueprintUpdateCheck{
		ProjectID:       proj,
		BlueprintName:   "foo",
		SourceURL:       "https://github.com/acme/widgets",
		LatestVersion:   "2.0.0",
		UpdateAvailable: true,
		LastCheckedAt:   &now,
		CreatedAt:       now,
		UpdatedAt:       now,
	}).Exec(ctx)
	require.NoError(t, err)

	c, rec := newAuthzCtx(t, http.MethodGet, "/api/blueprints/foo/updates", nil,
		&auth.AuthUser{ID: uuid.NewString(), ProjectID: proj})
	c.SetParamNames("name")
	c.SetParamValues("foo")

	require.NoError(t, h.GetBlueprintUpdates(c))
	assert.Equal(t, http.StatusOK, rec.Code)

	var check BlueprintUpdateCheck
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &check))
	assert.Equal(t, "foo", check.BlueprintName)
	assert.True(t, check.UpdateAvailable)
	assert.Equal(t, "2.0.0", check.LatestVersion)
}

func TestHandler_SaveGithubCredentials_RequiresProject(t *testing.T) {
	h, _ := newAuthzHandler(t)
	body, _ := json.Marshal(map[string]string{"token": "ghp_x"})
	c, _ := newAuthzCtx(t, http.MethodPut, "/api/blueprints/github-credentials", body, &auth.AuthUser{ID: uuid.NewString()})
	err := h.SaveGithubCredentials(c)
	assertAppErrorCode(t, err, apperror.ErrBadRequest.Code)
}

func TestHandler_SaveGithubCredentials_InertWithoutEncryptor(t *testing.T) {
	h, _ := newAuthzHandler(t) // no encryption key configured
	proj := uuid.NewString()
	body, _ := json.Marshal(map[string]string{"token": "ghp_x"})
	c, _ := newAuthzCtx(t, http.MethodPut, "/api/blueprints/github-credentials", body,
		&auth.AuthUser{ID: uuid.NewString(), ProjectID: proj})
	err := h.SaveGithubCredentials(c)
	assertAppErrorCode(t, err, apperror.ErrConflict.Code)
}

func TestHandler_SaveGithubCredentials_StoresToken(t *testing.T) {
	h := newUpdateHandler(t)
	proj := uuid.NewString()
	body, _ := json.Marshal(map[string]string{"token": "ghp_SECRET"})
	c, rec := newAuthzCtx(t, http.MethodPut, "/api/blueprints/github-credentials", body,
		&auth.AuthUser{ID: uuid.NewString(), ProjectID: proj})

	require.NoError(t, h.SaveGithubCredentials(c))
	assert.Equal(t, http.StatusNoContent, rec.Code)

	// The response never echoes the token.
	assert.NotContains(t, rec.Body.String(), "ghp_SECRET")
}
