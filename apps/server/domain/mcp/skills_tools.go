package mcp

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/emergent-company/emergent.memory/domain/skills"
)

// ============================================================================
// Skills Tool Definitions
// ============================================================================

func skillsToolDefinitions() []ToolDefinition {
	return []ToolDefinition{
		{
			Name:        "skill-list",
			Description: "List skills available to the current project. Returns an array of skill objects with id, name, description, content, scope, and metadata.",
			InputSchema: InputSchema{
				Type:       "object",
				Properties: map[string]PropertySchema{},
				Required:   []string{},
			},
		},
		{
			Name:        "skill-get",
			Description: "Get a single skill by its UUID. Returns the full skill including content, description, scope, and metadata.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]PropertySchema{
					"skill_id": {
						Type:        "string",
						Description: "UUID of the skill to retrieve",
					},
				},
				Required: []string{"skill_id"},
			},
		},
		{
			Name:        "skill-create",
			Description: "Create a new project-scoped skill. Returns the created skill's id, name, description, and scope.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]PropertySchema{
					"name": {
						Type:        "string",
						Description: "Skill name (lowercase alphanumeric with hyphens, e.g. 'my-skill')",
					},
					"description": {
						Type:        "string",
						Description: "Human-readable description of what this skill does",
					},
					"content": {
						Type:        "string",
						Description: "The skill content / instructions",
					},
				},
				Required: []string{"name"},
			},
		},
		{
			Name:        "skill-update",
			Description: "Update an existing skill's description, content, or metadata.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]PropertySchema{
					"skill_id": {
						Type:        "string",
						Description: "UUID of the skill to update",
					},
					"description": {
						Type:        "string",
						Description: "New description (optional)",
					},
					"content": {
						Type:        "string",
						Description: "New content (optional)",
					},
				},
				Required: []string{"skill_id"},
			},
		},
		{
			Name:        "skill-delete",
			Description: "Delete a skill by its UUID.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]PropertySchema{
					"skill_id": {
						Type:        "string",
						Description: "UUID of the skill to delete",
					},
				},
				Required: []string{"skill_id"},
			},
		},
	}
}

// ============================================================================
// Skills Tool Handlers
// ============================================================================

func (s *Service) executeListSkills(ctx context.Context, projectID string) (*ToolResult, error) {
	projectIDPtr := &projectID
	all, err := s.skillsRepo.FindAll(ctx, projectIDPtr, nil)
	if err != nil {
		return nil, fmt.Errorf("list_skills: %w", err)
	}
	dtos := make([]*skills.SkillDTO, len(all))
	for i, sk := range all {
		dtos[i] = sk.ToDTO()
	}
	return s.wrapResult(dtos)
}

func (s *Service) executeGetSkill(ctx context.Context, args map[string]any) (*ToolResult, error) {
	idStr, _ := args["skill_id"].(string)
	if idStr == "" {
		return nil, fmt.Errorf("get_skill: 'skill_id' is required")
	}
	id, err := uuid.Parse(idStr)
	if err != nil {
		return nil, fmt.Errorf("get_skill: invalid skill_id UUID: %w", err)
	}
	sk, err := s.skillsRepo.FindByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("get_skill: %w", err)
	}
	return s.wrapResult(sk.ToDTO())
}

func (s *Service) executeCreateSkill(ctx context.Context, projectID string, args map[string]any) (*ToolResult, error) {
	name, _ := args["name"].(string)
	if name == "" {
		return nil, fmt.Errorf("create_skill: 'name' is required")
	}
	description, _ := args["description"].(string)
	content, _ := args["content"].(string)

	sk := &skills.Skill{
		Name:        name,
		Description: description,
		Content:     content,
		ProjectID:   &projectID,
	}
	if err := s.skillsRepo.Create(ctx, sk, nil); err != nil {
		return nil, fmt.Errorf("create_skill: %w", err)
	}
	return s.wrapResult(sk.ToDTO())
}

func (s *Service) executeUpdateSkill(ctx context.Context, args map[string]any) (*ToolResult, error) {
	idStr, _ := args["skill_id"].(string)
	if idStr == "" {
		return nil, fmt.Errorf("update_skill: 'skill_id' is required")
	}
	id, err := uuid.Parse(idStr)
	if err != nil {
		return nil, fmt.Errorf("update_skill: invalid skill_id UUID: %w", err)
	}

	dto := &skills.UpdateSkillDTO{}
	descriptionChanged := false

	if v, ok := args["description"].(string); ok {
		dto.Description = &v
		descriptionChanged = true
	}
	if v, ok := args["content"].(string); ok {
		dto.Content = &v
	}

	updated, err := s.skillsRepo.Update(ctx, id, dto, nil, descriptionChanged)
	if err != nil {
		return nil, fmt.Errorf("update_skill: %w", err)
	}
	return s.wrapResult(updated.ToDTO())
}

func (s *Service) executeDeleteSkill(ctx context.Context, args map[string]any) (*ToolResult, error) {
	idStr, _ := args["skill_id"].(string)
	if idStr == "" {
		return nil, fmt.Errorf("delete_skill: 'skill_id' is required")
	}
	id, err := uuid.Parse(idStr)
	if err != nil {
		return nil, fmt.Errorf("delete_skill: invalid skill_id UUID: %w", err)
	}
	if err := s.skillsRepo.Delete(ctx, id); err != nil {
		return nil, fmt.Errorf("delete_skill: %w", err)
	}
	return s.wrapResult(map[string]any{"success": true, "skill_id": idStr})
}
