package docs

import (
	"net/http"

	"github.com/labstack/echo/v4"

	"github.com/emergent-company/emergent/pkg/apperror"
)

// Handler handles HTTP requests for documentation
type Handler struct {
	svc *Service
}

// NewHandler creates a new documentation handler
func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

// ListDocuments handles GET /api/docs
// @Summary      List all documentation
// @Description  Returns a list of all available documentation files with metadata (title, category, description, tags, etc.) but without content
// @Tags         documentation
// @Produce      json
// @Success      200 {object} DocumentList "List of documentation with metadata"
// @Failure      500 {object} apperror.Error "Internal server error"
// @Router       /api/docs [get]
func (h *Handler) ListDocuments(c echo.Context) error {
	docs, err := h.svc.ListDocuments()
	if err != nil {
		return apperror.ErrInternal.WithMessage("failed to list documents").WithInternal(err)
	}

	return c.JSON(http.StatusOK, DocumentList{
		Documents: docs,
		Total:     len(docs),
	})
}

// GetDocument handles GET /api/docs/:slug
// @Summary      Get documentation by slug
// @Description  Returns the full documentation document including markdown content for the specified slug (e.g., "template-pack-creation")
// @Tags         documentation
// @Produce      json
// @Param        slug path string true "Document slug (e.g., template-pack-creation)"
// @Success      200 {object} Document "Full document with markdown content"
// @Failure      404 {object} apperror.Error "Document not found"
// @Failure      500 {object} apperror.Error "Internal server error"
// @Router       /api/docs/{slug} [get]
func (h *Handler) GetDocument(c echo.Context) error {
	slug := c.Param("slug")
	if slug == "" {
		return apperror.ErrBadRequest.WithMessage("slug is required")
	}

	doc, err := h.svc.GetDocument(slug)
	if err != nil {
		// Check if it's a "not found" error
		if err.Error() == "document not found: "+slug {
			return apperror.ErrNotFound.WithMessage("document not found")
		}
		return apperror.ErrInternal.WithMessage("failed to get document").WithInternal(err)
	}

	return c.JSON(http.StatusOK, doc)
}

// GetCategories handles GET /api/docs/categories
// @Summary      Get documentation categories
// @Description  Returns all documentation categories with their metadata (name, description, icon) from index.json
// @Tags         documentation
// @Produce      json
// @Success      200 {object} CategoriesResponse "List of categories"
// @Failure      500 {object} apperror.Error "Internal server error"
// @Router       /api/docs/categories [get]
func (h *Handler) GetCategories(c echo.Context) error {
	categories, err := h.svc.GetCategories()
	if err != nil {
		return apperror.ErrInternal.WithMessage("failed to get categories").WithInternal(err)
	}

	return c.JSON(http.StatusOK, CategoriesResponse{
		Categories: categories,
		Total:      len(categories),
	})
}
