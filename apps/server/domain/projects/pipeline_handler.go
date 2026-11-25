package projects

import (
	"encoding/json"

	"github.com/labstack/echo/v4"

	"github.com/emergent-company/emergent.memory/domain/pipeline"
	"github.com/emergent-company/emergent.memory/pkg/apperror"
	"github.com/emergent-company/emergent.memory/pkg/auth"
)

// PipelineView is the resolved, read-back view of a project's knowledge-pipeline
// policy: the mode label, the fully-resolved policy JSON, whether the policy
// differs from the mode's preset (customized), and the schema generation the
// policy currently applies to.
type PipelineView struct {
	Mode             string          `json:"mode"`
	Policy           json.RawMessage `json:"policy"`
	Customized       bool            `json:"customized"`
	SchemaGeneration int             `json:"schema_generation"`
}

// PutPipelineRequest updates the policy. Setting Mode alone resets the policy to
// that mode's defaults; setting Policy stores the resolved policy verbatim.
// When both are set, Policy wins and Mode is derived from Policy.Mode (or kept
// when Policy omits a mode).
type PutPipelineRequest struct {
	Mode   *string          `json:"mode,omitempty"`
	Policy *json.RawMessage `json:"policy,omitempty"`
}

// resolvePipeline parses a stored policy and falls back to the mode preset when
// the stored value is empty (legacy rows). It returns whether the stored policy
// is customized relative to the preset.
func resolvePipeline(mode string, raw json.RawMessage) (pipeline.Policy, bool, error) {
	p, err := pipeline.Parse(raw)
	if err != nil {
		return pipeline.Policy{}, false, err
	}
	if p.IsZero() {
		return pipeline.DefaultPolicy(mode), false, nil
	}
	if err := pipeline.Validate(p); err != nil {
		return pipeline.Policy{}, false, err
	}
	return p, !p.Equal(pipeline.DefaultPolicy(mode)), nil
}

// GetPipeline handles GET /api/projects/:id/pipeline.
func (h *Handler) GetPipeline(c echo.Context) error {
	user := auth.MustGetUser(c)
	id := c.Param("id")

	if err := h.svc.authorizeProject(c.Request().Context(), id, user.ID, accessProjectMember); err != nil {
		return err
	}

	project, err := h.svc.repo.GetByID(c.Request().Context(), id, false)
	if err != nil {
		return err
	}
	if project == nil {
		return apperror.ErrProjectNotFound
	}

	p, customized, err := resolvePipeline(project.PipelineMode, project.PipelinePolicy)
	if err != nil {
		return apperror.New(500, "policy-invalid", "stored pipeline_policy is invalid").WithDetails(map[string]any{
			"pipeline_policy": []string{err.Error()},
		})
	}

	return c.JSON(200, PipelineView{
		Mode:             project.PipelineMode,
		Policy:           p.Marshal(),
		Customized:       customized,
		SchemaGeneration: project.SchemaGeneration,
	})
}

// applyPipelineUpdate applies a pipeline policy update to a project in place.
// Policy wins over mode when both are present; a mode-only update resets the
// policy to that preset's defaults. It returns a 400 apperror on invalid input.
func applyPipelineUpdate(project *Project, req UpdateProjectRequest) error {
	if req.PipelinePolicy != nil {
		p, err := pipeline.Parse(*req.PipelinePolicy)
		if err != nil {
			return apperror.New(400, "validation-failed", "invalid pipeline_policy").WithDetails(map[string]any{
				"pipeline_policy": []string{err.Error()},
			})
		}
		if p.Mode == "" {
			p.Mode = pipeline.ModeSuggest
		}
		if err := pipeline.Validate(p); err != nil {
			return apperror.New(400, "validation-failed", "invalid pipeline_policy").WithDetails(map[string]any{
				"pipeline_policy": []string{err.Error()},
			})
		}
		project.PipelinePolicy = p.Marshal()
		project.PipelineMode = p.Mode
		return nil
	}

	mode := *req.PipelineMode
	if err := pipeline.Validate(pipeline.Policy{Mode: mode}); err != nil {
		return apperror.New(400, "validation-failed", "invalid pipeline_mode").WithDetails(map[string]any{
			"pipeline_mode": []string{err.Error()},
		})
	}
	project.PipelineMode = mode
	project.PipelinePolicy = pipeline.DefaultPolicy(mode).Marshal()
	return nil
}

// PutPipeline handles PUT /api/projects/:id/pipeline.
func (h *Handler) PutPipeline(c echo.Context) error {
	user := auth.MustGetUser(c)
	id := c.Param("id")

	if err := h.svc.authorizeProject(c.Request().Context(), id, user.ID, accessProjectAdmin); err != nil {
		return err
	}

	var req PutPipelineRequest
	if err := c.Bind(&req); err != nil {
		return apperror.ErrBadRequest.WithMessage("invalid request body")
	}

	upd := UpdateProjectRequest{}
	if req.Mode != nil {
		upd.PipelineMode = req.Mode
	}
	if req.Policy != nil {
		upd.PipelinePolicy = req.Policy
	}

	if _, err := h.svc.Update(c.Request().Context(), id, upd); err != nil {
		return err
	}

	return h.GetPipeline(c)
}

// PreviewPipeline handles POST /api/projects/:id/pipeline/preview. In this first
// phase it returns the resolved policy and schema generation; apply/backfill
// forecasts are added once auto-apply lands.
func (h *Handler) PreviewPipeline(c echo.Context) error {
	return h.GetPipeline(c)
}
