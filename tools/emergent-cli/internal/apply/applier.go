package apply

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"

	sdkagents "github.com/emergent-company/emergent/apps/server-go/pkg/sdk/agentdefinitions"
	sdktpacks "github.com/emergent-company/emergent/apps/server-go/pkg/sdk/templatepacks"
)

// Applier orchestrates creating or updating packs and agent definitions.
type Applier struct {
	packs   *sdktpacks.Client
	agents  *sdkagents.Client
	dryRun  bool
	upgrade bool
	out     io.Writer
}

// NewApplier creates an Applier.  out receives human-readable progress lines; if
// nil, os.Stdout is used.
func NewApplier(
	packs *sdktpacks.Client,
	agents *sdkagents.Client,
	dryRun bool,
	upgrade bool,
	out io.Writer,
) *Applier {
	if out == nil {
		out = os.Stdout
	}
	return &Applier{
		packs:   packs,
		agents:  agents,
		dryRun:  dryRun,
		upgrade: upgrade,
		out:     out,
	}
}

// Run applies the given packs and agents, returning one ApplyResult per resource.
func (a *Applier) Run(ctx context.Context, packs []PackFile, agents []AgentFile) ([]ApplyResult, error) {
	var results []ApplyResult

	if a.dryRun {
		// Dry-run: print what would happen, make zero API calls.
		results = append(results, a.dryRunPacks(packs)...)
		results = append(results, a.dryRunAgents(agents)...)
		a.printSummary(results, true)
		return results, nil
	}

	// Fetch existing resources once up front.
	existingPacks, err := a.fetchExistingPacks(ctx)
	if err != nil {
		return nil, fmt.Errorf("list existing packs: %w", err)
	}

	existingAgents, err := a.fetchExistingAgents(ctx)
	if err != nil {
		return nil, fmt.Errorf("list existing agents: %w", err)
	}

	// Apply packs.
	for _, p := range packs {
		r := a.applyPack(ctx, p, existingPacks)
		results = append(results, r)
		a.printResult(r)
	}

	// Apply agents.
	for _, ag := range agents {
		r := a.applyAgent(ctx, ag, existingAgents)
		results = append(results, r)
		a.printResult(r)
	}

	a.printSummary(results, false)
	return results, nil
}

// ──────────────────────────────────────────────
// Pack application
// ──────────────────────────────────────────────

func (a *Applier) applyPack(ctx context.Context, p PackFile, existing map[string]sdktpacks.TemplatePackListItem) ApplyResult {
	item, found := existing[p.Name]

	if !found {
		return a.createPack(ctx, p)
	}

	if a.upgrade {
		return a.updatePack(ctx, p, item.ID)
	}

	return ApplyResult{
		ResourceType: "pack",
		Name:         p.Name,
		SourceFile:   p.SourceFile,
		Action:       ActionSkipped,
	}
}

func (a *Applier) createPack(ctx context.Context, p PackFile) ApplyResult {
	objSchemas, relSchemas, uiCfgs, exPrompts, err := marshalPackSchemas(p)
	if err != nil {
		return ApplyResult{ResourceType: "pack", Name: p.Name, SourceFile: p.SourceFile,
			Action: ActionError, Error: err}
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

	created, err := a.packs.CreatePack(ctx, req)
	if err != nil {
		return ApplyResult{ResourceType: "pack", Name: p.Name, SourceFile: p.SourceFile,
			Action: ActionError, Error: fmt.Errorf("create pack: %w", err)}
	}

	// Assign to current project.
	if _, err := a.packs.AssignPack(ctx, &sdktpacks.AssignPackRequest{
		TemplatePackID: created.ID,
	}); err != nil {
		return ApplyResult{ResourceType: "pack", Name: p.Name, SourceFile: p.SourceFile,
			Action: ActionError, Error: fmt.Errorf("assign pack: %w", err)}
	}

	return ApplyResult{ResourceType: "pack", Name: p.Name, SourceFile: p.SourceFile, Action: ActionCreated}
}

func (a *Applier) updatePack(ctx context.Context, p PackFile, packID string) ApplyResult {
	objSchemas, relSchemas, uiCfgs, exPrompts, err := marshalPackSchemas(p)
	if err != nil {
		return ApplyResult{ResourceType: "pack", Name: p.Name, SourceFile: p.SourceFile,
			Action: ActionError, Error: err}
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

	if _, err := a.packs.UpdatePack(ctx, packID, req); err != nil {
		return ApplyResult{ResourceType: "pack", Name: p.Name, SourceFile: p.SourceFile,
			Action: ActionError, Error: fmt.Errorf("update pack: %w", err)}
	}

	return ApplyResult{ResourceType: "pack", Name: p.Name, SourceFile: p.SourceFile, Action: ActionUpdated}
}

// ──────────────────────────────────────────────
// Agent application
// ──────────────────────────────────────────────

func (a *Applier) applyAgent(ctx context.Context, ag AgentFile, existing map[string]sdkagents.AgentDefinitionSummary) ApplyResult {
	item, found := existing[ag.Name]

	if !found {
		return a.createAgent(ctx, ag)
	}

	if a.upgrade {
		return a.updateAgent(ctx, ag, item.ID)
	}

	return ApplyResult{
		ResourceType: "agent",
		Name:         ag.Name,
		SourceFile:   ag.SourceFile,
		Action:       ActionSkipped,
	}
}

func (a *Applier) createAgent(ctx context.Context, ag AgentFile) ApplyResult {
	req := agentFileToCreateRequest(ag)
	if _, err := a.agents.Create(ctx, req); err != nil {
		return ApplyResult{ResourceType: "agent", Name: ag.Name, SourceFile: ag.SourceFile,
			Action: ActionError, Error: fmt.Errorf("create agent: %w", err)}
	}
	return ApplyResult{ResourceType: "agent", Name: ag.Name, SourceFile: ag.SourceFile, Action: ActionCreated}
}

func (a *Applier) updateAgent(ctx context.Context, ag AgentFile, agentID string) ApplyResult {
	req := agentFileToUpdateRequest(ag)
	if _, err := a.agents.Update(ctx, agentID, req); err != nil {
		return ApplyResult{ResourceType: "agent", Name: ag.Name, SourceFile: ag.SourceFile,
			Action: ActionError, Error: fmt.Errorf("update agent: %w", err)}
	}
	return ApplyResult{ResourceType: "agent", Name: ag.Name, SourceFile: ag.SourceFile, Action: ActionUpdated}
}

// ──────────────────────────────────────────────
// Dry-run helpers
// ──────────────────────────────────────────────

func (a *Applier) dryRunPacks(packs []PackFile) []ApplyResult {
	results := make([]ApplyResult, 0, len(packs))
	for _, p := range packs {
		action := "create"
		if a.upgrade {
			action = "create or update"
		}
		fmt.Fprintf(a.out, "[dry-run] would %s pack %q (%s)\n", action, p.Name, p.SourceFile)
		results = append(results, ApplyResult{
			ResourceType: "pack",
			Name:         p.Name,
			SourceFile:   p.SourceFile,
			Action:       ActionCreated, // treat as "would create" for count purposes
		})
	}
	return results
}

func (a *Applier) dryRunAgents(agents []AgentFile) []ApplyResult {
	results := make([]ApplyResult, 0, len(agents))
	for _, ag := range agents {
		action := "create"
		if a.upgrade {
			action = "create or update"
		}
		fmt.Fprintf(a.out, "[dry-run] would %s agent %q (%s)\n", action, ag.Name, ag.SourceFile)
		results = append(results, ApplyResult{
			ResourceType: "agent",
			Name:         ag.Name,
			SourceFile:   ag.SourceFile,
			Action:       ActionCreated,
		})
	}
	return results
}

// ──────────────────────────────────────────────
// Output helpers
// ──────────────────────────────────────────────

func (a *Applier) printResult(r ApplyResult) {
	switch r.Action {
	case ActionCreated:
		fmt.Fprintf(a.out, "  created  %s %q\n", r.ResourceType, r.Name)
	case ActionUpdated:
		fmt.Fprintf(a.out, "  updated  %s %q\n", r.ResourceType, r.Name)
	case ActionSkipped:
		fmt.Fprintf(a.out, "  skipped  %s %q (already exists; use --upgrade to update)\n", r.ResourceType, r.Name)
	case ActionError:
		fmt.Fprintf(a.out, "  error    %s %q: %v\n", r.ResourceType, r.Name, r.Error)
	}
}

func (a *Applier) printSummary(results []ApplyResult, dry bool) {
	var created, updated, skipped, errors int
	for _, r := range results {
		switch r.Action {
		case ActionCreated:
			created++
		case ActionUpdated:
			updated++
		case ActionSkipped:
			skipped++
		case ActionError:
			errors++
		}
	}

	prefix := "Apply"
	if dry {
		prefix = "Dry run"
	}
	fmt.Fprintf(a.out, "%s complete: %d created, %d updated, %d skipped, %d errors\n",
		prefix, created, updated, skipped, errors)
}

// ──────────────────────────────────────────────
// Fetch-existing helpers
// ──────────────────────────────────────────────

// fetchExistingPacks returns a name→item map of all packs visible to the current project.
func (a *Applier) fetchExistingPacks(ctx context.Context) (map[string]sdktpacks.TemplatePackListItem, error) {
	// GetAvailablePacks returns packs NOT yet installed; we need the installed ones too.
	// Merge both sets.
	available, err := a.packs.GetAvailablePacks(ctx)
	if err != nil {
		return nil, err
	}
	installed, err := a.packs.GetInstalledPacks(ctx)
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
func (a *Applier) fetchExistingAgents(ctx context.Context) (map[string]sdkagents.AgentDefinitionSummary, error) {
	resp, err := a.agents.List(ctx)
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
