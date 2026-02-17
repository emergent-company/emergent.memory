package superadmin

import (
	"net/http"
	"strconv"

	"github.com/labstack/echo/v4"

	"github.com/emergent-company/emergent/pkg/apperror"
	"github.com/emergent-company/emergent/pkg/auth"
)

// Handler handles HTTP requests for superadmin
type Handler struct {
	repo *Repository
}

// NewHandler creates a new superadmin handler
func NewHandler(repo *Repository) *Handler {
	return &Handler{repo: repo}
}

// getPaginationParams extracts pagination params from query string
func getPaginationParams(c echo.Context) (page, limit int) {
	page, _ = strconv.Atoi(c.QueryParam("page"))
	if page < 1 {
		page = 1
	}
	limit, _ = strconv.Atoi(c.QueryParam("limit"))
	if limit < 1 || limit > 100 {
		limit = 20
	}
	return page, limit
}

// getBoolParam extracts an optional bool query param
func getBoolParam(c echo.Context, name string) *bool {
	val := c.QueryParam(name)
	if val == "" {
		return nil
	}
	b := val == "true" || val == "1"
	return &b
}

// GetMe handles GET /api/superadmin/me
// @Summary      Get current user's superadmin status
// @Description  Returns whether the authenticated user has superadmin privileges. Returns null if not superadmin, otherwise returns {isSuperadmin: true}.
// @Tags         superadmin
// @Produce      json
// @Success      200 {object} SuperadminMeResponse "Superadmin status (or null if not superadmin)"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Router       /api/superadmin/me [get]
// @Security     bearerAuth
func (h *Handler) GetMe(c echo.Context) error {
	user := auth.GetUser(c)
	if user == nil {
		return apperror.ErrUnauthorized
	}

	isSuperadmin, err := h.repo.IsSuperadmin(c.Request().Context(), user.ID)
	if err != nil {
		return apperror.NewInternal("failed to check superadmin status", err)
	}

	if !isSuperadmin {
		// Return null to indicate the user is not a superadmin (matches NestJS behavior)
		return c.JSON(http.StatusOK, nil)
	}

	return c.JSON(http.StatusOK, SuperadminMeResponse{
		IsSuperadmin: true,
	})
}

// requireSuperadmin is a helper to check if the user is a superadmin
func (h *Handler) requireSuperadmin(c echo.Context) (string, error) {
	user := auth.GetUser(c)
	if user == nil {
		return "", apperror.ErrUnauthorized
	}

	isSuperadmin, err := h.repo.IsSuperadmin(c.Request().Context(), user.ID)
	if err != nil {
		return "", apperror.NewInternal("failed to check superadmin status", err)
	}

	if !isSuperadmin {
		return "", apperror.ErrForbidden
	}

	return user.ID, nil
}

// ListUsers handles GET /api/superadmin/users
// @Summary      List all users (superadmin only)
// @Description  Returns paginated list of all users with their org memberships and email addresses. Supports search filtering and org filtering.
// @Tags         superadmin
// @Produce      json
// @Param        page query int false "Page number (default: 1)" minimum(1)
// @Param        limit query int false "Results per page (default: 20, max: 100)" minimum(1) maximum(100)
// @Param        search query string false "Search by name or email"
// @Param        orgId query string false "Filter by organization ID (UUID)"
// @Success      200 {object} ListUsersResponse "User list with pagination"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Failure      403 {object} apperror.Error "Forbidden (not superadmin)"
// @Router       /api/superadmin/users [get]
// @Security     bearerAuth
func (h *Handler) ListUsers(c echo.Context) error {
	if _, err := h.requireSuperadmin(c); err != nil {
		return err
	}

	page, limit := getPaginationParams(c)
	search := c.QueryParam("search")
	orgID := c.QueryParam("orgId")

	users, total, err := h.repo.ListUsers(c.Request().Context(), page, limit, search, orgID)
	if err != nil {
		return apperror.NewInternal("failed to list users", err)
	}

	// Get org memberships for all users
	userIDs := make([]string, len(users))
	for i, u := range users {
		userIDs[i] = u.ID
	}
	orgMap, err := h.repo.GetUserOrganizations(c.Request().Context(), userIDs)
	if err != nil {
		return apperror.NewInternal("failed to get user organizations", err)
	}

	// Build response DTOs
	dtos := make([]SuperadminUserDTO, len(users))
	for i, u := range users {
		var primaryEmail *string
		if len(u.Emails) > 0 {
			primaryEmail = &u.Emails[0].Email
		}

		orgs := make([]UserOrgMembershipDTO, 0)
		if memberships, ok := orgMap[u.ID]; ok {
			for _, m := range memberships {
				orgName := ""
				if m.Organization != nil {
					orgName = m.Organization.Name
				}
				orgs = append(orgs, UserOrgMembershipDTO{
					OrgID:    m.OrganizationID,
					OrgName:  orgName,
					Role:     m.Role,
					JoinedAt: m.CreatedAt,
				})
			}
		}

		dtos[i] = SuperadminUserDTO{
			ID:             u.ID,
			ZitadelUserID:  u.ZitadelUserID,
			FirstName:      u.FirstName,
			LastName:       u.LastName,
			DisplayName:    u.DisplayName,
			PrimaryEmail:   primaryEmail,
			LastActivityAt: u.LastActivityAt,
			CreatedAt:      u.CreatedAt,
			Organizations:  orgs,
		}
	}

	return c.JSON(http.StatusOK, ListUsersResponse{
		Users: dtos,
		Meta:  NewPaginationMeta(page, limit, total),
	})
}

// DeleteUser handles DELETE /api/superadmin/users/:id
// @Summary      Soft-delete a user (superadmin only)
// @Description  Marks a user as deleted (soft delete). User data is retained but account is deactivated.
// @Tags         superadmin
// @Produce      json
// @Param        id path string true "User ID (UUID)"
// @Success      200 {object} SuccessResponse "User deleted successfully"
// @Failure      400 {object} apperror.Error "Invalid user ID"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Failure      403 {object} apperror.Error "Forbidden (not superadmin)"
// @Failure      404 {object} apperror.Error "User not found"
// @Router       /api/superadmin/users/{id} [delete]
// @Security     bearerAuth
func (h *Handler) DeleteUser(c echo.Context) error {
	deletedBy, err := h.requireSuperadmin(c)
	if err != nil {
		return err
	}

	userID := c.Param("id")
	if userID == "" {
		return apperror.ErrBadRequest
	}

	// Check user exists
	user, err := h.repo.GetUser(c.Request().Context(), userID)
	if err != nil {
		return apperror.NewInternal("failed to get user", err)
	}
	if user == nil {
		return apperror.ErrNotFound
	}

	if err := h.repo.SoftDeleteUser(c.Request().Context(), userID, deletedBy); err != nil {
		return apperror.NewInternal("failed to delete user", err)
	}

	return c.JSON(http.StatusOK, SuccessResponse{
		Success: true,
		Message: "User deleted successfully",
	})
}

// ListOrganizations handles GET /api/superadmin/organizations
// @Summary      List all organizations (superadmin only)
// @Description  Returns paginated list of all organizations with member and project counts.
// @Tags         superadmin
// @Produce      json
// @Param        page query int false "Page number (default: 1)" minimum(1)
// @Param        limit query int false "Results per page (default: 20, max: 100)" minimum(1) maximum(100)
// @Success      200 {object} ListOrganizationsResponse "Organization list with pagination"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Failure      403 {object} apperror.Error "Forbidden (not superadmin)"
// @Router       /api/superadmin/organizations [get]
// @Security     bearerAuth
func (h *Handler) ListOrganizations(c echo.Context) error {
	if _, err := h.requireSuperadmin(c); err != nil {
		return err
	}

	page, limit := getPaginationParams(c)

	orgs, total, err := h.repo.ListOrganizations(c.Request().Context(), page, limit)
	if err != nil {
		return apperror.NewInternal("failed to list organizations", err)
	}

	dtos := make([]SuperadminOrgDTO, len(orgs))
	for i, o := range orgs {
		dtos[i] = SuperadminOrgDTO{
			ID:           o.ID,
			Name:         o.Name,
			MemberCount:  o.MemberCount,
			ProjectCount: o.ProjectCount,
			CreatedAt:    o.CreatedAt,
			DeletedAt:    o.DeletedAt,
		}
	}

	return c.JSON(http.StatusOK, ListOrganizationsResponse{
		Organizations: dtos,
		Meta:          NewPaginationMeta(page, limit, total),
	})
}

// DeleteOrganization handles DELETE /api/superadmin/organizations/:id
// @Summary      Soft-delete an organization (superadmin only)
// @Description  Marks an organization as deleted (soft delete). Organization data is retained but org is deactivated.
// @Tags         superadmin
// @Produce      json
// @Param        id path string true "Organization ID (UUID)"
// @Success      200 {object} SuccessResponse "Organization deleted successfully"
// @Failure      400 {object} apperror.Error "Invalid organization ID"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Failure      403 {object} apperror.Error "Forbidden (not superadmin)"
// @Failure      404 {object} apperror.Error "Organization not found"
// @Router       /api/superadmin/organizations/{id} [delete]
// @Security     bearerAuth
func (h *Handler) DeleteOrganization(c echo.Context) error {
	deletedBy, err := h.requireSuperadmin(c)
	if err != nil {
		return err
	}

	orgID := c.Param("id")
	if orgID == "" {
		return apperror.ErrBadRequest
	}

	// Check org exists
	org, err := h.repo.GetOrg(c.Request().Context(), orgID)
	if err != nil {
		return apperror.NewInternal("failed to get organization", err)
	}
	if org == nil {
		return apperror.ErrNotFound
	}

	if err := h.repo.SoftDeleteOrg(c.Request().Context(), orgID, deletedBy); err != nil {
		return apperror.NewInternal("failed to delete organization", err)
	}

	return c.JSON(http.StatusOK, SuccessResponse{
		Success: true,
		Message: "Organization deleted successfully",
	})
}

// ListProjects handles GET /api/superadmin/projects
// @Summary      List all projects (superadmin only)
// @Description  Returns paginated list of all projects with org information and document counts. Supports org filtering.
// @Tags         superadmin
// @Produce      json
// @Param        page query int false "Page number (default: 1)" minimum(1)
// @Param        limit query int false "Results per page (default: 20, max: 100)" minimum(1) maximum(100)
// @Param        orgId query string false "Filter by organization ID (UUID)"
// @Success      200 {object} ListProjectsResponse "Project list with pagination"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Failure      403 {object} apperror.Error "Forbidden (not superadmin)"
// @Router       /api/superadmin/projects [get]
// @Security     bearerAuth
func (h *Handler) ListProjects(c echo.Context) error {
	if _, err := h.requireSuperadmin(c); err != nil {
		return err
	}

	page, limit := getPaginationParams(c)
	orgID := c.QueryParam("orgId")

	projects, total, err := h.repo.ListProjects(c.Request().Context(), page, limit, orgID)
	if err != nil {
		return apperror.NewInternal("failed to list projects", err)
	}

	dtos := make([]SuperadminProjectDTO, len(projects))
	for i, p := range projects {
		dtos[i] = SuperadminProjectDTO{
			ID:               p.ID,
			Name:             p.Name,
			OrganizationID:   p.OrganizationID,
			OrganizationName: p.OrganizationName,
			DocumentCount:    p.DocumentCount,
			CreatedAt:        p.CreatedAt,
			DeletedAt:        p.DeletedAt,
		}
	}

	return c.JSON(http.StatusOK, ListProjectsResponse{
		Projects: dtos,
		Meta:     NewPaginationMeta(page, limit, total),
	})
}

// DeleteProject handles DELETE /api/superadmin/projects/:id
// @Summary      Soft-delete a project (superadmin only)
// @Description  Marks a project as deleted (soft delete). Project data is retained but project is deactivated.
// @Tags         superadmin
// @Produce      json
// @Param        id path string true "Project ID (UUID)"
// @Success      200 {object} SuccessResponse "Project deleted successfully"
// @Failure      400 {object} apperror.Error "Invalid project ID"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Failure      403 {object} apperror.Error "Forbidden (not superadmin)"
// @Failure      404 {object} apperror.Error "Project not found"
// @Router       /api/superadmin/projects/{id} [delete]
// @Security     bearerAuth
func (h *Handler) DeleteProject(c echo.Context) error {
	deletedBy, err := h.requireSuperadmin(c)
	if err != nil {
		return err
	}

	projectID := c.Param("id")
	if projectID == "" {
		return apperror.ErrBadRequest
	}

	// Check project exists
	project, err := h.repo.GetProject(c.Request().Context(), projectID)
	if err != nil {
		return apperror.NewInternal("failed to get project", err)
	}
	if project == nil {
		return apperror.ErrNotFound
	}

	if err := h.repo.SoftDeleteProject(c.Request().Context(), projectID, deletedBy); err != nil {
		return apperror.NewInternal("failed to delete project", err)
	}

	return c.JSON(http.StatusOK, SuccessResponse{
		Success: true,
		Message: "Project deleted successfully",
	})
}

// ListEmailJobs handles GET /api/superadmin/email-jobs
// @Summary      List email job queue (superadmin only)
// @Description  Returns paginated list of email jobs with status, delivery info, and error details. Supports filtering by status, recipient, and date range.
// @Tags         superadmin
// @Produce      json
// @Param        page query int false "Page number (default: 1)" minimum(1)
// @Param        limit query int false "Results per page (default: 20, max: 100)" minimum(1) maximum(100)
// @Param        status query string false "Filter by status (pending, processing, sent, failed)"
// @Param        recipient query string false "Filter by recipient email address"
// @Param        fromDate query string false "Filter from date (ISO 8601)"
// @Param        toDate query string false "Filter to date (ISO 8601)"
// @Success      200 {object} ListEmailJobsResponse "Email job list with pagination"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Failure      403 {object} apperror.Error "Forbidden (not superadmin)"
// @Router       /api/superadmin/email-jobs [get]
// @Security     bearerAuth
func (h *Handler) ListEmailJobs(c echo.Context) error {
	if _, err := h.requireSuperadmin(c); err != nil {
		return err
	}

	page, limit := getPaginationParams(c)
	status := c.QueryParam("status")
	recipient := c.QueryParam("recipient")
	fromDate := c.QueryParam("fromDate")
	toDate := c.QueryParam("toDate")

	jobs, total, err := h.repo.ListEmailJobs(c.Request().Context(), page, limit, status, recipient, fromDate, toDate)
	if err != nil {
		return apperror.NewInternal("failed to list email jobs", err)
	}

	dtos := make([]SuperadminEmailJobDTO, len(jobs))
	for i, j := range jobs {
		dtos[i] = SuperadminEmailJobDTO{
			ID:               j.ID,
			TemplateName:     j.TemplateName,
			ToEmail:          j.ToEmail,
			ToName:           j.ToName,
			Subject:          j.Subject,
			Status:           j.Status,
			Attempts:         j.Attempts,
			MaxAttempts:      j.MaxAttempts,
			LastError:        j.LastError,
			CreatedAt:        j.CreatedAt,
			ProcessedAt:      j.ProcessedAt,
			SourceType:       j.SourceType,
			SourceID:         j.SourceID,
			DeliveryStatus:   j.DeliveryStatus,
			DeliveryStatusAt: j.DeliveryStatusAt,
		}
	}

	return c.JSON(http.StatusOK, ListEmailJobsResponse{
		EmailJobs: dtos,
		Meta:      NewPaginationMeta(page, limit, total),
	})
}

// GetEmailJobPreview handles GET /api/superadmin/email-jobs/:id/preview-json
// @Summary      Preview email job template data (superadmin only)
// @Description  Returns email job details including subject, recipient, and template data (full HTML rendering not implemented).
// @Tags         superadmin
// @Produce      json
// @Param        id path string true "Email job ID (UUID)"
// @Success      200 {object} EmailJobPreviewResponse "Email job preview data"
// @Failure      400 {object} apperror.Error "Invalid job ID"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Failure      403 {object} apperror.Error "Forbidden (not superadmin)"
// @Failure      404 {object} apperror.Error "Email job not found"
// @Router       /api/superadmin/email-jobs/{id}/preview-json [get]
// @Security     bearerAuth
func (h *Handler) GetEmailJobPreview(c echo.Context) error {
	if _, err := h.requireSuperadmin(c); err != nil {
		return err
	}

	id := c.Param("id")
	if id == "" {
		return apperror.ErrBadRequest
	}

	job, err := h.repo.GetEmailJob(c.Request().Context(), id)
	if err != nil {
		return apperror.NewInternal("failed to get email job", err)
	}
	if job == nil {
		return apperror.ErrNotFound
	}

	// Return the template data as the preview
	// In a full implementation, we would render the template
	return c.JSON(http.StatusOK, EmailJobPreviewResponse{
		HTML:    "", // Would need to render template here
		Subject: job.Subject,
		ToEmail: job.ToEmail,
		ToName:  job.ToName,
	})
}

// ListEmbeddingJobs handles GET /api/superadmin/embedding-jobs
// @Summary      List embedding job queue (superadmin only)
// @Description  Returns paginated list of embedding jobs (graph and chunk) with status, error details, and aggregate statistics. Supports filtering by status, project, and job type.
// @Tags         superadmin
// @Produce      json
// @Param        page query int false "Page number (default: 1)" minimum(1)
// @Param        limit query int false "Results per page (default: 20, max: 100)" minimum(1) maximum(100)
// @Param        status query string false "Filter by status (pending, processing, completed, failed)"
// @Param        hasError query boolean false "Filter jobs with errors"
// @Param        projectId query string false "Filter by project ID (UUID)"
// @Param        type query string false "Filter by job type (graph or chunk)"
// @Success      200 {object} ListEmbeddingJobsResponse "Embedding job list with stats and pagination"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Failure      403 {object} apperror.Error "Forbidden (not superadmin)"
// @Router       /api/superadmin/embedding-jobs [get]
// @Security     bearerAuth
func (h *Handler) ListEmbeddingJobs(c echo.Context) error {
	if _, err := h.requireSuperadmin(c); err != nil {
		return err
	}

	page, limit := getPaginationParams(c)
	status := c.QueryParam("status")
	hasError := getBoolParam(c, "hasError")
	projectID := c.QueryParam("projectId")
	jobType := c.QueryParam("type") // "graph" or "chunk"

	var allJobs []EmbeddingJobDTO
	var total int

	if jobType == "" || jobType == "graph" {
		graphJobs, graphTotal, err := h.repo.ListGraphEmbeddingJobs(c.Request().Context(), page, limit, status, hasError, projectID)
		if err != nil {
			return apperror.NewInternal("failed to list graph embedding jobs", err)
		}
		allJobs = append(allJobs, graphJobs...)
		total += graphTotal
	}

	if jobType == "" || jobType == "chunk" {
		chunkJobs, chunkTotal, err := h.repo.ListChunkEmbeddingJobs(c.Request().Context(), page, limit, status, hasError, projectID)
		if err != nil {
			return apperror.NewInternal("failed to list chunk embedding jobs", err)
		}
		allJobs = append(allJobs, chunkJobs...)
		total += chunkTotal
	}

	stats, err := h.repo.GetEmbeddingJobStats(c.Request().Context())
	if err != nil {
		return apperror.NewInternal("failed to get embedding job stats", err)
	}

	return c.JSON(http.StatusOK, ListEmbeddingJobsResponse{
		Jobs:  allJobs,
		Stats: stats,
		Meta:  NewPaginationMeta(page, limit, total),
	})
}

// DeleteEmbeddingJobs handles POST /api/superadmin/embedding-jobs/delete
// @Summary      Bulk delete embedding jobs (superadmin only)
// @Description  Deletes multiple embedding jobs by ID. Supports both graph and chunk embedding jobs.
// @Tags         superadmin
// @Accept       json
// @Produce      json
// @Param        request body DeleteJobsRequest true "Job IDs to delete and optional job type (graph or chunk)"
// @Success      200 {object} DeleteJobsResponse "Deletion summary"
// @Failure      400 {object} apperror.Error "Invalid request body"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Failure      403 {object} apperror.Error "Forbidden (not superadmin)"
// @Router       /api/superadmin/embedding-jobs/delete [post]
// @Security     bearerAuth
func (h *Handler) DeleteEmbeddingJobs(c echo.Context) error {
	if _, err := h.requireSuperadmin(c); err != nil {
		return err
	}

	var req DeleteJobsRequest
	if err := c.Bind(&req); err != nil {
		return apperror.ErrBadRequest
	}
	if len(req.IDs) == 0 {
		return apperror.ErrBadRequest
	}

	var deletedCount int
	var err error

	if req.Type == "chunk" {
		deletedCount, err = h.repo.DeleteChunkEmbeddingJobs(c.Request().Context(), req.IDs)
	} else {
		// Default to graph or delete both
		deletedCount, err = h.repo.DeleteGraphEmbeddingJobs(c.Request().Context(), req.IDs)
	}

	if err != nil {
		return apperror.NewInternal("failed to delete embedding jobs", err)
	}

	return c.JSON(http.StatusOK, DeleteJobsResponse{
		Success:      true,
		DeletedCount: deletedCount,
		Message:      "Embedding jobs deleted successfully",
	})
}

// CleanupOrphanEmbeddingJobs handles POST /api/superadmin/embedding-jobs/cleanup-orphans
// @Summary      Cleanup orphan embedding jobs (superadmin only)
// @Description  Removes embedding jobs that reference deleted objects or chunks (orphaned jobs).
// @Tags         superadmin
// @Produce      json
// @Success      200 {object} CleanupOrphansResponse "Cleanup summary"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Failure      403 {object} apperror.Error "Forbidden (not superadmin)"
// @Router       /api/superadmin/embedding-jobs/cleanup-orphans [post]
// @Security     bearerAuth
func (h *Handler) CleanupOrphanEmbeddingJobs(c echo.Context) error {
	if _, err := h.requireSuperadmin(c); err != nil {
		return err
	}

	deletedCount, err := h.repo.CleanupOrphanEmbeddingJobs(c.Request().Context())
	if err != nil {
		return apperror.NewInternal("failed to cleanup orphan embedding jobs", err)
	}

	return c.JSON(http.StatusOK, CleanupOrphansResponse{
		Success:      true,
		DeletedCount: deletedCount,
		Message:      "Orphan embedding jobs cleaned up successfully",
	})
}

// ListExtractionJobs handles GET /api/superadmin/extraction-jobs
// @Summary      List extraction job queue (superadmin only)
// @Description  Returns paginated list of object extraction jobs with status, error details, and aggregate statistics. Supports filtering by status, job type, project, and error state.
// @Tags         superadmin
// @Produce      json
// @Param        page query int false "Page number (default: 1)" minimum(1)
// @Param        limit query int false "Results per page (default: 20, max: 100)" minimum(1) maximum(100)
// @Param        status query string false "Filter by status (pending, processing, completed, failed)"
// @Param        jobType query string false "Filter by job type"
// @Param        projectId query string false "Filter by project ID (UUID)"
// @Param        hasError query boolean false "Filter jobs with errors"
// @Success      200 {object} ListExtractionJobsResponse "Extraction job list with stats and pagination"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Failure      403 {object} apperror.Error "Forbidden (not superadmin)"
// @Router       /api/superadmin/extraction-jobs [get]
// @Security     bearerAuth
func (h *Handler) ListExtractionJobs(c echo.Context) error {
	if _, err := h.requireSuperadmin(c); err != nil {
		return err
	}

	page, limit := getPaginationParams(c)
	status := c.QueryParam("status")
	jobType := c.QueryParam("jobType")
	projectID := c.QueryParam("projectId")
	hasError := getBoolParam(c, "hasError")

	jobs, total, err := h.repo.ListExtractionJobs(c.Request().Context(), page, limit, status, jobType, projectID, hasError)
	if err != nil {
		return apperror.NewInternal("failed to list extraction jobs", err)
	}

	stats, err := h.repo.GetExtractionJobStats(c.Request().Context())
	if err != nil {
		return apperror.NewInternal("failed to get extraction job stats", err)
	}

	return c.JSON(http.StatusOK, ListExtractionJobsResponse{
		Jobs:  jobs,
		Stats: stats,
		Meta:  NewPaginationMeta(page, limit, total),
	})
}

// DeleteExtractionJobs handles POST /api/superadmin/extraction-jobs/delete
// @Summary      Bulk delete extraction jobs (superadmin only)
// @Description  Deletes multiple object extraction jobs by ID.
// @Tags         superadmin
// @Accept       json
// @Produce      json
// @Param        request body DeleteJobsRequest true "Job IDs to delete"
// @Success      200 {object} DeleteJobsResponse "Deletion summary"
// @Failure      400 {object} apperror.Error "Invalid request body"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Failure      403 {object} apperror.Error "Forbidden (not superadmin)"
// @Router       /api/superadmin/extraction-jobs/delete [post]
// @Security     bearerAuth
func (h *Handler) DeleteExtractionJobs(c echo.Context) error {
	if _, err := h.requireSuperadmin(c); err != nil {
		return err
	}

	var req DeleteJobsRequest
	if err := c.Bind(&req); err != nil {
		return apperror.ErrBadRequest
	}
	if len(req.IDs) == 0 {
		return apperror.ErrBadRequest
	}

	deletedCount, err := h.repo.DeleteExtractionJobs(c.Request().Context(), req.IDs)
	if err != nil {
		return apperror.NewInternal("failed to delete extraction jobs", err)
	}

	return c.JSON(http.StatusOK, DeleteJobsResponse{
		Success:      true,
		DeletedCount: deletedCount,
		Message:      "Extraction jobs deleted successfully",
	})
}

// CancelExtractionJobs handles POST /api/superadmin/extraction-jobs/cancel
// @Summary      Bulk cancel extraction jobs (superadmin only)
// @Description  Cancels multiple running/pending object extraction jobs by ID.
// @Tags         superadmin
// @Accept       json
// @Produce      json
// @Param        request body CancelJobsRequest true "Job IDs to cancel"
// @Success      200 {object} CancelJobsResponse "Cancellation summary"
// @Failure      400 {object} apperror.Error "Invalid request body"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Failure      403 {object} apperror.Error "Forbidden (not superadmin)"
// @Router       /api/superadmin/extraction-jobs/cancel [post]
// @Security     bearerAuth
func (h *Handler) CancelExtractionJobs(c echo.Context) error {
	if _, err := h.requireSuperadmin(c); err != nil {
		return err
	}

	var req CancelJobsRequest
	if err := c.Bind(&req); err != nil {
		return apperror.ErrBadRequest
	}
	if len(req.IDs) == 0 {
		return apperror.ErrBadRequest
	}

	cancelledCount, err := h.repo.CancelExtractionJobs(c.Request().Context(), req.IDs)
	if err != nil {
		return apperror.NewInternal("failed to cancel extraction jobs", err)
	}

	return c.JSON(http.StatusOK, CancelJobsResponse{
		Success:        true,
		CancelledCount: cancelledCount,
		Message:        "Extraction jobs cancelled successfully",
	})
}

// ListDocumentParsingJobs handles GET /api/superadmin/document-parsing-jobs
// @Summary      List document parsing job queue (superadmin only)
// @Description  Returns paginated list of document parsing jobs with status, error details, and aggregate statistics. Supports filtering by status, project, and error state.
// @Tags         superadmin
// @Produce      json
// @Param        page query int false "Page number (default: 1)" minimum(1)
// @Param        limit query int false "Results per page (default: 20, max: 100)" minimum(1) maximum(100)
// @Param        status query string false "Filter by status (pending, processing, completed, failed)"
// @Param        projectId query string false "Filter by project ID (UUID)"
// @Param        hasError query boolean false "Filter jobs with errors"
// @Success      200 {object} ListDocumentParsingJobsResponse "Document parsing job list with stats and pagination"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Failure      403 {object} apperror.Error "Forbidden (not superadmin)"
// @Router       /api/superadmin/document-parsing-jobs [get]
// @Security     bearerAuth
func (h *Handler) ListDocumentParsingJobs(c echo.Context) error {
	if _, err := h.requireSuperadmin(c); err != nil {
		return err
	}

	page, limit := getPaginationParams(c)
	status := c.QueryParam("status")
	projectID := c.QueryParam("projectId")
	hasError := getBoolParam(c, "hasError")

	jobs, total, err := h.repo.ListDocumentParsingJobs(c.Request().Context(), page, limit, status, projectID, hasError)
	if err != nil {
		return apperror.NewInternal("failed to list document parsing jobs", err)
	}

	stats, err := h.repo.GetDocumentParsingJobStats(c.Request().Context())
	if err != nil {
		return apperror.NewInternal("failed to get document parsing job stats", err)
	}

	return c.JSON(http.StatusOK, ListDocumentParsingJobsResponse{
		Jobs:  jobs,
		Stats: stats,
		Meta:  NewPaginationMeta(page, limit, total),
	})
}

// DeleteDocumentParsingJobs handles POST /api/superadmin/document-parsing-jobs/delete
// @Summary      Bulk delete document parsing jobs (superadmin only)
// @Description  Deletes multiple document parsing jobs by ID.
// @Tags         superadmin
// @Accept       json
// @Produce      json
// @Param        request body DeleteJobsRequest true "Job IDs to delete"
// @Success      200 {object} DeleteJobsResponse "Deletion summary"
// @Failure      400 {object} apperror.Error "Invalid request body"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Failure      403 {object} apperror.Error "Forbidden (not superadmin)"
// @Router       /api/superadmin/document-parsing-jobs/delete [post]
// @Security     bearerAuth
func (h *Handler) DeleteDocumentParsingJobs(c echo.Context) error {
	if _, err := h.requireSuperadmin(c); err != nil {
		return err
	}

	var req DeleteJobsRequest
	if err := c.Bind(&req); err != nil {
		return apperror.ErrBadRequest
	}
	if len(req.IDs) == 0 {
		return apperror.ErrBadRequest
	}

	deletedCount, err := h.repo.DeleteDocumentParsingJobs(c.Request().Context(), req.IDs)
	if err != nil {
		return apperror.NewInternal("failed to delete document parsing jobs", err)
	}

	return c.JSON(http.StatusOK, DeleteJobsResponse{
		Success:      true,
		DeletedCount: deletedCount,
		Message:      "Document parsing jobs deleted successfully",
	})
}

// RetryDocumentParsingJobs handles POST /api/superadmin/document-parsing-jobs/retry
// @Summary      Bulk retry document parsing jobs (superadmin only)
// @Description  Re-queues multiple failed/cancelled document parsing jobs for retry.
// @Tags         superadmin
// @Accept       json
// @Produce      json
// @Param        request body RetryJobsRequest true "Job IDs to retry"
// @Success      200 {object} RetryJobsResponse "Retry summary"
// @Failure      400 {object} apperror.Error "Invalid request body"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Failure      403 {object} apperror.Error "Forbidden (not superadmin)"
// @Router       /api/superadmin/document-parsing-jobs/retry [post]
// @Security     bearerAuth
func (h *Handler) RetryDocumentParsingJobs(c echo.Context) error {
	if _, err := h.requireSuperadmin(c); err != nil {
		return err
	}

	var req RetryJobsRequest
	if err := c.Bind(&req); err != nil {
		return apperror.ErrBadRequest
	}
	if len(req.IDs) == 0 {
		return apperror.ErrBadRequest
	}

	retriedCount, err := h.repo.RetryDocumentParsingJobs(c.Request().Context(), req.IDs)
	if err != nil {
		return apperror.NewInternal("failed to retry document parsing jobs", err)
	}

	return c.JSON(http.StatusOK, RetryJobsResponse{
		Success:      true,
		RetriedCount: retriedCount,
		Message:      "Document parsing jobs queued for retry",
	})
}

// ListSyncJobs handles GET /api/superadmin/sync-jobs
// @Summary      List data source sync job queue (superadmin only)
// @Description  Returns paginated list of data source sync jobs with status, progress, error details, and aggregate statistics. Supports filtering by status, project, and error state.
// @Tags         superadmin
// @Produce      json
// @Param        page query int false "Page number (default: 1)" minimum(1)
// @Param        limit query int false "Results per page (default: 20, max: 100)" minimum(1) maximum(100)
// @Param        status query string false "Filter by status (pending, running, completed, failed)"
// @Param        projectId query string false "Filter by project ID (UUID)"
// @Param        hasError query boolean false "Filter jobs with errors"
// @Success      200 {object} ListSyncJobsResponse "Sync job list with stats and pagination"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Failure      403 {object} apperror.Error "Forbidden (not superadmin)"
// @Router       /api/superadmin/sync-jobs [get]
// @Security     bearerAuth
func (h *Handler) ListSyncJobs(c echo.Context) error {
	if _, err := h.requireSuperadmin(c); err != nil {
		return err
	}

	page, limit := getPaginationParams(c)
	status := c.QueryParam("status")
	projectID := c.QueryParam("projectId")
	hasError := getBoolParam(c, "hasError")

	jobs, total, err := h.repo.ListSyncJobs(c.Request().Context(), page, limit, status, projectID, hasError)
	if err != nil {
		return apperror.NewInternal("failed to list sync jobs", err)
	}

	stats, err := h.repo.GetSyncJobStats(c.Request().Context())
	if err != nil {
		return apperror.NewInternal("failed to get sync job stats", err)
	}

	return c.JSON(http.StatusOK, ListSyncJobsResponse{
		Jobs:  jobs,
		Stats: stats,
		Meta:  NewPaginationMeta(page, limit, total),
	})
}

// GetSyncJobLogs handles GET /api/superadmin/sync-jobs/:id/logs
// @Summary      Get data source sync job logs (superadmin only)
// @Description  Returns detailed logs and execution info for a specific data source sync job.
// @Tags         superadmin
// @Produce      json
// @Param        id path string true "Sync job ID (UUID)"
// @Success      200 {object} SyncJobLogsResponse "Sync job logs and metadata"
// @Failure      400 {object} apperror.Error "Invalid job ID"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Failure      403 {object} apperror.Error "Forbidden (not superadmin)"
// @Failure      404 {object} apperror.Error "Sync job not found"
// @Router       /api/superadmin/sync-jobs/{id}/logs [get]
// @Security     bearerAuth
func (h *Handler) GetSyncJobLogs(c echo.Context) error {
	if _, err := h.requireSuperadmin(c); err != nil {
		return err
	}

	id := c.Param("id")
	if id == "" {
		return apperror.ErrBadRequest
	}

	job, err := h.repo.GetSyncJob(c.Request().Context(), id)
	if err != nil {
		return apperror.NewInternal("failed to get sync job", err)
	}
	if job == nil {
		return apperror.ErrNotFound
	}

	return c.JSON(http.StatusOK, SyncJobLogsResponse{
		ID:           job.ID,
		Status:       job.Status,
		Logs:         job.Logs,
		ErrorMessage: job.ErrorMessage,
		CreatedAt:    job.CreatedAt,
		StartedAt:    job.StartedAt,
		CompletedAt:  job.CompletedAt,
	})
}

// DeleteSyncJobs handles POST /api/superadmin/sync-jobs/delete
// @Summary      Bulk delete data source sync jobs (superadmin only)
// @Description  Deletes multiple data source sync jobs by ID.
// @Tags         superadmin
// @Accept       json
// @Produce      json
// @Param        request body DeleteJobsRequest true "Job IDs to delete"
// @Success      200 {object} DeleteJobsResponse "Deletion summary"
// @Failure      400 {object} apperror.Error "Invalid request body"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Failure      403 {object} apperror.Error "Forbidden (not superadmin)"
// @Router       /api/superadmin/sync-jobs/delete [post]
// @Security     bearerAuth
func (h *Handler) DeleteSyncJobs(c echo.Context) error {
	if _, err := h.requireSuperadmin(c); err != nil {
		return err
	}

	var req DeleteJobsRequest
	if err := c.Bind(&req); err != nil {
		return apperror.ErrBadRequest
	}
	if len(req.IDs) == 0 {
		return apperror.ErrBadRequest
	}

	deletedCount, err := h.repo.DeleteSyncJobs(c.Request().Context(), req.IDs)
	if err != nil {
		return apperror.NewInternal("failed to delete sync jobs", err)
	}

	return c.JSON(http.StatusOK, DeleteJobsResponse{
		Success:      true,
		DeletedCount: deletedCount,
		Message:      "Sync jobs deleted successfully",
	})
}

// CancelSyncJobs handles POST /api/superadmin/sync-jobs/cancel
// @Summary      Bulk cancel data source sync jobs (superadmin only)
// @Description  Cancels multiple running/pending data source sync jobs by ID.
// @Tags         superadmin
// @Accept       json
// @Produce      json
// @Param        request body CancelJobsRequest true "Job IDs to cancel"
// @Success      200 {object} CancelJobsResponse "Cancellation summary"
// @Failure      400 {object} apperror.Error "Invalid request body"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Failure      403 {object} apperror.Error "Forbidden (not superadmin)"
// @Router       /api/superadmin/sync-jobs/cancel [post]
// @Security     bearerAuth
func (h *Handler) CancelSyncJobs(c echo.Context) error {
	if _, err := h.requireSuperadmin(c); err != nil {
		return err
	}

	var req CancelJobsRequest
	if err := c.Bind(&req); err != nil {
		return apperror.ErrBadRequest
	}
	if len(req.IDs) == 0 {
		return apperror.ErrBadRequest
	}

	cancelledCount, err := h.repo.CancelSyncJobs(c.Request().Context(), req.IDs)
	if err != nil {
		return apperror.NewInternal("failed to cancel sync jobs", err)
	}

	return c.JSON(http.StatusOK, CancelJobsResponse{
		Success:        true,
		CancelledCount: cancelledCount,
		Message:        "Sync jobs cancelled successfully",
	})
}
