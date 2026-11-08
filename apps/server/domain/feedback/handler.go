package feedback

import (
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/labstack/echo/v4"

	"github.com/emergent-company/emergent.memory/pkg/apperror"
	"github.com/emergent-company/emergent.memory/pkg/auth"
	"github.com/emergent-company/emergent.memory/pkg/logger"
)

// Keep apperror imported for swagger type resolution (@Failure {object} apperror.Error).
var _ = apperror.Error{}

// Handler serves the feedback REST surface.
type Handler struct {
	svc *Service
	log *slog.Logger
}

// NewHandler creates the feedback handler.
func NewHandler(svc *Service, log *slog.Logger) *Handler {
	if log == nil {
		log = slog.Default()
	}
	return &Handler{svc: svc, log: log.With(logger.Scope("feedback.handler"))}
}

// SubmitRequest is the body of POST /api/feedback.
type SubmitRequest struct {
	// Message is the user's feedback. Required.
	Message string `json:"message" example:"The MCP search tool returns stale results after I update an entity."`
	// Category is bug, feature, usability, docs, or other. Defaults to other.
	Category string `json:"category,omitempty" example:"bug"`
	// Severity is low, medium, or high. Optional.
	Severity string `json:"severity,omitempty" example:"medium"`
	// IncludeContext attaches project, environment and client details. Defaults
	// to true; the submitter's account identity is always attached.
	IncludeContext *bool `json:"include_context,omitempty" example:"true"`
}

// SubmitResponse is the result of a filed submission.
type SubmitResponse struct {
	IssueNumber int    `json:"issue_number"`
	IssueURL    string `json:"issue_url"`
	Duplicate   bool   `json:"duplicate,omitempty"`
	Message     string `json:"message"`
}

// Submit handles POST /api/feedback.
// @Summary      Submit feedback about Memory
// @Description  Files feedback from the authenticated user (or their agent) as a GitHub issue, with the submitter's account identity, project and basic environment details attached automatically. Write what you were trying to do, what happened, and what you expected; one or two sentences is enough.
// @Tags         feedback
// @Accept       json
// @Produce      json
// @Param        request body SubmitRequest true "Feedback (message is required)"
// @Success      201 {object} SubmitResponse "Feedback filed"
// @Failure      400 {object} apperror.Error "Invalid feedback"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Failure      429 {object} apperror.Error "Per-user submission limit reached"
// @Failure      502 {object} apperror.Error "GitHub issue creation failed"
// @Failure      503 {object} apperror.Error "Feedback is not enabled on this deployment"
// @Router       /api/feedback [post]
// @Security     bearerAuth
func (h *Handler) Submit(c echo.Context) error {
	user := auth.MustGetUser(c)

	var req SubmitRequest
	if err := c.Bind(&req); err != nil {
		return apperror.NewBadRequest("invalid JSON body")
	}

	includeContext := true
	if req.IncludeContext != nil {
		includeContext = *req.IncludeContext
	}

	// Neither source is trusted on its own: RequireAuth copies the raw
	// X-Project-ID header straight into user.ProjectID (pkg/auth/middleware.go),
	// and the request context can carry the same caller-supplied value. Keep an
	// identifier only when it is a well-formed UUID, so a stray value is neither
	// rendered into the issue nor used for a lookup that logs a warning on every
	// submission.
	ctx := c.Request().Context()
	projectID := normaliseID(auth.ProjectIDFromContext(ctx))
	if projectID == "" {
		projectID = normaliseID(user.ProjectID)
	}
	orgID := normaliseID(auth.OrgIDFromContext(ctx))
	if orgID == "" {
		orgID = normaliseID(user.OrgID)
	}

	result, err := h.svc.Submit(ctx, Submission{
		Message:        req.Message,
		Category:       req.Category,
		Severity:       req.Severity,
		IncludeContext: includeContext,
		Source:         SourceAPI,
		Client:         c.Request().UserAgent(),
		UserID:         user.ID,
		UserEmail:      user.Email,
		ProjectID:      projectID,
		OrgID:          orgID,
	})
	if err != nil {
		return h.mapError(c, err)
	}

	message := "Thanks — your feedback was filed."
	if result.Duplicate {
		message = "This feedback was already filed a few minutes ago; here is the existing issue."
	}
	return c.JSON(http.StatusCreated, SubmitResponse{
		IssueNumber: result.IssueNumber,
		IssueURL:    result.IssueURL,
		Duplicate:   result.Duplicate,
		Message:     message,
	})
}

func (h *Handler) mapError(c echo.Context, err error) error {
	var validation *ValidationError
	if errors.As(err, &validation) {
		return apperror.NewBadRequest(validation.Message)
	}

	var limited *RateLimitError
	if errors.As(err, &limited) {
		seconds := int(limited.RetryAfter.Round(time.Second) / time.Second)
		if seconds < 1 {
			seconds = 1
		}
		c.Response().Header().Set("Retry-After", strconv.Itoa(seconds))
		return apperror.New(http.StatusTooManyRequests, "feedback_rate_limited",
			"You have submitted a lot of feedback in the last hour. Please try again later.")
	}

	if errors.Is(err, ErrNotConfigured) {
		return apperror.NewServiceUnavailable("Feedback is not enabled on this deployment.", nil)
	}

	h.log.Warn("feedback submission failed", logger.Error(err))
	return apperror.New(http.StatusBadGateway, "feedback_delivery_failed",
		"Feedback could not be filed right now. Please try again later.")
}
