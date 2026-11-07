package chunking

import (
	"context"
	"encoding/json"

	"github.com/emergent-company/emergent.memory/pkg/apperror"
	"github.com/emergent-company/emergent.memory/pkg/textsplitter"
)

const (
	minChunkSize = 100
	maxChunkSize = 25000
)

// ChunkingConfigResponse is the effective chunking configuration for a project.
type ChunkingConfigResponse struct {
	Strategy     string `json:"strategy"`
	MaxChunkSize int    `json:"maxChunkSize"`
	Overlap      int    `json:"overlap"`
	ParentChild  bool   `json:"parentChild"`
	// AutoProfile selects chunking parameters per document from its detected
	// document shape instead of applying Strategy/MaxChunkSize/Overlap/
	// ParentChild project-wide.
	AutoProfile bool `json:"autoProfile"`
	// Profiles lists the per-shape parameters auto-profile will use. Present
	// only when AutoProfile is true.
	Profiles []ShapeProfile `json:"profiles,omitempty"`
}

// ShapeProfile is one document shape's chunking parameters.
type ShapeProfile struct {
	Shape        string `json:"shape"`
	Strategy     string `json:"strategy"`
	MaxChunkSize int    `json:"maxChunkSize"`
	Overlap      int    `json:"overlap"`
	ParentChild  bool   `json:"parentChild"`
}

// UpdateChunkingConfigRequest is the body for writing a project's chunking
// configuration. Absent fields fall back to defaults.
type UpdateChunkingConfigRequest struct {
	Strategy     *string `json:"strategy"`
	MaxChunkSize *int    `json:"maxChunkSize"`
	Overlap      *int    `json:"overlap"`
	ParentChild  *bool   `json:"parentChild"`
	AutoProfile  *bool   `json:"autoProfile"`
}

// validateChunkingConfig applies defaults and validates strategy and ranges.
// It is pure so it can be unit-tested without a database.
func validateChunkingConfig(req UpdateChunkingConfigRequest) (strategy string, size, overlap int, parentChild bool, err error) {
	def := textsplitter.DefaultConfig()

	strategy = string(textsplitter.NormalizeStrategy(derefString(req.Strategy)))
	if !textsplitter.IsSupportedStrategy(strategy) {
		return "", 0, 0, false, apperror.NewBadRequest("unsupported chunking strategy: " + strategy)
	}

	size = def.ChunkSize
	if req.MaxChunkSize != nil {
		size = *req.MaxChunkSize
	}
	if size < minChunkSize || size > maxChunkSize {
		return "", 0, 0, false, apperror.NewBadRequest("maxChunkSize must be between 100 and 25000")
	}

	overlap = def.ChunkOverlap
	if req.Overlap != nil {
		overlap = *req.Overlap
	}
	if overlap < 0 || overlap >= size {
		return "", 0, 0, false, apperror.NewBadRequest("overlap must be >= 0 and less than maxChunkSize")
	}

	parentChild = false
	if req.ParentChild != nil {
		parentChild = *req.ParentChild
	}
	if parentChild && strategy != string(textsplitter.StrategyMarkdownStructure) {
		return "", 0, 0, false, apperror.NewBadRequest("parentChild requires the markdown_structure strategy")
	}

	return strategy, size, overlap, parentChild, nil
}

// validateChunkingUpdate applies auto-profile semantics on top of
// validateChunkingConfig: under auto-profile the stored strategy and
// parent-child switch are unused, so their cross-field rule must not reject
// the write. Pure, so it is unit-testable without a database.
func validateChunkingUpdate(req UpdateChunkingConfigRequest) (strategy string, size, overlap int, parentChild, autoProfile bool, err error) {
	autoProfile = req.AutoProfile != nil && *req.AutoProfile
	if autoProfile {
		req.ParentChild = nil
	}
	strategy, size, overlap, parentChild, err = validateChunkingConfig(req)
	return strategy, size, overlap, parentChild, autoProfile, err
}

// GetChunkingConfig returns the effective chunking configuration for a project.
func (s *Service) GetChunkingConfig(ctx context.Context, projectID string) (*ChunkingConfigResponse, error) {
	proj := s.loadProjectChunkingConfig(ctx, projectID)

	strategy := string(textsplitter.NormalizeStrategy(derefString(proj.Strategy)))
	size := s.defaultChunkSize
	if proj.ChunkSize != nil {
		size = *proj.ChunkSize
	}
	overlap := s.defaultChunkOverlap
	if proj.ChunkOverlap != nil {
		overlap = *proj.ChunkOverlap
	}
	parentChild := proj.ParentChild != nil && *proj.ParentChild
	autoProfile := proj.AutoProfile != nil && *proj.AutoProfile
	resp := &ChunkingConfigResponse{
		Strategy:     strategy,
		MaxChunkSize: size,
		Overlap:      overlap,
		ParentChild:  parentChild,
		AutoProfile:  autoProfile,
	}
	if autoProfile {
		resp.Profiles = shapeProfiles()
	}
	return resp, nil
}

// shapeProfiles exposes the built-in per-shape parameters for discovery.
func shapeProfiles() []ShapeProfile {
	profiles := textsplitter.DefaultProfiles()
	out := make([]ShapeProfile, 0, len(profiles))
	for _, p := range profiles {
		out = append(out, ShapeProfile{
			Shape:        string(p.Shape),
			Strategy:     string(p.Options.Strategy),
			MaxChunkSize: p.Config.ChunkSize,
			Overlap:      p.Config.ChunkOverlap,
			ParentChild:  p.Options.ParentChild,
		})
	}
	return out
}

// UpdateChunkingConfig validates and persists a project's chunking configuration.
func (s *Service) UpdateChunkingConfig(ctx context.Context, projectID string, req UpdateChunkingConfigRequest) (*ChunkingConfigResponse, error) {
	strategy, size, overlap, parentChild, autoProfile, err := validateChunkingUpdate(req)
	if err != nil {
		return nil, err
	}

	cfgJSON, err := json.Marshal(map[string]any{
		"strategy":     strategy,
		"maxChunkSize": size,
		"overlap":      overlap,
		"parentChild":  parentChild,
		"autoProfile":  autoProfile,
	})
	if err != nil {
		return nil, apperror.NewInternal("failed to encode chunking config", err)
	}

	res, err := s.db.NewUpdate().
		TableExpr("kb.projects").
		Set("chunking_config = ?::jsonb", string(cfgJSON)).
		Set("updated_at = now()").
		Where("id = ?", projectID).
		Exec(ctx)
	if err != nil {
		return nil, apperror.NewInternal("failed to update project chunking config", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return nil, apperror.NewNotFound("project", projectID)
	}

	resp := &ChunkingConfigResponse{
		Strategy:     strategy,
		MaxChunkSize: size,
		Overlap:      overlap,
		ParentChild:  parentChild,
		AutoProfile:  autoProfile,
	}
	if autoProfile {
		resp.Profiles = shapeProfiles()
	}
	return resp, nil
}
