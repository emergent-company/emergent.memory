package graph

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"

	"github.com/emergent/emergent-core/pkg/apperror"
	"github.com/emergent/emergent-core/pkg/auth"
)

// Handler handles HTTP requests for graph operations.
type Handler struct {
	svc *Service
}

// NewHandler creates a new graph handler.
func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

// getProjectID extracts and parses the project ID from the auth user context.
func getProjectID(c echo.Context) (uuid.UUID, error) {
	user := auth.GetUser(c)
	if user == nil {
		return uuid.Nil, apperror.ErrUnauthorized
	}

	// First check API token project ID (automatically set for API token auth)
	if user.APITokenProjectID != "" {
		return uuid.Parse(user.APITokenProjectID)
	}

	// Then check X-Project-ID header
	if user.ProjectID == "" {
		return uuid.Nil, apperror.ErrBadRequest.WithMessage("project_id is required")
	}

	return uuid.Parse(user.ProjectID)
}

// getUserID extracts and parses the user ID from the auth user context.
func getUserID(c echo.Context) (*uuid.UUID, error) {
	user := auth.GetUser(c)
	if user == nil || user.ID == "" {
		return nil, nil
	}

	id, err := uuid.Parse(user.ID)
	if err != nil {
		return nil, err
	}
	return &id, nil
}

// ListObjects returns graph objects matching query parameters.
// GET /api/v2/graph/objects/search
func (h *Handler) ListObjects(c echo.Context) error {
	user := auth.GetUser(c)
	if user == nil {
		return apperror.ErrUnauthorized
	}

	projectID, err := getProjectID(c)
	if err != nil {
		return apperror.ErrBadRequest.WithMessage("invalid project_id")
	}

	// Parse query parameters
	params := ListParams{
		ProjectID:      projectID,
		IncludeDeleted: c.QueryParam("include_deleted") == "true",
		Limit:          20, // NestJS default is 20
	}

	if limitStr := c.QueryParam("limit"); limitStr != "" {
		if limit, err := strconv.Atoi(limitStr); err == nil {
			params.Limit = limit
		}
	}

	if cursor := c.QueryParam("cursor"); cursor != "" {
		params.Cursor = &cursor
	}

	// Support both "type" (NestJS single) and "types" (array)
	if singleType := c.QueryParam("type"); singleType != "" {
		params.Type = &singleType
	} else if types := c.QueryParams()["types"]; len(types) > 0 {
		params.Types = types
	}

	// Support both "label" (NestJS single) and "labels" (array)
	if singleLabel := c.QueryParam("label"); singleLabel != "" {
		params.Label = &singleLabel
	} else if labels := c.QueryParams()["labels"]; len(labels) > 0 {
		params.Labels = labels
	}

	if status := c.QueryParam("status"); status != "" {
		params.Status = &status
	}

	if key := c.QueryParam("key"); key != "" {
		params.Key = &key
	}

	// Parse order (asc/desc)
	if order := c.QueryParam("order"); order == "asc" || order == "desc" {
		params.Order = order
	}

	// Parse related_to_id
	if relatedToID := c.QueryParam("related_to_id"); relatedToID != "" {
		id, err := uuid.Parse(relatedToID)
		if err != nil {
			return apperror.ErrBadRequest.WithMessage("invalid related_to_id")
		}
		params.RelatedToID = &id
	}

	// Parse ids (comma-separated)
	if idsParam := c.QueryParam("ids"); idsParam != "" {
		idStrs := strings.Split(idsParam, ",")
		for _, idStr := range idStrs {
			idStr = strings.TrimSpace(idStr)
			if idStr == "" {
				continue
			}
			id, err := uuid.Parse(idStr)
			if err != nil {
				return apperror.ErrBadRequest.WithMessage("invalid id in ids parameter")
			}
			params.IDs = append(params.IDs, id)
		}
	}

	// Parse extraction_job_id
	if extractionJobID := c.QueryParam("extraction_job_id"); extractionJobID != "" {
		id, err := uuid.Parse(extractionJobID)
		if err != nil {
			return apperror.ErrBadRequest.WithMessage("invalid extraction_job_id")
		}
		params.ExtractionJobID = &id
	}

	// Handle branch_id (NestJS allows "null" string for main branch)
	if branchIDStr := c.QueryParam("branch_id"); branchIDStr != "" {
		if branchIDStr != "null" {
			branchID, err := uuid.Parse(branchIDStr)
			if err != nil {
				return apperror.ErrBadRequest.WithMessage("invalid branch_id")
			}
			params.BranchID = &branchID
		}
		// If branch_id=null, leave BranchID as nil (main branch)
	}

	result, err := h.svc.List(c.Request().Context(), params)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, result)
}

// GetObject returns a single graph object by ID.
// GET /api/v2/graph/objects/:id
// Query params:
//   - resolveHead: If true, returns the HEAD (latest) version when the ID refers to an
//     older version in the version chain.
func (h *Handler) GetObject(c echo.Context) error {
	user := auth.GetUser(c)
	if user == nil {
		return apperror.ErrUnauthorized
	}

	projectID, err := getProjectID(c)
	if err != nil {
		return apperror.ErrBadRequest.WithMessage("invalid project_id")
	}

	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return apperror.ErrBadRequest.WithMessage("invalid object id")
	}

	// Parse resolveHead option
	resolveHead := c.QueryParam("resolveHead")
	shouldResolveHead := resolveHead == "true" || resolveHead == "1" || resolveHead == "yes"

	result, err := h.svc.GetByID(c.Request().Context(), projectID, id, shouldResolveHead)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, result)
}

// CreateObject creates a new graph object.
// POST /api/v2/graph/objects
func (h *Handler) CreateObject(c echo.Context) error {
	user := auth.GetUser(c)
	if user == nil {
		return apperror.ErrUnauthorized
	}

	projectID, err := getProjectID(c)
	if err != nil {
		return apperror.ErrBadRequest.WithMessage("invalid project_id")
	}

	var req CreateGraphObjectRequest
	if err := c.Bind(&req); err != nil {
		return apperror.ErrBadRequest.WithMessage("invalid request body")
	}

	if req.Type == "" {
		return apperror.ErrBadRequest.WithMessage("type is required")
	}

	actorID, _ := getUserID(c)
	result, err := h.svc.Create(c.Request().Context(), projectID, &req, actorID)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusCreated, result)
}

// PatchObject updates a graph object by creating a new version.
// PATCH /api/v2/graph/objects/:id
func (h *Handler) PatchObject(c echo.Context) error {
	user := auth.GetUser(c)
	if user == nil {
		return apperror.ErrUnauthorized
	}

	projectID, err := getProjectID(c)
	if err != nil {
		return apperror.ErrBadRequest.WithMessage("invalid project_id")
	}

	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return apperror.ErrBadRequest.WithMessage("invalid object id")
	}

	var req PatchGraphObjectRequest
	if err := json.NewDecoder(c.Request().Body).Decode(&req); err != nil {
		return apperror.ErrBadRequest.WithMessage("invalid request body")
	}

	actorID, _ := getUserID(c)
	result, err := h.svc.Patch(c.Request().Context(), projectID, id, &req, actorID)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, result)
}

// DeleteObject soft-deletes a graph object.
// DELETE /api/v2/graph/objects/:id
func (h *Handler) DeleteObject(c echo.Context) error {
	user := auth.GetUser(c)
	if user == nil {
		return apperror.ErrUnauthorized
	}

	projectID, err := getProjectID(c)
	if err != nil {
		return apperror.ErrBadRequest.WithMessage("invalid project_id")
	}

	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return apperror.ErrBadRequest.WithMessage("invalid object id")
	}

	actorID, _ := getUserID(c)
	if err := h.svc.Delete(c.Request().Context(), projectID, id, actorID); err != nil {
		return err
	}

	return c.JSON(http.StatusOK, map[string]string{"status": "deleted"})
}

// RestoreObject restores a soft-deleted graph object.
// POST /api/v2/graph/objects/:id/restore
func (h *Handler) RestoreObject(c echo.Context) error {
	user := auth.GetUser(c)
	if user == nil {
		return apperror.ErrUnauthorized
	}

	projectID, err := getProjectID(c)
	if err != nil {
		return apperror.ErrBadRequest.WithMessage("invalid project_id")
	}

	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return apperror.ErrBadRequest.WithMessage("invalid object id")
	}

	actorID, _ := getUserID(c)
	result, err := h.svc.Restore(c.Request().Context(), projectID, id, actorID)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, result)
}

// GetObjectHistory returns version history for a graph object.
// GET /api/v2/graph/objects/:id/history
func (h *Handler) GetObjectHistory(c echo.Context) error {
	user := auth.GetUser(c)
	if user == nil {
		return apperror.ErrUnauthorized
	}

	projectID, err := getProjectID(c)
	if err != nil {
		return apperror.ErrBadRequest.WithMessage("invalid project_id")
	}

	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return apperror.ErrBadRequest.WithMessage("invalid object id")
	}

	result, err := h.svc.GetHistory(c.Request().Context(), projectID, id)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, result)
}

// GetObjectEdges returns incoming and outgoing relationships for an object.
// GET /api/v2/graph/objects/:id/edges
func (h *Handler) GetObjectEdges(c echo.Context) error {
	user := auth.GetUser(c)
	if user == nil {
		return apperror.ErrUnauthorized
	}

	projectID, err := getProjectID(c)
	if err != nil {
		return apperror.ErrBadRequest.WithMessage("invalid project_id")
	}

	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return apperror.ErrBadRequest.WithMessage("invalid object id")
	}

	result, err := h.svc.GetEdges(c.Request().Context(), projectID, id)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, result)
}

// =============================================================================
// Relationship Handlers
// =============================================================================

// ListRelationships returns relationships matching query parameters.
// GET /api/v2/graph/relationships/search
func (h *Handler) ListRelationships(c echo.Context) error {
	user := auth.GetUser(c)
	if user == nil {
		return apperror.ErrUnauthorized
	}

	projectID, err := getProjectID(c)
	if err != nil {
		return apperror.ErrBadRequest.WithMessage("invalid project_id")
	}

	// Parse query parameters
	params := RelationshipListParams{
		ProjectID:      projectID,
		IncludeDeleted: c.QueryParam("include_deleted") == "true",
		Limit:          20,
	}

	if limitStr := c.QueryParam("limit"); limitStr != "" {
		if limit, err := strconv.Atoi(limitStr); err == nil {
			params.Limit = limit
		}
	}

	if cursor := c.QueryParam("cursor"); cursor != "" {
		params.Cursor = &cursor
	}

	if relType := c.QueryParam("type"); relType != "" {
		params.Type = &relType
	}

	if srcID := c.QueryParam("src_id"); srcID != "" {
		id, err := uuid.Parse(srcID)
		if err != nil {
			return apperror.ErrBadRequest.WithMessage("invalid src_id")
		}
		params.SrcID = &id
	}

	if dstID := c.QueryParam("dst_id"); dstID != "" {
		id, err := uuid.Parse(dstID)
		if err != nil {
			return apperror.ErrBadRequest.WithMessage("invalid dst_id")
		}
		params.DstID = &id
	}

	if order := c.QueryParam("order"); order != "" {
		params.Order = order
	}

	if branchIDStr := c.QueryParam("branch_id"); branchIDStr != "" {
		branchID, err := uuid.Parse(branchIDStr)
		if err != nil {
			return apperror.ErrBadRequest.WithMessage("invalid branch_id")
		}
		params.BranchID = &branchID
	}

	result, err := h.svc.ListRelationships(c.Request().Context(), params)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, result)
}

// GetRelationship returns a single relationship by ID.
// GET /api/v2/graph/relationships/:id
func (h *Handler) GetRelationship(c echo.Context) error {
	user := auth.GetUser(c)
	if user == nil {
		return apperror.ErrUnauthorized
	}

	projectID, err := getProjectID(c)
	if err != nil {
		return apperror.ErrBadRequest.WithMessage("invalid project_id")
	}

	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return apperror.ErrBadRequest.WithMessage("invalid relationship id")
	}

	result, err := h.svc.GetRelationship(c.Request().Context(), projectID, id)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, result)
}

// CreateRelationship creates a new relationship.
// POST /api/v2/graph/relationships
func (h *Handler) CreateRelationship(c echo.Context) error {
	user := auth.GetUser(c)
	if user == nil {
		return apperror.ErrUnauthorized
	}

	projectID, err := getProjectID(c)
	if err != nil {
		return apperror.ErrBadRequest.WithMessage("invalid project_id")
	}

	var req CreateGraphRelationshipRequest
	if err := c.Bind(&req); err != nil {
		return apperror.ErrBadRequest.WithMessage("invalid request body")
	}

	if req.Type == "" {
		return apperror.ErrBadRequest.WithMessage("type is required")
	}
	if req.SrcID == uuid.Nil {
		return apperror.ErrBadRequest.WithMessage("src_id is required")
	}
	if req.DstID == uuid.Nil {
		return apperror.ErrBadRequest.WithMessage("dst_id is required")
	}

	result, err := h.svc.CreateRelationship(c.Request().Context(), projectID, &req)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusCreated, result)
}

// PatchRelationship updates a relationship by creating a new version.
// PATCH /api/v2/graph/relationships/:id
func (h *Handler) PatchRelationship(c echo.Context) error {
	user := auth.GetUser(c)
	if user == nil {
		return apperror.ErrUnauthorized
	}

	projectID, err := getProjectID(c)
	if err != nil {
		return apperror.ErrBadRequest.WithMessage("invalid project_id")
	}

	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return apperror.ErrBadRequest.WithMessage("invalid relationship id")
	}

	var req PatchGraphRelationshipRequest
	if err := json.NewDecoder(c.Request().Body).Decode(&req); err != nil {
		return apperror.ErrBadRequest.WithMessage("invalid request body")
	}

	result, err := h.svc.PatchRelationship(c.Request().Context(), projectID, id, &req)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, result)
}

// DeleteRelationship soft-deletes a relationship.
// DELETE /api/v2/graph/relationships/:id
func (h *Handler) DeleteRelationship(c echo.Context) error {
	user := auth.GetUser(c)
	if user == nil {
		return apperror.ErrUnauthorized
	}

	projectID, err := getProjectID(c)
	if err != nil {
		return apperror.ErrBadRequest.WithMessage("invalid project_id")
	}

	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return apperror.ErrBadRequest.WithMessage("invalid relationship id")
	}

	result, err := h.svc.DeleteRelationship(c.Request().Context(), projectID, id)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, result)
}

// RestoreRelationship restores a soft-deleted relationship.
// POST /api/v2/graph/relationships/:id/restore
func (h *Handler) RestoreRelationship(c echo.Context) error {
	user := auth.GetUser(c)
	if user == nil {
		return apperror.ErrUnauthorized
	}

	projectID, err := getProjectID(c)
	if err != nil {
		return apperror.ErrBadRequest.WithMessage("invalid project_id")
	}

	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return apperror.ErrBadRequest.WithMessage("invalid relationship id")
	}

	result, err := h.svc.RestoreRelationship(c.Request().Context(), projectID, id)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusCreated, result)
}

// GetRelationshipHistory returns version history for a relationship.
// GET /api/v2/graph/relationships/:id/history
func (h *Handler) GetRelationshipHistory(c echo.Context) error {
	user := auth.GetUser(c)
	if user == nil {
		return apperror.ErrUnauthorized
	}

	projectID, err := getProjectID(c)
	if err != nil {
		return apperror.ErrBadRequest.WithMessage("invalid project_id")
	}

	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return apperror.ErrBadRequest.WithMessage("invalid relationship id")
	}

	result, err := h.svc.GetRelationshipHistory(c.Request().Context(), projectID, id)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, result)
}

// =============================================================================
// Search Handlers
// =============================================================================

// FTSSearch performs full-text search on graph objects.
// GET /api/v2/graph/objects/fts
func (h *Handler) FTSSearch(c echo.Context) error {
	user := auth.GetUser(c)
	if user == nil {
		return apperror.ErrUnauthorized
	}

	projectID, err := getProjectID(c)
	if err != nil {
		return apperror.ErrBadRequest.WithMessage("invalid project_id")
	}

	// Parse query parameters
	req := &FTSSearchRequest{
		Query:          c.QueryParam("q"),
		IncludeDeleted: c.QueryParam("include_deleted") == "true",
		Limit:          20,
	}

	if req.Query == "" {
		return apperror.ErrBadRequest.WithMessage("query parameter 'q' is required")
	}

	if limitStr := c.QueryParam("limit"); limitStr != "" {
		if limit, err := strconv.Atoi(limitStr); err == nil {
			req.Limit = limit
		}
	}

	if types := c.QueryParams()["types"]; len(types) > 0 {
		req.Types = types
	}

	if labels := c.QueryParams()["labels"]; len(labels) > 0 {
		req.Labels = labels
	}

	if status := c.QueryParam("status"); status != "" {
		req.Status = &status
	}

	if branchIDStr := c.QueryParam("branch_id"); branchIDStr != "" {
		branchID, err := uuid.Parse(branchIDStr)
		if err != nil {
			return apperror.ErrBadRequest.WithMessage("invalid branch_id")
		}
		req.BranchID = &branchID
	}

	result, err := h.svc.FTSSearch(c.Request().Context(), projectID, req)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, result)
}

// VectorSearch performs vector similarity search on graph objects.
// POST /api/v2/graph/objects/vector-search
func (h *Handler) VectorSearch(c echo.Context) error {
	user := auth.GetUser(c)
	if user == nil {
		return apperror.ErrUnauthorized
	}

	projectID, err := getProjectID(c)
	if err != nil {
		return apperror.ErrBadRequest.WithMessage("invalid project_id")
	}

	var req VectorSearchRequest
	if err := c.Bind(&req); err != nil {
		return apperror.ErrBadRequest.WithMessage("invalid request body")
	}

	if len(req.Vector) == 0 {
		return apperror.ErrBadRequest.WithMessage("vector is required")
	}

	result, err := h.svc.VectorSearch(c.Request().Context(), projectID, &req)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, result)
}

// HybridSearch performs combined lexical and vector search.
// POST /api/v2/graph/search
// Query params:
//   - debug: If "true", includes timing and statistics in response (requires graph:search:debug scope)
func (h *Handler) HybridSearch(c echo.Context) error {
	user := auth.GetUser(c)
	if user == nil {
		return apperror.ErrUnauthorized
	}

	projectID, err := getProjectID(c)
	if err != nil {
		return apperror.ErrBadRequest.WithMessage("invalid project_id")
	}

	var req HybridSearchRequest
	if err := c.Bind(&req); err != nil {
		return apperror.ErrBadRequest.WithMessage("invalid request body")
	}

	// At least query or vector must be provided
	if req.Query == "" && len(req.Vector) == 0 {
		return apperror.ErrBadRequest.WithMessage("either query or vector is required")
	}

	// Determine if debug mode is requested (via body field or query param)
	wantsDebug := req.IncludeDebug || c.QueryParam("debug") == "true"

	// Check scope if debug mode requested
	if wantsDebug {
		hasDebugScope := false
		if user.Scopes != nil {
			for _, s := range user.Scopes {
				if s == "graph:search:debug" {
					hasDebugScope = true
					break
				}
			}
		}
		if !hasDebugScope {
			return apperror.ErrForbidden.WithMessage("graph:search:debug scope required for debug mode")
		}
	}

	opts := &HybridSearchOptions{Debug: wantsDebug}
	result, err := h.svc.HybridSearch(c.Request().Context(), projectID, &req, opts)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, result)
}

// =============================================================================
// Tags and Bulk Operations
// =============================================================================

// GetTags returns all distinct tags (labels) used by objects in a project.
// GET /api/v2/graph/objects/tags
func (h *Handler) GetTags(c echo.Context) error {
	user := auth.GetUser(c)
	if user == nil {
		return apperror.ErrUnauthorized
	}

	projectID, err := getProjectID(c)
	if err != nil {
		return apperror.ErrBadRequest.WithMessage("invalid project_id")
	}

	tags, err := h.svc.GetTags(c.Request().Context(), projectID)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, tags)
}

// BulkUpdateStatus updates the status of multiple objects.
// POST /api/v2/graph/objects/bulk-update-status
func (h *Handler) BulkUpdateStatus(c echo.Context) error {
	user := auth.GetUser(c)
	if user == nil {
		return apperror.ErrUnauthorized
	}

	projectID, err := getProjectID(c)
	if err != nil {
		return apperror.ErrBadRequest.WithMessage("invalid project_id")
	}

	var req BulkUpdateStatusRequest
	if err := c.Bind(&req); err != nil {
		return apperror.ErrBadRequest.WithMessage("invalid request body")
	}

	if len(req.IDs) == 0 {
		return apperror.ErrBadRequest.WithMessage("ids is required")
	}

	actorID, _ := getUserID(c)
	result, err := h.svc.BulkUpdateStatus(c.Request().Context(), projectID, &req, actorID)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, result)
}

// =============================================================================
// Search with Neighbors Handler
// =============================================================================

// SearchWithNeighbors performs FTS search and optionally retrieves neighbors.
// POST /api/v2/graph/search-with-neighbors
func (h *Handler) SearchWithNeighbors(c echo.Context) error {
	user := auth.GetUser(c)
	if user == nil {
		return apperror.ErrUnauthorized
	}

	projectID, err := getProjectID(c)
	if err != nil {
		return apperror.ErrBadRequest.WithMessage("invalid project_id")
	}

	var req SearchWithNeighborsRequest
	if err := c.Bind(&req); err != nil {
		return apperror.ErrBadRequest.WithMessage("invalid request body")
	}

	if req.Query == "" {
		return apperror.ErrBadRequest.WithMessage("query is required")
	}

	result, err := h.svc.SearchWithNeighbors(c.Request().Context(), projectID, &req)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, result)
}

// =============================================================================
// Similar Objects Handler
// =============================================================================

// GetSimilarObjects finds objects similar to a given object.
// GET /api/v2/graph/objects/:id/similar
func (h *Handler) GetSimilarObjects(c echo.Context) error {
	user := auth.GetUser(c)
	if user == nil {
		return apperror.ErrUnauthorized
	}

	projectID, err := getProjectID(c)
	if err != nil {
		return apperror.ErrBadRequest.WithMessage("invalid project_id")
	}

	objectID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return apperror.ErrBadRequest.WithMessage("invalid object id")
	}

	// Parse query parameters
	req := &SimilarObjectsRequest{}

	if limitStr := c.QueryParam("limit"); limitStr != "" {
		if limit, err := strconv.Atoi(limitStr); err == nil {
			req.Limit = limit
		}
	}

	if maxDistStr := c.QueryParam("maxDistance"); maxDistStr != "" {
		if maxDist, err := strconv.ParseFloat(maxDistStr, 32); err == nil {
			maxDistFloat := float32(maxDist)
			req.MaxDistance = &maxDistFloat
		}
	}

	if minScoreStr := c.QueryParam("minScore"); minScoreStr != "" {
		if minScore, err := strconv.ParseFloat(minScoreStr, 32); err == nil {
			minScoreFloat := float32(minScore)
			req.MinScore = &minScoreFloat
		}
	}

	if typeParam := c.QueryParam("type"); typeParam != "" {
		req.Type = &typeParam
	}

	if branchIDStr := c.QueryParam("branchId"); branchIDStr != "" {
		branchID, err := uuid.Parse(branchIDStr)
		if err != nil {
			return apperror.ErrBadRequest.WithMessage("invalid branchId")
		}
		req.BranchID = &branchID
	}

	if keyPrefix := c.QueryParam("keyPrefix"); keyPrefix != "" {
		req.KeyPrefix = &keyPrefix
	}

	if labelsAll := c.QueryParams()["labelsAll"]; len(labelsAll) > 0 {
		req.LabelsAll = labelsAll
	}

	if labelsAny := c.QueryParams()["labelsAny"]; len(labelsAny) > 0 {
		req.LabelsAny = labelsAny
	}

	result, err := h.svc.FindSimilarObjects(c.Request().Context(), projectID, objectID, req)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, result)
}

// =============================================================================
// Graph Expand Handler
// =============================================================================

// ExpandGraph performs bounded BFS graph expansion.
// POST /api/v2/graph/expand
func (h *Handler) ExpandGraph(c echo.Context) error {
	user := auth.GetUser(c)
	if user == nil {
		return apperror.ErrUnauthorized
	}

	projectID, err := getProjectID(c)
	if err != nil {
		return apperror.ErrBadRequest.WithMessage("invalid project_id")
	}

	var req GraphExpandRequest
	if err := c.Bind(&req); err != nil {
		return apperror.ErrBadRequest.WithMessage("invalid request body")
	}

	if len(req.RootIDs) == 0 {
		return apperror.ErrBadRequest.WithMessage("root_ids is required")
	}

	if len(req.RootIDs) > 50 {
		return apperror.ErrBadRequest.WithMessage("root_ids cannot exceed 50 items")
	}

	result, err := h.svc.ExpandGraph(c.Request().Context(), projectID, &req)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, result)
}

// =============================================================================
// Graph Traverse Handler
// =============================================================================

// TraverseGraph performs bounded BFS graph traversal.
// POST /api/v2/graph/traverse
func (h *Handler) TraverseGraph(c echo.Context) error {
	user := auth.GetUser(c)
	if user == nil {
		return apperror.ErrUnauthorized
	}

	projectID, err := getProjectID(c)
	if err != nil {
		return apperror.ErrBadRequest.WithMessage("invalid project_id")
	}

	var req TraverseGraphRequest
	if err := c.Bind(&req); err != nil {
		return apperror.ErrBadRequest.WithMessage("invalid request body")
	}

	if len(req.RootIDs) == 0 {
		return apperror.ErrBadRequest.WithMessage("root_ids is required")
	}

	if len(req.RootIDs) > 50 {
		return apperror.ErrBadRequest.WithMessage("root_ids cannot exceed 50 items")
	}

	result, err := h.svc.TraverseGraph(c.Request().Context(), projectID, &req)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, result)
}

// =============================================================================
// Branch Merge Handler
// =============================================================================

// MergeBranch performs dry-run or actual merge of a source branch into target branch.
// POST /api/v2/graph/branches/:targetBranchId/merge
func (h *Handler) MergeBranch(c echo.Context) error {
	user := auth.GetUser(c)
	if user == nil {
		return apperror.ErrUnauthorized
	}

	projectID, err := getProjectID(c)
	if err != nil {
		return apperror.ErrBadRequest.WithMessage("invalid project_id")
	}

	targetBranchID, err := uuid.Parse(c.Param("targetBranchId"))
	if err != nil {
		return apperror.ErrBadRequest.WithMessage("invalid target branch id")
	}

	var req BranchMergeRequest
	if err := c.Bind(&req); err != nil {
		return apperror.ErrBadRequest.WithMessage("invalid request body")
	}

	if req.SourceBranchID == uuid.Nil {
		return apperror.ErrBadRequest.WithMessage("sourceBranchId is required")
	}

	result, err := h.svc.MergeBranch(c.Request().Context(), projectID, targetBranchID, &req)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, result)
}
