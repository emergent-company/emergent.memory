package mcp

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/emergent-company/emergent.memory/domain/skills"
	"github.com/emergent-company/emergent.memory/pkg/auth"
)

// ============================================================================
// Skills Tool Definitions
// ============================================================================

func skillsToolDefinitions() []ToolDefinition {
	return []ToolDefinition{
		{
			Name:          "skill-list",
			OutputSchema:  objectOutputSchema(),
			RequiredScope: "skills:read",
			Description:   "List skills available to the current project. Returns id, name, description, and scope — use skill-get to retrieve full content for a specific skill.",
			InputSchema: InputSchema{
				Type:       "object",
				Properties: map[string]PropertySchema{},
				Required:   []string{},
			},
		},
		{
			Name:          "skill-get",
			OutputSchema:  objectOutputSchema(),
			RequiredScope: "skills:read",
			Description:   "Get a single skill by its UUID or name. Returns the full skill including content, description, scope, and metadata.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]PropertySchema{
					"skill_id": {
						Type:        "string",
						Description: "UUID or name of the skill to retrieve (e.g. 'caveman' or a full UUID)",
					},
				},
				Required: []string{"skill_id"},
			},
		},
		{
			Name:          "skill-create",
			OutputSchema:  objectOutputSchema(),
			RequiredScope: "skills:write",
			Description:   "Create a new project-scoped skill. Returns the created skill's id, name, description, and scope.",
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
			Name:          "skill-update",
			OutputSchema:  objectOutputSchema(),
			RequiredScope: "skills:write",
			Description:   "Update an existing skill's description, content, or metadata.",
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
			Name:          "skill-patch",
			OutputSchema:  objectOutputSchema(),
			RequiredScope: "skills:write",
			Description:   "Extend an existing learned skill by replacing an exact, unique substring of its content. Prefer this over skill-update for learned skills: `old` must occur exactly once.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]PropertySchema{
					"skill_id": {
						Type:        "string",
						Description: "UUID of the skill to patch",
					},
					"old": {
						Type:        "string",
						Description: "Exact substring of the current content to replace. Must occur exactly once.",
					},
					"new": {
						Type:        "string",
						Description: "Replacement text",
					},
				},
				Required: []string{"skill_id", "old", "new"},
			},
		},
		{
			Name:          "skill-delete",
			OutputSchema:  objectOutputSchema(),
			RequiredScope: "skills:write",
			Description:   "Delete a skill by its UUID.",
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

// skillListSummary is a slim view of a skill for list responses.
// Omits Content to keep agent context small — use skill-get for full content.
type skillListSummary struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Scope       string `json:"scope,omitempty"`
}

func (s *Service) executeListSkills(ctx context.Context, projectID string) (*ToolResult, error) {
	projectIDPtr := &projectID
	all, err := s.skillsRepo.FindAll(ctx, projectIDPtr, nil)
	if err != nil {
		return nil, fmt.Errorf("list_skills: %w", err)
	}
	// Return slim summaries — omit Content to keep agent context small.
	// Use skill-get to retrieve full content for a specific skill.
	summaries := make([]skillListSummary, len(all))
	for i, sk := range all {
		summaries[i] = skillListSummary{
			ID:          sk.ID.String(),
			Name:        sk.Name,
			Description: sk.Description,
			Scope:       sk.Scope(),
		}
	}
	return s.wrapResult(summaries)
}

func (s *Service) executeGetSkill(ctx context.Context, projectID string, args map[string]any) (*ToolResult, error) {
	idStr, _ := args["skill_id"].(string)
	if idStr == "" {
		return nil, fmt.Errorf("get_skill: 'skill_id' is required")
	}

	// Try UUID first.
	if id, err := uuid.Parse(idStr); err == nil {
		sk, err := s.skillsRepo.FindByID(ctx, id)
		if err != nil {
			return nil, fmt.Errorf("get_skill: %w", err)
		}
		// Same tier resolution as the REST GetSkill route (shared helper), so a
		// cross-project/org read by UUID is refused (404) rather than leaked.
		if err := s.skillsRepo.AuthorizeSkillAccess(ctx, sk); err != nil {
			return nil, fmt.Errorf("get_skill: %w", err)
		}
		return s.wrapResult(sk.ToDTO())
	}

	// Not a UUID — treat as skill name. Search project-scoped skills first,
	// then fall back to global skills.
	for _, lookup := range []func() ([]*skills.Skill, error){
		func() ([]*skills.Skill, error) { return s.skillsRepo.FindAll(ctx, &projectID, nil) },
		func() ([]*skills.Skill, error) { return s.skillsRepo.FindAll(ctx, nil, nil) },
	} {
		all, err := lookup()
		if err != nil {
			return nil, fmt.Errorf("get_skill: %w", err)
		}
		for _, sk := range all {
			if sk.Name == idStr {
				return s.wrapResult(sk.ToDTO())
			}
		}
	}
	return nil, fmt.Errorf("get_skill: skill %q not found", idStr)
}

func (s *Service) executeCreateSkill(ctx context.Context, projectID string, args map[string]any) (*ToolResult, error) {
	name, _ := args["name"].(string)
	description, _ := args["description"].(string)
	content, _ := args["content"].(string)

	// Shared validation (same rules as the REST handlers).
	dto := &skills.CreateSkillDTO{Name: name, Description: description, Content: content}
	if err := skills.ValidateCreateSkill(*dto, s.skillsRepo.MaxContentSize()); err != nil {
		return nil, fmt.Errorf("create_skill: %w", err)
	}
	// The scan is a hard gate on the agent write surface. A credential or a
	// prompt-injection payload must never enter the shared skill library.
	if scan := skills.ScanSkill(name, content); !scan.Passed() {
		return nil, fmt.Errorf("create_skill: security scan failed: %s", scan.Findings[0].Message)
	}

	sk := &skills.Skill{
		Name:        name,
		Description: description,
		Content:     content,
		ProjectID:   &projectID,
	}
	if isAgentActor(ctx) {
		sk.Metadata = &skills.SkillMetadata{
			Source:         skills.SourceAgent,
			CreatedBy:      "agent",
			LifecycleState: skills.SkillLifecycleActive,
		}
	}
	if err := s.skillsRepo.Create(ctx, sk); err != nil {
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

	// Authorize before mutating: same tier resolution as the REST write routes,
	// so a cross-project/org update is refused (404) and global writes (which
	// require superadmin_full on the REST surface) are refused here too.
	sk, err := s.skillsRepo.FindByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("update_skill: %w", err)
	}
	if err := s.skillsRepo.AuthorizeSkillWrite(ctx, sk); err != nil {
		return nil, fmt.Errorf("update_skill: %w", err)
	}
	if sk.IsLocked() {
		return nil, fmt.Errorf("update_skill: skill is locked; unlock it before editing")
	}

	dto := &skills.UpdateSkillDTO{}

	if v, ok := args["description"].(string); ok {
		dto.Description = &v
	}
	if v, ok := args["content"].(string); ok {
		dto.Content = &v
	}

	// Shared validation (same rules as the REST handlers).
	if err := skills.ValidateUpdateSkill(*dto, s.skillsRepo.MaxContentSize()); err != nil {
		return nil, fmt.Errorf("update_skill: %w", err)
	}
	if dto.Content != nil {
		if scan := skills.ScanSkill(sk.Name, *dto.Content); !scan.Passed() {
			return nil, fmt.Errorf("update_skill: security scan failed: %s", scan.Findings[0].Message)
		}
	}

	updated, err := s.skillsRepo.Update(ctx, id, dto)
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

	// Authorize before mutating: same tier resolution as the REST write routes,
	// so a cross-project/org delete is refused (404) and global deletes (which
	// require superadmin_full on the REST surface) are refused here too.
	sk, err := s.skillsRepo.FindByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("delete_skill: %w", err)
	}
	if err := s.skillsRepo.AuthorizeSkillWrite(ctx, sk); err != nil {
		return nil, fmt.Errorf("delete_skill: %w", err)
	}
	if sk.IsLocked() {
		return nil, fmt.Errorf("delete_skill: skill is locked; unlock it before deleting")
	}

	if err := s.skillsRepo.Delete(ctx, id); err != nil {
		return nil, fmt.Errorf("delete_skill: %w", err)
	}
	return s.wrapResult(map[string]any{"success": true, "skill_id": idStr})
}

// executePatchSkill replaces one exact, unique substring of a skill's content.
// It is the low-risk write the capture prompt teaches: the model quotes the
// text it is extending, so a concurrent edit fails closed instead of
// overwriting the base.
func (s *Service) executePatchSkill(ctx context.Context, args map[string]any) (*ToolResult, error) {
	idStr, _ := args["skill_id"].(string)
	if idStr == "" {
		return nil, fmt.Errorf("patch_skill: 'skill_id' is required")
	}
	id, err := uuid.Parse(idStr)
	if err != nil {
		return nil, fmt.Errorf("patch_skill: invalid skill_id UUID: %w", err)
	}
	oldText, _ := args["old"].(string)
	newText, _ := args["new"].(string)
	if strings.TrimSpace(oldText) == "" {
		return nil, fmt.Errorf("patch_skill: 'old' is required and must be a unique substring")
	}
	if newText == "" {
		return nil, fmt.Errorf("patch_skill: 'new' is required")
	}

	sk, err := s.skillsRepo.FindByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("patch_skill: %w", err)
	}
	if err := s.skillsRepo.AuthorizeSkillWrite(ctx, sk); err != nil {
		return nil, fmt.Errorf("patch_skill: %w", err)
	}
	if sk.IsLocked() {
		return nil, fmt.Errorf("patch_skill: skill is locked; unlock it before editing")
	}

	content, err := skills.ApplyPatch(sk.Content, oldText, newText)
	if err != nil {
		return nil, fmt.Errorf("patch_skill: %w", err)
	}
	if scan := skills.ScanSkill(sk.Name, content); !scan.Passed() {
		return nil, fmt.Errorf("patch_skill: security scan failed: %s", scan.Findings[0].Message)
	}
	dto := &skills.UpdateSkillDTO{Content: &content}
	if err := skills.ValidateUpdateSkill(*dto, s.skillsRepo.MaxContentSize()); err != nil {
		return nil, fmt.Errorf("patch_skill: %w", err)
	}
	updated, err := s.skillsRepo.Update(ctx, id, dto)
	if err != nil {
		return nil, fmt.Errorf("patch_skill: %w", err)
	}
	if updated.ProjectID != nil {
		s.skillsRepo.RecordSkillPatch(ctx, updated.ID, *updated.ProjectID)
	}
	return s.wrapResult(updated.ToDTO())
}

// isAgentActor reports whether the call is attributed to an agent rather than a
// human user, so a tool-created skill is marked agent-created (and therefore
// eligible for the curator). Unknown attribution stays manual.
func isAgentActor(ctx context.Context) bool {
	actorType, _, ok := auth.ActorFromContext(ctx)
	return ok && strings.EqualFold(actorType, "agent")
}
