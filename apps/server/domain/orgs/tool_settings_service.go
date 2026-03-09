package orgs

import (
	"context"
	"log/slog"

	"github.com/emergent-company/emergent.memory/pkg/apperror"
)

// GetOrgToolSettings returns all tool settings for an org.
// The caller must be a member of the org.
func (s *Service) GetOrgToolSettings(ctx context.Context, orgID, userID string) ([]OrgToolSettingDTO, error) {
	if err := s.requireOrgMember(ctx, orgID, userID); err != nil {
		return nil, err
	}

	settings, err := s.repo.FindOrgToolSettings(ctx, orgID)
	if err != nil {
		return nil, err
	}

	dtos := make([]OrgToolSettingDTO, len(settings))
	for i, s := range settings {
		dtos[i] = s.ToDTO()
	}
	return dtos, nil
}

// UpsertOrgToolSetting creates or updates an org-level tool setting.
// The caller must be a member of the org.
func (s *Service) UpsertOrgToolSetting(ctx context.Context, orgID, toolName, userID string, req UpsertOrgToolSettingRequest) (*OrgToolSettingDTO, error) {
	if err := s.requireOrgMember(ctx, orgID, userID); err != nil {
		return nil, err
	}

	if toolName == "" {
		return nil, apperror.ErrBadRequest.WithMessage("tool name is required")
	}

	setting := &OrgToolSetting{
		OrgID:    orgID,
		ToolName: toolName,
		Enabled:  req.Enabled,
		Config:   req.Config,
	}

	saved, err := s.repo.UpsertOrgToolSetting(ctx, setting)
	if err != nil {
		return nil, err
	}

	s.log.Info("org tool setting upserted",
		slog.String("orgID", orgID),
		slog.String("toolName", toolName),
		slog.Bool("enabled", req.Enabled))

	// Org settings affect all projects — invalidate entire tool pool cache.
	if s.toolPoolInvalidator != nil {
		s.toolPoolInvalidator.InvalidateAll()
	}

	dto := saved.ToDTO()
	return &dto, nil
}

// DeleteOrgToolSetting removes an org-level tool setting override.
// The caller must be a member of the org.
func (s *Service) DeleteOrgToolSetting(ctx context.Context, orgID, toolName, userID string) error {
	if err := s.requireOrgMember(ctx, orgID, userID); err != nil {
		return err
	}

	deleted, err := s.repo.DeleteOrgToolSetting(ctx, orgID, toolName)
	if err != nil {
		return err
	}
	if !deleted {
		return apperror.ErrNotFound.WithMessage("org tool setting not found")
	}

	s.log.Info("org tool setting deleted",
		slog.String("orgID", orgID),
		slog.String("toolName", toolName))

	// Org settings affect all projects — invalidate entire tool pool cache.
	if s.toolPoolInvalidator != nil {
		s.toolPoolInvalidator.InvalidateAll()
	}

	return nil
}

// requireOrgMember returns an error if the user is not a member of the org.
func (s *Service) requireOrgMember(ctx context.Context, orgID, userID string) error {
	if userID == "" {
		return apperror.ErrUnauthorized
	}
	member, err := s.repo.IsUserMember(ctx, orgID, userID)
	if err != nil {
		return err
	}
	if !member {
		return apperror.ErrForbidden
	}
	return nil
}
