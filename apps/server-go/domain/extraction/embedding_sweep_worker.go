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

	"github.com/emergent-company/emergent/pkg/logger"
)

// EmbeddingSweepConfig contains configuration for the embedding sweep worker.
type EmbeddingSweepConfig struct {
	// SweepIntervalSec is the interval between sweeps in seconds (default: 60)
	SweepIntervalSec int
	// BatchSize is the number of items to process per sweep (default: 50)
	BatchSize int
}

// DefaultEmbeddingSweepConfig returns the default sweep configuration.
func DefaultEmbeddingSweepConfig() *EmbeddingSweepConfig {
	return &EmbeddingSweepConfig{
		SweepIntervalSec: 60,
		BatchSize:        50,
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

	stopCh    chan struct{}
	stoppedCh chan struct{}
	running   bool
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
) *EmbeddingSweepWorker {
	return &EmbeddingSweepWorker{
		jobs:   jobs,
		embeds: embeds,
		db:     db,
		cfg:    cfg,
		log:    log.With(logger.Scope("embedding.sweep")),
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

// sweep runs one sweep cycle: objects first, then relationships.
func (w *EmbeddingSweepWorker) sweep(ctx context.Context) {
	select {
	case <-w.stopCh:
		return
	case <-ctx.Done():
		return
	default:
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
func (w *EmbeddingSweepWorker) sweepObjects(ctx context.Context) int {
	// Find objects missing embeddings that don't have pending/processing jobs
	var objectIDs []string
	err := w.db.NewRaw(`
		SELECT o.id::text
		FROM kb.graph_objects o
		WHERE o.embedding_v2 IS NULL
		  AND o.deleted_at IS NULL
		  AND NOT EXISTS (
		    SELECT 1 FROM kb.graph_embedding_jobs j
		    WHERE j.object_id = o.id
		      AND j.status IN ('pending', 'processing')
		  )
		ORDER BY o.created_at ASC
		LIMIT ?`, w.cfg.BatchSize).Scan(ctx, &objectIDs)
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

// relationshipSweepRow holds data needed to generate a relationship embedding.
type relationshipSweepRow struct {
	ID            string  `bun:"id"`
	Type          string  `bun:"type"`
	SrcProperties []byte  `bun:"src_properties"`
	SrcKey        *string `bun:"src_key"`
	SrcID         string  `bun:"src_id"`
	DstProperties []byte  `bun:"dst_properties"`
	DstKey        *string `bun:"dst_key"`
	DstID         string  `bun:"dst_id"`
}

// sweepRelationships finds relationships with NULL embedding and generates
// embeddings for them directly (no job queue).
func (w *EmbeddingSweepWorker) sweepRelationships(ctx context.Context) (embedded int, errors int) {
	var rows []relationshipSweepRow
	err := w.db.NewRaw(`
		SELECT r.id::text, r.type,
		       src.properties AS src_properties, src.key AS src_key, src.id::text AS src_id,
		       dst.properties AS dst_properties, dst.key AS dst_key, dst.id::text AS dst_id
		FROM kb.graph_relationships r
		JOIN kb.graph_objects src ON src.id = r.src_id
		JOIN kb.graph_objects dst ON dst.id = r.dst_id
		WHERE r.embedding IS NULL
		  AND r.deleted_at IS NULL
		  AND src.deleted_at IS NULL
		  AND dst.deleted_at IS NULL
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

	for _, row := range rows {
		select {
		case <-w.stopCh:
			return embedded, errors
		case <-ctx.Done():
			return embedded, errors
		default:
		}

		srcName := displayNameFromRow(row.SrcProperties, row.SrcKey, row.SrcID)
		dstName := displayNameFromRow(row.DstProperties, row.DstKey, row.DstID)
		tripletText := buildTripletText(srcName, dstName, row.Type)

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
	}

	w.addRelationshipsEmbedded(int64(embedded))
	w.addRelationshipErrors(int64(errors))
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
func buildTripletText(srcName, dstName, relType string) string {
	humanized := strings.ToLower(strings.ReplaceAll(relType, "_", " "))
	return fmt.Sprintf("%s %s %s", srcName, humanized, dstName)
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
