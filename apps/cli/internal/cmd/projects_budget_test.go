package cmd

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

// setBudgetPatch is one captured PATCH /api/projects/{id} body.
type setBudgetPatch struct {
	raw map[string]json.RawMessage
}

// newSetBudgetServer records the PATCH bodies runSetBudget sends and echoes a
// project so the command can print the resulting caps.
func newSetBudgetServer(t *testing.T) (*httptest.Server, func() []setBudgetPatch) {
	t.Helper()
	var (
		mu      sync.Mutex
		patches []setBudgetPatch
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method != http.MethodPatch {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
			return
		}
		body, _ := io.ReadAll(r.Body)
		var raw map[string]json.RawMessage
		if err := json.Unmarshal(body, &raw); err != nil {
			t.Errorf("decode PATCH body: %v", err)
		}
		mu.Lock()
		patches = append(patches, setBudgetPatch{raw: raw})
		mu.Unlock()
		_, _ = io.WriteString(w, `{"id":"00000000-0000-0000-0000-000000000001","name":"home","orgId":"o1"}`)
	}))
	t.Cleanup(srv.Close)
	return srv, func() []setBudgetPatch {
		mu.Lock()
		defer mu.Unlock()
		return append([]setBudgetPatch(nil), patches...)
	}
}

// runSetBudgetWithFlags drives the real runSetBudget with the given flag
// values, isolating the package-level flag state it reads.
func runSetBudgetWithFlags(t *testing.T, serverURL string, flags map[string]string) error {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("MEMORY_SERVER_URL", serverURL)
	t.Setenv("MEMORY_PROJECT_API_KEY", "emt_test_project_token")

	prevServer := viper.GetString("server")
	viper.Set("server", serverURL)
	t.Cleanup(func() { viper.Set("server", prevServer) })

	prevAmount, prevThreshold := setBudgetAmount, setBudgetThreshold
	prevWeekly, prevWeeklyThreshold := setBudgetWeeklyAmount, setBudgetWeeklyThreshold
	t.Cleanup(func() {
		setBudgetAmount, setBudgetThreshold = prevAmount, prevThreshold
		setBudgetWeeklyAmount, setBudgetWeeklyThreshold = prevWeekly, prevWeeklyThreshold
	})

	cmd := &cobra.Command{}
	cmd.Flags().String("config", "", "config file")
	cmd.Flags().Float64Var(&setBudgetAmount, "budget", 0, "")
	cmd.Flags().Float64Var(&setBudgetThreshold, "threshold", 0.8, "")
	cmd.Flags().Float64Var(&setBudgetWeeklyAmount, "weekly", 0, "")
	cmd.Flags().Float64Var(&setBudgetWeeklyThreshold, "weekly-threshold", 0.8, "")
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	for name, value := range flags {
		if err := cmd.Flags().Set(name, value); err != nil {
			t.Fatalf("set flag %s: %v", name, err)
		}
	}

	return runSetBudget(cmd, []string{"00000000-0000-0000-0000-000000000001"})
}

// TestRunSetBudget_WritesOnlyTheNamedWindow is the guard against the shape that
// makes independent windows pointless: a weekly-only invocation must not send a
// monthly cap (a zero value in the monthly field would clear it).
func TestRunSetBudget_WritesOnlyTheNamedWindow(t *testing.T) {
	srv, patches := newSetBudgetServer(t)
	if err := runSetBudgetWithFlags(t, srv.URL, map[string]string{"weekly": "15"}); err != nil {
		t.Fatalf("runSetBudget: %v", err)
	}

	got := patches()
	if len(got) != 1 {
		t.Fatalf("got %d PATCHes, want 1", len(got))
	}
	if _, ok := got[0].raw["budget_usd"]; ok {
		t.Errorf("weekly-only save must not send budget_usd: %v", got[0].raw)
	}
	if _, ok := got[0].raw["budget_weekly_usd"]; !ok {
		t.Errorf("weekly-only save must send budget_weekly_usd: %v", got[0].raw)
	}
	if _, ok := got[0].raw["budget_weekly_alert_threshold"]; ok {
		t.Errorf("weekly-threshold was not given, so it must be omitted: %v", got[0].raw)
	}
}

// TestRunSetBudget_WeeklyThreshold proves an explicit --weekly-threshold is sent
// alongside the cap.
func TestRunSetBudget_WeeklyThreshold(t *testing.T) {
	srv, patches := newSetBudgetServer(t)
	if err := runSetBudgetWithFlags(t, srv.URL, map[string]string{"weekly": "12.5", "weekly-threshold": "0.75"}); err != nil {
		t.Fatalf("runSetBudget: %v", err)
	}

	got := patches()
	if len(got) != 1 {
		t.Fatalf("got %d PATCHes, want 1", len(got))
	}
	var threshold float64
	if err := json.Unmarshal(got[0].raw["budget_weekly_alert_threshold"], &threshold); err != nil {
		t.Fatalf("decode weekly threshold: %v (%v)", err, got[0].raw)
	}
	if threshold != 0.75 {
		t.Errorf("weekly threshold = %v, want 0.75", threshold)
	}
}

// TestRunSetBudget_RequiresAWindow proves a bare invocation is refused rather
// than sending an empty PATCH.
func TestRunSetBudget_RequiresAWindow(t *testing.T) {
	srv, patches := newSetBudgetServer(t)
	if err := runSetBudgetWithFlags(t, srv.URL, nil); err == nil {
		t.Fatal("expected an error when neither --budget nor --weekly is given")
	}
	if len(patches()) != 0 {
		t.Errorf("a refused invocation must not reach the API")
	}
}

// TestBudgetCapLabel pins the printed caps, including the case that used to be
// wrong: clearing a cap with 0 must read "not set", not "$0.00".
func TestBudgetCapLabel(t *testing.T) {
	f := func(v float64) *float64 { return &v }
	tests := []struct {
		name    string
		changed bool
		flag    float64
		stored  *float64
		want    string
	}{
		{"flag set", true, 15, nil, "$15.00"},
		{"flag clears", true, 0, f(15), "not set"},
		{"stored echoed", false, 0, f(12.5), "$12.50"},
		{"stored zero is uncapped", false, 0, f(0), "not set"},
		{"absent", false, 0, nil, "not set"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := budgetCapLabel(tt.changed, tt.flag, tt.stored); got != tt.want {
				t.Errorf("budgetCapLabel = %q, want %q", got, tt.want)
			}
		})
	}
}
