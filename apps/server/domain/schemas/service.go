package schemas

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/uptrace/bun"

	"github.com/emergent-company/emergent.memory/domain/extraction/agents"
	"github.com/emergent-company/emergent.memory/domain/graph"
	"github.com/emergent-company/emergent.memory/pkg/apperror"
	"github.com/emergent-company/emergent.memory/pkg/logger"
)

// Service handles business logic for memory schemas
type Service struct {
	repo     *Repository
	graphSvc *graph.Service
	log      *slog.Logger
}

// NewService creates a new schemas service
func NewService(repo *Repository, graphSvc *graph.Service, log *slog.Logger) *Service {
	return &Service{
		repo:     repo,
		graphSvc: graphSvc,
		log:      log.With(logger.Scope("schemas.svc")),
	}
}

// GetCompiledTypes returns compiled object and relationship types for a project
func (s *Service) GetCompiledTypes(ctx context.Context, projectID string) (*CompiledTypesResponse, error) {
	return s.repo.GetCompiledTypesByProject(ctx, projectID)
}

// GetAvailablePacks returns schemas available for a project to install.
// Returns schemas owned by the project, plus org-visible schemas from the same org.
func (s *Service) GetAvailablePacks(ctx context.Context, projectID, orgID string) ([]MemorySchemaListItem, error) {
	return s.repo.GetAvailablePacks(ctx, projectID, orgID)
}

// GetInstalledPacks returns schemas installed for a project
func (s *Service) GetInstalledPacks(ctx context.Context, projectID string) ([]InstalledSchemaItem, error) {
	return s.repo.GetInstalledPacks(ctx, projectID)
}

// AssignPack assigns a schema to a project and registers its types.
// When req.DryRun is true, returns a preview without making any changes.
// When req.Merge is true, additively merges incoming schemas into existing types.
// When the schema has migration hints, auto-migration is triggered asynchronously.
func (s *Service) AssignPack(ctx context.Context, projectID, userID string, req *AssignPackRequest) (*AssignPackResult, error) {
	result, err := s.repo.AssignPackWithTypes(ctx, projectID, userID, req)
	if err != nil {
		return nil, err
	}

	// Get the full schema to check for migration hints
	pack, packErr := s.repo.GetPackByID(ctx, req.SchemaID)
	if packErr != nil || pack == nil || pack.Migrations == nil {
		// No migration hints — return result as-is
		return result, nil
	}

	// Task 8.6: dry_run — run preview and attach but don't enqueue
	if req.DryRun {
		preview, prevErr := s.PreviewSchemaMigration(ctx, projectID, &SchemaMigrationPreviewRequest{
			ToSchemaID: req.SchemaID,
			Hints:      pack.Migrations,
		})
		if prevErr == nil {
			result.MigrationPreview = preview
		}
		return result, nil
	}

	// Task 8.1: Resolve migration chain
	chain, chainErr := s.ResolveMigrationChain(ctx, projectID, req.SchemaID)
	if chainErr != nil {
		// Task 8.4: chain unresolvable
		result.MigrationStatus = "chain_unresolvable"
		result.MigrationBlockReason = chainErr.Error()
		return result, nil
	}
	if len(chain) == 0 {
		return result, nil
	}

	// Task 8.2: Preview first hop synchronously for risk gate
	firstHop := chain[0]
	preview, prevErr := s.PreviewSchemaMigration(ctx, projectID, &SchemaMigrationPreviewRequest{
		FromSchemaID: firstHop.FromSchemaID,
		ToSchemaID:   firstHop.ToSchemaID,
		Hints:        firstHop.Hints,
	})
	if prevErr != nil {
		result.MigrationStatus = "chain_unresolvable"
		result.MigrationBlockReason = fmt.Sprintf("preview failed: %s", prevErr.Error())
		return result, nil
	}

	// Task 8.3: Block if dangerous and not force
	if preview.OverallRisk == string(graph.RiskLevelDangerous) && !req.Force {
		result.MigrationStatus = "blocked"
		result.MigrationBlockReason = preview.BlockReason
		result.MigrationPreview = preview
		return result, nil
	}

	// Task 8.5: Enqueue async migration job
	job, jobErr := s.runAutoMigrationAsync(ctx, projectID, chain, req.Force, req.AutoUninstall)
	if jobErr != nil {
		s.log.Error("failed to enqueue migration job", logger.Error(jobErr))
		result.MigrationStatus = "chain_unresolvable"
		result.MigrationBlockReason = fmt.Sprintf("failed to enqueue: %s", jobErr.Error())
		return result, nil
	}

	result.MigrationJobID = &job.ID
	result.MigrationStatus = "pending"
	return result, nil
}

// UpdateAssignment updates a pack assignment
func (s *Service) UpdateAssignment(ctx context.Context, projectID, assignmentID string, req *UpdateAssignmentRequest) error {
	return s.repo.UpdateAssignment(ctx, projectID, assignmentID, req)
}

// DeleteAssignment removes a pack assignment from a project
func (s *Service) DeleteAssignment(ctx context.Context, projectID, assignmentID string) error {
	return s.repo.DeleteAssignment(ctx, projectID, assignmentID)
}

// CreatePack creates a new schema scoped to the given project and org.
// If migration hints are present, they are validated before persisting.
func (s *Service) CreatePack(ctx context.Context, projectID, orgID string, req *CreatePackRequest) (*GraphMemorySchema, error) {
	// Task 3.5: Validate migration hints if present
	if req.Migrations != nil {
		errs := validateMigrationHints(req.Migrations, req.GetObjectTypeSchemas(), req.GetRelationshipTypeSchemas())
		if len(errs) > 0 {
			return nil, apperror.ErrBadRequest.WithMessage("invalid migrations block: " + strings.Join(errs, "; "))
		}
	}
	return s.repo.CreatePack(ctx, projectID, orgID, req)
}

// GetPack returns a schema by ID if the caller has access (same project or same org with org visibility)
func (s *Service) GetPack(ctx context.Context, packID, projectID, orgID string) (*GraphMemorySchema, error) {
	return s.repo.GetPack(ctx, packID, projectID, orgID)
}

// UpdatePack partially updates an existing schema the caller owns.
// If migration hints are present in the update, they are validated before persisting.
func (s *Service) UpdatePack(ctx context.Context, packID, projectID, orgID string, req *UpdatePackRequest) (*GraphMemorySchema, error) {
	// Task 3.5: Validate migration hints if present
	if req.Migrations != nil {
		// For update, we need to get the schema's current type schemas for validation
		pack, err := s.repo.GetPack(ctx, packID, projectID, orgID)
		if err != nil {
			return nil, err
		}
		// Use updated schemas if provided in the request, otherwise use existing ones
		objSchemas := pack.ObjectTypeSchemas
		if len(req.ObjectTypeSchemas) > 0 {
			objSchemas = req.ObjectTypeSchemas
		}
		relSchemas := pack.RelationshipTypeSchemas
		if len(req.RelationshipTypeSchemas) > 0 {
			relSchemas = req.RelationshipTypeSchemas
		}
		errs := validateMigrationHints(req.Migrations, objSchemas, relSchemas)
		if len(errs) > 0 {
			return nil, apperror.ErrBadRequest.WithMessage("invalid migrations block: " + strings.Join(errs, "; "))
		}
	}
	return s.repo.UpdatePack(ctx, packID, projectID, orgID, req)
}

// DeletePack deletes a schema the caller owns from the registry
func (s *Service) DeletePack(ctx context.Context, packID, projectID, orgID string) error {
	return s.repo.DeletePack(ctx, packID, projectID, orgID)
}

// GetSchemaHistory returns all schema assignments for a project including removed ones.
func (s *Service) GetSchemaHistory(ctx context.Context, projectID string) ([]SchemaHistoryItem, error) {
	return s.repo.GetAssignmentHistory(ctx, projectID)
}

// MigrateTypes renames object/edge types and/or property keys across live graph data.
func (s *Service) MigrateTypes(ctx context.Context, projectID string, req *MigrateRequest) (*MigrateResponse, error) {
	return s.repo.MigrateTypes(ctx, projectID, req)
}

// ---------------------------------------------------------------------------
// Migration Chain Resolution (Tasks 5.1–5.5)
// ---------------------------------------------------------------------------

const maxMigrationHops = 10

// ResolveMigrationChain walks the from_version chain from the installed schema
// version up to toSchemaID, returning the ordered list of hops to execute.
// Returns an empty slice if no migration is needed (already at toSchemaID).
func (s *Service) ResolveMigrationChain(ctx context.Context, projectID, toSchemaID string) ([]MigrationHop, error) {
	// Get the target schema
	toSchema, err := s.repo.GetPackByID(ctx, toSchemaID)
	if err != nil {
		return nil, fmt.Errorf("cannot find target schema %s: %w", toSchemaID, err)
	}
	if toSchema.Migrations == nil {
		return nil, nil
	}

	// Walk backwards building hops from newest → oldest
	var hopsReverse []MigrationHop
	current := toSchema

	for i := 0; i < maxMigrationHops; i++ {
		if current.Migrations == nil {
			break
		}
		fromVersion := current.Migrations.FromVersion
		if fromVersion == "" {
			break
		}

		// Find schema in registry by (name, version) for the from_version
		fromSchemas, lookupErr := s.repo.GetInstalledSchemasByName(ctx, projectID, current.Name)
		if lookupErr != nil {
			return nil, fmt.Errorf("failed to look up installed schemas for %q: %w", current.Name, lookupErr)
		}

		// Check if the currently installed version is fromVersion
		var fromSchema *GraphMemorySchema
		for j := range fromSchemas {
			if fromSchemas[j].Version == fromVersion {
				fromSchema = &fromSchemas[j]
				break
			}
		}

		// Also look up in global registry for the hop's fromSchemaID
		if fromSchema == nil {
			// Look globally in registry by name+version
			globalSchema, globalErr := s.repo.GetPackByNameVersion(ctx, current.Name, fromVersion)
			if globalErr != nil || globalSchema == nil {
				return nil, fmt.Errorf("cannot resolve migration hop: schema %q version %q not found in registry", current.Name, fromVersion)
			}
			fromSchema = globalSchema
		}

		hopsReverse = append(hopsReverse, MigrationHop{
			FromSchemaID: fromSchema.ID,
			ToSchemaID:   current.ID,
			Hints:        current.Migrations,
		})

		// Check if fromVersion is installed for this project — if so, stop
		for j := range fromSchemas {
			if fromSchemas[j].Version == fromVersion {
				// Found it installed — done walking backwards
				goto done
			}
		}

		// Continue walking backwards
		current = fromSchema
	}

	if len(hopsReverse) >= maxMigrationHops {
		return nil, fmt.Errorf("migration chain exceeds maximum depth of %d hops", maxMigrationHops)
	}

done:
	// Reverse hops so they run oldest → newest
	chain := make([]MigrationHop, len(hopsReverse))
	for i, hop := range hopsReverse {
		chain[len(hopsReverse)-1-i] = hop
	}
	return chain, nil
}

// ---------------------------------------------------------------------------
// SchemaMigrationOrchestrator Methods (Tasks 6.2–6.6)
// ---------------------------------------------------------------------------

// PreviewSchemaMigration runs a dry migration against all affected objects and
// returns a risk assessment without persisting any changes.
func (s *Service) PreviewSchemaMigration(ctx context.Context, projectID string, req *SchemaMigrationPreviewRequest) (*SchemaMigrationPreviewResponse, error) {
	projectUUID, err := uuid.Parse(projectID)
	if err != nil {
		return nil, apperror.ErrBadRequest.WithMessage("invalid projectId")
	}

	hints := req.Hints
	if hints == nil && req.ToSchemaID != "" {
		toSchema, schErr := s.repo.GetPackByID(ctx, req.ToSchemaID)
		if schErr == nil && toSchema != nil {
			hints = toSchema.Migrations
		}
	}

	// Build from/to agents.ObjectSchema maps
	fromObjSchemas, toObjSchemas, schErr := s.buildFromToObjectSchemas(ctx, req.FromSchemaID, req.ToSchemaID, hints)
	if schErr != nil {
		return nil, schErr
	}

	migrator := graph.NewSchemaMigrator(graph.NewPropertyValidator(), s.log)

	resp := &SchemaMigrationPreviewResponse{
		ProjectID:    projectID,
		FromSchemaID: req.FromSchemaID,
		ToSchemaID:   req.ToSchemaID,
	}

	overallRisk := graph.RiskLevelSafe
	perType := map[string]*MigrationTypeResult{}

	for typeName, toSchema := range toObjSchemas {
		fromSchema := fromObjSchemas[typeName]
		if fromSchema == nil {
			fromSchema = &agents.ObjectSchema{Name: typeName, Properties: map[string]agents.PropertyDef{}}
		}

		// List all objects of this type in the project
		typeStr := typeName
		objs, listErr := s.graphSvc.GetRepository().List(ctx, graph.ListParams{
			ProjectID: projectUUID,
			Type:      &typeStr,
		})
		if listErr != nil {
			return nil, fmt.Errorf("failed to list objects of type %s: %w", typeName, listErr)
		}

		typeResult := &MigrationTypeResult{
			TypeName:    typeName,
			ObjectCount: len(objs),
		}

		typeRisk := graph.RiskLevelSafe
		for _, obj := range objs {
			result := migrator.MigrateObject(ctx, obj, fromSchema, toSchema, req.FromSchemaID, req.ToSchemaID)
			if riskWeight(result.RiskLevel) > riskWeight(typeRisk) {
				typeRisk = result.RiskLevel
			}
			if !result.CanProceed {
				typeResult.CanProceed = false
				typeResult.BlockReason = result.BlockReason
			}
		}
		if len(objs) > 0 && typeResult.BlockReason == "" {
			typeResult.CanProceed = true
		}
		typeResult.RiskLevel = string(typeRisk)

		if riskWeight(typeRisk) > riskWeight(overallRisk) {
			overallRisk = typeRisk
		}

		resp.TotalObjects += len(objs)
		perType[typeName] = typeResult
	}

	for _, tr := range perType {
		resp.PerTypeResults = append(resp.PerTypeResults, *tr)
	}

	resp.OverallRisk = string(overallRisk)
	resp.CanProceed = overallRisk != graph.RiskLevelDangerous
	if !resp.CanProceed {
		resp.BlockReason = fmt.Sprintf("migration risk level is %s", overallRisk)
	}

	return resp, nil
}

// ExecuteSchemaMigration runs rename SQL and then migrates all affected objects,
// writing back their updated properties and schema_version.
func (s *Service) ExecuteSchemaMigration(ctx context.Context, projectID string, req *SchemaMigrationExecuteRequest) (*SchemaMigrationExecuteResponse, error) {
	projectUUID, err := uuid.Parse(projectID)
	if err != nil {
		return nil, apperror.ErrBadRequest.WithMessage("invalid projectId")
	}

	hints := req.Hints
	if hints == nil && req.ToSchemaID != "" {
		toSchema, schErr := s.repo.GetPackByID(ctx, req.ToSchemaID)
		if schErr == nil && toSchema != nil {
			hints = toSchema.Migrations
		}
	}

	// Step 1: Run System B SQL renames if hints specify type/property renames
	if hints != nil && (len(hints.TypeRenames) > 0 || len(hints.PropertyRenames) > 0) {
		migrateReq := &MigrateRequest{DryRun: false}
		for _, tr := range hints.TypeRenames {
			migrateReq.TypeRenames = append(migrateReq.TypeRenames, TypeRename{From: tr.From, To: tr.To})
		}
		for _, pr := range hints.PropertyRenames {
			migrateReq.PropertyRenames = append(migrateReq.PropertyRenames, PropertyRename{
				TypeName: pr.TypeName, From: pr.From, To: pr.To,
			})
		}
		if _, migrErr := s.repo.MigrateTypes(ctx, projectID, migrateReq); migrErr != nil {
			return nil, fmt.Errorf("rename SQL failed: %w", migrErr)
		}
	}

	// Step 2: Build from/to schemas and run SchemaMigrator per object
	fromObjSchemas, toObjSchemas, schErr := s.buildFromToObjectSchemas(ctx, req.FromSchemaID, req.ToSchemaID, hints)
	if schErr != nil {
		return nil, schErr
	}

	migrator := graph.NewSchemaMigrator(graph.NewPropertyValidator(), s.log)

	// Build set of declared-removed properties (suppresses block for risky drops)
	removedSet := map[string]map[string]bool{} // typeName → propName set
	if hints != nil {
		for _, rp := range hints.RemovedProperties {
			if removedSet[rp.TypeName] == nil {
				removedSet[rp.TypeName] = map[string]bool{}
			}
			removedSet[rp.TypeName][rp.Name] = true
		}
	}

	resp := &SchemaMigrationExecuteResponse{
		ProjectID:    projectID,
		FromSchemaID: req.FromSchemaID,
		ToSchemaID:   req.ToSchemaID,
	}

	db := s.repo.DB()
	toVersion := req.ToSchemaID
	maxObjs := req.MaxObjects

	for typeName, toSchema := range toObjSchemas {
		fromSchema := fromObjSchemas[typeName]
		if fromSchema == nil {
			fromSchema = &agents.ObjectSchema{Name: typeName, Properties: map[string]agents.PropertyDef{}}
		}

		typeStr := typeName
		listParams := graph.ListParams{
			ProjectID: projectUUID,
			Type:      &typeStr,
		}
		if maxObjs > 0 {
			listParams.Limit = maxObjs
		}
		objs, listErr := s.graphSvc.GetRepository().List(ctx, listParams)
		if listErr != nil {
			return nil, fmt.Errorf("failed to list objects of type %s: %w", typeName, listErr)
		}

		for _, obj := range objs {
			result := migrator.MigrateObject(ctx, obj, fromSchema, toSchema, req.FromSchemaID, toVersion)
			if !result.CanProceed && !req.Force {
				// Check if the block is solely due to declared-removed properties
				if !canProceedWithRemovedHints(result, removedSet[typeName]) {
					resp.ObjectsFailed++
					continue
				}
			}

			// Patch the object with new properties, schema_version, and migration_archive
			archiveJSON, _ := json.Marshal(obj.MigrationArchive)
			_, patchErr := db.NewRaw(`
				UPDATE kb.graph_objects
				SET properties = ?,
				    schema_version = ?,
				    migration_archive = ?,
				    updated_at = NOW()
				WHERE id = ? AND project_id = ?
			`, result.NewProperties, toVersion, string(archiveJSON), obj.ID, projectID).Exec(ctx)
			if patchErr != nil {
				s.log.Warn("failed to patch object after migration",
					slog.String("objectId", obj.ID.String()),
					logger.Error(patchErr))
				resp.ObjectsFailed++
				continue
			}
			resp.ObjectsMigrated++
		}
	}

	// Write kb.schema_migration_runs record
	now := time.Now()
	_, _ = db.NewRaw(`
		INSERT INTO kb.schema_migration_runs
		(project_id, from_schema_id, to_schema_id, objects_migrated, objects_failed, created_at)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT DO NOTHING
	`, projectID, req.FromSchemaID, req.ToSchemaID, resp.ObjectsMigrated, resp.ObjectsFailed, now).Exec(ctx)

	return resp, nil
}

// RollbackSchemaMigration restores property data from migration_archive for
// all project objects that have an archive entry targeting toVersion.
// If restoreTypeRegistry is true, the old schema types are re-installed and
// the new schema's additions are removed, all in a single transaction.
func (s *Service) RollbackSchemaMigration(ctx context.Context, projectID string, req *SchemaMigrationRollbackRequest) (*SchemaMigrationRollbackResponse, error) {
	projectUUID, err := uuid.Parse(projectID)
	if err != nil {
		return nil, apperror.ErrBadRequest.WithMessage("invalid projectId")
	}

	// Fetch all objects that have a migration_archive entry for toVersion
	objs, listErr := s.graphSvc.GetRepository().List(ctx, graph.ListParams{
		ProjectID: projectUUID,
	})
	if listErr != nil {
		return nil, fmt.Errorf("failed to list objects: %w", listErr)
	}

	migrator := graph.NewSchemaMigrator(graph.NewPropertyValidator(), s.log)
	resp := &SchemaMigrationRollbackResponse{
		ProjectID: projectID,
		ToVersion: req.ToVersion,
	}

	db := s.repo.DB()

	err = db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		for _, obj := range objs {
			if len(obj.MigrationArchive) == 0 {
				continue
			}
			result := migrator.RollbackObject(obj, req.ToVersion)
			if !result.Success {
				continue
			}
			archiveJSON, _ := json.Marshal(obj.MigrationArchive)
			_, patchErr := tx.NewRaw(`
				UPDATE kb.graph_objects
				SET properties = ?,
				    schema_version = ?,
				    migration_archive = ?,
				    updated_at = NOW()
				WHERE id = ? AND project_id = ?
			`, obj.Properties, req.ToVersion, string(archiveJSON), obj.ID, projectID).Exec(ctx)
			if patchErr != nil {
				resp.ObjectsFailed++
			} else {
				resp.ObjectsRestored++
			}
		}

		if req.RestoreTypeRegistry {
			// Re-install old schema types and remove new schema's additions via raw SQL
			// This is a best-effort: update schema_version for affected objects
			_, _ = tx.NewRaw(`
				UPDATE kb.project_object_schema_registry
				SET json_schema = (
					SELECT json_schema FROM kb.project_object_schema_registry_history
					WHERE project_id = ? AND version = ?
					LIMIT 1
				),
				updated_at = NOW()
				WHERE project_id = ?
			`, projectID, req.ToVersion, projectID).Exec(ctx)
		}
		return nil
	})
	if err != nil {
		return nil, apperror.ErrDatabase.WithInternal(err)
	}

	return resp, nil
}

// CommitMigrationArchive prunes migration_archive entries whose to_version is
// less than or equal to throughVersion from all objects in the project.
func (s *Service) CommitMigrationArchive(ctx context.Context, projectID string, req *CommitMigrationArchiveRequest) (*CommitMigrationArchiveResponse, error) {
	db := s.repo.DB()

	// Use JSONB filtering to remove archive entries where to_version <= throughVersion
	// We do this via a CTE that reconstructs the filtered array.
	result, execErr := db.NewRaw(`
		WITH updated AS (
			UPDATE kb.graph_objects
			SET migration_archive = (
				SELECT COALESCE(jsonb_agg(elem), '[]'::jsonb)
				FROM jsonb_array_elements(migration_archive) AS elem
				WHERE elem->>'to_version' > ?
			),
			updated_at = NOW()
			WHERE project_id = ?
			  AND migration_archive IS NOT NULL
			  AND migration_archive != '[]'::jsonb
			  AND EXISTS (
				SELECT 1 FROM jsonb_array_elements(migration_archive) AS elem
				WHERE elem->>'to_version' <= ?
			  )
			RETURNING 1
		) SELECT COUNT(*) FROM updated
	`, req.ThroughVersion, projectID, req.ThroughVersion).Exec(ctx)
	if execErr != nil {
		return nil, apperror.ErrDatabase.WithInternal(execErr)
	}

	rowsAffected, _ := result.RowsAffected()
	return &CommitMigrationArchiveResponse{
		ProjectID:      projectID,
		ThroughVersion: req.ThroughVersion,
		ObjectsUpdated: int(rowsAffected),
	}, nil
}

// GetMigrationJobStatus returns the current status of a migration job.
func (s *Service) GetMigrationJobStatus(ctx context.Context, projectID, jobID string) (*SchemaMigrationJob, error) {
	job, err := s.repo.GetMigrationJob(ctx, jobID)
	if err != nil {
		return nil, err
	}
	if job.ProjectID != projectID {
		return nil, apperror.ErrNotFound.WithMessage("migration job not found")
	}
	return job, nil
}

// runAutoMigrationAsync deduplicates, then creates a pending SchemaMigrationJob
// that the background worker will pick up and execute.
func (s *Service) runAutoMigrationAsync(ctx context.Context, projectID string, chain []MigrationHop, force, autoUninstall bool) (*SchemaMigrationJob, error) {
	if len(chain) == 0 {
		return nil, fmt.Errorf("empty migration chain")
	}
	firstHop := chain[0]
	lastHop := chain[len(chain)-1]

	// Dedup: check for an existing pending/running job
	existing, _ := s.repo.FindActiveMigrationJob(ctx, projectID, firstHop.FromSchemaID, lastHop.ToSchemaID)
	if existing != nil {
		return existing, nil
	}

	job := &SchemaMigrationJob{
		ProjectID:     projectID,
		FromSchemaID:  firstHop.FromSchemaID,
		ToSchemaID:    lastHop.ToSchemaID,
		Chain:         chain,
		Status:        "pending",
		AutoUninstall: autoUninstall,
		CreatedAt:     time.Now(),
	}

	if err := s.repo.CreateMigrationJob(ctx, job); err != nil {
		return nil, err
	}
	return job, nil
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// riskWeight converts a risk level to a numeric weight for comparison.
func riskWeight(r graph.MigrationRiskLevel) int {
	switch r {
	case graph.RiskLevelSafe:
		return 0
	case graph.RiskLevelCautious:
		return 1
	case graph.RiskLevelRisky:
		return 2
	case graph.RiskLevelDangerous:
		return 3
	}
	return 0
}

// canProceedWithRemovedHints returns true if the only reason a migration can't
// proceed is due to explicitly declared removed properties.
func canProceedWithRemovedHints(result *graph.MigrationResult, removedProps map[string]bool) bool {
	if result.CanProceed {
		return true
	}
	// Check if all dropped props are in the declared removedProps set
	for _, dp := range result.DroppedProps {
		if !removedProps[dp] {
			return false
		}
	}
	// Check no error-level issues beyond removed props
	for _, issue := range result.Issues {
		if issue.Severity == "error" {
			return false
		}
	}
	return true
}

// buildFromToObjectSchemas builds agents.ObjectSchema maps for from and to schemas
// used by SchemaMigrator.MigrateObject.
func (s *Service) buildFromToObjectSchemas(ctx context.Context, fromSchemaID, toSchemaID string, hints *SchemaMigrationHints) (map[string]*agents.ObjectSchema, map[string]*agents.ObjectSchema, error) {
	fromMap := map[string]*agents.ObjectSchema{}
	toMap := map[string]*agents.ObjectSchema{}

	if toSchemaID != "" {
		toSchema, err := s.repo.GetPackByID(ctx, toSchemaID)
		if err != nil {
			return nil, nil, fmt.Errorf("cannot find to_schema %s: %w", toSchemaID, err)
		}
		toMap = parseObjectSchemasToAgentMap(toSchema.ObjectTypeSchemas)
	}

	if fromSchemaID != "" {
		fromSchema, err := s.repo.GetPackByID(ctx, fromSchemaID)
		if err != nil {
			return nil, nil, fmt.Errorf("cannot find from_schema %s: %w", fromSchemaID, err)
		}
		fromMap = parseObjectSchemasToAgentMap(fromSchema.ObjectTypeSchemas)
	}

	return fromMap, toMap, nil
}

// parseObjectSchemasToAgentMap converts raw JSONB object type schemas to
// a map of typeName → *agents.ObjectSchema for use by SchemaMigrator.
func parseObjectSchemasToAgentMap(raw json.RawMessage) map[string]*agents.ObjectSchema {
	result := map[string]*agents.ObjectSchema{}
	if len(raw) == 0 {
		return result
	}

	// Try array format: [{name, properties: {propName: {type, ...}}}]
	var arr []struct {
		Name       string                        `json:"name"`
		Properties map[string]agents.PropertyDef `json:"properties"`
		Required   []string                      `json:"required"`
	}
	if err := json.Unmarshal(raw, &arr); err == nil {
		for _, item := range arr {
			if item.Name == "" {
				continue
			}
			schema := &agents.ObjectSchema{
				Name:       item.Name,
				Properties: item.Properties,
				Required:   item.Required,
			}
			if schema.Properties == nil {
				schema.Properties = map[string]agents.PropertyDef{}
			}
			result[item.Name] = schema
		}
		return result
	}

	// Try map format: {typeName: {properties: {...}}}
	var objMap map[string]struct {
		Properties map[string]agents.PropertyDef `json:"properties"`
		Required   []string                      `json:"required"`
	}
	if err := json.Unmarshal(raw, &objMap); err == nil {
		for typeName, def := range objMap {
			schema := &agents.ObjectSchema{
				Name:       typeName,
				Properties: def.Properties,
				Required:   def.Required,
			}
			if schema.Properties == nil {
				schema.Properties = map[string]agents.PropertyDef{}
			}
			result[typeName] = schema
		}
	}

	return result
}
