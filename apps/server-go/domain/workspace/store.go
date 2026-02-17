package workspace

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
func (s *Store) Create(ctx context.Context, ws *AgentWorkspace) (*AgentWorkspace, error) {
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
func (s *Store) GetByID(ctx context.Context, id string) (*AgentWorkspace, error) {
	ws := new(AgentWorkspace)
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
func (s *Store) List(ctx context.Context, filters *ListFilters) ([]*AgentWorkspace, error) {
	var workspaces []*AgentWorkspace
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
func (s *Store) Update(ctx context.Context, ws *AgentWorkspace, fields ...string) (*AgentWorkspace, error) {
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
		Model((*AgentWorkspace)(nil)).
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
func (s *Store) ListPersistentMCPServers(ctx context.Context) ([]*AgentWorkspace, error) {
	var workspaces []*AgentWorkspace
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

// ListExpired returns ephemeral workspaces whose TTL has passed.
func (s *Store) ListExpired(ctx context.Context) ([]*AgentWorkspace, error) {
	var workspaces []*AgentWorkspace
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
		Model((*AgentWorkspace)(nil)).
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
		Model((*AgentWorkspace)(nil)).
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
		Model((*AgentWorkspace)(nil)).
		Set("last_used_at = ?", time.Now()).
		Where("id = ?", id)

	if extendTTL != nil {
		q = q.Set("expires_at = ?", *extendTTL)
	}

	_, err := q.Exec(ctx)
	return err
}

// GetBySessionID returns a workspace attached to the given agent session.
func (s *Store) GetBySessionID(ctx context.Context, sessionID string) (*AgentWorkspace, error) {
	ws := new(AgentWorkspace)
	err := s.db.NewSelect().
		Model(ws).
		Where("agent_session_id = ?", sessionID).
		Where("status NOT IN (?)", bun.In([]Status{StatusStopped, StatusError})).
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
func (s *Store) GetBySnapshotID(ctx context.Context, snapshotID string) ([]*AgentWorkspace, error) {
	var workspaces []*AgentWorkspace
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
