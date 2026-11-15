package main

import (
	"context"
	"time"
)

// pendingInputTTL bounds how stale the Approvals nav badge may be. The count is
// an attention cue, not a ledger: a decision taken outside this gateway (an API
// client, another replica) can take this long to show. A decision handled here
// invalidates the entry at once.
const pendingInputTTL = 30 * time.Second

// pendingInputEntry is one cached human-input queue reading for a project.
type pendingInputEntry struct {
	count int
	at    time.Time
}

// cachedPendingInputCount returns how many items are waiting for a human on the
// Approvals page: pending tool approvals plus unanswered agent questions that
// are not already represented by one. pendingQuestions applies the same dedupe
// the page uses (a tool-confirmation question renders on its approval row), so
// the badge and the page cannot disagree about what counts as one item.
//
// The approval read is the server-filtered pending list, so the badge never
// pulls a project's whole approval history onto the page-render path.
//
// A failed approval fetch returns the error and the caller hides the badge. A
// failed question fetch does not: the approval count is still true, so the
// badge shows that and only the question contribution is lost.
func (s *Server) cachedPendingInputCount(ctx context.Context) (int, error) {
	projectID := s.currentProjectID(ctx)
	if projectID == "" {
		return 0, nil
	}

	s.pendingInputMu.Lock()
	entry, ok := s.pendingInputCache[projectID]
	s.pendingInputMu.Unlock()
	if ok && time.Since(entry.at) < pendingInputTTL {
		return entry.count, nil
	}

	approvals, err := s.memory.ListPendingToolApprovals(ctx)
	if err != nil {
		return 0, err
	}
	count := 0
	for _, a := range approvals {
		// The server filters, but keep the guard: a legacy or misbehaving
		// upstream that ignores ?decision would otherwise inflate the badge
		// with decided rows.
		if a.Decision == "pending" {
			count++
		}
	}
	if questions, qErr := s.memory.ListAgentQuestions(ctx); qErr != nil {
		captureError(qErr)
	} else {
		count += len(pendingQuestions(questions, approvals))
	}

	s.pendingInputMu.Lock()
	if s.pendingInputCache == nil {
		s.pendingInputCache = make(map[string]pendingInputEntry)
	}
	s.pendingInputCache[projectID] = pendingInputEntry{count: count, at: time.Now()}
	s.pendingInputMu.Unlock()
	return count, nil
}

// invalidatePendingInputCount drops the cached count after a decision, so the
// badge reflects an approve / reject / cancel at once instead of up to
// pendingInputTTL later.
func (s *Server) invalidatePendingInputCount(ctx context.Context) {
	projectID := s.currentProjectID(ctx)
	if projectID == "" {
		return
	}
	s.pendingInputMu.Lock()
	delete(s.pendingInputCache, projectID)
	s.pendingInputMu.Unlock()
}
