package journal

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/uptrace/bun"
)

// Repository handles DB access for the journal domain.
type Repository struct {
	db bun.IDB
}

// NewRepository creates a new Repository.
func NewRepository(db bun.IDB) *Repository {
	return &Repository{db: db}
}

// Insert inserts a new journal entry.
func (r *Repository) Insert(ctx context.Context, entry *JournalEntry) error {
	_, err := r.db.NewInsert().Model(entry).Exec(ctx)
	return err
}

// InsertNote inserts a new journal note.
func (r *Repository) InsertNote(ctx context.Context, note *JournalNote) error {
	_, err := r.db.NewInsert().Model(note).Exec(ctx)
	return err
}

// List returns journal entries for a project with optional since filter and pagination.
// Attached notes for each entry are loaded in a second query.
func (r *Repository) List(ctx context.Context, params ListParams) ([]*JournalEntry, int, error) {
	limit := params.Limit
	if limit <= 0 {
		limit = 100
	}
	page := params.Page
	if page < 1 {
		page = 1
	}
	offset := (page - 1) * limit

	q := r.db.NewSelect().
		TableExpr("kb.project_journal AS je").
		ColumnExpr("je.*").
		Where("je.project_id = ?", params.ProjectID).
		OrderExpr("je.created_at DESC").
		Limit(limit).
		Offset(offset)

	if params.BranchID != nil {
		q = q.Where("je.branch_id = ?", params.BranchID)
	} else {
		q = q.Where("je.branch_id IS NULL")
	}

	if params.Since != nil {
		q = q.Where("je.created_at >= ?", params.Since)
	}

	var entries []*JournalEntry
	count, err := q.ScanAndCount(ctx, &entries)
	if err != nil {
		return nil, 0, err
	}

	// Load attached notes for these entries in a single query.
	if len(entries) > 0 {
		ids := make([]uuid.UUID, len(entries))
		for i, e := range entries {
			ids[i] = e.ID
		}
		var notes []*JournalNote
		err = r.db.NewSelect().
			TableExpr("kb.project_journal_notes AS jn").
			ColumnExpr("jn.*").
			Where("jn.journal_id IN (?)", bun.In(ids)).
			OrderExpr("jn.created_at ASC").
			Scan(ctx, &notes)
		if err != nil {
			return nil, 0, err
		}
		notesByEntry := make(map[uuid.UUID][]*JournalNote)
		for _, n := range notes {
			if n.JournalID != nil {
				notesByEntry[*n.JournalID] = append(notesByEntry[*n.JournalID], n)
			}
		}
		for _, e := range entries {
			e.Notes = notesByEntry[e.ID]
		}
	}

	return entries, count, nil
}

// ListStandaloneNotes returns notes with no journal_id for a project, optionally filtered by branch.
func (r *Repository) ListStandaloneNotes(ctx context.Context, projectID uuid.UUID, branchID *uuid.UUID, since *time.Time, limit int) ([]*JournalNote, error) {
	if limit <= 0 {
		limit = 100
	}
	q := r.db.NewSelect().
		TableExpr("kb.project_journal_notes AS jn").
		ColumnExpr("jn.*").
		Where("jn.project_id = ?", projectID).
		Where("jn.journal_id IS NULL").
		OrderExpr("jn.created_at DESC").
		Limit(limit)
	if branchID != nil {
		q = q.Where("jn.branch_id = ?", branchID)
	} else {
		q = q.Where("jn.branch_id IS NULL")
	}
	if since != nil {
		q = q.Where("jn.created_at >= ?", since)
	}
	var notes []*JournalNote
	err := q.Scan(ctx, &notes)
	return notes, err
}
