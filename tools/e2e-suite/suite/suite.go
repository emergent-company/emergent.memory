package suite

import (
	"context"
	"fmt"

	sdk "github.com/emergent-company/emergent/apps/server-go/pkg/sdk"
)

// Suite is the interface every test scenario must implement.
type Suite interface {
	Name() string
	Description() string
	Run(ctx context.Context, client *sdk.Client, cfg *Config) (*Result, error)
}

// Runner runs one or more suites sequentially and collects results.
type Runner struct {
	Suites []Suite
	Cfg    *Config
}

// Run executes all suites sequentially. It creates a new SDK client per suite
// so suites can mutate client context without interfering with each other.
func (r *Runner) Run(ctx context.Context) []*Result {
	var results []*Result

	for _, s := range r.Suites {
		fmt.Printf("\n>>> Running suite: %s\n", s.Name())
		fmt.Printf("    %s\n\n", s.Description())

		if r.Cfg.DryRun {
			fmt.Printf("[dry-run] would run suite %q\n", s.Name())
			result := NewResult(s.Name())
			result.Finalize()
			results = append(results, result)
			continue
		}

		client, err := sdk.New(sdk.Config{
			ServerURL: r.Cfg.ServerURL,
			Auth: sdk.AuthConfig{
				Mode:   "apikey",
				APIKey: r.Cfg.APIKey,
			},
			OrgID:     r.Cfg.OrgID,
			ProjectID: r.Cfg.ProjectID,
		})
		if err != nil {
			result := NewResult(s.Name())
			result.AddItem(ItemResult{
				ID:     "init",
				Name:   "SDK initialisation",
				Status: StatusFailed,
				Error:  err.Error(),
			})
			result.Finalize()
			results = append(results, result)
			continue
		}

		result, err := s.Run(ctx, client, r.Cfg)
		if err != nil {
			if result == nil {
				result = NewResult(s.Name())
			}
			result.AddItem(ItemResult{
				ID:     "suite_error",
				Name:   "suite-level error",
				Status: StatusFailed,
				Error:  err.Error(),
			})
		}
		if result != nil {
			result.Finalize()
		}
		results = append(results, result)
	}

	return results
}

// PrintSummary prints a combined summary of all suite results.
func PrintSummary(results []*Result, format string) {
	totalPassed, totalFailed, totalSkipped, totalTimeout := 0, 0, 0, 0
	for _, r := range results {
		if r == nil {
			continue
		}
		if format == "json" {
			r.PrintJSON()
		} else {
			r.Print()
		}
		totalPassed += r.Passed
		totalFailed += r.Failed
		totalSkipped += r.Skipped
		totalTimeout += r.Timeout
	}

	if len(results) > 1 {
		fmt.Printf("\n=== COMBINED SUMMARY ===\n")
		fmt.Printf("Suites run: %d\n", len(results))
		fmt.Printf("Passed:     %d\n", totalPassed)
		fmt.Printf("Failed:     %d\n", totalFailed)
		fmt.Printf("Skipped:    %d\n", totalSkipped)
		fmt.Printf("Timeout:    %d\n", totalTimeout)
	}
}
