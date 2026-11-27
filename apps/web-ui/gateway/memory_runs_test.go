package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestGetRunOrderersChunksOverCap pins the batching loop in GetRunOrderers: the
// server caps one ?ids= read at agentRunIDsFilterMax, so a page with more runs
// must be read in consecutive batches and the results merged. The single-batch
// test above cannot catch an off-by-one in the loop cursor or a dropped final
// batch; this test does, because it asserts the request boundaries AND that
// every id in the last (partial) batch comes back.
func TestGetRunOrderersChunksOverCap(t *testing.T) {
	const total = 2*agentRunIDsFilterMax + 7 // two full batches and one partial
	runIDs := make([]string, total)
	for i := range runIDs {
		runIDs[i] = fmt.Sprintf("run-%04d", i)
	}

	var batches [][]string
	var limits []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ids := strings.Split(r.URL.Query().Get("ids"), ",")
		batches = append(batches, ids)
		limits = append(limits, r.URL.Query().Get("limit"))

		items := make([]string, 0, len(ids))
		for _, id := range ids {
			items = append(items, fmt.Sprintf(`{"id":%q,"orderedByUserId":"u-%s"}`, id, id))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{"success":true,"data":{"items":[%s],"totalCount":%d,"limit":%d,"offset":0}}`,
			strings.Join(items, ","), len(items), len(items))
	}))
	t.Cleanup(srv.Close)

	orderers, err := NewMemoryClient(srv.URL, "proj").GetRunOrderers(sessProjCtx("tok123", "proj"), runIDs)
	if err != nil {
		t.Fatal(err)
	}

	wantSizes := []int{agentRunIDsFilterMax, agentRunIDsFilterMax, 7}
	if len(batches) != len(wantSizes) {
		t.Fatalf("requests = %d, want %d (over-cap sets must be read in batches, never truncated)", len(batches), len(wantSizes))
	}
	for i, want := range wantSizes {
		if len(batches[i]) != want {
			t.Errorf("batch %d ids = %d, want %d", i, len(batches[i]), want)
		}
		if limits[i] != fmt.Sprint(want) {
			t.Errorf("batch %d limit = %q, want %d", i, limits[i], want)
		}
		if want > 0 {
			wantFirst := runIDs[i*agentRunIDsFilterMax]
			if batches[i][0] != wantFirst {
				t.Errorf("batch %d starts at %q, want %q (cursor skipped an id)", i, batches[i][0], wantFirst)
			}
		}
	}
	// The merged map must carry the final, partial batch too: a loop that stops
	// at the last full batch would leave those runs unattributed.
	if len(orderers) != total {
		t.Fatalf("orderers = %d entries, want %d (a dropped batch silently loses attribution)", len(orderers), total)
	}
	for _, id := range runIDs {
		if got, want := orderers[id], "u-"+id; got != want {
			t.Errorf("orderers[%s] = %q, want %q", id, got, want)
		}
	}
}
