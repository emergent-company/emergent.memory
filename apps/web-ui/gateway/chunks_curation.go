package main

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/labstack/echo/v4"
)

// editChunk implements PATCH /api/chunks/:id — replace a chunk's text.
func (s *Server) editChunk(c echo.Context) error {
	var body struct {
		Text string `json:"text"`
	}
	if err := c.Bind(&body); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid request body"})
	}
	if strings.TrimSpace(body.Text) == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "text is required"})
	}
	res, err := s.memory.EditChunk(c.Request().Context(), c.Param("id"), body.Text)
	if err != nil {
		return documentError(c, err)
	}
	return c.JSON(http.StatusOK, res)
}

// listChunkRevisions implements GET /api/chunks/:id/revisions.
func (s *Server) listChunkRevisions(c echo.Context) error {
	revs, err := s.memory.ListChunkRevisions(c.Request().Context(), c.Param("id"))
	if err != nil {
		return documentError(c, err)
	}
	if revs == nil {
		revs = []ChunkRevision{}
	}
	return c.JSON(http.StatusOK, map[string]any{"data": revs, "totalCount": len(revs)})
}

// diffChunkRevisions implements GET /api/chunks/:id/revisions/diff.
func (s *Server) diffChunkRevisions(c echo.Context) error {
	from, err := strconv.Atoi(c.QueryParam("from"))
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid from revision"})
	}
	to, err := strconv.Atoi(c.QueryParam("to"))
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid to revision"})
	}
	diff, err := s.memory.DiffChunkRevisions(c.Request().Context(), c.Param("id"), from, to)
	if err != nil {
		return documentError(c, err)
	}
	return c.JSON(http.StatusOK, diff)
}

// rollbackChunk implements POST /api/chunks/:id/rollback.
func (s *Server) rollbackChunk(c echo.Context) error {
	var body struct {
		Seq int `json:"seq"`
	}
	if err := c.Bind(&body); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid request body"})
	}
	if body.Seq < 1 {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "seq is required"})
	}
	res, err := s.memory.RollbackChunk(c.Request().Context(), c.Param("id"), body.Seq)
	if err != nil {
		return documentError(c, err)
	}
	return c.JSON(http.StatusOK, res)
}
