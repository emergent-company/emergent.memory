package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// dsTestSyncJobs builds n newest-first jobs whose status message names the job,
// so a test can tell exactly which rows a page rendered.
func dsTestSyncJobs(n int) []DSSyncJob {
	jobs := make([]DSSyncJob, n)
	for i := range jobs {
		msg := fmt.Sprintf("sync job %d", i+1)
		jobs[i] = DSSyncJob{
			ID:              fmt.Sprintf("job-%d", i+1),
			Status:          "completed",
			TotalItems:      1,
			ProcessedItems:  1,
			SuccessfulItems: 1,
			TriggerType:     "scheduled",
			StatusMessage:   &msg,
			CreatedAt:       "2026-09-01T00:00:00Z",
		}
	}
	return jobs
}

// TestUISyncHistoryPagesFiveAndLoadsMore is the #2333 regression test: the
// detail page renders only the newest five jobs, and "Load more" appends the
// next page through the htmx partial and disappears on the last page.
func TestUISyncHistoryPagesFiveAndLoadsMore(t *testing.T) {
	f := &fakeMemory{
		dsIntegrations: []DSIntegration{{ID: "s1", Name: "Team Drive", ProviderType: "google_drive", Status: "active", Config: map[string]any{"folder_id": "f1"}}},
		dsSyncJobs:     map[string][]DSSyncJob{"s1": dsTestSyncJobs(12)},
	}
	e := newDSUIServer(f)

	// Default page: exactly the newest five, with a load-more control pointing at
	// the next offset. The badge still reports the source's true total.
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/data-sources/s1", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("detail status = %d, body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if got := strings.Count(body, `data-testid="ds-sync-job-row"`); got != 5 {
		t.Errorf("default page rendered %d job rows, want 5\n%s", got, body)
	}
	for _, want := range []string{
		`data-testid="ds-sync-history-load-more"`,
		`hx-get="/data-sources/s1/sync-jobs/partial?offset=5"`,
		`hx-target="#ds-sync-jobs-list"`,
		`hx-swap="beforeend"`,
		`12 jobs`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("default page missing %q", want)
		}
	}
	for i := 1; i <= 5; i++ {
		if !strings.Contains(body, fmt.Sprintf("sync job %d", i)) {
			t.Errorf("default page missing newest job %d", i)
		}
	}
	if strings.Contains(body, "sync job 6") {
		t.Error("default page rendered job 6; only the newest five belong here")
	}
	if strings.Contains(body, `data-testid="ds-sync-history-end"`) {
		t.Error("default page with more pages must not render the end note")
	}

	// First "Load more": the next five rows plus a replacement control for the
	// following page.
	rec = httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/data-sources/s1/sync-jobs/partial?offset=5", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("first partial status = %d, body=%s", rec.Code, rec.Body.String())
	}
	body = rec.Body.String()
	if got := strings.Count(body, `data-testid="ds-sync-job-row"`); got != 5 {
		t.Errorf("first partial rendered %d job rows, want 5\n%s", got, body)
	}
	for i := 6; i <= 10; i++ {
		if !strings.Contains(body, fmt.Sprintf("sync job %d", i)) {
			t.Errorf("first partial missing job %d", i)
		}
	}
	for _, want := range []string{
		`id="ds-sync-history-more"`,
		`hx-swap-oob="true"`,
		`hx-get="/data-sources/s1/sync-jobs/partial?offset=10"`,
		`data-testid="ds-sync-history-load-more"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("first partial missing %q", want)
		}
	}

	// Last "Load more": the two remaining rows, the terminal note, and no button.
	rec = httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/data-sources/s1/sync-jobs/partial?offset=10", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("last partial status = %d, body=%s", rec.Code, rec.Body.String())
	}
	body = rec.Body.String()
	if got := strings.Count(body, `data-testid="ds-sync-job-row"`); got != 2 {
		t.Errorf("last partial rendered %d job rows, want 2\n%s", got, body)
	}
	for _, want := range []string{"sync job 11", "sync job 12", `data-testid="ds-sync-history-end"`, "All 12 sync jobs loaded."} {
		if !strings.Contains(body, want) {
			t.Errorf("last partial missing %q", want)
		}
	}
	if strings.Contains(body, `data-testid="ds-sync-history-load-more"`) {
		t.Error("last partial must remove the load-more control")
	}
}

// TestUISyncHistoryPartialFailureIs502 pins that a failed next page is an HTMX
// error rather than a silently empty append.
func TestUISyncHistoryPartialFailureIs502(t *testing.T) {
	f := &fakeMemory{dsSyncJobsErr: errors.New("upstream down")}
	e := newDSUIServer(f)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/data-sources/s1/sync-jobs/partial?offset=5", nil))
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502", rec.Code)
	}
}

// TestListDSSyncJobsHandlerForwardsPaging pins that the gateway API does not
// drop ?limit/?offset: the page size comes from the caller and the total is the
// full history, not the page length.
func TestListDSSyncJobsHandlerForwardsPaging(t *testing.T) {
	f := &fakeMemory{dsSyncJobs: map[string][]DSSyncJob{"s1": dsTestSyncJobs(3)}}
	_, e := newDSAPIServer(f, "")
	rec := dsJSONReq(e, http.MethodGet, "/api/data-source-integrations/s1/sync-jobs?limit=2&offset=1", "", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var got DSListSyncJobsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Items) != 2 {
		t.Errorf("items = %d, want 2 (limit forwarded)", len(got.Items))
	}
	if got.Total != 3 {
		t.Errorf("total = %d, want the full history count 3", got.Total)
	}
	if len(got.Items) == 2 && got.Items[0].ID != "job-2" {
		t.Errorf("first item = %q, want job-2 (offset forwarded)", got.Items[0].ID)
	}
}
