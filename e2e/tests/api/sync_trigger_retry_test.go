// Unit tests for the trigger-conflict retry policy (#2493). These do not need
// the e2e stack: the trigger and the wait are injected fakes, so the policy is
// exercised deterministically and the real helpers only supply HTTP and polling.
package api_test

import (
	"errors"
	"strings"
	"testing"
)

// conflictResponse is the server's "single active job" reply: success=false
// with the active job id set.
func conflictResponse(jobID string) triggerSyncResponse {
	return triggerSyncResponse{
		Success: false,
		Message: "sync already in progress",
		JobID:   &jobID,
	}
}

func startedResponse(jobID string) triggerSyncResponse {
	return triggerSyncResponse{Success: true, Message: "sync started", JobID: &jobID}
}

func TestTriggerSyncWithRetry_WaitsForActiveJobThenRetriggers(t *testing.T) {
	var waited []string
	calls := 0
	trigger := func() (triggerSyncResponse, error) {
		calls++
		if calls == 1 {
			return conflictResponse("job-active"), nil
		}
		return startedResponse("job-retriggered"), nil
	}

	got, err := triggerSyncWithRetry(trigger, func(jobID string) { waited = append(waited, jobID) }, 3)
	if err != nil {
		t.Fatalf("retry after a legitimate conflict: %v", err)
	}
	if got != "job-retriggered" {
		t.Fatalf("returned job id = %q, want the retriggered job", got)
	}
	if calls != 2 {
		t.Fatalf("trigger called %d times, want 2 (conflict, retrigger)", calls)
	}
	if len(waited) != 1 || waited[0] != "job-active" {
		t.Fatalf("waited for %v, want exactly [job-active]", waited)
	}
}

func TestTriggerSyncWithRetry_FailsLoudWhenBudgetExhausted(t *testing.T) {
	calls, waits := 0, 0
	trigger := func() (triggerSyncResponse, error) {
		calls++
		return conflictResponse("job-stuck"), nil
	}
	wait := func(string) { waits++ }

	_, err := triggerSyncWithRetry(trigger, wait, 3)
	if err == nil {
		t.Fatal("persistent conflict returned no error; a genuine conflict must fail")
	}
	for _, want := range []string{"still in progress after 3 attempt(s)", "job-stuck"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %q", err, want)
		}
	}
	if calls != 3 {
		t.Errorf("trigger called %d times, want the 3-attempt budget", calls)
	}
	if waits != 2 {
		t.Errorf("waited %d times, want one wait between attempts (2)", waits)
	}
}

func TestTriggerSyncWithRetry_ConflictWithoutJobIDFailsImmediately(t *testing.T) {
	calls, waits := 0, 0
	trigger := func() (triggerSyncResponse, error) {
		calls++
		return triggerSyncResponse{Success: false, Message: "sync already in progress"}, nil
	}
	wait := func(string) { waits++ }

	_, err := triggerSyncWithRetry(trigger, wait, 3)
	if err == nil {
		t.Fatal("conflict without a job id returned no error")
	}
	if !strings.Contains(err.Error(), "no active job id") {
		t.Errorf("error %q does not explain that there was nothing to wait for", err)
	}
	if calls != 1 || waits != 0 {
		t.Errorf("calls=%d waits=%d, want 1 call and no wait when no job id is returned", calls, waits)
	}
}

func TestTriggerSyncWithRetry_SuccessWithoutJobIDFails(t *testing.T) {
	trigger := func() (triggerSyncResponse, error) {
		return triggerSyncResponse{Success: true, Message: "sync started"}, nil
	}
	_, err := triggerSyncWithRetry(trigger, func(string) { t.Fatal("wait called for a success response") }, 3)
	if err == nil || !strings.Contains(err.Error(), "without a job id") {
		t.Fatalf("error = %v, want a loud missing-job-id failure", err)
	}
}

func TestTriggerSyncWithRetry_SingleAttemptDoesNotWait(t *testing.T) {
	waits := 0
	trigger := func() (triggerSyncResponse, error) { return conflictResponse("job-active"), nil }
	_, err := triggerSyncWithRetry(trigger, func(string) { waits++ }, 1)
	if err == nil {
		t.Fatal("a one-attempt budget still in conflict returned no error")
	}
	if waits != 0 {
		t.Errorf("waited %d times with a one-attempt budget, want 0", waits)
	}
}

func TestTriggerSyncWithRetry_NonPositiveBudgetBehavesAsOneAttempt(t *testing.T) {
	waits := 0
	trigger := func() (triggerSyncResponse, error) { return conflictResponse("job-active"), nil }
	_, err := triggerSyncWithRetry(trigger, func(string) { waits++ }, 0)
	if err == nil {
		t.Fatal("a zero-attempt budget returned no error")
	}
	if !strings.Contains(err.Error(), "after 1 attempt(s)") {
		t.Errorf("error %q does not report the clamped one-attempt budget", err)
	}
	if waits != 0 {
		t.Errorf("waited %d times with a clamped one-attempt budget, want 0", waits)
	}
}

func TestSyncJobFreedAdmission(t *testing.T) {
	cases := map[string]bool{
		"pending":    false,
		"processing": false,
		"":           false,
		"completed":  true,
		"failed":     true,
		"cancelled":  true,
	}
	for status, want := range cases {
		if got := syncJobFreedAdmission(status); got != want {
			t.Errorf("syncJobFreedAdmission(%q) = %v, want %v", status, got, want)
		}
	}
}

func TestTriggerSyncWithRetry_PropagatesTriggerError(t *testing.T) {
	sentinel := errors.New("connection reset")
	trigger := func() (triggerSyncResponse, error) { return triggerSyncResponse{}, sentinel }
	_, err := triggerSyncWithRetry(trigger, func(string) { t.Fatal("wait called after a trigger error") }, 3)
	if !errors.Is(err, sentinel) {
		t.Fatalf("error = %v, want the trigger error", err)
	}
}
