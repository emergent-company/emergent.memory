package superadmin

import (
	"net/http"
	"strconv"

	"github.com/labstack/echo/v4"

	"github.com/emergent/emergent-core/pkg/apperror"
	"github.com/emergent/emergent-core/pkg/auth"
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
// Returns the current user's superadmin status
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
