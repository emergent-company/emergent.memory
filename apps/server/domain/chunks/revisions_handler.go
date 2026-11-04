package chunks

import (
	"net/http"
	"strconv"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"

	"github.com/emergent-company/emergent.memory/pkg/apperror"
	"github.com/emergent-company/emergent.memory/pkg/auth"
)

// EditChunk handles PATCH /chunks/:id
// @Summary Edit a chunk
// @Description Replace a chunk's text, recording a revision and scheduling re-embedding
// @Tags chunks
// @Accept json
// @Produce json
// @Param id path string true "Chunk ID"
// @Param body body EditChunkRequest true "New chunk text"
// @Success 200 {object} EditChunkResponse
// @Failure 400 {object} apperror.Error
// @Failure 404 {object} apperror.Error
// @Router /chunks/{id} [patch]
func (h *Handler) EditChunk(c echo.Context) error {
	projectID, err := auth.GetProjectUUID(c)
	if err != nil {
		return err
	}
	chunkID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return apperror.NewBadRequest("invalid chunk ID")
	}
	var req EditChunkRequest
	if err := c.Bind(&req); err != nil {
		return apperror.NewBadRequest("invalid request body")
	}
	resp, err := h.svc.EditChunk(c.Request().Context(), projectID, chunkID, req.Text)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, resp)
}

// ListRevisions handles GET /chunks/:id/revisions
// @Summary List chunk revisions
// @Description List a chunk's revisions ordered by ascending sequence
// @Tags chunks
// @Produce json
// @Param id path string true "Chunk ID"
// @Success 200 {object} ListChunkRevisionsResponse
// @Failure 404 {object} apperror.Error
// @Router /chunks/{id}/revisions [get]
func (h *Handler) ListRevisions(c echo.Context) error {
	projectID, err := auth.GetProjectUUID(c)
	if err != nil {
		return err
	}
	chunkID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return apperror.NewBadRequest("invalid chunk ID")
	}
	resp, err := h.svc.ListRevisions(c.Request().Context(), projectID, chunkID)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, resp)
}

// DiffRevisions handles GET /chunks/:id/revisions/diff
// @Summary Diff chunk revisions
// @Description Return the line diff between two revisions of a chunk
// @Tags chunks
// @Produce json
// @Param id path string true "Chunk ID"
// @Param from query int true "From revision seq"
// @Param to query int true "To revision seq"
// @Success 200 {object} ChunkDiffResponse
// @Failure 400 {object} apperror.Error
// @Failure 404 {object} apperror.Error
// @Router /chunks/{id}/revisions/diff [get]
func (h *Handler) DiffRevisions(c echo.Context) error {
	projectID, err := auth.GetProjectUUID(c)
	if err != nil {
		return err
	}
	chunkID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return apperror.NewBadRequest("invalid chunk ID")
	}
	from, err := strconv.Atoi(c.QueryParam("from"))
	if err != nil {
		return apperror.NewBadRequest("invalid from revision")
	}
	to, err := strconv.Atoi(c.QueryParam("to"))
	if err != nil {
		return apperror.NewBadRequest("invalid to revision")
	}
	resp, err := h.svc.DiffRevisions(c.Request().Context(), projectID, chunkID, from, to)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, resp)
}

// RollbackChunk handles POST /chunks/:id/rollback
// @Summary Roll back a chunk
// @Description Restore a previous revision's text as a new revision
// @Tags chunks
// @Accept json
// @Produce json
// @Param id path string true "Chunk ID"
// @Param body body RollbackChunkRequest true "Target revision"
// @Success 200 {object} EditChunkResponse
// @Failure 400 {object} apperror.Error
// @Failure 404 {object} apperror.Error
// @Router /chunks/{id}/rollback [post]
func (h *Handler) RollbackChunk(c echo.Context) error {
	projectID, err := auth.GetProjectUUID(c)
	if err != nil {
		return err
	}
	chunkID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return apperror.NewBadRequest("invalid chunk ID")
	}
	var req RollbackChunkRequest
	if err := c.Bind(&req); err != nil {
		return apperror.NewBadRequest("invalid request body")
	}
	resp, err := h.svc.RollbackChunk(c.Request().Context(), projectID, chunkID, req.Seq)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, resp)
}
