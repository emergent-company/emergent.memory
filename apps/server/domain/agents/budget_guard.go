package agents

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
)

// BudgetChecker reports whether a project has exhausted either spending window
// (weekly or monthly). *provider.UsageService satisfies it; the interface is
// declared here so the executor, the repository and unit tests share one
// budget gate without importing the concrete service.
type BudgetChecker interface {
	CheckBudgetExceeded(ctx context.Context, projectID string) (bool, error)
}

// checkProjectBudget is the single budget gate used by every run-creation and
// execution entry point.
//
// It is a no-op when no checker is wired, enforcement is disabled, or the
// project is unknown. A failed budget query fails OPEN: a broken usage query
// must never halt agent execution. When either spending window is exhausted it
// returns a typed *BudgetExceededError so callers can map it (HTTP 402) and the
// worker pool can retire the run without consuming failure budgets.
func checkProjectBudget(ctx context.Context, checker BudgetChecker, enabled bool, projectID string, log *slog.Logger) error {
	if checker == nil || !enabled || projectID == "" {
		return nil
	}
	exceeded, err := checker.CheckBudgetExceeded(ctx, projectID)
	if err != nil {
		if log != nil {
			log.Warn("budget pre-flight check failed, proceeding",
				slog.String("project_id", projectID),
				slog.String("error", err.Error()),
			)
		}
		return nil
	}
	if !exceeded {
		return nil
	}
	if log != nil {
		log.Warn("agent execution blocked: project budget exceeded",
			slog.String("project_id", projectID),
		)
	}
	return &BudgetExceededError{
		ProjectID: projectID,
		Message:   fmt.Sprintf("project %s has exceeded its spending budget", projectID),
	}
}

// isBudgetExceededError reports whether err is a project spending-cap block.
// Budget blocks are deliberately distinct from provider quota failures: a
// budget block must not touch the agent failure counter or the per-item
// failure budget.
func isBudgetExceededError(err error) bool {
	if err == nil {
		return false
	}
	_, ok := errors.AsType[*BudgetExceededError](err)
	return ok
}
