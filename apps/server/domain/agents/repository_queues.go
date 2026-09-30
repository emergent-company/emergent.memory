package agents

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// ErrQueueNotEmpty is returned when deleting a queue that still has pending or
// processing jobs.
var ErrQueueNotEmpty = errors.New("agent queue has pending or processing jobs")

// ErrQueueNotFound is returned when a queue does not exist for the project.
var ErrQueueNotFound = errors.New("agent queue not found")

// EnsureDefaultQueue creates the project's default queue if it does not exist.
func (r *Repository) EnsureDefaultQueue(ctx context.Context, projectID string) error {
	q := &AgentQueue{
		ProjectID:   projectID,
		Name:        DefaultQueueName,
		DisplayName: "Default",
		Description: "Default agent work queue",
		Concurrency: 5,
		Priority:    DefaultQueuePriority,
		Enabled:     true,
	}
	if _, err := r.db.NewInsert().Model(q).
		On("CONFLICT (project_id, name) DO NOTHING").
		Exec(ctx); err != nil {
		return fmt.Errorf("ensure default queue: %w", err)
	}
	return nil
}

// EnsureDefaultQueues creates a default queue for every project that lacks one.
// Idempotent; safe to run periodically from the worker supervisor so projects
// created after the migration still have a routable default queue.
func (r *Repository) EnsureDefaultQueues(ctx context.Context) error {
	if _, err := r.db.NewRaw(`
		INSERT INTO kb.agent_queues (project_id, name, display_name, description, concurrency, priority, enabled)
		SELECT p.id, 'default', 'Default', 'Default agent work queue', 5, 100, TRUE
		FROM kb.projects p
		ON CONFLICT (project_id, name) DO NOTHING`).Exec(ctx); err != nil {
		return fmt.Errorf("ensure default queues: %w", err)
	}
	return nil
}

// ListEnabledQueues returns all enabled queues across projects for the worker
// supervisor to size its per-queue worker sets.
func (r *Repository) ListEnabledQueues(ctx context.Context) ([]AgentQueue, error) {
	var queues []AgentQueue
	if err := r.db.NewSelect().Model(&queues).
		Where("aq.enabled = true").
		Scan(ctx); err != nil {
		return nil, fmt.Errorf("list enabled queues: %w", err)
	}
	return queues, nil
}

// ListQueues returns a project's queues with live pending/processing depth.
func (r *Repository) ListQueues(ctx context.Context, projectID string) ([]AgentQueue, error) {
	var queues []AgentQueue
	if err := r.db.NewSelect().Model(&queues).
		Where("aq.project_id = ?", projectID).
		OrderExpr("aq.name ASC").
		Scan(ctx); err != nil {
		return nil, fmt.Errorf("list agent queues: %w", err)
	}
	depths, err := r.queueDepths(ctx, projectID)
	if err != nil {
		return nil, err
	}
	for i := range queues {
		if d, ok := depths[queues[i].Name]; ok {
			queues[i].Pending = d.pending
			queues[i].Processing = d.processing
		}
	}
	return queues, nil
}

// GetQueue returns a single queue by project and name.
func (r *Repository) GetQueue(ctx context.Context, projectID, name string) (*AgentQueue, error) {
	q := new(AgentQueue)
	err := r.db.NewSelect().Model(q).
		Where("aq.project_id = ?", projectID).
		Where("aq.name = ?", name).
		Scan(ctx)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrQueueNotFound
		}
		return nil, fmt.Errorf("get agent queue: %w", err)
	}
	return q, nil
}

// UpsertQueue inserts or updates a queue by (project_id, name).
func (r *Repository) UpsertQueue(ctx context.Context, q *AgentQueue) error {
	q.UpdatedAt = time.Now()
	if _, err := r.db.NewInsert().Model(q).
		On(`CONFLICT (project_id, name) DO UPDATE SET
			display_name = EXCLUDED.display_name,
			description  = EXCLUDED.description,
			concurrency  = EXCLUDED.concurrency,
			priority     = EXCLUDED.priority,
			enabled      = EXCLUDED.enabled,
			updated_at   = now()`).
		Exec(ctx); err != nil {
		return fmt.Errorf("upsert agent queue: %w", err)
	}
	return nil
}

// UpdateQueue updates a queue's mutable fields.
func (r *Repository) UpdateQueue(ctx context.Context, q *AgentQueue) error {
	q.UpdatedAt = time.Now()
	res, err := r.db.NewUpdate().Model(q).
		Column("display_name", "description", "concurrency", "priority", "enabled", "updated_at").
		WherePK().
		Exec(ctx)
	if err != nil {
		return fmt.Errorf("update agent queue: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrQueueNotFound
	}
	return nil
}

// DeleteQueue removes a queue that has no pending or processing jobs.
func (r *Repository) DeleteQueue(ctx context.Context, projectID, name string) error {
	pending, processing, err := r.CountQueueJobs(ctx, projectID, name)
	if err != nil {
		return err
	}
	if pending+processing > 0 {
		return ErrQueueNotEmpty
	}
	res, err := r.db.NewDelete().Model((*AgentQueue)(nil)).
		Where("project_id = ?", projectID).
		Where("name = ?", name).
		Exec(ctx)
	if err != nil {
		return fmt.Errorf("delete agent queue: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrQueueNotFound
	}
	return nil
}

// CountQueueJobs returns the pending and processing job counts for a queue.
func (r *Repository) CountQueueJobs(ctx context.Context, projectID, name string) (pending, processing int, err error) {
	depths, err := r.queueDepths(ctx, projectID)
	if err != nil {
		return 0, 0, err
	}
	d := depths[name]
	return d.pending, d.processing, nil
}

type queueDepth struct {
	pending    int
	processing int
}

func (r *Repository) queueDepths(ctx context.Context, projectID string) (map[string]queueDepth, error) {
	type row struct {
		Queue      string `bun:"queue"`
		Pending    int    `bun:"pending"`
		Processing int    `bun:"processing"`
	}
	var rows []row
	if err := r.db.NewRaw(`
		SELECT arj.queue AS queue,
		       COUNT(*) FILTER (WHERE arj.status = 'pending')    AS pending,
		       COUNT(*) FILTER (WHERE arj.status = 'processing') AS processing
		FROM kb.agent_run_jobs arj
		JOIN kb.agent_runs ar ON ar.id = arj.run_id
		JOIN kb.agents a ON a.id = ar.agent_id
		WHERE a.project_id = ?
		  AND arj.status IN ('pending', 'processing')
		GROUP BY arj.queue`, projectID).Scan(ctx, &rows); err != nil {
		return nil, fmt.Errorf("queue depths: %w", err)
	}
	m := make(map[string]queueDepth, len(rows))
	for _, rw := range rows {
		m[rw.Queue] = queueDepth{pending: rw.Pending, processing: rw.Processing}
	}
	return m, nil
}

// resolveQueueAndPriority resolves the dispatch queue and priority for an agent,
// preferring an explicit override, then the runtime agent's config, then the
// definition's binding, then the default queue.
func (r *Repository) resolveQueueAndPriority(ctx context.Context, agentID, override string, priority int) (string, int) {
	queue := override
	if queue == "" {
		if agent, err := r.FindByID(ctx, agentID, nil); err == nil && agent != nil {
			if v, ok := agent.Config["queue"].(string); ok && v != "" {
				queue = v
			} else if def, _ := r.ResolveDefinitionForAgent(ctx, agent); def != nil && def.DefaultQueue != "" {
				queue = def.DefaultQueue
			}
		}
	}
	if queue == "" {
		queue = DefaultQueueName
	}
	if priority == 0 {
		priority = DefaultQueuePriority
	}
	return queue, priority
}
