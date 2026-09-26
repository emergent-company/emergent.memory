package orgs

import (
	"context"
	"log/slog"

	"github.com/emergent-company/emergent.memory/pkg/apperror"
)

// GetOrgToolSettings returns all tool settings for an org.
// The caller must be a member of the org (tool settings are not PII; a member
// may read which tools are enabled for their org).
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
// The caller must be an org_admin of the org (org-wide tool settings are an
// org-tier write).
func (s *Service) UpsertOrgToolSetting(ctx context.Context, orgID, toolName, userID string, req UpsertOrgToolSettingRequest) (*OrgToolSettingDTO, error) {
	if err := s.requireOrgAdmin(ctx, orgID, userID); err != nil {
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
// The caller must be an org_admin of the org.
func (s *Service) DeleteOrgToolSetting(ctx context.Context, orgID, toolName, userID string) error {
	if err := s.requireOrgAdmin(ctx, orgID, userID); err != nil {
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

// requireOrgAdmin returns an error unless the user is an org_admin of the
// addressed org. The role is derived server-side from
// kb.organization_memberships keyed on the authenticated user ID (never a
// client-supplied org id), reusing the same GetMembershipRole primitive that
// projects.Transfer relies on. An empty user (unauthenticated) yields
// ErrUnauthorized; a non-member or a plain member yields ErrForbidden
// (uniform fail-closed, no org-existence oracle — matching requireOrgMember).
func (s *Service) requireOrgAdmin(ctx context.Context, orgID, userID string) error {
	if userID == "" {
		return apperror.ErrUnauthorized
	}
	role, err := s.repo.GetMembershipRole(ctx, orgID, userID)
	if err != nil {
		return err
	}
	if role != "org_admin" {
		return apperror.ErrForbidden
	}
	return nil
}
