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
