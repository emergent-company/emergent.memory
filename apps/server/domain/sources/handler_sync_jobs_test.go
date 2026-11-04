package sources

import "testing"

// TestSyncJobsPageBounds pins the listing's paging contract: the endpoint is
// always bounded, a caller cannot ask for an unbounded page, and bad paging
// input falls back to the first page instead of failing the request.
func TestSyncJobsPageBounds(t *testing.T) {
	tests := []struct {
		name          string
		limit, offset string
		wantLimit     int
		wantOffset    int
	}{
		{name: "defaults", wantLimit: syncJobsDefaultLimit, wantOffset: 0},
		{name: "explicit", limit: "5", offset: "10", wantLimit: 5, wantOffset: 10},
		{name: "clamps above max", limit: "1000", wantLimit: syncJobsMaxLimit},
		{name: "zero limit uses default", limit: "0", wantLimit: syncJobsDefaultLimit},
		{name: "negative limit uses default", limit: "-3", wantLimit: syncJobsDefaultLimit},
		{name: "invalid limit uses default", limit: "many", wantLimit: syncJobsDefaultLimit},
		{name: "negative offset means first page", offset: "-5", wantLimit: syncJobsDefaultLimit, wantOffset: 0},
		{name: "invalid offset means first page", offset: "nope", wantLimit: syncJobsDefaultLimit, wantOffset: 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			limit, offset := syncJobsPage(tt.limit, tt.offset)
			if limit != tt.wantLimit || offset != tt.wantOffset {
				t.Fatalf("syncJobsPage(%q, %q) = (%d, %d), want (%d, %d)",
					tt.limit, tt.offset, limit, offset, tt.wantLimit, tt.wantOffset)
			}
		})
	}
}
