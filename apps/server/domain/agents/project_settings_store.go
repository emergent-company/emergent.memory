package agents

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
)

// GetProjectSetting retrieves a single project setting by category + key.
func (r *Repository) GetProjectSetting(ctx context.Context, projectID, category, key string) (*ProjectSetting, error) {
	setting := new(ProjectSetting)
	err := r.db.NewSelect().
		Model(setting).
		Where("project_id = ?", projectID).
		Where("category = ?", category).
		Where("key = ?", key).
		Scan(ctx)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("get project setting: %w", err)
	}
	return setting, nil
}

// ListProjectSettings retrieves all settings for a project within a given category.
func (r *Repository) ListProjectSettings(ctx context.Context, projectID, category string) ([]*ProjectSetting, error) {
	var settings []*ProjectSetting
	err := r.db.NewSelect().
		Model(&settings).
		Where("project_id = ?", projectID).
		Where("category = ?", category).
		Order("key ASC").
		Scan(ctx)

	if err != nil {
		return nil, fmt.Errorf("list project settings: %w", err)
	}
	return settings, nil
}

// UpsertProjectSetting creates or updates a project setting (INSERT … ON CONFLICT UPDATE).
func (r *Repository) UpsertProjectSetting(ctx context.Context, projectID, category, key string, value map[string]any) (*ProjectSetting, error) {
	setting := &ProjectSetting{
		ProjectID: projectID,
		Category:  category,
		Key:       key,
		Value:     value,
	}

	_, err := r.db.NewInsert().
		Model(setting).
		On("CONFLICT (project_id, category, key) DO UPDATE").
		Set("value = EXCLUDED.value").
		Set("updated_at = now()").
		Returning("*").
		Exec(ctx)

	if err != nil {
		return nil, fmt.Errorf("upsert project setting: %w", err)
	}
	return setting, nil
}

// DeleteProjectSetting removes a project setting by category + key.
// Returns true if a row was deleted.
func (r *Repository) DeleteProjectSetting(ctx context.Context, projectID, category, key string) (bool, error) {
	result, err := r.db.NewDelete().
		Model((*ProjectSetting)(nil)).
		Where("project_id = ?", projectID).
		Where("category = ?", category).
		Where("key = ?", key).
		Exec(ctx)

	if err != nil {
		return false, fmt.Errorf("delete project setting: %w", err)
	}
	rows, _ := result.RowsAffected()
	return rows > 0, nil
}

// GetAgentOverride retrieves the agent configuration override for the given agent name.
// Returns nil if no override exists.
func (r *Repository) GetAgentOverride(ctx context.Context, projectID, agentName string) (*AgentOverride, error) {
	setting, err := r.GetProjectSetting(ctx, projectID, SettingsCategoryAgentOverride, agentName)
	if err != nil {
		return nil, err
	}
	if setting == nil {
		return nil, nil
	}

	// Decode the JSONB value into an AgentOverride struct.
	raw, marshalErr := json.Marshal(setting.Value)
	if marshalErr != nil {
		return nil, fmt.Errorf("marshal setting value: %w", marshalErr)
	}
	var override AgentOverride
	if unmarshalErr := json.Unmarshal(raw, &override); unmarshalErr != nil {
		return nil, fmt.Errorf("unmarshal agent override: %w", unmarshalErr)
	}
	return &override, nil
}

// SetAgentOverride saves a partial agent configuration override for the given agent name.
func (r *Repository) SetAgentOverride(ctx context.Context, projectID, agentName string, override *AgentOverride) (*ProjectSetting, error) {
	raw, err := json.Marshal(override)
	if err != nil {
		return nil, fmt.Errorf("marshal agent override: %w", err)
	}
	var value map[string]any
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil, fmt.Errorf("unmarshal to map: %w", err)
	}
	return r.UpsertProjectSetting(ctx, projectID, SettingsCategoryAgentOverride, agentName, value)
}

// DeleteAgentOverride removes the agent configuration override, reverting to defaults.
func (r *Repository) DeleteAgentOverride(ctx context.Context, projectID, agentName string) (bool, error) {
	return r.DeleteProjectSetting(ctx, projectID, SettingsCategoryAgentOverride, agentName)
}

// ApplyAgentOverride merges an override on top of a canonical AgentDefinition.
// Only non-nil/non-empty override fields replace the definition's values.
func ApplyAgentOverride(def *AgentDefinition, override *AgentOverride) {
	if override == nil {
		return
	}
	if override.SystemPrompt != nil {
		def.SystemPrompt = override.SystemPrompt
	}
	if override.Model != nil {
		if def.Model == nil {
			def.Model = &ModelConfig{}
		}
		if override.Model.Name != "" {
			def.Model.Name = override.Model.Name
		}
		if override.Model.Temperature != nil {
			def.Model.Temperature = override.Model.Temperature
		}
		if override.Model.MaxTokens != nil {
			def.Model.MaxTokens = override.Model.MaxTokens
		}
		if len(override.Model.NativeTools) > 0 {
			def.Model.NativeTools = override.Model.NativeTools
		}
	}
	if override.Tools != nil {
		def.Tools = override.Tools
	}
	if override.MaxSteps != nil {
		def.MaxSteps = override.MaxSteps
	}
	if override.SandboxConfig != nil {
		def.SandboxConfig = override.SandboxConfig
	}
}
