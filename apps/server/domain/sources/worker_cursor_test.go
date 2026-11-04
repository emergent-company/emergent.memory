package sources

import (
	"fmt"
	"slices"
	"testing"
	"time"
)

// windowCursor models the part of a chunked connector the worker depends on: a
// window index stored in the cursor, and a proposed next cursor that moves to
// the following window. A held cursor replays the same window, which is what
// makes a retry observable in these tests.
func windowCursor(index int) map[string]any { return map[string]any{"window": index} }

func storedWindow(cursor map[string]any) int {
	switch n := cursor["window"].(type) {
	case int:
		return n
	case int64:
		return int(n)
	case float64:
		return int(n)
	}
	return 0
}

// syncWindow runs one worker-side batch against a chunked connector: it
// enumerates the window the stored cursor points at, asks fail whether each item
// fails on this sync, and composes the cursor exactly as ProcessJob does.
func syncWindow(prev map[string]any, windows [][]string, sync int, now time.Time, fail func(id string, sync int) bool) (map[string]any, cursorDecision, []string) {
	index := storedWindow(prev)
	var items []string
	if index >= 0 && index < len(windows) {
		items = windows[index]
	}
	var failed []string
	for _, id := range items {
		if fail(id, sync) {
			failed = append(failed, id)
		}
	}
	returned := buildSourceCursor(prev, windowCursor(index+1), len(items), failed, now)
	return returned.cursor, returned, items
}

func attemptsOf(decision cursorDecision) map[string]int {
	if decision.attempts == nil {
		return map[string]int{}
	}
	return decision.attempts
}

// buildSourceCursor is the one place connector resume state and the worker's
// counters meet. Connector state may not advance past an item that failed to
// apply — that loses the item — but it also may not freeze forever behind an
// item that will never succeed (#2219, #2220).
func TestBuildSourceCursor(t *testing.T) {
	now := time.Date(2025, 3, 4, 10, 30, 0, 0, time.UTC)
	stored := map[string]any{
		"total_items":    float64(4),
		"synced_at":      "2025-03-03T09:00:00Z",
		"gmail_scope":    "in:inbox\x1f\x1f",
		"gmail_boundary": "2025-01-01",
	}
	proposed := map[string]any{
		"gmail_scope":    "in:inbox\x1f\x1f",
		"gmail_boundary": "2024-12-02",
	}

	t.Run("clean batch advances the connector cursor", func(t *testing.T) {
		got := buildSourceCursor(stored, proposed, 7, nil, now)
		if got.cursor["gmail_boundary"] != "2024-12-02" {
			t.Errorf("boundary = %v, want the proposed %q", got.cursor["gmail_boundary"], "2024-12-02")
		}
		if got.cursor["total_items"] != 7 {
			t.Errorf("total_items = %v, want 7", got.cursor["total_items"])
		}
		if got.cursor["synced_at"] != now.Format(time.RFC3339) {
			t.Errorf("synced_at = %v, want the batch time", got.cursor["synced_at"])
		}
		if got.cursor["gmail_scope"] != proposed["gmail_scope"] {
			t.Errorf("scope = %v, want it carried through", got.cursor["gmail_scope"])
		}
		if got.held {
			t.Error("held = true for a clean batch, want the resume point advanced")
		}
		if _, ok := got.cursor[cursorItemFailureAttemptsKey]; ok {
			t.Error("a clean batch recorded a failure budget")
		}
	})

	t.Run("a failure still within budget holds the connector cursor", func(t *testing.T) {
		got := buildSourceCursor(stored, proposed, 7, []string{"thread-1", "thread-2"}, now)
		if got.cursor["gmail_boundary"] != "2025-01-01" {
			t.Fatalf("boundary = %v, want the stored %q so the window is re-listed", got.cursor["gmail_boundary"], "2025-01-01")
		}
		if !got.held {
			t.Error("held = false, want a within-budget failure to keep the resume point")
		}
		if got.cursor["total_items"] != 7 || got.cursor["synced_at"] != now.Format(time.RFC3339) {
			t.Errorf("counters = %v/%v, want this batch's values", got.cursor["total_items"], got.cursor["synced_at"])
		}
		if got.cursor["gmail_scope"] != stored["gmail_scope"] {
			t.Errorf("scope = %v, want the stored one", got.cursor["gmail_scope"])
		}
		failures, ok := got.cursor[cursorItemFailureAttemptsKey].(map[string]int)
		if !ok {
			t.Fatalf("%s = %T, want the per-item budget stored with the held cursor",
				cursorItemFailureAttemptsKey, got.cursor[cursorItemFailureAttemptsKey])
		}
		if failures["thread-1"] != 1 || failures["thread-2"] != 1 {
			t.Errorf("attempts = %v, want one attempt for each failed item", failures)
		}
		if len(got.abandoned) != 0 {
			t.Errorf("abandoned = %v, want none before the budget is exhausted", got.abandoned)
		}
	})

	t.Run("a first-batch failure leaves no watermark", func(t *testing.T) {
		got := buildSourceCursor(nil, proposed, 7, []string{"thread-1"}, now)
		for _, key := range []string{"gmail_scope", "gmail_boundary"} {
			if _, ok := got.cursor[key]; ok {
				t.Errorf("%s = %v on the first failed batch; the next sync must restart, not resume", key, got.cursor[key])
			}
		}
		if got.cursor["total_items"] != 7 || got.cursor["synced_at"] != now.Format(time.RFC3339) {
			t.Errorf("counters = %v/%v", got.cursor["total_items"], got.cursor["synced_at"])
		}
		if attemptsOf(got)["thread-1"] != 1 {
			t.Errorf("attempts = %v, want the failure recorded so a retry counts", got.attempts)
		}
	})

	t.Run("a single-pass connector keeps the previous cursor shape", func(t *testing.T) {
		got := buildSourceCursor(stored, nil, 3, nil, now)
		if _, ok := got.cursor["gmail_boundary"]; ok {
			t.Error("connector keys appeared for a connector that returned no cursor")
		}
		if got.cursor["total_items"] != 3 || got.cursor["synced_at"] != now.Format(time.RFC3339) {
			t.Errorf("counters = %v/%v", got.cursor["total_items"], got.cursor["synced_at"])
		}
	})
}

// TestPermanentItemFailureEventuallyAdvances is the regression for #2220: one
// item that fails on every sync must not freeze the coverage watermark. The
// cursor advances once the item has exhausted its budget, and later windows are
// enumerated on the following sync.
func TestPermanentItemFailureEventuallyAdvances(t *testing.T) {
	now := time.Date(2025, 3, 4, 10, 30, 0, 0, time.UTC)
	windows := [][]string{
		{"broken", "healthy"},
		{"later-window"},
	}
	fail := func(id string, _ int) bool { return id == "broken" }

	var cursor map[string]any
	var afterBudget map[string]any
	enumerated := make([][]string, 0, maxItemFailureAttempts+1)
	abandonedOn := -1
	for sync := 1; sync <= maxItemFailureAttempts+1; sync++ {
		var decision cursorDecision
		var items []string
		cursor, decision, items = syncWindow(cursor, windows, sync, now, fail)
		enumerated = append(enumerated, items)
		if sync == maxItemFailureAttempts {
			afterBudget = cursor
		}

		if len(decision.abandoned) > 0 {
			if abandonedOn != -1 {
				t.Fatalf("abandoned twice, at syncs %d and %d", abandonedOn, sync)
			}
			abandonedOn = sync
			if !slices.Equal(decision.abandoned, []string{"broken"}) {
				t.Fatalf("abandoned = %v, want [broken]", decision.abandoned)
			}
		}
		if sync < maxItemFailureAttempts {
			if decision.cursor["window"] != nil {
				t.Fatalf("sync %d advanced the watermark to %v while the item was still retryable",
					sync, decision.cursor["window"])
			}
			if !decision.held {
				t.Fatalf("sync %d held = false, want the window re-listed", sync)
			}
			if attemptsOf(decision)["broken"] != sync {
				t.Fatalf("sync %d attempts = %v, want broken at %d", sync, decision.attempts, sync)
			}
		}
	}

	if abandonedOn != maxItemFailureAttempts {
		t.Fatalf("abandoned on sync %d, want %d", abandonedOn, maxItemFailureAttempts)
	}
	// The budget-exhausting sync must advance in the same batch that abandons:
	// otherwise a permanently broken item holds the watermark forever.
	if got := storedWindow(afterBudget); got != 1 {
		t.Fatalf("watermark window = %d, want 1 after the budget is exhausted", got)
	}
	if _, ok := afterBudget[cursorItemFailureAttemptsKey]; ok {
		t.Errorf("%s = %v, want the abandoned item dropped from the budget",
			cursorItemFailureAttemptsKey, afterBudget[cursorItemFailureAttemptsKey])
	}
	// The next sync must enumerate the following window, not replay the old one.
	if got := enumerated[maxItemFailureAttempts]; !slices.Equal(got, []string{"later-window"}) {
		t.Fatalf("sync %d enumerated %v, want the later window", maxItemFailureAttempts+1, got)
	}
}

// TestTransientItemFailureHoldsThenRetries covers the contract #2219 relies on:
// a failure that clears must hold the cursor exactly one sync, the same window
// must be re-listed, and the retry must clear the item's budget.
func TestTransientItemFailureHoldsThenRetries(t *testing.T) {
	now := time.Date(2025, 3, 4, 10, 30, 0, 0, time.UTC)
	windows := [][]string{
		{"throttled", "healthy"},
		{"later-window"},
	}
	failFirstSync := func(id string, sync int) bool {
		return id == "throttled" && sync == 1
	}

	var cursor map[string]any
	var first, second cursorDecision
	var firstItems, secondItems []string
	cursor, first, firstItems = syncWindow(cursor, windows, 1, now, failFirstSync)
	cursor, second, secondItems = syncWindow(cursor, windows, 2, now, failFirstSync)

	if !slices.Equal(firstItems, []string{"throttled", "healthy"}) {
		t.Fatalf("first sync enumerated %v, want the first window", firstItems)
	}
	if !first.held || first.cursor["window"] != nil {
		t.Fatalf("first sync held = %v, window = %v; want the cursor held so the item is retried",
			first.held, first.cursor["window"])
	}
	if attemptsOf(first)["throttled"] != 1 {
		t.Errorf("first sync attempts = %v, want throttled recorded", first.attempts)
	}

	// The held cursor must make the connector replay the same window, so the
	// failed item is fetched again rather than skipped as covered.
	if !slices.Equal(secondItems, []string{"throttled", "healthy"}) {
		t.Fatalf("second sync enumerated %v, want the same window re-listed", secondItems)
	}
	if second.held {
		t.Error("second sync held = true, want the recovery to advance")
	}
	if got := storedWindow(cursor); got != 1 {
		t.Errorf("window = %d after recovery, want 1", got)
	}
	if _, ok := cursor[cursorItemFailureAttemptsKey]; ok {
		t.Errorf("%s = %v, want a successful retry to clear the budget",
			cursorItemFailureAttemptsKey, cursor[cursorItemFailureAttemptsKey])
	}
	if len(second.abandoned) != 0 {
		t.Errorf("abandoned = %v, want none for a transient failure", second.abandoned)
	}

	// And the sync after that must move on rather than replaying the window.
	var third cursorDecision
	var thirdItems []string
	cursor, third, thirdItems = syncWindow(cursor, windows, 3, now, failFirstSync)
	if !slices.Equal(thirdItems, []string{"later-window"}) {
		t.Fatalf("third sync enumerated %v, want the later window", thirdItems)
	}
	if third.held {
		t.Error("third sync held = true, want it to keep advancing")
	}
	if got := storedWindow(cursor); got != 2 {
		t.Errorf("window = %d, want 2", got)
	}
}

// TestMixedBatchMakesPartialProgress covers the interaction that makes the
// budget safe: a permanently broken item must not drag a retryable one out of
// the window, and the retryable item must not keep the permanently broken one
// from eventually being abandoned.
func TestMixedBatchMakesPartialProgress(t *testing.T) {
	now := time.Date(2025, 3, 4, 10, 30, 0, 0, time.UTC)
	windows := [][]string{
		{"broken", "flaky", "healthy"},
		{"later-window"},
	}
	fail := func(id string, sync int) bool {
		switch id {
		case "broken":
			return true
		case "flaky":
			return sync == 1
		}
		return false
	}

	var cursor map[string]any
	enumerated := make([][]string, 0, maxItemFailureAttempts+1)
	for sync := 1; sync <= maxItemFailureAttempts+1; sync++ {
		var decision cursorDecision
		var items []string
		cursor, decision, items = syncWindow(cursor, windows, sync, now, fail)
		enumerated = append(enumerated, items)

		if sync <= maxItemFailureAttempts && slices.Contains(items, "later-window") {
			t.Fatalf("sync %d skipped ahead before the broken item exhausted its budget", sync)
		}

		switch sync {
		case 1:
			if !decision.held {
				t.Fatal("sync 1 held = false, want the retryable failures to hold the cursor")
			}
			if got := attemptsOf(decision); got["broken"] != 1 || got["flaky"] != 1 {
				t.Fatalf("sync 1 attempts = %v, want one attempt for each failed item", got)
			}
			if len(decision.abandoned) != 0 {
				t.Fatalf("sync 1 abandoned = %v, want none", decision.abandoned)
			}
		case 2:
			// The transient item was re-listed with the window and succeeded,
			// so only the permanent failure may remain in the budget.
			if !slices.Contains(items, "flaky") {
				t.Fatalf("sync 2 enumerated %v, want the transient item retried", items)
			}
			if got := attemptsOf(decision); got["broken"] != 2 || got["flaky"] != 0 {
				t.Fatalf("sync 2 attempts = %v, want broken at 2 and flaky cleared", got)
			}
			if !decision.held {
				t.Fatal("sync 2 held = false, want the still-retryable broken item to hold")
			}
		case maxItemFailureAttempts:
			if !slices.Equal(decision.abandoned, []string{"broken"}) {
				t.Fatalf("sync %d abandoned = %v, want [broken]", sync, decision.abandoned)
			}
			if got := storedWindow(decision.cursor); got != 1 {
				t.Fatalf("sync %d window = %d, want the partial batch to advance once every failure is accounted for",
					sync, got)
			}
		}
	}

	// The transient item was fetched again on the very next sync and then
	// dropped from the budget; the permanent one was retried for every sync the
	// budget allows and no longer keeps the watermark from moving.
	if got := enumerated[maxItemFailureAttempts]; !slices.Equal(got, []string{"later-window"}) {
		t.Errorf("sync %d enumerated %v, want the later window", maxItemFailureAttempts+1, got)
	}
	if _, ok := cursor[cursorItemFailureAttemptsKey]; ok {
		t.Errorf("%s = %v, want nothing carried past the abandoned window",
			cursorItemFailureAttemptsKey, cursor[cursorItemFailureAttemptsKey])
	}
}

// TestBuildSourceCursor_UntrackablyLargeFailureHoldsAndStaysBounded pins the
// growth guard: a batch with more failures than the budget can track is a
// source-wide failure, so the cursor holds and the stored budget is not
// replaced with one entry per failed item.
func TestBuildSourceCursor_UntrackablyLargeFailureHoldsAndStaysBounded(t *testing.T) {
	now := time.Date(2025, 3, 4, 10, 30, 0, 0, time.UTC)
	stored := map[string]any{
		"window":                     float64(4),
		cursorItemFailureAttemptsKey: map[string]any{"known-bad": float64(2)},
	}
	failed := make([]string, maxTrackedItemFailures+1)
	for i := range failed {
		failed[i] = fmt.Sprintf("item-%d", i)
	}

	got := buildSourceCursor(stored, windowCursor(5), len(failed), failed, now)
	if !got.held {
		t.Fatal("held = false, want a wholesale failure to keep the cursor")
	}
	if got := storedWindow(got.cursor); got != 4 {
		t.Errorf("window = %d, want the stored 4", got)
	}
	if len(got.attempts) != 1 || got.attempts["known-bad"] != 2 {
		t.Errorf("attempts = %v, want the previously stored budget preserved and nothing added", got.attempts)
	}
	if len(got.abandoned) != 0 {
		t.Errorf("abandoned = %v, want none for an untrackable wholesale failure", got.abandoned)
	}
}

// TestParseItemFailureAttempts_DecodesStoredCursor covers the jsonb round trip:
// values arrive as float64 after a decode, counts are clamped to the budget, and
// junk entries are ignored rather than corrupting the policy.
func TestParseItemFailureAttempts_DecodesStoredCursor(t *testing.T) {
	prev := map[string]any{
		cursorItemFailureAttemptsKey: map[string]any{
			"decoded":  float64(2),
			"clamped":  float64(99),
			"zero":     float64(0),
			"negative": float64(-1),
			"junk":     "two",
			"":         float64(1),
		},
	}
	got := parseItemFailureAttempts(prev)
	want := itemFailureAttempts{"decoded": 2, "clamped": maxItemFailureAttempts}
	if len(got) != len(want) {
		t.Fatalf("attempts = %v, want %v", got, want)
	}
	for id, n := range want {
		if got[id] != n {
			t.Errorf("attempts[%q] = %d, want %d", id, got[id], n)
		}
	}
	// A cursor built in-process stores ints; it must decode identically.
	inProcess := map[string]any{cursorItemFailureAttemptsKey: map[string]int{"decoded": 2}}
	if got := parseItemFailureAttempts(inProcess); got["decoded"] != 2 {
		t.Errorf("in-process attempts = %v, want decoded at 2", got)
	}
}
