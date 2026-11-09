package feedback

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/emergent-company/emergent.memory/pkg/logger"
)

const (
	// claimTTL bounds how long a pending claim is trusted. The GitHub request
	// has a 20 second timeout; 2 minutes leaves generous room for a slow call
	// and database work while keeping a crashed process from blocking a user
	// for the full dedupe window. A pending claim older than this is treated as
	// abandoned and may be taken over.
	claimTTL = 2 * time.Minute

	// claimAttempts bounds the acquire loop. A zero-row INSERT means another
	// caller holds the pair; we read it to answer duplicate or in-progress.
	// Only a row released between the two statements makes us retry.
	claimAttempts = 3
)

// claimDecision is the result of trying to claim one (identity, dedupe_key)
// pair. Exactly one of the three outcomes applies.
type claimDecision struct {
	state      claimState
	token      string
	duplicate  *Result
	retryAfter time.Duration
}

type claimState int

const (
	claimAcquired claimState = iota
	claimDuplicate
	claimInProgress
)

// claimRow is one row of kb.feedback_dedupe_claims.
type claimRow struct {
	State       string    `bun:"state"`
	ClaimToken  string    `bun:"claim_token"`
	IssueNumber *int      `bun:"issue_number"`
	IssueURL    *string   `bun:"issue_url"`
	ClaimedAt   time.Time `bun:"claimed_at"`
}

// acquireClaimSQL is the arbiter for duplicate suppression: one statement whose
// success (a returned row) means this caller owns the pair. It inserts a
// pending claim, or takes over an existing row when that offer is stale:
//
//   - a pending row older than claimTTL belongs to a process that is assumed to
//     have crashed, so the claim is rotated in place;
//   - a filed row older than dedupeWindow is outside the suppression window, so
//     the same content may be filed again.
//
// The WHERE clause makes a fresh pending or filed row untouchable, so the
// INSERT returns no row and the caller reads the row to answer "duplicate" or
// "in progress". Taking over updates the SAME row (rotate, not accumulate) and
// replaces the claim token, so the stale owner cannot later complete or release
// the new owner's claim.
const acquireClaimSQL = `
INSERT INTO kb.feedback_dedupe_claims AS c
    (owner_identity, dedupe_key, state, claim_token, issue_number, issue_url, claimed_at, updated_at)
VALUES (?, ?, 'pending', ?, NULL, NULL, ?, ?)
ON CONFLICT (owner_identity, dedupe_key) DO UPDATE
SET state        = 'pending',
    claim_token  = EXCLUDED.claim_token,
    issue_number = NULL,
    issue_url    = NULL,
    claimed_at   = EXCLUDED.claimed_at,
    updated_at   = EXCLUDED.updated_at
WHERE (c.state = 'pending' AND c.claimed_at <= ?)
   OR (c.state = 'filed' AND c.claimed_at <= ?)
RETURNING state, claim_token, issue_number, issue_url, claimed_at`

const readClaimSQL = `
SELECT state, claim_token, issue_number, issue_url, claimed_at
FROM kb.feedback_dedupe_claims
WHERE owner_identity = ? AND dedupe_key = ?`

const markFiledSQL = `
UPDATE kb.feedback_dedupe_claims
SET state = 'filed', issue_number = ?, issue_url = ?, updated_at = ?
WHERE owner_identity = ? AND dedupe_key = ? AND claim_token = ? AND state = 'pending'`

const releaseClaimSQL = `
DELETE FROM kb.feedback_dedupe_claims
WHERE owner_identity = ? AND dedupe_key = ? AND claim_token = ? AND state = 'pending'`

// acquireClaim atomically claims (identity, dedupeKey) before the GitHub call.
// A second concurrent caller never files: it sees either the issued result, or
// this claim as pending and is told to retry shortly.
func (s *Service) acquireClaim(ctx context.Context, identity, dedupeKey string) (*claimDecision, error) {
	now := s.now()

	for range claimAttempts {
		token := uuid.NewString()
		var row claimRow
		err := s.db.NewRaw(acquireClaimSQL,
			identity, dedupeKey, token, now, now,
			now.Add(-claimTTL), now.Add(-dedupeWindow),
		).Scan(ctx, &row)
		if err == nil {
			return &claimDecision{state: claimAcquired, token: token}, nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("claim feedback dedupe key: %w", err)
		}

		// The pair is held by someone else (or was released between the two
		// statements). The INSERT above already decided that; this read only
		// renders the answer.
		existing, err := s.readClaim(ctx, identity, dedupeKey)
		if err != nil {
			return nil, err
		}
		if existing == nil {
			continue
		}

		switch existing.State {
		case "filed":
			if existing.IssueNumber != nil && existing.IssueURL != nil &&
				now.Sub(existing.ClaimedAt) < dedupeWindow {
				return &claimDecision{
					state: claimDuplicate,
					duplicate: &Result{
						IssueNumber: *existing.IssueNumber,
						IssueURL:    *existing.IssueURL,
						Duplicate:   true,
					},
				}, nil
			}
			// Stale filed row: loop and let the INSERT take it over.
			continue
		case "pending":
			if now.Sub(existing.ClaimedAt) >= claimTTL {
				continue
			}
			retry := claimTTL - now.Sub(existing.ClaimedAt)
			if retry < time.Second {
				retry = time.Second
			}
			return &claimDecision{state: claimInProgress, retryAfter: retry}, nil
		default:
			return nil, fmt.Errorf("feedback: unexpected dedupe claim state %q", existing.State)
		}
	}

	return nil, fmt.Errorf("feedback: dedupe claim for this submission is contended; retry shortly")
}

func (s *Service) readClaim(ctx context.Context, identity, dedupeKey string) (*claimRow, error) {
	var row claimRow
	err := s.db.NewRaw(readClaimSQL, identity, dedupeKey).Scan(ctx, &row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read feedback dedupe claim: %w", err)
	}
	return &row, nil
}

// markClaimFiled records the created issue so a later retry is answered with
// the same issue. The token guard means a claim that was taken over after its
// TTL is not completed by its stale owner.
func (s *Service) markClaimFiled(ctx context.Context, identity, dedupeKey, token string, issue *createdIssue) error {
	res, err := s.db.NewRaw(markFiledSQL,
		issue.Number, issue.HTMLURL, s.now(),
		identity, dedupeKey, token,
	).Exec(ctx)
	if err != nil {
		return fmt.Errorf("record filed feedback claim: %w", err)
	}
	if n, err := res.RowsAffected(); err == nil && n == 0 {
		// The claim expired and another caller took it over while this call was
		// in flight. The issue exists and is returned to this caller, but the
		// dedupe row now belongs to the new owner; do not overwrite it.
		s.log.Warn("feedback: dedupe claim was taken over before the issue was recorded",
			slog.String("identity", identity))
	}
	return nil
}

// releaseClaim gives the pair back after a failed GitHub call so a retry can
// file. Only the owner's pending claim is removed; a claim taken over after the
// TTL is left to its new owner.
func (s *Service) releaseClaim(ctx context.Context, identity, dedupeKey, token string) {
	if _, err := s.db.NewRaw(releaseClaimSQL, identity, dedupeKey, token).Exec(ctx); err != nil {
		s.log.Warn("feedback: could not release dedupe claim", logger.Error(err))
	}
}
