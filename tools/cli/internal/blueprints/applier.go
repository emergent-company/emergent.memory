package blueprints

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	sdkagents "github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/agentdefinitions"
	sdkprojects "github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/projects"
	sdkschemas "github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/schemas"
	sdkskills "github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/skills"
)

// Blueprinter orchestrates creating or updating packs, agent definitions, and skills.
type Blueprinter struct {
	projects  *sdkprojects.Client
	projectID string
	packs     *sdkschemas.Client
	agents    *sdkagents.Client
	skills    *sdkskills.Client
	dryRun    bool
	upgrade   bool
	out       io.Writer

	// packsByName is populated at the start of Run so printResult can show type counts.
	packsByName map[string]PackFile
}

// NewBlueprintsApplier creates a Blueprinter. out receives human-readable
// progress lines; if nil, os.Stdout is used.
func NewBlueprintsApplier(
	projects *sdkprojects.Client,
	projectID string,
	packs *sdkschemas.Client,
	agents *sdkagents.Client,
	skills *sdkskills.Client,
	dryRun bool,
	upgrade bool,
	out io.Writer,
) *Blueprinter {
	if out == nil {
		out = os.Stdout
	}
	return &Blueprinter{
		projects:  projects,
		projectID: projectID,
		packs:     packs,
		agents:    agents,
		skills:    skills,
		dryRun:    dryRun,
		upgrade:   upgrade,
		out:       out,
	}
}

// Run applies the given project config, packs, agents, and skills, returning one BlueprintsResult per resource.
func (b *Blueprinter) Run(ctx context.Context, project *ProjectFile, packs []PackFile, agents []AgentFile, skills []SkillFile) ([]BlueprintsResult, error) {
	var results []BlueprintsResult

	// Build name→pack lookup for richer output.
	b.packsByName = make(map[string]PackFile, len(packs))
	for _, p := range packs {
		b.packsByName[p.Name] = p
	}

	if b.dryRun {
		// Dry-run: print what would happen, make zero API calls.
		if project != nil {
			results = append(results, b.dryRunProject(project))
		}
		results = append(results, b.dryRunPacks(packs)...)
		results = append(results, b.dryRunAgents(agents)...)
		results = append(results, b.dryRunSkills(skills)...)
		b.printSummary(results, true)
		return results, nil
	}

	// Apply project config first (if present).
	if project != nil {
		r := b.applyProject(ctx, project)
		results = append(results, r)
		b.printResult(r)
	}

	// Fetch existing resources once up front (only when there are items to apply).
	existingPacks, err := b.fetchExistingPacks(ctx)
	if err != nil {
		return nil, fmt.Errorf("list existing packs: %w", err)
	}

	var existingAgents map[string]sdkagents.AgentDefinitionSummary
	if len(agents) > 0 {
		existingAgents, err = b.fetchExistingAgents(ctx)
		if err != nil {
			return nil, fmt.Errorf("list existing agents: %w", err)
		}
	}

	var existingSkills map[string]*sdkskills.Skill
	if len(skills) > 0 {
		existingSkills, err = b.fetchExistingSkills(ctx)
		if err != nil {
			return nil, fmt.Errorf("list existing skills: %w", err)
		}
	}

	// Apply packs.
	for _, p := range packs {
		r := b.blueprintPack(ctx, p, existingPacks)
		results = append(results, r)
		b.printResult(r)
	}

	// Apply agents.
	for _, ag := range agents {
		r := b.blueprintAgent(ctx, ag, existingAgents)
		results = append(results, r)
		b.printResult(r)
	}

	// Apply skills.
	for _, sk := range skills {
		r := b.blueprintSkill(ctx, sk, existingSkills)
		results = append(results, r)
		b.printResult(r)
	}

	b.printSummary(results, false)
	return results, nil
}

// ──────────────────────────────────────────────
// Project application
// ──────────────────────────────────────────────

// applyProject sets project-level settings from the blueprint project file.
// It always applies (no skip/upgrade logic — the operation is idempotent).
func (b *Blueprinter) applyProject(ctx context.Context, pf *ProjectFile) BlueprintsResult {
	if strings.TrimSpace(pf.ProjectInfo) == "" {
		return BlueprintsResult{
			ResourceType: "project",
			Name:         "project info",
			SourceFile:   pf.SourceFile,
			Action:       BlueprintsActionSkipped,
		}
	}

	info := pf.ProjectInfo
	_, err := b.projects.Update(ctx, b.projectID, &sdkprojects.UpdateProjectRequest{
		ProjectInfo: &info,
	})
	if err != nil {
		return BlueprintsResult{
			ResourceType: "project",
			Name:         "project info",
			SourceFile:   pf.SourceFile,
			Action:       BlueprintsActionError,
			Error:        fmt.Errorf("update project info: %w", err),
		}
	}
	return BlueprintsResult{
		ResourceType: "project",
		Name:         "project info",
		SourceFile:   pf.SourceFile,
		Action:       BlueprintsActionUpdated,
	}
}

// ──────────────────────────────────────────────
// Pack application
// ──────────────────────────────────────────────

func (b *Blueprinter) blueprintPack(ctx context.Context, p PackFile, existing map[string]sdkschemas.MemorySchemaListItem) BlueprintsResult {
	item, found := existing[p.Name]

	if !found {
		return b.createPack(ctx, p)
	}

	if b.upgrade {
		return b.updatePack(ctx, p, item.ID)
	}

	return BlueprintsResult{
		ResourceType: "pack",
		Name:         p.Name,
		SourceFile:   p.SourceFile,
		Action:       BlueprintsActionSkipped,
	}
}

func (b *Blueprinter) createPack(ctx context.Context, p PackFile) BlueprintsResult {
	objSchemas, relSchemas, uiCfgs, exPrompts, err := marshalPackSchemas(p)
	if err != nil {
		return BlueprintsResult{ResourceType: "pack", Name: p.Name, SourceFile: p.SourceFile,
			Action: BlueprintsActionError, Error: err}
	}

	req := &sdkschemas.CreatePackRequest{
		Name:                         p.Name,
		Version:                      p.Version,
		ObjectTypeSchemasSnake:       objSchemas,
		RelationshipTypeSchemasSnake: relSchemas,
		UIConfigsSnake:               uiCfgs,
		ExtractionPromptsSnake:       exPrompts,
	}
	if p.Description != "" {
		req.Description = &p.Description
	}
	if p.Author != "" {
		req.Author = &p.Author
	}
	if p.License != "" {
		req.License = &p.License
	}
	if p.RepositoryURL != "" {
		req.RepositoryURL = &p.RepositoryURL
	}
	if p.DocumentationURL != "" {
		req.DocumentationURL = &p.DocumentationURL
	}

	created, err := b.packs.CreatePack(ctx, req)
	if err != nil {
		return BlueprintsResult{ResourceType: "pack", Name: p.Name, SourceFile: p.SourceFile,
			Action: BlueprintsActionError, Error: fmt.Errorf("create pack: %w", err)}
	}

	// Assign to current project.
	if _, err := b.packs.AssignPack(ctx, &sdkschemas.AssignPackRequest{
		SchemaID: created.ID,
	}); err != nil {
		return BlueprintsResult{ResourceType: "pack", Name: p.Name, SourceFile: p.SourceFile,
			Action: BlueprintsActionError, Error: fmt.Errorf("assign pack: %w", err)}
	}

	return BlueprintsResult{ResourceType: "pack", Name: p.Name, SourceFile: p.SourceFile, Action: BlueprintsActionCreated}
}

func (b *Blueprinter) updatePack(ctx context.Context, p PackFile, packID string) BlueprintsResult {
	objSchemas, relSchemas, uiCfgs, exPrompts, err := marshalPackSchemas(p)
	if err != nil {
		return BlueprintsResult{ResourceType: "pack", Name: p.Name, SourceFile: p.SourceFile,
			Action: BlueprintsActionError, Error: err}
	}

	ver := p.Version
	req := &sdkschemas.UpdatePackRequest{
		Version:                 &ver,
		ObjectTypeSchemas:       objSchemas,
		RelationshipTypeSchemas: relSchemas,
		UIConfigs:               uiCfgs,
		ExtractionPrompts:       exPrompts,
	}
	if p.Description != "" {
		req.Description = &p.Description
	}
	if p.Author != "" {
		req.Author = &p.Author
	}
	if p.License != "" {
		req.License = &p.License
	}
	if p.RepositoryURL != "" {
		req.RepositoryURL = &p.RepositoryURL
	}
	if p.DocumentationURL != "" {
		req.DocumentationURL = &p.DocumentationURL
	}

	if _, err := b.packs.UpdatePack(ctx, packID, req); err != nil {
		return BlueprintsResult{ResourceType: "pack", Name: p.Name, SourceFile: p.SourceFile,
			Action: BlueprintsActionError, Error: fmt.Errorf("update pack: %w", err)}
	}

	return BlueprintsResult{ResourceType: "pack", Name: p.Name, SourceFile: p.SourceFile, Action: BlueprintsActionUpdated}
}

// ──────────────────────────────────────────────
// Agent application
// ──────────────────────────────────────────────

func (b *Blueprinter) blueprintAgent(ctx context.Context, ag AgentFile, existing map[string]sdkagents.AgentDefinitionSummary) BlueprintsResult {
	item, found := existing[ag.Name]

	if !found {
		return b.createAgent(ctx, ag)
	}

	if b.upgrade {
		return b.updateAgent(ctx, ag, item.ID)
	}

	return BlueprintsResult{
		ResourceType: "agent",
		Name:         ag.Name,
		SourceFile:   ag.SourceFile,
		Action:       BlueprintsActionSkipped,
	}
}

func (b *Blueprinter) createAgent(ctx context.Context, ag AgentFile) BlueprintsResult {
	req := agentFileToCreateRequest(ag)
	if _, err := b.agents.Create(ctx, req); err != nil {
		return BlueprintsResult{ResourceType: "agent", Name: ag.Name, SourceFile: ag.SourceFile,
			Action: BlueprintsActionError, Error: fmt.Errorf("create agent: %w", err)}
	}
	return BlueprintsResult{ResourceType: "agent", Name: ag.Name, SourceFile: ag.SourceFile, Action: BlueprintsActionCreated}
}

func (b *Blueprinter) updateAgent(ctx context.Context, ag AgentFile, agentID string) BlueprintsResult {
	req := agentFileToUpdateRequest(ag)
	if _, err := b.agents.Update(ctx, agentID, req); err != nil {
		return BlueprintsResult{ResourceType: "agent", Name: ag.Name, SourceFile: ag.SourceFile,
			Action: BlueprintsActionError, Error: fmt.Errorf("update agent: %w", err)}
	}
	return BlueprintsResult{ResourceType: "agent", Name: ag.Name, SourceFile: ag.SourceFile, Action: BlueprintsActionUpdated}
}

// ──────────────────────────────────────────────
// Skill application
// ──────────────────────────────────────────────

func (b *Blueprinter) blueprintSkill(ctx context.Context, sk SkillFile, existing map[string]*sdkskills.Skill) BlueprintsResult {
	item, found := existing[sk.Name]

	if !found {
		return b.createSkill(ctx, sk)
	}

	if b.upgrade {
		return b.updateSkill(ctx, sk, item.ID)
	}

	return BlueprintsResult{
		ResourceType: "skill",
		Name:         sk.Name,
		SourceFile:   sk.SourceFile,
		Action:       BlueprintsActionSkipped,
	}
}

func (b *Blueprinter) createSkill(ctx context.Context, sk SkillFile) BlueprintsResult {
	req := &sdkskills.CreateSkillRequest{
		Name:        sk.Name,
		Description: sk.Description,
		Content:     sk.Content,
		Metadata:    sk.Metadata,
	}
	// Global skill — empty projectID
	if _, err := b.skills.Create(ctx, "", req); err != nil {
		return BlueprintsResult{ResourceType: "skill", Name: sk.Name, SourceFile: sk.SourceFile,
			Action: BlueprintsActionError, Error: fmt.Errorf("create skill: %w", err)}
	}
	return BlueprintsResult{ResourceType: "skill", Name: sk.Name, SourceFile: sk.SourceFile, Action: BlueprintsActionCreated}
}

func (b *Blueprinter) updateSkill(ctx context.Context, sk SkillFile, skillID string) BlueprintsResult {
	desc := sk.Description
	content := sk.Content
	req := &sdkskills.UpdateSkillRequest{
		Description: &desc,
		Content:     &content,
		Metadata:    sk.Metadata,
	}
	if _, err := b.skills.Update(ctx, skillID, req); err != nil {
		return BlueprintsResult{ResourceType: "skill", Name: sk.Name, SourceFile: sk.SourceFile,
			Action: BlueprintsActionError, Error: fmt.Errorf("update skill: %w", err)}
	}
	return BlueprintsResult{ResourceType: "skill", Name: sk.Name, SourceFile: sk.SourceFile, Action: BlueprintsActionUpdated}
}

// ──────────────────────────────────────────────
// Dry-run helpers
// ──────────────────────────────────────────────

func (b *Blueprinter) dryRunProject(pf *ProjectFile) BlueprintsResult {
	if strings.TrimSpace(pf.ProjectInfo) == "" {
		fmt.Fprintf(b.out, "[dry-run] project info is empty — would skip\n")
		return BlueprintsResult{ResourceType: "project", Name: "project info", SourceFile: pf.SourceFile, Action: BlueprintsActionSkipped}
	}
	fmt.Fprintf(b.out, "[dry-run] would set project_info on project %q (%s)\n", b.projectID, pf.SourceFile)
	return BlueprintsResult{ResourceType: "project", Name: "project info", SourceFile: pf.SourceFile, Action: BlueprintsActionUpdated}
}

func (b *Blueprinter) dryRunPacks(packs []PackFile) []BlueprintsResult {
	results := make([]BlueprintsResult, 0, len(packs))
	for _, p := range packs {
		action := "create"
		if b.upgrade {
			action = "create or update"
		}
		fmt.Fprintf(b.out, "[dry-run] would %s pack %q (%s)\n", action, p.Name, p.SourceFile)
		results = append(results, BlueprintsResult{
			ResourceType: "pack",
			Name:         p.Name,
			SourceFile:   p.SourceFile,
			Action:       BlueprintsActionCreated, // treat as "would create" for count purposes
		})
	}
	return results
}

func (b *Blueprinter) dryRunAgents(agents []AgentFile) []BlueprintsResult {
	results := make([]BlueprintsResult, 0, len(agents))
	for _, ag := range agents {
		action := "create"
		if b.upgrade {
			action = "create or update"
		}
		fmt.Fprintf(b.out, "[dry-run] would %s agent %q (%s)\n", action, ag.Name, ag.SourceFile)
		results = append(results, BlueprintsResult{
			ResourceType: "agent",
			Name:         ag.Name,
			SourceFile:   ag.SourceFile,
			Action:       BlueprintsActionCreated,
		})
	}
	return results
}

func (b *Blueprinter) dryRunSkills(skills []SkillFile) []BlueprintsResult {
	results := make([]BlueprintsResult, 0, len(skills))
	for _, sk := range skills {
		action := "create"
		if b.upgrade {
			action = "create or update"
		}
		fmt.Fprintf(b.out, "[dry-run] would %s skill %q (%s)\n", action, sk.Name, sk.SourceFile)
		results = append(results, BlueprintsResult{
			ResourceType: "skill",
			Name:         sk.Name,
			SourceFile:   sk.SourceFile,
			Action:       BlueprintsActionCreated,
		})
	}
	return results
}

// ──────────────────────────────────────────────
// Output helpers
// ──────────────────────────────────────────────

// packDetail returns a parenthetical detail string like "(21 object types, 42 relationship types)"
// for a pack result. Returns "" if the pack is not found in the map.
func packDetail(name string, packsByName map[string]PackFile) string {
	p, ok := packsByName[name]
	if !ok {
		return ""
	}
	nObj := len(p.ObjectTypes)
	nRel := len(p.RelationshipTypes)
	if nObj == 0 && nRel == 0 {
		return ""
	}
	parts := make([]string, 0, 2)
	if nObj > 0 {
		parts = append(parts, fmt.Sprintf("%d object types", nObj))
	}
	if nRel > 0 {
		parts = append(parts, fmt.Sprintf("%d relationship types", nRel))
	}
	return " (" + strings.Join(parts, ", ") + ")"
}

func (b *Blueprinter) printResult(r BlueprintsResult) {
	detail := ""
	if r.ResourceType == "pack" {
		detail = packDetail(r.Name, b.packsByName)
	}
	switch r.Action {
	case BlueprintsActionCreated:
		fmt.Fprintf(b.out, "  created  %s %q%s\n", r.ResourceType, r.Name, detail)
	case BlueprintsActionUpdated:
		fmt.Fprintf(b.out, "  updated  %s %q%s\n", r.ResourceType, r.Name, detail)
	case BlueprintsActionSkipped:
		fmt.Fprintf(b.out, "  skipped  %s %q%s (already exists; use --upgrade to update)\n", r.ResourceType, r.Name, detail)
	case BlueprintsActionError:
		fmt.Fprintf(b.out, "  error    %s %q: %v\n", r.ResourceType, r.Name, r.Error)
	}
}

func (b *Blueprinter) printSummary(results []BlueprintsResult, dry bool) {
	var created, updated, skipped, errors int
	for _, r := range results {
		switch r.Action {
		case BlueprintsActionCreated:
			created++
		case BlueprintsActionUpdated:
			updated++
		case BlueprintsActionSkipped:
			skipped++
		case BlueprintsActionError:
			errors++
		}
	}

	prefix := "Blueprints"
	if dry {
		prefix = "Dry run"
	}
	fmt.Fprintf(b.out, "%s complete: %d created, %d updated, %d skipped, %d errors\n",
		prefix, created, updated, skipped, errors)
}

// PrintDiscoverySummary prints a human-readable summary of what was found in
// the loaded blueprint directory, before any apply operations.
func PrintDiscoverySummary(
	out io.Writer,
	project *ProjectFile,
	packs []PackFile,
	agents []AgentFile,
	skills []SkillFile,
	seedObjects []SeedObjectRecord,
	seedRels []SeedRelationshipRecord,
) {
	var parts []string
	if project != nil && strings.TrimSpace(project.ProjectInfo) != "" {
		parts = append(parts, "project info")
	}
	if len(packs) > 0 {
		totalObj, totalRel := 0, 0
		for _, p := range packs {
			totalObj += len(p.ObjectTypes)
			totalRel += len(p.RelationshipTypes)
		}
		s := fmt.Sprintf("%d schema pack(s)", len(packs))
		if totalObj > 0 || totalRel > 0 {
			detail := make([]string, 0, 2)
			if totalObj > 0 {
				detail = append(detail, fmt.Sprintf("%d object types", totalObj))
			}
			if totalRel > 0 {
				detail = append(detail, fmt.Sprintf("%d relationship types", totalRel))
			}
			s += " (" + strings.Join(detail, ", ") + ")"
		}
		parts = append(parts, s)
	}
	if len(agents) > 0 {
		parts = append(parts, fmt.Sprintf("%d agent(s)", len(agents)))
	}
	if len(skills) > 0 {
		parts = append(parts, fmt.Sprintf("%d skill(s)", len(skills)))
	}
	if len(seedObjects) > 0 {
		parts = append(parts, fmt.Sprintf("%d seed object(s)", len(seedObjects)))
	}
	if len(seedRels) > 0 {
		parts = append(parts, fmt.Sprintf("%d seed relationship(s)", len(seedRels)))
	}

	if len(parts) == 0 {
		return
	}
	fmt.Fprintf(out, "Discovered: %s\n", strings.Join(parts, ", "))
}

// PrintInspect prints a detailed listing of the contents of a loaded blueprint.
func PrintInspect(
	out io.Writer,
	project *ProjectFile,
	packs []PackFile,
	agents []AgentFile,
	skills []SkillFile,
	seedObjects []SeedObjectRecord,
	seedRels []SeedRelationshipRecord,
) {
	if project != nil && strings.TrimSpace(project.ProjectInfo) != "" {
		fmt.Fprintf(out, "Project Info:\n")
		// Print first line of project info as preview.
		info := strings.TrimSpace(project.ProjectInfo)
		lines := strings.SplitN(info, "\n", 2)
		fmt.Fprintf(out, "  %s\n", strings.TrimSpace(lines[0]))
		fmt.Fprintln(out)
	}

	for _, p := range packs {
		fmt.Fprintf(out, "Pack %q (v%s):\n", p.Name, p.Version)
		if p.Description != "" {
			desc := strings.TrimSpace(p.Description)
			// Show first 120 chars of description.
			if len(desc) > 120 {
				desc = desc[:120] + "..."
			}
			fmt.Fprintf(out, "  %s\n", desc)
		}

		if len(p.ObjectTypes) > 0 {
			fmt.Fprintf(out, "  Object types (%d):\n", len(p.ObjectTypes))
			for _, ot := range p.ObjectTypes {
				label := ot.Label
				if label == "" {
					label = ot.Name
				}
				nProps := len(ot.Properties)
				if nProps > 0 {
					fmt.Fprintf(out, "    - %s (%d properties)\n", label, nProps)
				} else {
					fmt.Fprintf(out, "    - %s\n", label)
				}
			}
		}

		if len(p.RelationshipTypes) > 0 {
			fmt.Fprintf(out, "  Relationship types (%d):\n", len(p.RelationshipTypes))
			for _, rt := range p.RelationshipTypes {
				label := rt.Label
				if label == "" {
					label = rt.Name
				}
				src := rt.SourceType
				if len(rt.SourceTypes) > 0 {
					src = strings.Join(rt.SourceTypes, "|")
				}
				dst := rt.TargetType
				if len(rt.TargetTypes) > 0 {
					dst = strings.Join(rt.TargetTypes, "|")
				}
				if src != "" && dst != "" {
					fmt.Fprintf(out, "    - %s (%s -> %s)\n", label, src, dst)
				} else {
					fmt.Fprintf(out, "    - %s\n", label)
				}
			}
		}
		fmt.Fprintln(out)
	}

	for _, ag := range agents {
		desc := ""
		if ag.Description != "" {
			desc = " — " + ag.Description
		}
		fmt.Fprintf(out, "Agent %q%s\n", ag.Name, desc)
	}
	if len(agents) > 0 {
		fmt.Fprintln(out)
	}

	for _, sk := range skills {
		desc := ""
		if sk.Description != "" {
			desc = " — " + sk.Description
		}
		fmt.Fprintf(out, "Skill %q%s\n", sk.Name, desc)
	}
	if len(skills) > 0 {
		fmt.Fprintln(out)
	}

	if len(seedObjects) > 0 || len(seedRels) > 0 {
		fmt.Fprintf(out, "Seed data:\n")
		if len(seedObjects) > 0 {
			// Count by type.
			typeCounts := make(map[string]int)
			for _, o := range seedObjects {
				typeCounts[o.Type]++
			}
			fmt.Fprintf(out, "  Objects (%d total):\n", len(seedObjects))
			for t, c := range typeCounts {
				fmt.Fprintf(out, "    - %s: %d\n", t, c)
			}
		}
		if len(seedRels) > 0 {
			typeCounts := make(map[string]int)
			for _, r := range seedRels {
				typeCounts[r.Type]++
			}
			fmt.Fprintf(out, "  Relationships (%d total):\n", len(seedRels))
			for t, c := range typeCounts {
				fmt.Fprintf(out, "    - %s: %d\n", t, c)
			}
		}
		fmt.Fprintln(out)
	}

	// Print totals.
	totalObj, totalRel := 0, 0
	for _, p := range packs {
		totalObj += len(p.ObjectTypes)
		totalRel += len(p.RelationshipTypes)
	}
	fmt.Fprintf(out, "Totals: %d pack(s), %d object types, %d relationship types, %d agent(s), %d skill(s), %d seed object(s), %d seed relationship(s)\n",
		len(packs), totalObj, totalRel, len(agents), len(skills), len(seedObjects), len(seedRels))
}

// ──────────────────────────────────────────────
// Fetch-existing helpers
// ──────────────────────────────────────────────

// fetchExistingPacks returns a name→item map of all packs visible to the current project.
func (b *Blueprinter) fetchExistingPacks(ctx context.Context) (map[string]sdkschemas.MemorySchemaListItem, error) {
	// GetAvailablePacks returns packs NOT yet installed; we need the installed ones too.
	// Merge both sets.
	available, err := b.packs.GetAvailablePacks(ctx)
	if err != nil {
		return nil, err
	}
	installed, err := b.packs.GetInstalledPacks(ctx)
	if err != nil {
		return nil, err
	}

	m := make(map[string]sdkschemas.MemorySchemaListItem, len(available)+len(installed))
	for _, p := range available {
		m[p.Name] = p
	}
	for _, p := range installed {
		m[p.Name] = sdkschemas.MemorySchemaListItem{
			ID:          p.SchemaID,
			Name:        p.Name,
			Version:     p.Version,
			Description: p.Description,
		}
	}
	return m, nil
}

// fetchExistingAgents returns a name→summary map of all agent definitions in the project.
func (b *Blueprinter) fetchExistingAgents(ctx context.Context) (map[string]sdkagents.AgentDefinitionSummary, error) {
	resp, err := b.agents.List(ctx)
	if err != nil {
		return nil, err
	}
	m := make(map[string]sdkagents.AgentDefinitionSummary, len(resp.Data))
	for _, ag := range resp.Data {
		m[ag.Name] = ag
	}
	return m, nil
}

// fetchExistingSkills returns a name→skill map of all global skills.
func (b *Blueprinter) fetchExistingSkills(ctx context.Context) (map[string]*sdkskills.Skill, error) {
	list, err := b.skills.List(ctx, "")
	if err != nil {
		return nil, err
	}
	m := make(map[string]*sdkskills.Skill, len(list))
	for _, sk := range list {
		m[sk.Name] = sk
	}
	return m, nil
}

// ──────────────────────────────────────────────
// Conversion helpers
// ──────────────────────────────────────────────

// relTypeAPI is the JSON shape sent to the server for relationship type schemas.
// It always uses the plural sourceTypes/targetTypes arrays so the server-side
// parser (which supports both conventions) receives the richest data.
type relTypeAPI struct {
	Name        string   `json:"name"`
	Label       string   `json:"label,omitempty"`
	Description string   `json:"description,omitempty"`
	SourceTypes []string `json:"sourceTypes,omitempty"`
	TargetTypes []string `json:"targetTypes,omitempty"`
}

// marshalPackSchemas converts the typed ObjectTypes / RelationshipTypes slices
// to the raw JSON blobs expected by the API.
func marshalPackSchemas(p PackFile) (objSchemas, relSchemas, uiCfgs, exPrompts json.RawMessage, err error) {
	if len(p.ObjectTypes) > 0 {
		objSchemas, err = json.Marshal(p.ObjectTypes)
		if err != nil {
			return nil, nil, nil, nil, fmt.Errorf("marshal objectTypes: %w", err)
		}
	}
	if len(p.RelationshipTypes) > 0 {
		apiRels := make([]relTypeAPI, len(p.RelationshipTypes))
		for i, r := range p.RelationshipTypes {
			apiRels[i] = relTypeAPI{
				Name:        r.Name,
				Label:       r.Label,
				Description: r.Description,
				SourceTypes: r.GetSourceTypes(),
				TargetTypes: r.GetTargetTypes(),
			}
		}
		relSchemas, err = json.Marshal(apiRels)
		if err != nil {
			return nil, nil, nil, nil, fmt.Errorf("marshal relationshipTypes: %w", err)
		}
	}
	if len(p.UIConfigs) > 0 {
		uiCfgs = p.UIConfigs
	}
	if len(p.ExtractionPrompts) > 0 {
		exPrompts = p.ExtractionPrompts
	}
	return objSchemas, relSchemas, uiCfgs, exPrompts, nil
}

func agentFileToCreateRequest(ag AgentFile) *sdkagents.CreateAgentDefinitionRequest {
	req := &sdkagents.CreateAgentDefinitionRequest{
		Name:           ag.Name,
		Tools:          ag.Tools,
		Skills:         ag.Skills,
		FlowType:       ag.FlowType,
		Visibility:     ag.Visibility,
		IsDefault:      &ag.IsDefault,
		MaxSteps:       ag.MaxSteps,
		DefaultTimeout: ag.DefaultTimeout,
		Config:         ag.Config,
	}
	if ag.DispatchMode != "" {
		req.DispatchMode = ag.DispatchMode
	}
	if ag.Description != "" {
		req.Description = &ag.Description
	}
	if ag.SystemPrompt != "" {
		req.SystemPrompt = &ag.SystemPrompt
	}
	if ag.Model != nil {
		req.Model = &sdkagents.ModelConfig{
			Name:        ag.Model.Name,
			Temperature: ag.Model.Temperature,
			MaxTokens:   ag.Model.MaxTokens,
		}
	}
	return req
}

func agentFileToUpdateRequest(ag AgentFile) *sdkagents.UpdateAgentDefinitionRequest {
	req := &sdkagents.UpdateAgentDefinitionRequest{
		Name:           &ag.Name,
		Tools:          ag.Tools,
		Skills:         ag.Skills,
		IsDefault:      &ag.IsDefault,
		MaxSteps:       ag.MaxSteps,
		DefaultTimeout: ag.DefaultTimeout,
		Config:         ag.Config,
	}
	if ag.DispatchMode != "" {
		req.DispatchMode = &ag.DispatchMode
	}
	if ag.Description != "" {
		req.Description = &ag.Description
	}
	if ag.SystemPrompt != "" {
		req.SystemPrompt = &ag.SystemPrompt
	}
	if ag.FlowType != "" {
		req.FlowType = &ag.FlowType
	}
	if ag.Visibility != "" {
		req.Visibility = &ag.Visibility
	}
	if ag.Model != nil {
		req.Model = &sdkagents.ModelConfig{
			Name:        ag.Model.Name,
			Temperature: ag.Model.Temperature,
			MaxTokens:   ag.Model.MaxTokens,
		}
	}
	return req
}
