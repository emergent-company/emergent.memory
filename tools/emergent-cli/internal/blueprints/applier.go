package blueprints

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"

	sdkagents "github.com/emergent-company/emergent.memory/apps/server-go/pkg/sdk/agentdefinitions"
	sdktpacks "github.com/emergent-company/emergent.memory/apps/server-go/pkg/sdk/templatepacks"
)

// Blueprinter orchestrates creating or updating packs and agent definitions.
type Blueprinter struct {
	packs   *sdktpacks.Client
	agents  *sdkagents.Client
	dryRun  bool
	upgrade bool
	out     io.Writer
}

// NewBlueprintsApplier creates a Blueprinter. out receives human-readable
// progress lines; if nil, os.Stdout is used.
func NewBlueprintsApplier(
	packs *sdktpacks.Client,
	agents *sdkagents.Client,
	dryRun bool,
	upgrade bool,
	out io.Writer,
) *Blueprinter {
	if out == nil {
		out = os.Stdout
	}
	return &Blueprinter{
		packs:   packs,
		agents:  agents,
		dryRun:  dryRun,
		upgrade: upgrade,
		out:     out,
	}
}

// Run applies the given packs and agents, returning one BlueprintsResult per resource.
func (b *Blueprinter) Run(ctx context.Context, packs []PackFile, agents []AgentFile) ([]BlueprintsResult, error) {
	var results []BlueprintsResult

	if b.dryRun {
		// Dry-run: print what would happen, make zero API calls.
		results = append(results, b.dryRunPacks(packs)...)
		results = append(results, b.dryRunAgents(agents)...)
		b.printSummary(results, true)
		return results, nil
	}

	// Fetch existing resources once up front.
	existingPacks, err := b.fetchExistingPacks(ctx)
	if err != nil {
		return nil, fmt.Errorf("list existing packs: %w", err)
	}

	existingAgents, err := b.fetchExistingAgents(ctx)
	if err != nil {
		return nil, fmt.Errorf("list existing agents: %w", err)
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

	b.printSummary(results, false)
	return results, nil
}

// ──────────────────────────────────────────────
// Pack application
// ──────────────────────────────────────────────

func (b *Blueprinter) blueprintPack(ctx context.Context, p PackFile, existing map[string]sdktpacks.TemplatePackListItem) BlueprintsResult {
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

	req := &sdktpacks.CreatePackRequest{
		Name:                    p.Name,
		Version:                 p.Version,
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

	created, err := b.packs.CreatePack(ctx, req)
	if err != nil {
		return BlueprintsResult{ResourceType: "pack", Name: p.Name, SourceFile: p.SourceFile,
			Action: BlueprintsActionError, Error: fmt.Errorf("create pack: %w", err)}
	}

	// Assign to current project.
	if _, err := b.packs.AssignPack(ctx, &sdktpacks.AssignPackRequest{
		TemplatePackID: created.ID,
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
	req := &sdktpacks.UpdatePackRequest{
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
// Dry-run helpers
// ──────────────────────────────────────────────

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

// ──────────────────────────────────────────────
// Output helpers
// ──────────────────────────────────────────────

func (b *Blueprinter) printResult(r BlueprintsResult) {
	switch r.Action {
	case BlueprintsActionCreated:
		fmt.Fprintf(b.out, "  created  %s %q\n", r.ResourceType, r.Name)
	case BlueprintsActionUpdated:
		fmt.Fprintf(b.out, "  updated  %s %q\n", r.ResourceType, r.Name)
	case BlueprintsActionSkipped:
		fmt.Fprintf(b.out, "  skipped  %s %q (already exists; use --upgrade to update)\n", r.ResourceType, r.Name)
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

// ──────────────────────────────────────────────
// Fetch-existing helpers
// ──────────────────────────────────────────────

// fetchExistingPacks returns a name→item map of all packs visible to the current project.
func (b *Blueprinter) fetchExistingPacks(ctx context.Context) (map[string]sdktpacks.TemplatePackListItem, error) {
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

	m := make(map[string]sdktpacks.TemplatePackListItem, len(available)+len(installed))
	for _, p := range available {
		m[p.Name] = p
	}
	for _, p := range installed {
		m[p.Name] = sdktpacks.TemplatePackListItem{
			ID:          p.TemplatePackID,
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

// ──────────────────────────────────────────────
// Conversion helpers
// ──────────────────────────────────────────────

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
		relSchemas, err = json.Marshal(p.RelationshipTypes)
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
		FlowType:       ag.FlowType,
		Visibility:     ag.Visibility,
		IsDefault:      &ag.IsDefault,
		MaxSteps:       ag.MaxSteps,
		DefaultTimeout: ag.DefaultTimeout,
		Config:         ag.Config,
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
		IsDefault:      &ag.IsDefault,
		MaxSteps:       ag.MaxSteps,
		DefaultTimeout: ag.DefaultTimeout,
		Config:         ag.Config,
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
