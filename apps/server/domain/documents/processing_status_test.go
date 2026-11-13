package documents

import (
	"strings"
	"testing"
)

// TestProcessingStatusExprTreatsDeadLetterAsFailure pins the status contract
// that made nine permanently failed Huma documents render as "Ready": object
// extraction writes dead_letter, never an intermediate failed, so the derived
// status must treat both as extraction_failed.
func TestProcessingStatusExprTreatsDeadLetterAsFailure(t *testing.T) {
	expr := processingStatusExpr()
	if !strings.Contains(expr, "IN ('failed', 'dead_letter')") {
		t.Fatalf("processingStatusExpr does not classify dead_letter as extraction_failed:\n%s", expr)
	}
}

// TestProcessingStatusExprSurfacesBudgetBlocked pins the durable budget signal
// to the visible status: a pending embedding job carrying
// last_error='budget_exceeded' must produce the blocked state, otherwise the
// document keeps rendering as ready while its embeddings never arrive.
func TestProcessingStatusExprSurfacesBudgetBlocked(t *testing.T) {
	expr := processingStatusExpr()
	if !strings.Contains(expr, "'budget_exceeded'") {
		t.Fatalf("processingStatusExpr does not look for the budget_exceeded signal:\n%s", expr)
	}
	if !strings.Contains(expr, "'blocked'") {
		t.Fatalf("processingStatusExpr never yields the blocked state:\n%s", expr)
	}
}
