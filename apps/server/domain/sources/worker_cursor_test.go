package sources

import (
	"testing"
	"time"
)

// buildSourceCursor is the one place connector resume state and the worker's
// counters meet, and the failure case is the dangerous one: a connector cursor
// that advances past an item that failed to apply loses that item for good, so
// the row must keep the resume point it already had.
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
		got := buildSourceCursor(stored, proposed, 7, 0, now)
		if got["gmail_boundary"] != "2024-12-02" {
			t.Errorf("boundary = %v, want the proposed %q", got["gmail_boundary"], "2024-12-02")
		}
		if got["total_items"] != 7 {
			t.Errorf("total_items = %v, want 7", got["total_items"])
		}
		if got["synced_at"] != now.Format(time.RFC3339) {
			t.Errorf("synced_at = %v, want the batch time", got["synced_at"])
		}
		if got["gmail_scope"] != proposed["gmail_scope"] {
			t.Errorf("scope = %v, want it carried through", got["gmail_scope"])
		}
	})

	t.Run("failed items hold the connector cursor", func(t *testing.T) {
		got := buildSourceCursor(stored, proposed, 7, 2, now)
		if got["gmail_boundary"] != "2025-01-01" {
			t.Fatalf("boundary = %v, want the stored %q so the window is re-listed", got["gmail_boundary"], "2025-01-01")
		}
		if got["total_items"] != 7 || got["synced_at"] != now.Format(time.RFC3339) {
			t.Errorf("counters = %v/%v, want this batch's values", got["total_items"], got["synced_at"])
		}
		if got["gmail_scope"] != stored["gmail_scope"] {
			t.Errorf("scope = %v, want the stored one", got["gmail_scope"])
		}
	})

	t.Run("a first-batch failure leaves no watermark", func(t *testing.T) {
		got := buildSourceCursor(nil, proposed, 7, 1, now)
		for _, key := range []string{"gmail_scope", "gmail_boundary"} {
			if _, ok := got[key]; ok {
				t.Errorf("%s = %v on the first failed batch; the next sync must restart, not resume", key, got[key])
			}
		}
		if got["total_items"] != 7 || got["synced_at"] != now.Format(time.RFC3339) {
			t.Errorf("counters = %v/%v", got["total_items"], got["synced_at"])
		}
	})

	t.Run("a single-pass connector keeps the previous cursor shape", func(t *testing.T) {
		got := buildSourceCursor(stored, nil, 3, 0, now)
		if _, ok := got["gmail_boundary"]; ok {
			t.Error("connector keys appeared for a connector that returned no cursor")
		}
		if got["total_items"] != 3 || got["synced_at"] != now.Format(time.RFC3339) {
			t.Errorf("counters = %v/%v", got["total_items"], got["synced_at"])
		}
	})
}
