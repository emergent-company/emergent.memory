package cmd

import (
	"bytes"
	"strings"
	"testing"
)

func TestPrintEmbeddingProgressIncludesStaleFailed(t *testing.T) {
	result := map[string]any{
		"objects": map[string]any{
			"pending":     float64(10),
			"processing":  float64(2),
			"completed":   float64(5),
			"failed":      float64(3),
			"staleFailed": float64(7),
			"deadLetter":  float64(1),
		},
	}

	var buf bytes.Buffer
	printEmbeddingProgress(&buf, result)
	out := buf.String()

	if !strings.Contains(out, "failed=3") {
		t.Errorf("expected genuine failed value to appear, got:\n%s", out)
	}
	if !strings.Contains(out, "stale_failed=7") {
		t.Errorf("expected stale-failed value to appear, got:\n%s", out)
	}
}

func TestPrintEmbeddingProgressStaleFailedReconcilesTotal(t *testing.T) {
	// Regression: stale-failed rows must be counted in the denominator, so a
	// queue whose only non-completed work is stale-failed does not report 100%.
	result := map[string]any{
		"objects": map[string]any{
			"completed":   float64(5),
			"staleFailed": float64(4),
		},
	}

	var buf bytes.Buffer
	printEmbeddingProgress(&buf, result)
	out := buf.String()

	if !strings.Contains(out, "stale_failed=4") {
		t.Errorf("expected stale-failed column, got:\n%s", out)
	}
	// 5 completed / (5 completed + 4 stale-failed) = 55.6%, not 100%.
	if strings.Contains(out, "100.0%") {
		t.Errorf("stale-failed was not counted in total: got:\n%s", out)
	}
}
