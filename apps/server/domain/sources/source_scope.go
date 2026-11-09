package sources

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/emergent-company/emergent.memory/pkg/apperror"
)

// SourceScopeInput is the user-editable sync scope of a typed source.
type SourceScopeInput struct {
	Filter               map[string]any `json:"filter,omitempty"`
	ObjectTypes          []string       `json:"objectTypes,omitempty"`
	MCPWriteToolsEnabled bool           `json:"mcpWriteToolsEnabled"`
}

// SourceScopeRequest is a partial scope change. Omitted fields keep the stored
// value; MCPWriteToolsEnabled is a pointer so false is expressible.
type SourceScopeRequest struct {
	Filter               map[string]any `json:"filter"`
	ObjectTypes          []string       `json:"objectTypes"`
	MCPWriteToolsEnabled *bool          `json:"mcpWriteToolsEnabled"`
}

// SourceScopePreview is the validated result of a proposed scope.
type SourceScopePreview struct {
	ProviderType  string           `json:"providerType"`
	CompiledQuery string           `json:"compiledQuery"`
	Current       SourceScopeInput `json:"current"`
	Proposed      SourceScopeInput `json:"proposed"`
	PreviewHash   string           `json:"previewHash"`
	BaseScopeHash string           `json:"baseScopeHash"`
	Changed       bool             `json:"changed"`
}

// PreviewSourceScope validates and hashes a proposed scope without persisting.
func (s *Service) PreviewSourceScope(ctx context.Context, projectID, sourceID uuid.UUID, req *SourceScopeRequest) (*SourceScopePreview, error) {
	src, err := s.repo.GetByID(ctx, sourceID, projectID)
	if err != nil {
		return nil, err
	}
	provider, ok := s.providers.Get(src.Provider)
	if !ok {
		return nil, apperror.NewValidation("unsupported provider: " + src.Provider)
	}
	if src.Provider != ProviderTypeGmail {
		return nil, apperror.NewValidation("scope editing is not supported for provider " + src.Provider)
	}
	current := sourceScopeFromConfig(src.Config)
	proposed := mergeSourceScope(current, req)
	cfg, err := configWithScope(src.Config, proposed)
	if err != nil {
		return nil, err
	}
	if err := provider.ValidateConfig(cfg); err != nil {
		return nil, err
	}
	previewProvider, ok := provider.(FilterPreviewProvider)
	if !ok {
		return nil, apperror.NewValidation("provider does not support filter preview")
	}
	query, err := previewProvider.FilterPreview(cfg)
	if err != nil {
		return nil, err
	}
	return &SourceScopePreview{
		ProviderType:  src.Provider,
		CompiledQuery: query,
		Current:       current,
		Proposed:      proposed,
		PreviewHash:   sourceScopeHash(src.Provider, proposed),
		BaseScopeHash: sourceScopeHash(src.Provider, current),
		Changed:       sourceScopeHash(src.Provider, current) != sourceScopeHash(src.Provider, proposed),
	}, nil
}

// UpdateSourceScope applies a validated, hash-guarded scope change.
func (s *Service) UpdateSourceScope(ctx context.Context, projectID, sourceID uuid.UUID, req *SourceScopeRequest, previewHash, baseScopeHash string) (*SourceScopePreview, error) {
	if strings.TrimSpace(previewHash) == "" || strings.TrimSpace(baseScopeHash) == "" {
		return nil, apperror.NewValidation("previewHash and baseScopeHash are required; call source-scope-preview first")
	}
	src, err := s.repo.GetByIDWithCredentials(ctx, sourceID, projectID)
	if err != nil {
		return nil, err
	}
	provider, ok := s.providers.Get(src.Provider)
	if !ok {
		return nil, apperror.NewValidation("unsupported provider: " + src.Provider)
	}
	if src.Provider != ProviderTypeGmail {
		return nil, apperror.NewValidation("scope editing is not supported for provider " + src.Provider)
	}
	current := sourceScopeFromConfig(src.Config)
	proposed := mergeSourceScope(current, req)
	currentHash := sourceScopeHash(src.Provider, current)
	if currentHash != strings.TrimSpace(baseScopeHash) {
		return nil, apperror.NewConflict("source scope changed since preview; run preview again")
	}
	proposedHash := sourceScopeHash(src.Provider, proposed)
	if proposedHash != strings.TrimSpace(previewHash) {
		return nil, apperror.NewConflict("preview hash does not match the proposed scope; run preview again")
	}
	cfg, err := configWithScope(src.Config, proposed)
	if err != nil {
		return nil, err
	}
	if err := provider.ValidateConfig(cfg); err != nil {
		return nil, err
	}
	previewProvider := provider.(FilterPreviewProvider)
	query, err := previewProvider.FilterPreview(cfg)
	if err != nil {
		return nil, err
	}
	changed := currentHash != proposedHash
	if changed {
		src.Config = cfg
		if _, err := s.repo.Update(ctx, src); err != nil {
			return nil, err
		}
		if err := s.repo.ResetGmailScopeCursor(ctx, projectID, sourceID); err != nil {
			return nil, err
		}
		if err := s.provisionSourceMCP(ctx, src); err != nil {
			return nil, apperror.NewInternal("provision source MCP server", err)
		}
		s.log.Info("source scope updated",
			"source_id", sourceID,
			"project_id", projectID,
			"compiled_query", query,
			"component", "sources.scope")
	}
	return &SourceScopePreview{
		ProviderType:  src.Provider,
		CompiledQuery: query,
		Current:       current,
		Proposed:      proposed,
		PreviewHash:   proposedHash,
		BaseScopeHash: currentHash,
		Changed:       changed,
	}, nil
}

func sourceScopeFromConfig(cfg map[string]any) SourceScopeInput {
	out := SourceScopeInput{ObjectTypes: []string{"EmailMessage", "EmailThread"}}
	if v, ok := cfg["filter"].(map[string]any); ok {
		out.Filter = cloneScopeMap(v)
	}
	if types, err := stringList(cfg["object_types"]); err == nil && len(types) > 0 {
		out.ObjectTypes = types
	}
	if v, ok := cfg["mcp_write_tools_enabled"].(bool); ok {
		out.MCPWriteToolsEnabled = v
	}
	sort.Strings(out.ObjectTypes)
	return out
}

func mergeSourceScope(current SourceScopeInput, req *SourceScopeRequest) SourceScopeInput {
	out := SourceScopeInput{
		Filter:               cloneScopeMap(current.Filter),
		ObjectTypes:          append([]string(nil), current.ObjectTypes...),
		MCPWriteToolsEnabled: current.MCPWriteToolsEnabled,
	}
	if req == nil {
		return out
	}
	if req.Filter != nil {
		out.Filter = cloneScopeMap(req.Filter)
	}
	if req.ObjectTypes != nil {
		out.ObjectTypes = append([]string(nil), req.ObjectTypes...)
	}
	if req.MCPWriteToolsEnabled != nil {
		out.MCPWriteToolsEnabled = *req.MCPWriteToolsEnabled
	}
	sort.Strings(out.ObjectTypes)
	return out
}

func configWithScope(base map[string]any, scope SourceScopeInput) (map[string]any, error) {
	cfg := cloneScopeMap(base)
	cfg["filter"] = cloneScopeMap(scope.Filter)
	cfg["object_types"] = append([]string(nil), scope.ObjectTypes...)
	cfg["mcp_write_tools_enabled"] = scope.MCPWriteToolsEnabled
	// Reject accidental mutation outside the scope-owned keys.
	return cfg, nil
}

func sourceScopeHash(provider string, scope SourceScopeInput) string {
	payload := map[string]any{
		"provider":             provider,
		"filter":               scope.Filter,
		"objectTypes":          scope.ObjectTypes,
		"mcpWriteToolsEnabled": scope.MCPWriteToolsEnabled,
	}
	b, _ := json.Marshal(payload)
	sum := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func cloneScopeMap(in map[string]any) map[string]any {
	if in == nil {
		return nil
	}
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

// ResetGmailScopeCursor clears the Gmail backfill cursor so the next sync walks
// the mailbox under the new scope.
func (r *Repository) ResetGmailScopeCursor(ctx context.Context, projectID, sourceID uuid.UUID) error {
	_, err := r.db.NewUpdate().
		Model((*Source)(nil)).
		Set("cursor = jsonb_build_object()").
		Set("updated_at = ?", time.Now().UTC()).
		Where("id = ?", sourceID).
		Where("project_id = ?", projectID).
		Exec(ctx)
	if err != nil {
		r.log.Error("failed to reset gmail scope cursor", "error", err)
		return apperror.NewDatabase("failed to reset gmail scope cursor", err)
	}
	return nil
}
