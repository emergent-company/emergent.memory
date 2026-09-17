package blueprints

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"

	"github.com/google/uuid"

	"github.com/emergent-company/emergent.memory/domain/agents"
	"github.com/emergent-company/emergent.memory/domain/graph"
	"github.com/emergent-company/emergent.memory/domain/schemas"
	"github.com/emergent-company/emergent.memory/domain/skills"
	"github.com/emergent-company/emergent.memory/pkg/apperror"
	"github.com/emergent-company/emergent.memory/pkg/logger"
)

// ApplyOptions controls blueprint apply behavior.
type ApplyOptions struct {
	// Force is accepted for API compatibility; currently unused.
	Force bool `json:"force,omitempty"`
}

// ApplyCounts reports per-entity-type materialization counts.
type ApplyCounts struct {
	Created int `json:"created"`
	Updated int `json:"updated"`
	Skipped int `json:"skipped"`
}

// ApplyResult is the response from Apply.
//
// NOTE: apply is NOT atomic — there is no cross-domain rollback. A failure in
// any step aborts the apply at that point, leaving earlier steps already
// materialized. Each step is idempotent (create-or-update by name / key), so a
// re-apply converges to the same end state; a failed apply should simply be
// retried after fixing the manifest.
type ApplyResult struct {
	BlueprintID   string      `json:"blueprintId"`
	BlueprintName string      `json:"blueprintName"`
	Version       string      `json:"version"`
	Checksum      string      `json:"checksum"`
	Packs         ApplyCounts `json:"packs"`
	Agents        ApplyCounts `json:"agents"`
	Skills        ApplyCounts `json:"skills"`
	Seed          ApplyCounts `json:"seed"`
}

// Apply materializes a blueprint's manifest into concrete memory entities:
// schema packs (create-or-skip globally, ALWAYS assigned to the project),
// agent definitions (create-or-update by name), global skills (create-or-update
// by name), and seed graph objects (key-based upsert) and relationships.
//
// NOT atomic: packs/agents/skills fail fast on the first material error, and
// seed errors are best-effort (recorded in the result counts). Each step is
// idempotent, so re-applying converges; retry after fixing the manifest.
func (s *Service) Apply(ctx context.Context, blueprintID, projectID, userID string, opts ApplyOptions) (*ApplyResult, error) {
	bp, err := s.repo.GetByID(ctx, projectID, blueprintID)
	if err != nil {
		return nil, err
	}
	if bp.Status != StatusDraft && bp.Status != StatusPublished {
		return nil, apperror.ErrConflict.WithMessage("deprecated blueprints cannot be applied")
	}

	projectUUID, err := uuid.Parse(projectID)
	if err != nil {
		return nil, apperror.ErrBadRequest.WithMessage("invalid projectId")
	}

	var manifest BlueprintManifest
	if err := json.Unmarshal(bp.Manifest, &manifest); err != nil {
		return nil, apperror.ErrBadRequest.WithMessage("blueprint manifest is not valid JSON: " + err.Error())
	}

	var actorID *uuid.UUID
	if uid, err := uuid.Parse(userID); err == nil {
		actorID = &uid
	}

	result := &ApplyResult{
		BlueprintID:   bp.ID,
		BlueprintName: bp.Name,
		Version:       bp.Version,
		Checksum:      bp.Checksum,
	}

	// Packs: create-or-skip by (name, version); assign runs on EVERY apply so
	// any project applying the blueprint gets the pack's types registered.
	result.Packs, err = s.applyPacks(ctx, projectID, blueprintID, userID, manifest.Packs)
	if err != nil {
		return nil, err
	}

	// Agents: create-or-update by name (skipped entirely if repo not wired).
	result.Agents, err = s.applyAgents(ctx, projectID, blueprintID, manifest.Agents)
	if err != nil {
		return nil, err
	}

	// Skills: create-or-update by name (global scope: project_id NULL, org_id NULL).
	result.Skills, err = s.applySkills(ctx, manifest.Skills)
	if err != nil {
		return nil, err
	}

	// Seed: idempotent object upserts then relationships (best-effort, non-fatal).
	result.Seed = s.applySeed(ctx, projectUUID, actorID, manifest.Seed)

	// Record the application (provenance) only after all materialization steps
	// succeeded. A partial apply that errors out returns before this, so it is
	// never recorded; a retry converges and then records.
	if err := s.recordApplication(ctx, bp, projectID, userID); err != nil {
		return nil, err
	}

	return result, nil
}

// applyPacks materializes schema packs. The pack itself is created globally
// only once (create-or-skip by name+version), but AssignPack runs on every
// apply so that a second project applying the same blueprint still gets the
// pack's types registered. AssignPack with Merge=true is additive (existing
// project types win property conflicts) and idempotent when already assigned.
func (s *Service) applyPacks(ctx context.Context, projectID, blueprintID, userID string, packs []PackManifest) (ApplyCounts, error) {
	var counts ApplyCounts
	for _, p := range packs {
		if p.Name == "" || p.Version == "" {
			return counts, apperror.ErrBadRequest.WithMessage("pack name and version are required")
		}

		pack, err := s.schemasRepo.GetPackByNameVersion(ctx, p.Name, p.Version)
		switch {
		case err == nil:
			// Pack exists globally. Sync its definition (description + type
			// schemas) from the manifest so source cleanups propagate without a
			// version bump. Only the owning project can update; a non-owner 404
			// is benign (the pack still assigns below).
			if uerr := s.updateExistingPack(ctx, projectID, pack, p); uerr != nil && !isNotFound(uerr) {
				return counts, uerr
			}
			counts.Skipped++ // pack already exists globally; still assign below
		case isNotFound(err):
			objectTypes, err := json.Marshal(p.ObjectTypes)
			if err != nil {
				return counts, apperror.ErrBadRequest.WithMessage("invalid objectTypes in pack " + p.Name)
			}
			relationshipTypes, err := json.Marshal(p.RelationshipTypes)
			if err != nil {
				return counts, apperror.ErrBadRequest.WithMessage("invalid relationshipTypes in pack " + p.Name)
			}
			pack, err = s.schemasSvc.CreatePack(ctx, projectID, &schemas.CreatePackRequest{
				Name:                         p.Name,
				Version:                      p.Version,
				Description:                  &p.Description,
				Author:                       &p.Author,
				Migrations:                   p.Migrations,
				ObjectTypeSchemasSnake:       objectTypes,
				RelationshipTypeSchemasSnake: relationshipTypes,
			})
			if err != nil {
				return counts, err
			}
			counts.Created++
		default:
			return counts, err
		}

		// ALWAYS assign (idempotent; Merge=true additive). AutoUninstall makes
		// the migration worker soft-delete the previous version's assignment
		// after a successful migration, so the compiled schema view does not
		// keep duplicated types from every installed version.
		if _, err := s.schemasSvc.AssignPack(ctx, projectID, userID, &schemas.AssignPackRequest{
			SchemaID:          pack.ID,
			Merge:             true,
			SourceBlueprintID: &blueprintID,
			AutoUninstall:     true,
		}); err != nil {
			return counts, err
		}
		// Record this blueprint's dependency claim on the pack (idempotent).
		if err := s.repo.AddPackClaim(ctx, blueprintID, projectID, pack.ID); err != nil {
			return counts, err
		}
	}
	return counts, nil
}

// updateExistingPack syncs an already-existing global pack's definition
// (description + object/relationship type schemas) with the current manifest.
// This propagates source cleanups (e.g. removed template hints from type
// descriptions) to the registry without requiring a version bump. Only the
// owning project can update; a non-owner returns ErrNotFound (benign).
func (s *Service) updateExistingPack(ctx context.Context, projectID string, pack *schemas.GraphMemorySchema, p PackManifest) error {
	objectTypes, err := json.Marshal(p.ObjectTypes)
	if err != nil {
		return apperror.ErrBadRequest.WithMessage("invalid objectTypes in pack " + p.Name)
	}
	relationshipTypes, err := json.Marshal(p.RelationshipTypes)
	if err != nil {
		return apperror.ErrBadRequest.WithMessage("invalid relationshipTypes in pack " + p.Name)
	}
	_, err = s.schemasSvc.UpdatePack(ctx, pack.ID, projectID, &schemas.UpdatePackRequest{
		Description:             &p.Description,
		ObjectTypeSchemas:       objectTypes,
		RelationshipTypeSchemas: relationshipTypes,
		Migrations:              p.Migrations,
	})
	return err
}

// applyAgents creates or updates agent definitions by name within the project.
// The update path mutates the EXISTING definition in place, overwriting only
// manifest-driven fields and preserving unmanaged fields (AutoLoadSkills,
// MaxSessionEvents, ACPConfig, ProductID, Enabled, ID, and timestamps).
// BannedTools is manifest-driven on create and preserve-when-omitted on
// update. Requires the agents repository to be wired via SetAgentRepo;
// otherwise the step is skipped with a warning and all entries counted as
// skipped.
func (s *Service) applyAgents(ctx context.Context, projectID, blueprintID string, agentManifests []AgentManifest) (ApplyCounts, error) {
	var counts ApplyCounts
	if s.agentRepo == nil {
		if len(agentManifests) > 0 {
			s.log.Warn("agents repository not wired; skipping agent materialization",
				slog.Int("count", len(agentManifests)))
		}
		counts.Skipped = len(agentManifests)
		return counts, nil
	}

	for _, am := range agentManifests {
		if am.Name == "" {
			return counts, apperror.ErrBadRequest.WithMessage("agent name is required")
		}

		existing, err := s.agentRepo.FindDefinitionByName(ctx, projectID, am.Name)
		if err != nil {
			return counts, apperror.ErrDatabase.WithInternal(err)
		}
		if existing != nil {
			// Owned by a different blueprint — skip, do not overwrite.
			if existing.SourceBlueprintID != nil && *existing.SourceBlueprintID != blueprintID {
				counts.Skipped++
				continue
			}
			// Manual/pre-existing (nil) or owned by us — update in place, leave
			// ownership untouched (do not claim pre-existing definitions).
			applyAgentManifestToExisting(existing, &am)
			if err := s.agentRepo.UpdateDefinition(ctx, existing); err != nil {
				return counts, apperror.ErrDatabase.WithInternal(err)
			}
			counts.Updated++
		} else {
			def := buildAgentDefinition(&am, projectID)
			def.SourceBlueprintID = &blueprintID
			if err := s.agentRepo.CreateDefinition(ctx, def); err != nil {
				return counts, apperror.ErrDatabase.WithInternal(err)
			}
			counts.Created++
		}
	}
	return counts, nil
}

// applySkills creates or updates global skills by name (project_id NULL,
// org_id NULL). Both paths run the exported skills validation (name pattern,
// non-empty description/content, content size cap). Updates preserve
// provenance metadata unless the manifest explicitly provides new metadata.
func (s *Service) applySkills(ctx context.Context, skillManifests []SkillManifest) (ApplyCounts, error) {
	var counts ApplyCounts
	existing, err := s.skillsRepo.FindAll(ctx, nil, nil)
	if err != nil {
		return counts, apperror.ErrDatabase.WithInternal(err)
	}
	byName := make(map[string]*skills.Skill, len(existing))
	for _, sk := range existing {
		byName[sk.Name] = sk
	}

	maxContentSize := s.skillsRepo.MaxContentSize()
	for _, sm := range skillManifests {
		md := toSkillMetadata(sm.Metadata)

		if cur := byName[sm.Name]; cur != nil {
			dto := &skills.UpdateSkillDTO{
				Description: &sm.Description,
				Content:     &sm.Content,
			}
			if md != nil {
				dto.Metadata = md
			}
			if err := skills.ValidateUpdateSkill(*dto, maxContentSize); err != nil {
				return counts, err
			}
			if _, err := s.skillsRepo.Update(ctx, cur.ID, dto); err != nil {
				return counts, err
			}
			counts.Updated++
			continue
		}

		if err := skills.ValidateCreateSkill(skills.CreateSkillDTO{
			Name:        sm.Name,
			Description: sm.Description,
			Content:     sm.Content,
		}, maxContentSize); err != nil {
			return counts, err
		}

		skill := &skills.Skill{
			Name:        sm.Name,
			Description: sm.Description,
			Content:     sm.Content,
			Metadata:    md,
			// nil ProjectID/OrgID = global scope
		}
		if err := s.skillsRepo.Create(ctx, skill); err != nil {
			return counts, err
		}
		counts.Created++
	}
	return counts, nil
}

// applySeed materializes seed graph objects and relationships. Both are
// best-effort: per-item failures and unresolvable relationship endpoints are
// counted as skipped rather than failing the apply. Objects use key-based
// upsert (CreateOrUpdate): identical objects are no-ops, differing objects get
// a new version.
func (s *Service) applySeed(ctx context.Context, projectID uuid.UUID, actorID *uuid.UUID, seed *SeedManifest) ApplyCounts {
	var counts ApplyCounts
	if seed == nil {
		return counts
	}

	// Objects — idempotent key-based upsert. Objects without a Key are skipped
	// (a key is required for upsert semantics; creating keyless duplicates on
	// re-apply is worse than skipping).
	for _, o := range seed.Objects {
		if o.Key == "" {
			s.log.Warn("seed object without key skipped", slog.String("type", o.Type))
			counts.Skipped++
			continue
		}
		item := graph.CreateGraphObjectRequest{
			Type:       o.Type,
			Key:        &o.Key,
			Properties: o.Properties,
			Labels:     o.Labels,
		}
		if o.Status != "" {
			item.Status = &o.Status
		}

		_, created, err := s.graphSvc.CreateOrUpdate(ctx, projectID, &item, actorID)
		if err != nil {
			s.log.Warn("seed object upsert failed", slog.String("key", o.Key), logger.Error(err))
			counts.Skipped++
			continue
		}
		if created {
			counts.Created++
		} else {
			// CreateOrUpdate's bool distinguishes created vs not-created only:
			// an unchanged no-op and a genuine new-version update both report
			// false, so both count as "updated" here.
			counts.Updated++
		}
	}

	// Relationships (endpoints referenced by seed object keys)
	if len(seed.Relationships) > 0 {
		created, skipped := s.applySeedRelationships(ctx, projectID, seed.Relationships)
		counts.Created += created
		counts.Skipped += skipped
	}

	return counts
}

// applySeedRelationships resolves relationship endpoints by object key
// (optionally disambiguated by srcType/dstType) and bulk-creates them with
// Upsert semantics. Relationships whose src or dst key cannot be resolved are
// skipped (counted), never fatal.
func (s *Service) applySeedRelationships(ctx context.Context, projectID uuid.UUID, rels []SeedRelationshipRecord) (int, int) {
	created, skipped := 0, 0
	keyCache := map[string]*uuid.UUID{} // "type\x00key" → canonical object id
	keyOnlyWarned := false

	resolve := func(key, typ string) *uuid.UUID {
		if key == "" {
			return nil
		}
		cacheKey := typ + "\x00" + key
		if id, ok := keyCache[cacheKey]; ok {
			return id
		}
		params := graph.ListParams{
			ProjectID: projectID,
			Key:       &key,
			Limit:     1,
		}
		if typ != "" {
			// Type + key is unambiguous.
			params.Type = &typ
		} else if !keyOnlyWarned {
			// Fall back to key-only resolution, but warn once per apply.
			s.log.Warn("seed relationship endpoint without srcType/dstType; resolving by key only")
			keyOnlyWarned = true
		}
		objs, err := s.graphSvc.GetRepository().List(ctx, params)
		if err != nil || len(objs) == 0 {
			return nil
		}
		id := objs[0].CanonicalID
		keyCache[cacheKey] = &id
		return &id
	}

	var items []graph.CreateGraphRelationshipRequest
	for _, r := range rels {
		srcID := resolve(r.SrcKey, r.SrcType)
		dstID := resolve(r.DstKey, r.DstType)
		if srcID == nil || dstID == nil {
			skipped++
			continue
		}
		items = append(items, graph.CreateGraphRelationshipRequest{
			Type:       r.Type,
			SrcID:      *srcID,
			DstID:      *dstID,
			Properties: r.Properties,
			Upsert:     true, // idempotent create-or-skip on (type, src, dst)
		})
	}

	if len(items) > 0 {
		resp, err := s.graphSvc.BulkCreateRelationships(ctx, projectID, &graph.BulkCreateRelationshipsRequest{Items: items})
		if err != nil {
			s.log.Warn("seed relationship bulk create failed", logger.Error(err))
			skipped += len(items)
		} else {
			created += resp.Success
			skipped += resp.Failed
		}
	}
	return created, skipped
}

// buildAgentDefinition converts an AgentManifest into a fresh
// agents.AgentDefinition for the CREATE path. Empty model Name means provider
// default. Flow type, visibility, and dispatch mode fall back to the domain
// defaults when omitted in the manifest. Enabled defaults to true.
func buildAgentDefinition(m *AgentManifest, projectID string) *agents.AgentDefinition {
	flowType := agents.AgentFlowType(m.FlowType)
	if flowType == "" {
		flowType = agents.FlowTypeSingle
	}
	visibility := agents.AgentVisibility(m.Visibility)
	if visibility == "" {
		visibility = agents.VisibilityProject
	}
	dispatchMode := agents.AgentDispatchMode(m.DispatchMode)
	if dispatchMode == "" {
		dispatchMode = agents.DispatchModeSync
	}

	def := &agents.AgentDefinition{
		ProjectID:      projectID,
		Name:           m.Name,
		Tools:          m.Tools,
		BannedTools:    m.BannedTools,
		Skills:         m.Skills,
		FlowType:       flowType,
		Enabled:        true,
		Visibility:     visibility,
		Config:         m.Config,
		SandboxConfig:  m.WorkspaceConfig,
		DispatchMode:   dispatchMode,
		MaxSteps:       m.MaxSteps,
		DefaultTimeout: m.DefaultTimeout,
	}
	if m.IsDefault != nil {
		def.IsDefault = *m.IsDefault
	}
	if def.Tools == nil {
		def.Tools = []string{}
	}
	if def.BannedTools == nil {
		def.BannedTools = []string{}
	}
	if def.Skills == nil {
		def.Skills = []string{}
	}
	if m.Description != "" {
		def.Description = &m.Description
	}
	if m.SystemPrompt != "" {
		def.SystemPrompt = &m.SystemPrompt
	}
	if m.Model != nil {
		def.Model = &agents.ModelConfig{
			Name:           m.Model.Name,
			Temperature:    m.Model.Temperature,
			MaxTokens:      m.Model.MaxTokens,
			EnableThinking: m.Model.EnableThinking,
		}
	} else {
		// Empty ModelConfig.Name selects the provider default model.
		def.Model = &agents.ModelConfig{}
	}
	if len(m.ToolPolicies) > 0 {
		def.ToolPolicies = make(map[string]agents.ToolPolicy, len(m.ToolPolicies))
		for name, p := range m.ToolPolicies {
			def.ToolPolicies[name] = agents.ToolPolicy{
				Confirm: p.Confirm,
				Message: p.Message,
			}
		}
	}
	if ui := agentUIConfig(m.UI); ui != nil {
		def.UIConfig = ui
	}
	return def
}

// applyAgentManifestToExisting overwrites ONLY the manifest-driven fields on an
// existing agent definition for the UPDATE path. Everything not driven by the
// manifest — AutoLoadSkills, MaxSessionEvents, ACPConfig, ProductID, Enabled,
// ID, timestamps, and Name — is preserved untouched.
// Description/SystemPrompt/FlowType/Visibility/DispatchMode are only set when
// the manifest provides a non-empty value (omitted = keep current); Tools,
// Skills, Config, SandboxConfig, IsDefault, MaxSteps, DefaultTimeout, and
// ToolPolicies are overwritten from the manifest. BannedTools is applied only
// when the manifest provides a non-empty list (preserve-when-omitted), so a
// user's manual banned-tool edits survive a re-apply that omits the field.
// Enabled is never set here.
func applyAgentManifestToExisting(def *agents.AgentDefinition, m *AgentManifest) {
	if m.Description != "" {
		def.Description = &m.Description
	}
	if m.SystemPrompt != "" {
		def.SystemPrompt = &m.SystemPrompt
	}
	if m.Model != nil {
		def.Model = &agents.ModelConfig{
			Name:           m.Model.Name,
			Temperature:    m.Model.Temperature,
			MaxTokens:      m.Model.MaxTokens,
			EnableThinking: m.Model.EnableThinking,
		}
	}
	def.Tools = m.Tools
	if def.Tools == nil {
		def.Tools = []string{}
	}
	if len(m.BannedTools) > 0 {
		def.BannedTools = m.BannedTools
	}
	def.Skills = m.Skills
	if def.Skills == nil {
		def.Skills = []string{}
	}
	if m.FlowType != "" {
		def.FlowType = agents.AgentFlowType(m.FlowType)
	}
	if m.Visibility != "" {
		def.Visibility = agents.AgentVisibility(m.Visibility)
	}
	if m.DispatchMode != "" {
		def.DispatchMode = agents.AgentDispatchMode(m.DispatchMode)
	}
	// Optional pointer/map fields are applied only when the manifest provides
	// them; omitted means "keep current value" (preserve-when-omitted).
	if m.IsDefault != nil {
		def.IsDefault = *m.IsDefault
	}
	if m.MaxSteps != nil {
		def.MaxSteps = m.MaxSteps
	}
	if m.DefaultTimeout != nil {
		def.DefaultTimeout = m.DefaultTimeout
	}
	if m.Config != nil {
		def.Config = m.Config
	}
	if m.WorkspaceConfig != nil {
		def.SandboxConfig = m.WorkspaceConfig
	}
	if len(m.ToolPolicies) > 0 {
		def.ToolPolicies = make(map[string]agents.ToolPolicy, len(m.ToolPolicies))
		for name, p := range m.ToolPolicies {
			def.ToolPolicies[name] = agents.ToolPolicy{
				Confirm: p.Confirm,
				Message: p.Message,
			}
		}
	}
	// UI is optional: applied only when the manifest carries a non-empty
	// icon/color; omitted = preserve the current ui_config.
	if ui := agentUIConfig(m.UI); ui != nil {
		def.UIConfig = ui
	}
}

// agentUIConfig marshals an AgentUIManifest into the opaque ui_config JSONB
// blob stored on agents.AgentDefinition.UIConfig. Returns nil when the block is
// absent or has no non-empty icon/color, so callers leave ui_config at the DB
// default '{}' (create) or preserve the existing value (update).
func agentUIConfig(ui *AgentUIManifest) json.RawMessage {
	if ui == nil {
		return nil
	}
	// Trim to match the web-UI editor semantics: whitespace-only values are
	// "not declared" rather than junk that would overwrite an existing
	// appearance on update.
	icon := strings.TrimSpace(ui.Icon)
	color := strings.TrimSpace(ui.Color)
	if icon == "" && color == "" {
		return nil
	}
	m := map[string]string{}
	if icon != "" {
		m["icon"] = icon
	}
	if color != "" {
		m["color"] = color
	}
	raw, err := json.Marshal(m)
	if err != nil {
		return nil
	}
	return raw
}

// toSkillMetadata converts manifest metadata (opaque map) into a typed
// skills.SkillMetadata. Returns nil when the manifest provides no metadata so
// updates preserve existing provenance fields.
func toSkillMetadata(m map[string]any) *skills.SkillMetadata {
	if len(m) == 0 {
		return nil
	}
	raw, err := json.Marshal(m)
	if err != nil {
		return nil
	}
	var md skills.SkillMetadata
	if err := json.Unmarshal(raw, &md); err != nil {
		return nil
	}
	return &md
}

// isNotFound reports whether err is an apperror with the not_found code.
func isNotFound(err error) bool {
	var appErr *apperror.Error
	return errors.As(err, &appErr) && appErr.Code == apperror.ErrNotFound.Code
}
