package documents

import (
	"context"
	"log/slog"

	"github.com/labstack/echo/v4"

	"github.com/emergent-company/emergent.memory/pkg/logger"
)

// ProjectAutoExtractReader resolves a project's auto_extract_objects setting.
// It is defined here (in the documents package) so the upload/create handlers can
// honour the project default without importing the projects or extraction
// packages, which would create an import cycle. The extraction module provides
// the implementation.
type ProjectAutoExtractReader interface {
	AutoExtractObjectsForProject(ctx context.Context, projectID string) (bool, error)
}

// requestedAutoExtractFromQuery returns the explicit autoExtract query parameter
// as a tri-state: nil when the caller did not specify it. An explicit value
// (including "false") always overrides the project default.
func requestedAutoExtractFromQuery(c echo.Context) *bool {
	if !c.QueryParams().Has("autoExtract") {
		return nil
	}
	v := c.QueryParam("autoExtract") == "true"
	return &v
}

// requestedAutoExtractFromForm returns the explicit autoExtract form field as a
// tri-state: nil when the caller did not specify it. FormParams may return an
// error; it is ignored and treated as "not specified".
func requestedAutoExtractFromForm(c echo.Context) *bool {
	params, _ := c.FormParams()
	if !params.Has("autoExtract") {
		return nil
	}
	v := c.FormValue("autoExtract") == "true"
	return &v
}

// resolveAutoExtract applies the tri-state override semantics:
//   - an explicit request value always wins;
//   - with no reader, auto-extract is off;
//   - a reader error is logged and treated as off so document creation never
//     fails because the project default could not be read;
//   - an unknown project resolves to off.
func resolveAutoExtract(ctx context.Context, log *slog.Logger, reader ProjectAutoExtractReader, projectID string, requested *bool) bool {
	if requested != nil {
		return *requested
	}
	if reader == nil {
		return false
	}
	enabled, err := reader.AutoExtractObjectsForProject(ctx, projectID)
	if err != nil {
		log.Warn("failed to resolve project auto-extract setting",
			slog.String("project_id", projectID),
			logger.Error(err))
		return false
	}
	return enabled
}

// effectiveAutoExtract resolves the effective auto-extract value for a raw-content
// document create.
func (h *Handler) effectiveAutoExtract(ctx context.Context, projectID string, requested *bool) bool {
	return resolveAutoExtract(ctx, h.log, h.projectAutoExtract, projectID, requested)
}

// effectiveAutoExtract resolves the effective auto-extract value for an upload.
func (h *UploadHandler) effectiveAutoExtract(ctx context.Context, projectID string, requested *bool) bool {
	return resolveAutoExtract(ctx, h.log, h.projectAutoExtract, projectID, requested)
}
