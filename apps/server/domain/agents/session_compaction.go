package agents

// session_compaction.go — ancestry record for session compression.
//
// compressSession rewrites the ADK session (delete + create is the only way to
// shrink an event log), which makes the act of summarising invisible: nothing
// in the schema records that history was dropped, which events were summarised,
// or in what order. kb.agent_session_compactions is that chain. The durable
// per-run transcript (kb.agent_run_messages / kb.agent_run_tool_calls) keeps the
// full conversation independently of the working session, so the row is for
// lineage and operator forensics, not for restoring text.
//
// Writing is best-effort: a failed insert must never fail a run, because the
// session has already been rebuilt by the time the row is written.

import (
	"context"
	"database/sql"
	"time"

	"github.com/uptrace/bun"
)

// SessionCompaction is one compression event for one ADK session.
// Table: kb.agent_session_compactions
type SessionCompaction struct {
	bun.BaseModel `bun:"table:kb.agent_session_compactions,alias:asc"`

	ID             string    `bun:"id,pk,type:uuid,default:gen_random_uuid()" json:"id"`
	SessionID      string    `bun:"session_id,notnull" json:"sessionId"`
	Seq            int       `bun:"seq,notnull" json:"seq"`
	DroppedEvents  int       `bun:"dropped_events,notnull" json:"droppedEvents"`
	RetainedEvents int       `bun:"retained_events,notnull" json:"retainedEvents"`
	FirstDroppedID string    `bun:"first_dropped_event_id" json:"firstDroppedEventId,omitempty"`
	LastDroppedID  string    `bun:"last_dropped_event_id" json:"lastDroppedEventId,omitempty"`
	Summary        string    `bun:"summary,notnull" json:"summary"`
	SummaryTokens  int       `bun:"summary_tokens,notnull,default:0" json:"summaryTokens"`
	Model          string    `bun:"model,notnull,default:''" json:"model"`
	Trigger        string    `bun:"trigger,notnull,default:'load'" json:"trigger"`
	CreatedAt      time.Time `bun:"created_at,nullzero,notnull,default:current_timestamp" json:"createdAt"`
}

// Compaction triggers. A row records which path rebuilt the session so the two
// callers can be told apart in operator forensics.
const (
	// CompactionTriggerLoad is the pre-run path: the history in a resumed
	// session is trimmed (and optionally compressed) before the run starts.
	CompactionTriggerLoad = "load"
	// CompactionTriggerOverflow is the reactive path: a model call failed with
	// a context-window rejection mid-run, the oldest history was summarised,
	// and the step was retried once (#1138).
	CompactionTriggerOverflow = "overflow"
)

// RecordSessionCompaction appends one compression record, assigning the next
// seq for the session. It is nil-receiver safe and returns an error the caller
// is expected to log rather than propagate: the session has already been
// rebuilt when this runs, so a bookkeeping failure must not fail the run.
func (r *Repository) RecordSessionCompaction(ctx context.Context, row *SessionCompaction) error {
	if r == nil || row == nil || row.SessionID == "" {
		return nil
	}
	if row.Trigger == "" {
		row.Trigger = CompactionTriggerLoad
	}
	return r.db.RunInTx(ctx, &sql.TxOptions{}, func(ctx context.Context, tx bun.Tx) error {
		var maxSeq int
		if err := tx.NewSelect().
			ColumnExpr("COALESCE(MAX(seq), 0)").
			TableExpr("kb.agent_session_compactions").
			Where("session_id = ?", row.SessionID).
			Scan(ctx, &maxSeq); err != nil {
			return err
		}
		row.Seq = maxSeq + 1
		_, err := tx.NewInsert().Model(row).Returning("*").Exec(ctx)
		return err
	})
}
