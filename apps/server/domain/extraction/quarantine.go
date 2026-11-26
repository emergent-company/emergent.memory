package extraction

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/uptrace/bun"

	"github.com/emergent-company/emergent.memory/pkg/logger"
)

// QuarantineItem is one dropped extraction result: an object or relationship
// whose type is not in the installed schema (strict typing). It is recorded so
// a reviewer can see what a run leaked instead of losing it silently.
type QuarantineItem struct {
	Kind     string         `json:"kind"`      // "object" | "relationship"
	TypeName string         `json:"type_name"` // the (unknown) type
	Payload  map[string]any `json:"payload"`   // the raw entity/relationship
	Reason   string         `json:"reason"`    // "unknown_type"
}

// QuarantineEntry is a stored quarantine row returned to the admin surface.
type QuarantineEntry struct {
	ID         string         `json:"id"`
	ProjectID  string         `json:"project_id"`
	DocumentID *string        `json:"document_id,omitempty"`
	JobID      *string        `json:"job_id,omitempty"`
	Kind       string         `json:"kind"`
	TypeName   string         `json:"type_name"`
	Payload    map[string]any `json:"payload"`
	Reason     string         `json:"reason"`
	Status     string         `json:"status"`
	CreatedAt  time.Time      `json:"created_at"`
	ResolvedAt *time.Time     `json:"resolved_at,omitempty"`
}

// ListQuarantine returns open quarantine rows for a project, newest first, up
// to limit. status filters by row status ("" or "open" are equivalent; the
// admin surface reads open rows).
func (s *ObjectExtractionJobsService) ListQuarantine(ctx context.Context, projectID string, status string, limit int) ([]QuarantineEntry, error) {
	if limit <= 0 || limit > 1000 {
		limit = 100
	}
	q := s.db.NewSelect().
		Table("kb.extraction_quarantine").
		Where("project_id = ?", projectID).
		Order("created_at DESC").
		Limit(limit)
	if status != "" && status != "all" {
		q = q.Where("status = ?", status)
	}
	var rows []QuarantineEntry
	if err := q.Scan(ctx, &rows); err != nil {
		return nil, fmt.Errorf("list quarantine: %w", err)
	}
	return rows, nil
}

// DismissQuarantine marks the given quarantine rows resolved for a project so a
// reviewer can clear items they have looked at. It resolves only rows that are
// still open, and returns how many it changed.
func (s *ObjectExtractionJobsService) DismissQuarantine(ctx context.Context, projectID string, ids []string) (int, error) {
	if len(ids) == 0 {
		return 0, nil
	}
	res, err := s.db.NewUpdate().
		Table("kb.extraction_quarantine").
		Set("status = ?", "dismissed").
		Set("resolved_at = now()").
		Where("project_id = ?", projectID).
		Where("id IN (?)", bun.In(ids)).
		Where("status = 'open'").
		Exec(ctx)
	if err != nil {
		return 0, fmt.Errorf("dismiss quarantine: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("dismiss quarantine rows: %w", err)
	}
	return int(n), nil
}

// QuarantineDropped persists dropped extraction results to
// kb.extraction_quarantine. documentID/jobID are optional provenance (nil when
// unknown, e.g. a chunk-sourced job). It returns the number of rows written.
// A failure writes nothing: it is a single batched insert so the quarantine
// cannot be half-populated, and the worker treats it as best-effort.
func (s *ObjectExtractionJobsService) QuarantineDropped(
	ctx context.Context,
	projectID string,
	documentID, jobID *string,
	items []QuarantineItem,
) (int, error) {
	if len(items) == 0 {
		return 0, nil
	}
	for i := range items {
		if items[i].Reason == "" {
			items[i].Reason = "unknown_type"
		}
	}

	// One INSERT per item inside a single transaction is the simplest correct
	// batched write; the payload is marshalled per row so a malformed entity
	// cannot fail its siblings at the driver level.
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("begin quarantine tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	written := 0
	for _, item := range items {
		payload, err := json.Marshal(item.Payload)
		if err != nil {
			s.log.Warn("quarantine: dropping unmarshalable payload",
				slog.String("type", item.TypeName), logger.Error(err))
			continue
		}
		_, err = tx.NewRaw(`
			INSERT INTO kb.extraction_quarantine
				(project_id, document_id, job_id, kind, type_name, payload, reason)
			VALUES (?, ?, ?, ?, ?, ?::jsonb, ?)`,
			projectID, documentID, jobID, item.Kind, item.TypeName, string(payload), item.Reason,
		).Exec(ctx)
		if err != nil {
			return written, fmt.Errorf("insert quarantine row: %w", err)
		}
		written++
	}
	if err := tx.Commit(); err != nil {
		return written, fmt.Errorf("commit quarantine tx: %w", err)
	}
	return written, nil
}
