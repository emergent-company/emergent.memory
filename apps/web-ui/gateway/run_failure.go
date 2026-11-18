package main

import "strings"

// runFailureText resolves the human-readable reason a run failed. The explicit
// error message wins when present; otherwise a recorded failure class is
// humanised ("deterministic" → "Failure: Deterministic"). An empty string means
// the run carries no failure reason at all — it may have succeeded, or it may
// predate failure visibility.
func runFailureText(errorMessage *string, failureClass *string) string {
	if errorMessage != nil {
		if msg := strings.TrimSpace(*errorMessage); msg != "" {
			return msg
		}
	}
	if failureClass != nil {
		if class := strings.TrimSpace(*failureClass); class != "" {
			return humanRunToken(class) + " failure"
		}
	}
	return ""
}
