package suite

import (
	"encoding/json"
	"fmt"
	"os"
	"text/tabwriter"
	"time"
)

// Status represents the outcome of a single test item.
type Status string

const (
	StatusPassed  Status = "passed"
	StatusFailed  Status = "failed"
	StatusSkipped Status = "skipped"
	StatusTimeout Status = "timeout"
)

// ItemResult holds the outcome of a single item within a suite run.
type ItemResult struct {
	ID       string        `json:"id"`
	Name     string        `json:"name"`
	Status   Status        `json:"status"`
	Duration time.Duration `json:"duration_ms"`
	Error    string        `json:"error,omitempty"`
}

// Result holds the aggregated outcome of a full suite run.
type Result struct {
	SuiteName string        `json:"suite"`
	StartTime time.Time     `json:"start_time"`
	Duration  time.Duration `json:"duration_ms"`
	Items     []ItemResult  `json:"items"`

	// Derived counts (populated by Finalize)
	Passed  int `json:"passed"`
	Failed  int `json:"failed"`
	Skipped int `json:"skipped"`
	Timeout int `json:"timeout"`
}

// NewResult creates a Result with the suite name and current start time.
func NewResult(suiteName string) *Result {
	return &Result{
		SuiteName: suiteName,
		StartTime: time.Now(),
	}
}

// AddItem appends an item result and updates derived counts.
func (r *Result) AddItem(item ItemResult) {
	r.Items = append(r.Items, item)
	switch item.Status {
	case StatusPassed:
		r.Passed++
	case StatusFailed:
		r.Failed++
	case StatusSkipped:
		r.Skipped++
	case StatusTimeout:
		r.Timeout++
	}
}

// Finalize sets the total duration. Call after all items are added.
func (r *Result) Finalize() {
	r.Duration = time.Since(r.StartTime)
}

// Print writes a human-readable summary table to stdout.
func (r *Result) Print() {
	fmt.Printf("\n=== %s ===\n", r.SuiteName)
	fmt.Printf("Duration: %s\n\n", r.Duration.Round(time.Second))

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "STATUS\tNAME\tDURATION\tERROR")
	fmt.Fprintln(w, "------\t----\t--------\t-----")

	for _, item := range r.Items {
		icon := statusIcon(item.Status)
		errStr := item.Error
		if len(errStr) > 60 {
			errStr = errStr[:57] + "..."
		}
		fmt.Fprintf(w, "%s %s\t%s\t%s\t%s\n",
			icon, item.Status,
			item.Name,
			item.Duration.Round(time.Millisecond),
			errStr,
		)
	}
	w.Flush()

	fmt.Printf("\nSummary: %d passed, %d failed, %d skipped, %d timeout  (total: %d)\n",
		r.Passed, r.Failed, r.Skipped, r.Timeout, len(r.Items))
}

// PrintJSON writes machine-readable JSON to stdout.
func (r *Result) PrintJSON() {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	_ = enc.Encode(r)
}

func statusIcon(s Status) string {
	switch s {
	case StatusPassed:
		return "✓"
	case StatusFailed:
		return "✗"
	case StatusTimeout:
		return "⏱"
	default:
		return "~"
	}
}
