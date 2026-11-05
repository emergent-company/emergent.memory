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

	"github.com/emergent-company/emergent.memory/pkg/apperror"
	"github.com/emergent-company/emergent.memory/pkg/auth"
)

func TestHandler_ApplyBlueprintUpdates_RequiresProject(t *testing.T) {
	h, _ := newAuthzHandler(t)
	c, _ := newAuthzCtx(t, http.MethodPost, "/api/blueprints/foo/updates/apply", nil, &auth.AuthUser{ID: uuid.NewString()})
	c.SetParamNames("name")
	c.SetParamValues("foo")
	err := h.ApplyBlueprintUpdates(c)
	assertAppErrorCode(t, err, apperror.ErrBadRequest.Code)
}

func TestHandler_ApplyBlueprintUpdates_EnqueuesJob(t *testing.T) {
	h, _ := newAuthzHandler(t)
	proj := uuid.NewString()
	c, rec := newAuthzCtx(t, http.MethodPost, "/api/blueprints/foo/updates/apply", nil,
		&auth.AuthUser{ID: uuid.NewString(), ProjectID: proj})
	c.SetParamNames("name")
	c.SetParamValues("foo")

	require.NoError(t, h.ApplyBlueprintUpdates(c))
	assert.Equal(t, http.StatusAccepted, rec.Code)

	var job BlueprintUpdateJob
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &job))
	assert.Equal(t, UpdateActionUpdate, job.Action)
	assert.Equal(t, "foo", job.BlueprintName)
	assert.Equal(t, proj, job.ProjectID)
	assert.Equal(t, UpdateJobStatusPending, job.Status)
}

func TestHandler_ApplyBlueprintUpdates_DuplicateRejected(t *testing.T) {
	h, _ := newAuthzHandler(t)
	proj := uuid.NewString()
	ctx := context.Background()

	// Seed a pending update job so the enqueue must be rejected.
	_, err := h.svc.repo.EnqueueUpdateJob(ctx, proj, "foo", UpdateActionUpdate)
	require.NoError(t, err)

	c, _ := newAuthzCtx(t, http.MethodPost, "/api/blueprints/foo/updates/apply", nil,
		&auth.AuthUser{ID: uuid.NewString(), ProjectID: proj})
	c.SetParamNames("name")
	c.SetParamValues("foo")

	err = h.ApplyBlueprintUpdates(c)
	assertAppErrorCode(t, err, apperror.ErrConflict.Code)
}

func TestHandler_GetBlueprintUpdates_IncludesLatestJob(t *testing.T) {
	h, _ := newAuthzHandler(t)
	ctx := context.Background()
	proj := uuid.NewString()

	now := time.Now()
	require.NoError(t, h.svc.repo.UpsertUpdateCheck(ctx, &BlueprintUpdateCheck{
		ProjectID:       proj,
		BlueprintName:   "foo",
		SourceURL:       "https://github.com/acme/widgets",
		LatestVersion:   "2.0.0",
		UpdateAvailable: true,
		LastCheckedAt:   &now,
		UpdatedAt:       now,
	}))
	_, err := h.svc.repo.EnqueueUpdateJob(ctx, proj, "foo", UpdateActionUpdate)
	require.NoError(t, err)

	c, rec := newAuthzCtx(t, http.MethodGet, "/api/blueprints/foo/updates", nil,
		&auth.AuthUser{ID: uuid.NewString(), ProjectID: proj})
	c.SetParamNames("name")
	c.SetParamValues("foo")

	require.NoError(t, h.GetBlueprintUpdates(c))
	assert.Equal(t, http.StatusOK, rec.Code)

	var status BlueprintUpdateStatus
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &status))
	require.NotNil(t, status.BlueprintUpdateCheck)
	assert.Equal(t, "foo", status.BlueprintUpdateCheck.BlueprintName)
	assert.True(t, status.BlueprintUpdateCheck.UpdateAvailable)

	require.NotNil(t, status.LatestJob)
	assert.Equal(t, UpdateActionUpdate, status.LatestJob.Action)
	assert.Equal(t, UpdateJobStatusPending, status.LatestJob.Status)
}

func TestHandler_ApplyBlueprintUpdates_MergeFlag(t *testing.T) {
	h, _ := newAuthzHandler(t)
	proj := uuid.NewString()
	body := []byte(`{"mergeTypeConflicts":true}`)
	c, rec := newAuthzCtx(t, http.MethodPost, "/api/blueprints/foo/updates/apply", body,
		&auth.AuthUser{ID: uuid.NewString(), ProjectID: proj})
	c.SetParamNames("name")
	c.SetParamValues("foo")

	require.NoError(t, h.ApplyBlueprintUpdates(c))
	assert.Equal(t, http.StatusAccepted, rec.Code)

	var job BlueprintUpdateJob
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &job))
	assert.True(t, job.MergeTypeConflicts)
}

func TestHandler_ApplyBlueprintUpdates_RejectsInvalidBody(t *testing.T) {
	h, _ := newAuthzHandler(t)
	proj := uuid.NewString()
	c, _ := newAuthzCtx(t, http.MethodPost, "/api/blueprints/foo/updates/apply", []byte(`not-json`),
		&auth.AuthUser{ID: uuid.NewString(), ProjectID: proj})
	c.SetParamNames("name")
	c.SetParamValues("foo")

	err := h.ApplyBlueprintUpdates(c)
	assertAppErrorCode(t, err, apperror.ErrBadRequest.Code)
}
