package main

import "testing"

func TestRunFailureText(t *testing.T) {
	cases := []struct {
		name         string
		errorMessage *string
		failureClass *string
		want         string
	}{
		{"error wins over class", strPtr("boom"), strPtr("deterministic"), "boom"},
		{"error only", strPtr("  boom  "), nil, "boom"},
		{"class fallback", nil, strPtr("deterministic"), "Failure: Deterministic"},
		{"empty error falls back to class", strPtr("  "), strPtr("retryable"), "Failure: Retryable"},
		{"neither", nil, nil, ""},
		{"empty class", nil, strPtr("  "), ""},
	}
	for _, c := range cases {
		if got := runFailureText(c.errorMessage, c.failureClass); got != c.want {
			t.Errorf("%s: runFailureText = %q, want %q", c.name, got, c.want)
		}
	}
}
