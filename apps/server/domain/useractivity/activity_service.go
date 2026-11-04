package useractivity

import (
	"context"
	"database/sql"
	"errors"
	"strconv"
	"time"

	"github.com/emergent-company/emergent.memory/pkg/activity"
	"github.com/emergent-company/emergent.memory/pkg/apperror"
	"github.com/emergent-company/emergent.memory/pkg/auth"
	"github.com/emergent-company/emergent.memory/pkg/logger"
)

// Activity windows offered by the UI. Anything else falls back to the default.
const (
	defaultActivityDays = 30
	maxActivityDays     = 365
)

// ParseActivityRange maps a ?range= value (7d/30d/90d) to a day count.
func ParseActivityRange(raw string) int {
	switch raw {
	case "7d":
		return 7
	case "90d":
		return 90
	case "30d", "":
		return defaultActivityDays
	}
	if n, err := strconv.Atoi(raw); err == nil && n > 0 && n <= maxActivityDays {
		return n
	}
	return defaultActivityDays
}

// OrgActivity returns the org's members ranked by activity. Member PII and
// cross-member activity are admin-only, matching ListMembers.
func (s *Service) OrgActivity(ctx context.Context, orgID, actorID string, days int) (*ActivityResponse, error) {
	ok, err := auth.CanAdministerOrg(ctx, s.repo.db, orgID, actorID)
	if err != nil {
		return nil, apperror.NewDatabase("activity operation failed", err)
	}
	if !ok {
		return nil, apperror.ErrForbidden
	}
	since := time.Now().UTC().AddDate(0, 0, -days)
	rows, err := s.repo.ListOrgActivity(ctx, orgID, since)
	if err != nil {
		return nil, err
	}
	return decorateActivity(rows, days, nil), nil
}

// ProjectActivity returns the project's members ranked by activity. It is
// gated to project admins and org admins; a plain member can see the member
// list but not the cross-member activity ranking.
func (s *Service) ProjectActivity(ctx context.Context, projectID, actorID string, days int) (*ActivityResponse, error) {
	project, err := s.repo.GetProjectOrg(ctx, projectID)
	if err != nil {
		return nil, err
	}
	projectAdmin, err := auth.CanAdministerProject(ctx, s.repo.db, projectID, actorID)
	if err != nil {
		return nil, apperror.NewDatabase("activity operation failed", err)
	}
	orgAdmin := false
	if !projectAdmin && project != "" {
		orgAdmin, err = auth.CanAdministerOrg(ctx, s.repo.db, project, actorID)
		if err != nil {
			return nil, apperror.NewDatabase("activity operation failed", err)
		}
	}
	if !projectAdmin && !orgAdmin {
		return nil, apperror.ErrForbidden
	}
	since := time.Now().UTC().AddDate(0, 0, -days)
	rows, err := s.repo.ListProjectActivity(ctx, projectID, since)
	if err != nil {
		return nil, err
	}
	return decorateActivity(rows, days, nil), nil
}

// MyActivity returns the caller's own summary and is never cross-member.
func (s *Service) MyActivity(ctx context.Context, userID string, days int) (*MyActivityResponse, error) {
	since := time.Now().UTC().AddDate(0, 0, -days)
	row, err := s.repo.MyActivity(ctx, userID, since)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	return &MyActivityResponse{
		RangeDays:    days,
		LastActiveAt: row.LastSeenAt,
		RequestCount: row.RequestCount,
		ActionCount:  row.ActionCount,
		ActiveDays:   row.ActiveDays,
		Tier:         activity.TierFor(row.LastSeenAt, false, now),
		Score:        activity.Score(row.RequestCount, row.ActionCount, row.ActiveDays),
	}, nil
}

// decorateActivity converts raw rows into DTOs, computing tier and score. The
// optional online set lets a caller fold in live presence.
func decorateActivity(rows []MemberActivityRow, days int, online map[string]bool) *ActivityResponse {
	now := time.Now().UTC()
	out := make([]MemberActivityDTO, 0, len(rows))
	for _, row := range rows {
		isOnline := online != nil && online[row.UserID]
		out = append(out, MemberActivityDTO{
			UserID:       row.UserID,
			Email:        row.Email,
			DisplayName:  row.DisplayName,
			FirstName:    row.FirstName,
			LastName:     row.LastName,
			Role:         row.Role,
			JoinedAt:     row.JoinedAt,
			LastActiveAt: row.LastSeenAt,
			LastActionAt: row.LastActionAt,
			RequestCount: row.RequestCount,
			ActionCount:  row.ActionCount,
			ActiveDays:   row.ActiveDays,
			Tier:         activity.TierFor(row.LastSeenAt, isOnline, now),
			Score:        activity.Score(row.RequestCount, row.ActionCount, row.ActiveDays),
		})
	}
	return &ActivityResponse{RangeDays: days, Members: out}
}

// ProjectActivityVisibleTo reports whether actorID may see the project's
// cross-member activity (project admin or owning-org admin).
func (s *Service) ProjectActivityVisibleTo(ctx context.Context, projectID, actorID string) bool {
	ok, err := auth.CanAdministerProject(ctx, s.repo.db, projectID, actorID)
	if err != nil {
		s.log.Debug("project admin lookup failed", logger.Error(err))
		return false
	}
	if ok {
		return true
	}
	orgID, err := s.repo.GetProjectOrg(ctx, projectID)
	if err != nil || orgID == "" {
		return false
	}
	ok, err = auth.CanAdministerOrg(ctx, s.repo.db, orgID, actorID)
	return err == nil && ok
}

// GetProjectOrg returns the owning organization id for a project, or "" when
// the project does not exist.
func (r *Repository) GetProjectOrg(ctx context.Context, projectID string) (string, error) {
	var orgID string
	err := r.db.NewRaw(
		`SELECT organization_id::text FROM kb.projects WHERE id = ?::uuid`, projectID,
	).Scan(ctx, &orgID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", apperror.NewNotFound("Project", projectID)
		}
		r.log.Error("failed to resolve project org", logger.Error(err))
		return "", apperror.NewDatabase("activity operation failed", err)
	}
	return orgID, nil
}
