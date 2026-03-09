package orgs

import (
	"context"
	"database/sql"
	"log/slog"
	"time"

	"github.com/emergent-company/emergent.memory/pkg/apperror"
	"github.com/emergent-company/emergent.memory/pkg/logger"
)

// FindOrgToolSettings returns all tool settings for an org.
func (r *Repository) FindOrgToolSettings(ctx context.Context, orgID string) ([]OrgToolSetting, error) {
	var settings []OrgToolSetting

	err := r.db.NewSelect().
		Model(&settings).
		Where("ots.org_id = ?", orgID).
		Order("ots.tool_name ASC").
		Scan(ctx)

	if err != nil {
		r.log.Error("failed to list org tool settings", logger.Error(err), slog.String("orgID", orgID))
		return nil, apperror.ErrDatabase.WithInternal(err)
	}

	return settings, nil
}

// FindOrgToolSetting returns a single org tool setting by org and tool name.
func (r *Repository) FindOrgToolSetting(ctx context.Context, orgID, toolName string) (*OrgToolSetting, error) {
	var setting OrgToolSetting

	err := r.db.NewSelect().
		Model(&setting).
		Where("ots.org_id = ?", orgID).
		Where("ots.tool_name = ?", toolName).
		Scan(ctx)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, apperror.ErrNotFound.WithMessage("Org tool setting not found")
		}
		r.log.Error("failed to get org tool setting", logger.Error(err),
			slog.String("orgID", orgID),
			slog.String("toolName", toolName))
		return nil, apperror.ErrDatabase.WithInternal(err)
	}

	return &setting, nil
}

// UpsertOrgToolSetting creates or updates an org tool setting.
func (r *Repository) UpsertOrgToolSetting(ctx context.Context, setting *OrgToolSetting) (*OrgToolSetting, error) {
	setting.UpdatedAt = time.Now()

	_, err := r.db.NewInsert().
		Model(setting).
		On("CONFLICT (org_id, tool_name) DO UPDATE").
		Set("enabled = EXCLUDED.enabled").
		Set("config = EXCLUDED.config").
		Set("updated_at = EXCLUDED.updated_at").
		Returning("*").
		Exec(ctx)

	if err != nil {
		r.log.Error("failed to upsert org tool setting", logger.Error(err),
			slog.String("orgID", setting.OrgID),
			slog.String("toolName", setting.ToolName))
		return nil, apperror.ErrDatabase.WithInternal(err)
	}

	return setting, nil
}

// DeleteOrgToolSetting removes an org tool setting by org and tool name.
func (r *Repository) DeleteOrgToolSetting(ctx context.Context, orgID, toolName string) (bool, error) {
	result, err := r.db.NewDelete().
		Model((*OrgToolSetting)(nil)).
		Where("org_id = ?", orgID).
		Where("tool_name = ?", toolName).
		Exec(ctx)

	if err != nil {
		r.log.Error("failed to delete org tool setting", logger.Error(err),
			slog.String("orgID", orgID),
			slog.String("toolName", toolName))
		return false, apperror.ErrDatabase.WithInternal(err)
	}

	rowsAffected, _ := result.RowsAffected()
	return rowsAffected > 0, nil
}
