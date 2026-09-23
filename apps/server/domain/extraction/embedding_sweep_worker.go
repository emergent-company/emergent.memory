package extraction

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/uptrace/bun"

	"github.com/emergent-company/emergent.memory/pkg/auth"
	"github.com/emergent-company/emergent.memory/pkg/embeddings/vertex"
	"github.com/emergent-company/emergent.memory/pkg/logger"
)

// EmbeddingSweepConfig contains configuration for the embedding sweep worker.
type EmbeddingSweepConfig struct {
	// SweepIntervalSec is the interval between sweeps in seconds (default: 30)
	SweepIntervalSec int
	// BatchSize is the maximum number of relationships scanned per sweep
	// (default: 200). It no longer bounds object admission — object admission
	// is keyed off available queue depth via ObjectQueueTargetDepth.
	BatchSize int
	// ObjectQueueTargetDepth is the target number of active (pending|processing)
	// kb.graph_embedding_jobs. Each sweep admits at most
	// max(0, ObjectQueueTargetDepth - active) objects. A value <= 0 falls back
	// to defaultSweepObjectQueueTargetDepth.
	ObjectQueueTargetDepth int
	// RelationshipRequestBatchSize is the maximum number of relationships sent
	// in one multi-object embedding request. 0 uses
	// defaultEmbeddingRequestBatchSize; a negative value forces the serial
	// fallback (one request per relationship).
	RelationshipRequestBatchSize int
}

// defaultSweepObjectQueueTargetDepth is the fallback for
// EmbeddingSweepConfig.ObjectQueueTargetDepth when it is <= 0.
const defaultSweepObjectQueueTargetDepth = 2000

// DefaultEmbeddingSweepConfig returns the default sweep configuration.
func DefaultEmbeddingSweepConfig() *EmbeddingSweepConfig {
	return &EmbeddingSweepConfig{
		SweepIntervalSec:             30,
		BatchSize:                    200,
		ObjectQueueTargetDepth:       defaultSweepObjectQueueTargetDepth,
		RelationshipRequestBatchSize: 0,
	}
}

// SweepInterval returns the sweep interval as a Duration.
func (c *EmbeddingSweepConfig) SweepInterval() time.Duration {
	return time.Duration(c.SweepIntervalSec) * time.Second
}

// EmbeddingSweepWorker periodically scans for objects and relationships with
// missing embeddings and regenerates them. This is a self-healing mechanism
// that catches any entities that fell through the cracks — e.g. relationships
// created when the embedding model was down, objects missed by the job queue,
// or PatchRelationship calls that didn't regenerate embeddings.
//
// Objects: enqueued into the existing GraphEmbeddingJobsService queue.
// Relationships: embeddings generated and stored directly (no job queue).
type EmbeddingSweepWorker struct {
	jobs   *GraphEmbeddingJobsService
	embeds EmbeddingService
	db     bun.IDB
	cfg    *EmbeddingSweepConfig
	log    *slog.Logger

	// Usage tracking & budget enforcement
	usage                    EmbeddingUsageRecorder
	budget                   BudgetChecker
	budgetEnforcementEnabled bool
	orgCache                 *orgIDCache

	stopCh    chan struct{}
	stoppedCh chan struct{}
	running   bool
	paused    bool
	mu        sync.Mutex

	// Metrics
	metricsMu             sync.RWMutex
	objectsEnqueued       int64
	relationshipsEmbedded int64
	relationshipErrors    int64
	sweepCount            int64
}

// NewEmbeddingSweepWorker creates a new embedding sweep worker.
func NewEmbeddingSweepWorker(
	jobs *GraphEmbeddingJobsService,
	embeds EmbeddingService,
	db bun.IDB,
	cfg *EmbeddingSweepConfig,
	log *slog.Logger,
	usage EmbeddingUsageRecorder,
	budget BudgetChecker,
	budgetEnforcementEnabled bool,
) *EmbeddingSweepWorker {
	return &EmbeddingSweepWorker{
		jobs:                     jobs,
		embeds:                   embeds,
		db:                       db,
		cfg:                      cfg,
		log:                      log.With(logger.Scope("embedding.sweep")),
		usage:                    usage,
		budget:                   budget,
		budgetEnforcementEnabled: budgetEnforcementEnabled,
		orgCache:                 newOrgIDCache(db, log),
	}
}

// Start begins the sweep worker's polling loop.
func (w *EmbeddingSweepWorker) Start(ctx context.Context) error {
	w.mu.Lock()
	if w.running {
		w.mu.Unlock()
		return nil
	}

	if !w.embeds.IsEnabled() {
		w.log.Info("embedding sweep worker not started (embeddings not enabled)")
		w.mu.Unlock()
		return nil
	}

	w.running = true
	w.stopCh = make(chan struct{})
	w.stoppedCh = make(chan struct{})
	w.mu.Unlock()

	w.log.Info("embedding sweep worker starting",
		slog.Duration("sweep_interval", w.cfg.SweepInterval()),
		slog.Int("batch_size", w.cfg.BatchSize))

	go w.run(ctx)

	return nil
}

// Stop gracefully stops the sweep worker.
func (w *EmbeddingSweepWorker) Stop(ctx context.Context) error {
	w.mu.Lock()
	if !w.running {
		w.mu.Unlock()
		return nil
	}
	w.running = false
	close(w.stopCh)
	w.mu.Unlock()

	w.log.Debug("waiting for embedding sweep worker to stop...")

	select {
	case <-w.stoppedCh:
		w.log.Info("embedding sweep worker stopped gracefully")
	case <-ctx.Done():
		w.log.Warn("embedding sweep worker stop timeout, forcing shutdown")
	}

	return nil
}

// run is the main sweep loop.
func (w *EmbeddingSweepWorker) run(ctx context.Context) {
	defer close(w.stoppedCh)

	ticker := time.NewTicker(w.cfg.SweepInterval())
	defer ticker.Stop()

	for {
		select {
		case <-w.stopCh:
			return
		case <-ctx.Done():
			return
		case <-ticker.C:
			w.sweep(ctx)
		}
	}
}

// Pause suspends sweep processing without stopping the worker goroutine.
func (w *EmbeddingSweepWorker) Pause() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.paused = true
	w.log.Info("embedding sweep worker paused")
}

// Resume resumes sweep processing after a Pause.
func (w *EmbeddingSweepWorker) Resume() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.paused = false
	w.log.Info("embedding sweep worker resumed")
}

// IsPaused returns whether the sweep worker is currently paused.
func (w *EmbeddingSweepWorker) IsPaused() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.paused
}

// sweep runs one sweep cycle: objects first, then relationships.
func (w *EmbeddingSweepWorker) sweep(ctx context.Context) {
	select {
	case <-w.stopCh:
		return
	case <-ctx.Done():
		return
	default:
	}

	// Check if paused
	w.mu.Lock()
	paused := w.paused
	w.mu.Unlock()
	if paused {
		return
	}

	w.incrementSweepCount()

	objectsEnqueued := w.sweepObjects(ctx)
	relsEmbedded, relsErrors := w.sweepRelationships(ctx)

	if objectsEnqueued > 0 || relsEmbedded > 0 || relsErrors > 0 {
		w.log.Info("sweep completed",
			slog.Int("objects_enqueued", objectsEnqueued),
			slog.Int("relationships_embedded", relsEmbedded),
			slog.Int("relationship_errors", relsErrors))
	}
}

// sweepObjects finds graph objects with NULL embedding_v2 that don't already
// have active jobs in the queue, and enqueues them.
//
// An object is only enqueued when its project has an embedding model — set
// either on the project (kb.project_model_config) or on a provider credential
// (kb.project_provider_configs), the same project → provider fallback the
// embedding resolver (modelconfig.EmbeddingResolverAdapter) applies when it
// actually generates the vector.
func (w *EmbeddingSweepWorker) sweepObjects(ctx context.Context) int {
	target := w.cfg.ObjectQueueTargetDepth
	if target <= 0 {
		target = defaultSweepObjectQueueTargetDepth
	}

	active, err := w.activeGraphEmbeddingJobs(ctx)
	if err != nil {
		// Never flood on a failed depth read: if we cannot see how full the
		// queue is, do not enqueue anything this sweep.
		w.log.Warn("sweep: failed to read active graph embedding job count",
			slog.String("error", err.Error()))
		return 0
	}

	admission := target - active
	if admission <= 0 {
		w.log.Debug("sweep: graph embedding queue at target depth, skipping object admission",
			slog.Int("active", active),
			slog.Int("target", target))
		return 0
	}

	// Find objects missing embeddings that don't have pending/processing jobs
	var objectIDs []string
	err = w.db.NewRaw(`
		SELECT o.id::text
		FROM kb.graph_objects o
		JOIN kb.projects p ON p.id = o.project_id
		WHERE o.embedding_v2 IS NULL
		  AND o.deleted_at IS NULL
		  AND NOT EXISTS (
		    SELECT 1 FROM kb.graph_embedding_jobs j
		    WHERE j.object_id = o.id
		      AND j.status IN ('pending', 'processing')
		  )
		  AND (
		    EXISTS (
		      SELECT 1 FROM kb.project_model_config pmc
		      WHERE pmc.project_id = o.project_id
		        AND pmc.embedding_model IS NOT NULL
		        AND pmc.embedding_model != ''
		    )
		    OR EXISTS (
		      SELECT 1 FROM kb.project_provider_configs ppc
		      WHERE ppc.project_id = o.project_id
		        AND ppc.embedding_model IS NOT NULL
		        AND ppc.embedding_model != ''
		    )
		  )
		ORDER BY o.created_at ASC
		LIMIT ?`, admission).Scan(ctx, &objectIDs)
	if err != nil {
		w.log.Warn("sweep: failed to query objects with missing embeddings",
			slog.String("error", err.Error()))
		return 0
	}

	if len(objectIDs) == 0 {
		return 0
	}

	w.log.Info("sweep: found objects with missing embeddings",
		slog.Int("count", len(objectIDs)))

	enqueued, err := w.jobs.EnqueueBatch(ctx, objectIDs, 0)
	if err != nil {
		w.log.Warn("sweep: failed to enqueue object embedding jobs",
			slog.String("error", err.Error()))
		return 0
	}

	w.addObjectsEnqueued(int64(enqueued))
	return enqueued
}

// activeGraphEmbeddingJobs returns the number of active (pending|processing)
// graph embedding jobs currently in the queue.
func (w *EmbeddingSweepWorker) activeGraphEmbeddingJobs(ctx context.Context) (int, error) {
	var count int
	err := w.db.NewRaw(`SELECT count(*) FROM kb.graph_embedding_jobs
		WHERE status IN ('pending', 'processing')`).Scan(ctx, &count)
	if err != nil {
		return 0, err
	}
	return count, nil
}

// relationshipSweepRow holds data needed to generate a relationship embedding.
type relationshipSweepRow struct {
	ID            string  `bun:"id"`
	Type          string  `bun:"type"`
	Label         *string `bun:"label"`
	RelProperties []byte  `bun:"rel_properties"`
	ProjectID     string  `bun:"project_id"`
	SrcProperties []byte  `bun:"src_properties"`
	SrcKey        *string `bun:"src_key"`
	SrcType       string  `bun:"src_type"`
	DstProperties []byte  `bun:"dst_properties"`
	DstKey        *string `bun:"dst_key"`
	DstType       string  `bun:"dst_type"`
}

// sweepRelationships finds relationships with NULL embedding and generates
// embeddings for them directly (no job queue).
//
// Mirrors sweepObjects' model-config gate: a relationship is only embedded when
// its project has an embedding model on the project or a provider credential.
func (w *EmbeddingSweepWorker) sweepRelationships(ctx context.Context) (embedded int, errors int) {
	var rows []relationshipSweepRow
	err := w.db.NewRaw(`
		SELECT r.id::text, r.type, r.label, r.properties AS rel_properties,
		       r.project_id::text AS project_id,
		       src.properties AS src_properties, src.key AS src_key, src.type AS src_type,
		       dst.properties AS dst_properties, dst.key AS dst_key, dst.type AS dst_type
		FROM kb.graph_relationships r
		JOIN kb.graph_objects src ON src.id = r.src_id
		JOIN kb.graph_objects dst ON dst.id = r.dst_id
		JOIN kb.projects p ON p.id = r.project_id
		WHERE r.embedding IS NULL
		  AND r.deleted_at IS NULL
		  AND src.deleted_at IS NULL
		  AND dst.deleted_at IS NULL
		  AND (
		    EXISTS (
		      SELECT 1 FROM kb.project_model_config pmc
		      WHERE pmc.project_id = r.project_id
		        AND pmc.embedding_model IS NOT NULL
		        AND pmc.embedding_model != ''
		    )
		    OR EXISTS (
		      SELECT 1 FROM kb.project_provider_configs ppc
		      WHERE ppc.project_id = r.project_id
		        AND ppc.embedding_model IS NOT NULL
		        AND ppc.embedding_model != ''
		    )
		  )
		ORDER BY r.created_at ASC
		LIMIT ?`, w.cfg.BatchSize).Scan(ctx, &rows)
	if err != nil {
		w.log.Warn("sweep: failed to query relationships with missing embeddings",
			slog.String("error", err.Error()))
		return 0, 0
	}

	if len(rows) == 0 {
		return 0, 0
	}

	w.log.Info("sweep: found relationships with missing embeddings",
		slog.Int("count", len(rows)))

	// Group by project (preserving first-seen group order): embedding
	// credentials and budgets are resolved per project, so a whole project's
	// relationships are embedded with one budget pre-flight and one set of
	// requests.
	groups := w.groupRelationshipsByProject(rows)

	for _, group := range groups {
		select {
		case <-w.stopCh:
			return embedded, errors
		case <-ctx.Done():
			return embedded, errors
		default:
		}

		groupProjectID := group[0].ProjectID
		groupCtx := ctx
		if groupProjectID != "" {
			groupCtx = auth.ContextWithProjectID(ctx, groupProjectID)
		}

		// Budget pre-flight — one check per project group. Fail-open on error
		// exactly as before; the budget is project-scoped so checking once per
		// group is equivalent to the old per-row checks.
		if w.budget != nil && groupProjectID != "" {
			exceeded, err := w.budget.CheckBudgetExceeded(groupCtx, groupProjectID)
			if err != nil {
				w.log.Warn("sweep: budget check failed, proceeding (fail-open)",
					slog.String("project_id", groupProjectID),
					slog.String("error", err.Error()))
			} else if exceeded && w.budgetEnforcementEnabled {
				w.log.Info("sweep: skipping relationship embeddings, project budget exceeded",
					slog.String("project_id", groupProjectID),
					slog.Int("count", len(group)))
				continue
			}
		}

		batchSize := w.cfg.RelationshipRequestBatchSize
		if batchSize == 0 {
			batchSize = defaultEmbeddingRequestBatchSize
		}

		batcher, ok := w.embeds.(batchEmbeddingService)
		if ok && batchSize > 0 {
			embedded, errors = w.embedRelationshipGroupBatched(groupCtx, groupProjectID, group, batcher, batchSize, embedded, errors)
		} else {
			embedded, errors = w.embedRelationshipGroupSerial(groupCtx, group, embedded, errors)
		}
	}

	w.addRelationshipsEmbedded(int64(embedded))
	w.addRelationshipErrors(int64(errors))
	return embedded, errors
}

// groupRelationshipsByProject groups rows by their ProjectID, preserving the
// first-seen order of the groups.
func (w *EmbeddingSweepWorker) groupRelationshipsByProject(rows []relationshipSweepRow) [][]relationshipSweepRow {
	var groups [][]relationshipSweepRow
	index := make(map[string]int)
	for _, row := range rows {
		i, ok := index[row.ProjectID]
		if !ok {
			i = len(groups)
			index[row.ProjectID] = i
			groups = append(groups, []relationshipSweepRow{row})
			continue
		}
		groups[i] = append(groups[i], row)
	}
	return groups
}

// embedRelationshipGroupBatched embeds a project's relationships using one
// multi-object request per consecutive sub-batch of batchSize rows. Each row's
// vector is stored independently; a per-row store failure counts only that row
// as an error (its embedding stays NULL for a later sweep).
func (w *EmbeddingSweepWorker) embedRelationshipGroupBatched(
	ctx context.Context,
	groupProjectID string,
	group []relationshipSweepRow,
	batcher batchEmbeddingService,
	batchSize int,
	embedded, errors int,
) (int, int) {
	for start := 0; start < len(group); start += batchSize {
		select {
		case <-w.stopCh:
			return embedded, errors
		case <-ctx.Done():
			return embedded, errors
		default:
		}

		end := start + batchSize
		if end > len(group) {
			end = len(group)
		}
		subRows := group[start:end]

		texts := make([]string, len(subRows))
		for i, row := range subRows {
			srcName := displayNameFromRow(row.SrcProperties, row.SrcKey, row.SrcType)
			dstName := displayNameFromRow(row.DstProperties, row.DstKey, row.DstType)
			texts[i] = buildTripletText(srcName, dstName, row.Type, row.Label, row.RelProperties)
		}

		result, err := batcher.EmbedDocumentsWithUsage(ctx, texts)
		if err != nil {
			errors += len(subRows)
			w.log.Warn("sweep: failed to embed relationship batch",
				slog.String("project_id", groupProjectID),
				slog.Int("count", len(subRows)),
				slog.String("error", err.Error()))
			continue
		}
		if result == nil || len(result.Embeddings) != len(texts) {
			errors += len(subRows)
			w.log.Warn("sweep: relationship batch returned mismatched vectors",
				slog.String("project_id", groupProjectID),
				slog.Int("count", len(subRows)))
			continue
		}

		// One usage event per sub-batch: the batch API reports aggregate tokens.
		if w.usage != nil && groupProjectID != "" {
			orgID := w.orgCache.resolve(ctx, groupProjectID)
			recordEmbeddingUsage(w.usage, groupProjectID, orgID, &vertex.EmbedResult{
				Usage:    result.Usage,
				Model:    result.Model,
				Provider: result.Provider,
			})
		}

		for i, row := range subRows {
			now := time.Now()
			_, err := w.db.NewRaw(`UPDATE kb.graph_relationships
				SET embedding = ?::vector, embedding_updated_at = ?
				WHERE id = ?`,
				vectorToString(result.Embeddings[i]), now, row.ID).Exec(ctx)
			if err != nil {
				errors++
				w.log.Warn("sweep: failed to update relationship embedding",
					slog.String("id", row.ID),
					slog.String("error", err.Error()))
				continue
			}
			embedded++
		}
	}
	return embedded, errors
}

// embedRelationshipGroupSerial embeds a project's relationships one request at
// a time. Used when the embedding service has no multi-object method or when
// RelationshipRequestBatchSize is negative. The per-row budget skip is handled
// at the group level (see sweepRelationships), so here each row is embedded,
// stored, and counted independently.
func (w *EmbeddingSweepWorker) embedRelationshipGroupSerial(
	ctx context.Context,
	group []relationshipSweepRow,
	embedded, errors int,
) (int, int) {
	for _, row := range group {
		select {
		case <-w.stopCh:
			return embedded, errors
		case <-ctx.Done():
			return embedded, errors
		default:
		}

		srcName := displayNameFromRow(row.SrcProperties, row.SrcKey, row.SrcType)
		dstName := displayNameFromRow(row.DstProperties, row.DstKey, row.DstType)
		tripletText := buildTripletText(srcName, dstName, row.Type, row.Label, row.RelProperties)

		result, err := w.embeds.EmbedQueryWithUsage(ctx, tripletText)
		if err != nil {
			errors++
			w.log.Warn("sweep: failed to embed relationship",
				slog.String("id", row.ID),
				slog.String("triplet", tripletText),
				slog.String("error", err.Error()))
			continue
		}

		if result == nil || len(result.Embedding) == 0 {
			errors++
			w.log.Warn("sweep: embedding returned nil for relationship",
				slog.String("id", row.ID))
			continue
		}

		now := time.Now()
		_, err = w.db.NewRaw(`UPDATE kb.graph_relationships
			SET embedding = ?::vector, embedding_updated_at = ?
			WHERE id = ?`,
			vectorToString(result.Embedding), now, row.ID).Exec(ctx)
		if err != nil {
			errors++
			w.log.Warn("sweep: failed to update relationship embedding",
				slog.String("id", row.ID),
				slog.String("error", err.Error()))
			continue
		}

		embedded++

		// Record embedding usage
		if w.usage != nil && row.ProjectID != "" {
			orgID := w.orgCache.resolve(ctx, row.ProjectID)
			recordEmbeddingUsage(w.usage, row.ProjectID, orgID, result)
		}
	}
	return embedded, errors
}

// --- helpers (duplicated from backfill CLI to avoid import cycles) ---

// displayNameFromRow picks the best display name from properties/key/id.
func displayNameFromRow(propsJSON []byte, key *string, id string) string {
	if len(propsJSON) > 0 {
		var props map[string]any
		if err := json.Unmarshal(propsJSON, &props); err == nil {
			if name, ok := props["name"].(string); ok && name != "" {
				return name
			}
		}
	}
	if key != nil && *key != "" {
		return *key
	}
	return id
}

// buildTripletText creates a natural language triplet for embedding.
// Uses label if provided, otherwise humanizes the rel type.
// relPropsJSON optionally contains relationship properties; any meaningful
// string values are appended after the triplet to enrich the embedding signal.
func buildTripletText(srcName, dstName, relType string, label *string, relPropsJSON []byte) string {
	var predicate string
	if label != nil && *label != "" {
		predicate = *label
	} else {
		predicate = strings.ToLower(strings.ReplaceAll(relType, "_", " "))
	}
	base := fmt.Sprintf("%s %s %s", srcName, predicate, dstName)

	if len(relPropsJSON) == 0 {
		return base
	}

	var props map[string]any
	if err := json.Unmarshal(relPropsJSON, &props); err != nil || len(props) == 0 {
		return base
	}

	var extras []string
	for k, v := range props {
		lk := strings.ToLower(k)
		// Reuse the same skip list as graph objects — skip IDs, URLs, timestamps, etc.
		if skipEmbeddingKey[lk] {
			continue
		}
		s, ok := v.(string)
		if !ok || s == "" {
			continue
		}
		// Skip UUIDs and bare URLs.
		if len(s) == 36 && strings.Count(s, "-") == 4 {
			continue
		}
		if strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://") {
			continue
		}
		extras = append(extras, s)
	}

	if len(extras) == 0 {
		return base
	}
	return base + " " + strings.Join(extras, " ")
}

// --- metrics ---

func (w *EmbeddingSweepWorker) incrementSweepCount() {
	w.metricsMu.Lock()
	w.sweepCount++
	w.metricsMu.Unlock()
}

func (w *EmbeddingSweepWorker) addObjectsEnqueued(n int64) {
	w.metricsMu.Lock()
	w.objectsEnqueued += n
	w.metricsMu.Unlock()
}

func (w *EmbeddingSweepWorker) addRelationshipsEmbedded(n int64) {
	w.metricsMu.Lock()
	w.relationshipsEmbedded += n
	w.metricsMu.Unlock()
}

func (w *EmbeddingSweepWorker) addRelationshipErrors(n int64) {
	w.metricsMu.Lock()
	w.relationshipErrors += n
	w.metricsMu.Unlock()
}

// Metrics returns current sweep worker metrics.
func (w *EmbeddingSweepWorker) Metrics() EmbeddingSweepWorkerMetrics {
	w.metricsMu.RLock()
	defer w.metricsMu.RUnlock()
	return EmbeddingSweepWorkerMetrics{
		SweepCount:            w.sweepCount,
		ObjectsEnqueued:       w.objectsEnqueued,
		RelationshipsEmbedded: w.relationshipsEmbedded,
		RelationshipErrors:    w.relationshipErrors,
	}
}

// EmbeddingSweepWorkerMetrics contains sweep worker metrics.
type EmbeddingSweepWorkerMetrics struct {
	SweepCount            int64 `json:"sweep_count"`
	ObjectsEnqueued       int64 `json:"objects_enqueued"`
	RelationshipsEmbedded int64 `json:"relationships_embedded"`
	RelationshipErrors    int64 `json:"relationship_errors"`
}

// IsRunning returns whether the sweep worker is currently running.
func (w *EmbeddingSweepWorker) IsRunning() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.running
}
