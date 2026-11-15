package extraction

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/uptrace/bun"

	domainagents "github.com/emergent-company/emergent.memory/domain/agents"
	"github.com/emergent-company/emergent.memory/domain/graph"
	"github.com/emergent-company/emergent.memory/domain/mcp"
	"github.com/emergent-company/emergent.memory/pkg/auth"
	"github.com/emergent-company/emergent.memory/pkg/branchorigin"
	"github.com/emergent-company/emergent.memory/pkg/branchscope"
	"github.com/emergent-company/emergent.memory/pkg/logger"
)

// extractionAgentSelection is the resolved project choice for agent-run
// extraction: the definition plus the optional per-project run budget.
type extractionAgentSelection struct {
	definition *domainagents.AgentDefinition
	maxSteps   *int
	timeout    *time.Duration
}

// selectedExtractionAgent resolves the project's extraction agent setting.
//
// An unset setting (or no agents surface wired) means the built-in pipeline, so
// it returns nil. A stored selection that no longer resolves — deleted or
// disabled definition — also returns nil after logging: extraction must never
// stop because a setting dangles, and the built-in pipeline is a safe fallback.
// Only a definition that exists and is enabled selects agent-run mode; if it
// then fails at run time the job fails and retries, because silently paying
// twice would be worse than surfacing the failure.
func (w *ObjectExtractionWorker) selectedExtractionAgent(ctx context.Context, job *ObjectExtractionJob) *extractionAgentSelection {
	if w.agentsRepo == nil || w.agentRunner == nil || job == nil {
		return nil
	}
	ps, err := w.agentsRepo.GetProjectSetting(ctx, job.ProjectID,
		domainagents.SettingsCategoryExtraction, domainagents.SettingsKeyExtractionAgentID)
	if err != nil || ps == nil {
		return nil
	}
	id, _ := ps.Value["agentId"].(string)
	id = strings.TrimSpace(id)
	if id == "" {
		return nil
	}

	def, err := w.agentsRepo.FindDefinitionByID(ctx, id, &job.ProjectID)
	if err != nil {
		w.log.Warn("failed to load the project's extraction agent; falling back to the built-in pipeline",
			"project_id", job.ProjectID, "agent_definition_id", id, logger.Error(err))
		return nil
	}
	if def == nil {
		w.log.Warn("the project's extraction agent no longer exists; falling back to the built-in pipeline",
			"project_id", job.ProjectID, "agent_definition_id", id)
		return nil
	}
	if !def.Enabled {
		w.log.Warn("the project's extraction agent is disabled; falling back to the built-in pipeline",
			"project_id", job.ProjectID, "agent_definition_id", id)
		return nil
	}

	sel := &extractionAgentSelection{definition: def}
	if ps, err := w.agentsRepo.GetProjectSetting(ctx, job.ProjectID,
		domainagents.SettingsCategoryExtraction, domainagents.SettingsKeyExtractionMaxSteps); err == nil && ps != nil {
		if v, ok := ps.Value["value"].(float64); ok && v >= 1 {
			n := int(v)
			sel.maxSteps = &n
		}
	}
	if ps, err := w.agentsRepo.GetProjectSetting(ctx, job.ProjectID,
		domainagents.SettingsCategoryExtraction, domainagents.SettingsKeyExtractionTimeoutSeconds); err == nil && ps != nil {
		if v, ok := ps.Value["value"].(float64); ok && v >= 1 {
			d := time.Duration(v) * time.Second
			sel.timeout = &d
		}
	}
	return sel
}

// agentExtractionRunContext installs the job defaults the graph tools read:
// the extraction-run provenance/status/branch, a fixed branch boundary limited
// to the job's extraction branch, the system actor, and the synthesis lineage.
// Extracted from runAgentExtraction so the contract is unit-testable without a
// database.
func agentExtractionRunContext(ctx context.Context, job *ObjectExtractionJob, branchRef string, documentVersion int) (context.Context, error) {
	runCtx := mcp.ContextWithExtractionRun(ctx, mcp.ExtractionRunConfig{
		JobID:      job.ID,
		BranchRef:  branchRef,
		Status:     "suggested",
		Provenance: agentExtractionProvenance(job, documentVersion),
	})
	if branchRef != "" {
		scope, err := branchscope.New(branchscope.Config{
			Unmerged:    true,
			Origins:     []string{string(branchorigin.Extraction)},
			Branches:    []string{branchRef},
			MaxBranches: 1,
		})
		if err != nil {
			return nil, fmt.Errorf("agent extraction: install branch scope: %w", err)
		}
		runCtx = mcp.ContextWithFixedBranchScope(runCtx, scope)
	} else {
		// No staging branch (creation failed): keep the run main-only rather than
		// leaving the model free to name any branch it likes.
		runCtx = mcp.ContextWithFixedBranchScope(runCtx, nil)
	}
	runCtx = auth.WithActor(runCtx, graph.ActorSystem, nil)
	return withExtractionLineage(runCtx, job), nil
}

// runAgentExtraction executes the project-selected agent for each extraction
// batch and returns the run's counts. The agent writes through the normal graph
// tools; the installed ExtractionRunConfig makes those writes land on the job's
// staging branch with the pipeline's provenance and suggested status.
func (w *ObjectExtractionWorker) runAgentExtraction(
	ctx context.Context,
	job *ObjectExtractionJob,
	sel *extractionAgentSelection,
	stagingBranchID *uuid.UUID,
	schemas *ExtractionSchemas,
	batches []string,
	documentVersion int,
) (*ObjectExtractionResults, error) {
	if sel == nil || sel.definition == nil {
		return nil, fmt.Errorf("agent extraction: no definition selected")
	}
	if w.agentRunner == nil || w.agentsRepo == nil {
		return nil, fmt.Errorf("agent extraction: agent execution is not configured")
	}

	runAgentID, err := w.ensureExtractionRuntimeAgent(ctx, job.ProjectID, sel.definition)
	if err != nil {
		return nil, fmt.Errorf("agent extraction: prepare runtime agent: %w", err)
	}
	runtimeAgent, err := w.agentsRepo.FindByID(ctx, runAgentID, &job.ProjectID)
	if err != nil || runtimeAgent == nil {
		return nil, fmt.Errorf("agent extraction: load runtime agent: %w", err)
	}

	orgID := ""
	if w.projectsRepo != nil {
		if id, ok, oErr := w.projectsRepo.GetOrganizationID(ctx, job.ProjectID); oErr == nil && ok {
			orgID = id
		}
	}

	branchRef := ""
	if stagingBranchID != nil {
		branchRef = w.stagingBranchName(job)
	}

	runCtx, ctxErr := agentExtractionRunContext(ctx, job, branchRef, documentVersion)
	if ctxErr != nil {
		return nil, ctxErr
	}

	triggerSource := "extraction"
	for i, batch := range batches {
		if len(batches) > 1 {
			w.log.Info("processing agent extraction batch",
				"batch", i+1, "total_batches", len(batches), "chars", len(batch))
		}
		message := buildAgentExtractionMessage(job, sel.definition, schemas, batch, branchRef)
		res, execErr := w.agentRunner.Execute(runCtx, domainagents.ExecuteRequest{
			Agent:           runtimeAgent,
			AgentDefinition: sel.definition,
			ProjectID:       job.ProjectID,
			OrgID:           orgID,
			UserMessage:     message,
			MaxSteps:        sel.maxSteps,
			Timeout:         sel.timeout,
			TriggerSource:   &triggerSource,
			TriggerMetadata: map[string]any{
				"extraction_job_id": job.ID,
				"document_id":       stringValue(job.DocumentID),
				"batch":             i + 1,
			},
		})
		if execErr != nil {
			return nil, fmt.Errorf("agent extraction (batch %d/%d): %w", i+1, len(batches), execErr)
		}
		if res != nil && res.RunID != "" && job.RunID == nil {
			if err := w.jobsService.SetRunID(ctx, job.ID, res.RunID); err != nil {
				w.log.Warn("failed to link agent extraction run to the job",
					"job_id", job.ID, "run_id", res.RunID, logger.Error(err))
			}
			runID := res.RunID
			job.RunID = &runID
		}
	}

	return w.countAgentExtractionOutput(ctx, job, stagingBranchID)
}

// agentExtractionProvenance mirrors persistResults' stamp so an agent-created
// object is indistinguishable from a pipeline-created one to reviewers, merges
// and the deletion resolver.
func agentExtractionProvenance(job *ObjectExtractionJob, documentVersion int) map[string]any {
	props := map[string]any{"_extraction_job_id": job.ID}
	if isDocumentSource(job) {
		props["_source_id"] = *job.SourceID
		props["_sources"] = buildSourcesArray(job, documentVersion)
		if documentVersion > 0 {
			props["_document_version"] = documentVersion
		}
	}
	if job.SourceType != nil {
		props["_extraction_source"] = *job.SourceType
	}
	return props
}

// withExtractionLineage declares the document/job inputs the run synthesizes
// from, matching persistResults' lineage stamp.
func withExtractionLineage(ctx context.Context, job *ObjectExtractionJob) context.Context {
	var inputs []graph.SynthesisInput
	if job.DocumentID != nil {
		if id, err := uuid.Parse(*job.DocumentID); err == nil {
			inputs = append(inputs, graph.SynthesisInput{Kind: graph.LineageInputDocument, ID: id})
		}
	}
	if id, err := uuid.Parse(job.ID); err == nil {
		inputs = append(inputs, graph.SynthesisInput{Kind: graph.LineageInputExtractionJob, ID: id})
	}
	if len(inputs) == 0 {
		return ctx
	}
	return graph.WithSynthesisInputs(ctx, inputs...)
}

// ensureExtractionRuntimeAgent returns (creating if needed) the kb.agents row
// that backs the selected definition. Agent runs have a FK to kb.agents, so a
// definition cannot run without one; the row is a stable per-definition shell
// and is never scheduled (cron 0 0 * * *, disabled).
//
// The shell deliberately does NOT hold the definition link: it is internal,
// auto-managed and never-scheduled, so letting it be the definition's canonical
// runtime row would break the blueprint apply and trigger paths, which link
// their own row (23505) and then operate on the disabled shell. Its definition
// identity lives in the strategy_type marker ('extraction-agent:<defID>') and is
// resolved by findExtractionRuntimeAgentID, not through ResolveDefinitionForAgent.
func (w *ObjectExtractionWorker) ensureExtractionRuntimeAgent(ctx context.Context, projectID string, def *domainagents.AgentDefinition) (string, error) {
	agentID, err := w.findExtractionRuntimeAgentID(ctx, projectID, def.ID)
	if err != nil {
		return "", err
	}
	if agentID != "" {
		return agentID, nil
	}
	name := "extraction-agent:" + def.Name
	strategyType := "extraction-agent:" + def.ID
	// The row carries no agent_definition_id, so the (project_id,
	// agent_definition_id) partial unique index does not cover it and a create
	// race could mint two shells. Serialise the create on an advisory lock keyed
	// by (project, definition), re-read inside the lock, and insert only when
	// the shell is still absent — the read-then-insert-under-lock shape the graph
	// upsert paths already use. This is the race-safety mechanism for this shape.
	lockKey := "extraction-agent:" + projectID + ":" + def.ID
	err = w.jobsService.db.RunInTx(ctx, &sql.TxOptions{}, func(ctx context.Context, tx bun.Tx) error {
		if _, err := tx.NewRaw("SELECT pg_advisory_xact_lock(hashtextextended(?, 0))", lockKey).Exec(ctx); err != nil {
			return err
		}
		var id string
		if err := tx.NewSelect().
			TableExpr("kb.agents").
			ColumnExpr("id::text").
			Where("project_id = ? AND strategy_type = ?", projectID, strategyType).
			Order("created_at ASC").
			Limit(1).
			Scan(ctx, &id); err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if id != "" {
			agentID = id
			return nil
		}
		return tx.NewRaw(`
			INSERT INTO kb.agents
				(project_id, name, strategy_type, cron_schedule, enabled, trigger_type, config, description)
			VALUES (?, ?, ?, '0 0 * * *', false, 'manual', '{}'::jsonb,
			        'Runs document extraction with the project-selected agent definition. Managed automatically.')
			RETURNING id::text`,
			projectID, name, strategyType).Scan(ctx, &agentID)
	})
	if err != nil {
		return "", fmt.Errorf("create extraction runtime agent: %w", err)
	}
	return agentID, nil
}

// findExtractionRuntimeAgentID returns the runtime agent id backing a definition
// within a project, or "" when the definition has no runtime row yet. The shell
// is identified by its strategy_type marker ('extraction-agent:<defID>') rather
// than by agent_definition_id, because the shell intentionally carries no
// definition link (see ensureExtractionRuntimeAgent).
func (w *ObjectExtractionWorker) findExtractionRuntimeAgentID(ctx context.Context, projectID, definitionID string) (string, error) {
	var agentID string
	err := w.jobsService.db.NewSelect().
		TableExpr("kb.agents").
		ColumnExpr("id::text").
		Where("project_id = ? AND strategy_type = ?", projectID, "extraction-agent:"+definitionID).
		Order("created_at ASC", "id ASC").
		Limit(1).
		Scan(ctx, &agentID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return "", fmt.Errorf("find extraction runtime agent: %w", err)
	}
	return agentID, nil
}

// countAgentExtractionOutput reports what the run wrote on its staging branch.
// Objects carry graph_objects.extraction_job_id; relationships carry the same id
// in properties (_extraction_job_id), so both counts are job-scoped even when a
// branch already held objects from another run.
func (w *ObjectExtractionWorker) countAgentExtractionOutput(ctx context.Context, job *ObjectExtractionJob, stagingBranchID *uuid.UUID) (*ObjectExtractionResults, error) {
	result := &ObjectExtractionResults{}

	objQ := w.jobsService.db.NewSelect().
		TableExpr("kb.graph_objects").
		ColumnExpr("count(*)").
		Where("project_id = ?", job.ProjectID).
		Where("extraction_job_id = ?", job.ID).
		Where("deleted_at IS NULL")
	relQ := w.jobsService.db.NewSelect().
		TableExpr("kb.graph_relationships").
		ColumnExpr("count(*)").
		Where("project_id = ?", job.ProjectID).
		Where("properties->>'_extraction_job_id' = ?", job.ID).
		Where("deleted_at IS NULL")
	if stagingBranchID != nil {
		objQ = objQ.Where("branch_id = ?", *stagingBranchID)
		relQ = relQ.Where("branch_id = ?", *stagingBranchID)
	} else {
		objQ = objQ.Where("branch_id IS NULL")
		relQ = relQ.Where("branch_id IS NULL")
	}

	if err := objQ.Scan(ctx, &result.ObjectsCreated); err != nil {
		return nil, fmt.Errorf("count agent extraction objects: %w", err)
	}
	if err := relQ.Scan(ctx, &result.RelationshipsCreated); err != nil {
		return nil, fmt.Errorf("count agent extraction relationships: %w", err)
	}

	var ids []string
	idQ := w.jobsService.db.NewSelect().
		TableExpr("kb.graph_objects").
		ColumnExpr("id::text").
		Where("project_id = ?", job.ProjectID).
		Where("extraction_job_id = ?", job.ID).
		Where("deleted_at IS NULL")
	if stagingBranchID != nil {
		idQ = idQ.Where("branch_id = ?", *stagingBranchID)
	} else {
		idQ = idQ.Where("branch_id IS NULL")
	}
	if err := idQ.Scan(ctx, &ids); err == nil {
		result.CreatedObjectIDs = ids
	}

	result.DebugInfo = JSON{
		"mode":           "agent",
		"staging_branch": stringValue(job.StagingBranchID),
	}
	return result, nil
}

// buildAgentExtractionMessage is the whole task the agent receives: the
// extraction contract, the registered schema, the batch text, and the staging
// branch it must write to (also installed in context, so the explicit
// instruction is belt-and-braces rather than the only guard).
func buildAgentExtractionMessage(job *ObjectExtractionJob, def *domainagents.AgentDefinition, schemas *ExtractionSchemas, batch, branchRef string) string {
	objectSchema, _ := json.Marshal(schemas.ObjectSchemas)
	relationshipSchema, _ := json.Marshal(schemas.RelationshipSchemas)

	var b strings.Builder
	b.WriteString("Extract knowledge-graph objects and relationships from the document text below.\n")
	b.WriteString("Write the results with the graph tools (entity-create, relationship-create).\n\n")
	b.WriteString("## Rules\n")
	b.WriteString("- Create one entity per distinct real-world object, keyed by its name; do not create entities for document lines, headers or dates.\n")
	b.WriteString("- Use ONLY the object types and property names declared in the schema below.\n")
	b.WriteString("- Put every fact about an entity into its properties; name, type and description stay top-level fields.\n")
	b.WriteString("- After the entities exist, create the relationships between them, using only declared relationship types.\n")
	b.WriteString("- Do not fabricate facts the text does not support.\n")
	if branchRef != "" {
		fmt.Fprintf(&b, "- Write everything to branch %q. It is already selected for your tool calls; do not target another branch.\n", branchRef)
	}
	fmt.Fprintf(&b, "- Extraction job id: %s\n", job.ID)
	if def != nil && def.Name != "" {
		fmt.Fprintf(&b, "- Extraction agent: %s\n", def.Name)
	}
	b.WriteString("\n## Object types\n")
	b.Write(objectSchema)
	b.WriteString("\n\n## Relationship types\n")
	b.Write(relationshipSchema)
	b.WriteString("\n\n## Document text\n")
	b.WriteString(batch)
	return b.String()
}
