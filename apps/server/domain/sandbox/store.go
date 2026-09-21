package sandbox

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/uptrace/bun"
)

// Store handles database operations for agent workspaces.
type Store struct {
	db bun.IDB
}

// NewStore creates a new workspace store.
func NewStore(db bun.IDB) *Store {
	return &Store{db: db}
}

// Create inserts a new agent workspace record.
func (s *Store) Create(ctx context.Context, ws *AgentSandbox) (*AgentSandbox, error) {
	_, err := s.db.NewInsert().
		Model(ws).
		Returning("*").
		Exec(ctx)
	if err != nil {
		return nil, err
	}
	return ws, nil
}

// GetByID returns a workspace by ID.
func (s *Store) GetByID(ctx context.Context, id string) (*AgentSandbox, error) {
	ws := new(AgentSandbox)
	err := s.db.NewSelect().
		Model(ws).
		Where("id = ?", id).
		Scan(ctx)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return ws, nil
}

// List returns workspaces matching optional filters.
func (s *Store) List(ctx context.Context, filters *ListFilters) ([]*AgentSandbox, error) {
	var workspaces []*AgentSandbox
	q := s.db.NewSelect().
		Model(&workspaces).
		Order("created_at DESC")

	if filters != nil {
		if filters.ContainerType != "" {
			q = q.Where("container_type = ?", filters.ContainerType)
		}
		if filters.Provider != "" {
			q = q.Where("provider = ?", filters.Provider)
		}
		if filters.Status != "" {
			q = q.Where("status = ?", filters.Status)
		}
		if filters.AgentSessionID != "" {
			q = q.Where("agent_session_id = ?", filters.AgentSessionID)
		}
		if filters.Limit > 0 {
			q = q.Limit(filters.Limit)
		}
		if filters.Offset > 0 {
			q = q.Offset(filters.Offset)
		}
	}

	err := q.Scan(ctx)
	if err != nil {
		return nil, err
	}
	return workspaces, nil
}

// Update updates selected fields on a workspace by ID.
func (s *Store) Update(ctx context.Context, ws *AgentSandbox, fields ...string) (*AgentSandbox, error) {
	q := s.db.NewUpdate().
		Model(ws).
		Where("id = ?", ws.ID).
		Returning("*")

	if len(fields) > 0 {
		q = q.Column(fields...)
	}

	_, err := q.Exec(ctx)
	if err != nil {
		return nil, err
	}
	if ws.ID == "" {
		return nil, nil
	}
	return ws, nil
}

// Delete removes a workspace record by ID.
func (s *Store) Delete(ctx context.Context, id string) (bool, error) {
	result, err := s.db.NewDelete().
		Model((*AgentSandbox)(nil)).
		Where("id = ?", id).
		Exec(ctx)
	if err != nil {
		return false, err
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return false, err
	}
	return rowsAffected > 0, nil
}

// ListPersistentMCPServers returns all persistent MCP servers (for auto-start on boot).
func (s *Store) ListPersistentMCPServers(ctx context.Context) ([]*AgentSandbox, error) {
	var workspaces []*AgentSandbox
	err := s.db.NewSelect().
		Model(&workspaces).
		Where("container_type = ?", ContainerTypeMCPServer).
		Where("lifecycle = ?", LifecyclePersistent).
		Order("created_at ASC").
		Scan(ctx)
	if err != nil {
		return nil, err
	}
	return workspaces, nil
}

// ListActive returns all workspaces whose status is neither stopped nor errored.
// Used by reconciliation to protect containers still referenced by live work.
func (s *Store) ListActive(ctx context.Context) ([]*AgentSandbox, error) {
	var workspaces []*AgentSandbox
	err := s.db.NewSelect().
		Model(&workspaces).
		Where("status NOT IN (?)", bun.In([]Status{StatusStopped, StatusError})).
		Scan(ctx)
	if err != nil {
		return nil, err
	}
	return workspaces, nil
}

// GetIdlePersistentMCPServer re-reads a persistent MCP server row and returns
// it only if it is still eligible for idle reclamation: present, a persistent
// MCP server, not in an in-flight lifecycle state (creating/stopping), and not
// used since idleBefore (last_used_at falls back to creation time, so a server
// never called since creation is judged by its creation time). It returns
// (nil, nil) when the row vanished, was touched inside the window, or entered
// an in-flight state — closing the window between the candidate SELECT and the
// reclaim's StopRuntime/destroy.
func (s *Store) GetIdlePersistentMCPServer(ctx context.Context, id string, idleBefore time.Time) (*AgentSandbox, error) {
	ws := new(AgentSandbox)
	err := s.db.NewSelect().
		Model(ws).
		Where("id = ?", id).
		Where("container_type = ?", ContainerTypeMCPServer).
		Where("lifecycle = ?", LifecyclePersistent).
		Where("status NOT IN (?)", bun.In([]Status{StatusCreating, StatusStopping})).
		Where("COALESCE(last_used_at, created_at) < ?", idleBefore).
		Scan(ctx)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return ws, nil
}

// ListOrphanedSandboxes returns non-stopped agent-sandbox rows whose owning run
// (agent_session_id) is no longer live — the run's status is not one of
// liveRunStatuses. Rows with no linked run are not returned: without a run we
// cannot distinguish an orphan from an unlinked workspace.
//
// The caller transitions the returned rows out of their non-stopped state (see
// agents.recoverOrphanedSandboxes). This store does not destroy containers
// itself; container and volume reclamation for the transitioned rows is
// performed by the label-based orphan reconciler owned by
// fix-warm-pool-orphan-reaping.
func (s *Store) ListOrphanedSandboxes(ctx context.Context, liveRunStatuses []string) ([]*AgentSandbox, error) {
	var workspaces []*AgentSandbox
	err := s.db.NewSelect().
		Model(&workspaces).
		Where("container_type = ?", ContainerTypeAgentSandbox).
		Where("status NOT IN (?)", bun.In([]Status{StatusStopped, StatusError})).
		Where("agent_session_id IS NOT NULL").
		Where("agent_session_id IN (SELECT id FROM kb.agent_runs WHERE status NOT IN (?))", bun.In(liveRunStatuses)).
		Order("created_at ASC").Scan(ctx)
	if err != nil {
		return nil, err
	}
	return workspaces, nil
}

// ListExpired returns ephemeral workspaces whose TTL has passed.
func (s *Store) ListExpired(ctx context.Context) ([]*AgentSandbox, error) {
	var workspaces []*AgentSandbox
	err := s.db.NewSelect().
		Model(&workspaces).
		Where("expires_at IS NOT NULL").
		Where("expires_at < ?", time.Now()).
		Where("status != ?", StatusStopped).
		Order("expires_at ASC").
		Scan(ctx)
	if err != nil {
		return nil, err
	}
	return workspaces, nil
}

// CountByStatus returns the number of workspaces with the given status.
func (s *Store) CountByStatus(ctx context.Context, status Status) (int, error) {
	count, err := s.db.NewSelect().
		Model((*AgentSandbox)(nil)).
		Where("status = ?", status).
		Count(ctx)
	if err != nil {
		return 0, err
	}
	return count, nil
}

// CountActive returns the number of non-stopped workspaces (for concurrency limiting).
func (s *Store) CountActive(ctx context.Context) (int, error) {
	count, err := s.db.NewSelect().
		Model((*AgentSandbox)(nil)).
		Where("status NOT IN (?)", bun.In([]Status{StatusStopped, StatusError})).
		Count(ctx)
	if err != nil {
		return 0, err
	}
	return count, nil
}

// TouchLastUsed updates last_used_at and optionally extends expires_at.
func (s *Store) TouchLastUsed(ctx context.Context, id string, extendTTL *time.Time) error {
	q := s.db.NewUpdate().
		Model((*AgentSandbox)(nil)).
		Set("last_used_at = ?", time.Now()).
		Where("id = ?", id)

	if extendTTL != nil {
		q = q.Set("expires_at = ?", *extendTTL)
	}

	_, err := q.Exec(ctx)
	return err
}

// GetBySessionID returns a workspace attached to the given agent session.
func (s *Store) GetBySessionID(ctx context.Context, sessionID string) (*AgentSandbox, error) {
	ws := new(AgentSandbox)
	err := s.db.NewSelect().
		Model(ws).
		Where("agent_session_id = ?", sessionID).
		Where("status NOT IN (?)", bun.In([]Status{StatusStopped, StatusError})).
		Order("created_at DESC").
		Limit(1).
		Scan(ctx)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return ws, nil
}

// GetBySnapshotID returns workspaces that were created from the given snapshot.
func (s *Store) GetBySnapshotID(ctx context.Context, snapshotID string) ([]*AgentSandbox, error) {
	var workspaces []*AgentSandbox
	err := s.db.NewSelect().
		Model(&workspaces).
		Where("snapshot_id = ?", snapshotID).
		Order("created_at DESC").
		Scan(ctx)
	if err != nil {
		return nil, err
	}
	return workspaces, nil
}

// ListFilters holds optional filters for listing workspaces.
type ListFilters struct {
	ContainerType  ContainerType `json:"container_type,omitempty"`
	Provider       ProviderType  `json:"provider,omitempty"`
	Status         Status        `json:"status,omitempty"`
	AgentSessionID string        `json:"agent_session_id,omitempty"`
	Limit          int           `json:"limit,omitempty"`
	Offset         int           `json:"offset,omitempty"`
}
