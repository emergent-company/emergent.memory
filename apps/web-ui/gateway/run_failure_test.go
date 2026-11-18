package main

import (
	"strings"
	"testing"
)

func TestRunStatusFailed(t *testing.T) {
	cases := map[string]bool{
		"failed":    true,
		" FAILED ":  true,
		"error":     true,
		"completed": false,
		"cancelled": false,
		"canceled":  false,
		"working":   false,
		"queued":    false,
		"":          false,
	}
	for status, want := range cases {
		if got := runStatusFailed(status); got != want {
			t.Errorf("runStatusFailed(%q) = %v, want %v", status, got, want)
		}
	}
}

func TestRunFailureText(t *testing.T) {
	cases := []struct {
		name         string
		status       string
		errorMessage *string
		failureClass *string
		summary      map[string]any
		want         string
	}{
		{"error wins over class", "failed", strPtr("boom"), strPtr("deterministic"), nil, "boom"},
		{"error only", "failed", strPtr("  boom  "), nil, nil, "boom"},
		{"summary error before class", "failed", nil, strPtr("deterministic"), map[string]any{"error": "from summary"}, "from summary"},
		{"blank error falls to summary", "failed", strPtr("  "), nil, map[string]any{"error": "from summary"}, "from summary"},
		{"blank summary error falls to class", "failed", nil, strPtr("retryable"), map[string]any{"error": "  "}, "Transient error; retry the run"},
		{"neither", "failed", nil, nil, nil, ""},
		{"completed with error", "completed", strPtr("boom"), nil, nil, ""},
		{"cancelled with error", "cancelled", strPtr("boom"), strPtr("deterministic"), nil, ""},
		{"working with error", "working", strPtr("boom"), nil, nil, ""},
		{"legacy error status", "error", strPtr("boom"), nil, nil, "boom"},
		{"empty class", "failed", nil, strPtr("  "), nil, ""},
	}
	for _, c := range cases {
		if got := runFailureText(c.status, c.errorMessage, c.failureClass, c.summary); got != c.want {
			t.Errorf("%s: runFailureText = %q, want %q", c.name, got, c.want)
		}
	}
}

func TestRunFailureTextClassFallback(t *testing.T) {
	cases := map[string]string{
		"deterministic": "The agent hit an unrecoverable error",
		"retryable":     "Transient error; retry the run",
		"human":         "Needs human review",
		"quota":         "Provider quota exceeded",
		"capability":    "Agent lacks the required capability",
		"protocol":      "Run ended without completing its work",
		"timeout":       "Run timed out",
		"budget":        "Project budget exhausted",
		"unknown":       "Run failed",
		"":              "Run failed",
		"weird_class":   "Weird class",
	}
	for class, want := range cases {
		class := class
		var ptr *string
		if class != "" {
			ptr = strPtr(class)
		}
		if got := humanFailureClass(class); got != want {
			t.Errorf("humanFailureClass(%q) = %q, want %q", class, got, want)
		}
		if class == "" {
			// An absent class carries no failure reason at all.
			if got := runFailureText("failed", nil, ptr, nil); got != "" {
				t.Errorf("absent class: runFailureText = %q, want empty", got)
			}
			continue
		}
		if got := runFailureText("failed", nil, ptr, nil); got != want {
			t.Errorf("class %q: runFailureText = %q, want %q", class, got, want)
		}
	}
}

func TestRunFailureTextCollapsesWhitespace(t *testing.T) {
	got := runFailureText("failed", strPtr("boom\n\n  on\tline two  "), nil, nil)
	if want := "boom on line two"; got != want {
		t.Errorf("runFailureText = %q, want %q", got, want)
	}
}

func TestRunFailureTextBounds(t *testing.T) {
	got := runFailureText("failed", strPtr(strings.Repeat("x", 600)), nil, nil)
	if n := len([]rune(got)); n != maxRunFailureTextRunes {
		t.Errorf("bounded length = %d, want %d", n, maxRunFailureTextRunes)
	}
	if !strings.HasSuffix(got, "…") {
		t.Errorf("bounded text must end with an ellipsis: %q", got)
	}
	if !strings.HasPrefix(got, strings.Repeat("x", maxRunFailureTextRunes-1)) {
		t.Error("bounded text must be a prefix of the original message")
	}

	// The summary error is bounded too.
	got = runFailureText("failed", nil, nil, map[string]any{"error": strings.Repeat("y", 600)})
	if n := len([]rune(got)); n != maxRunFailureTextRunes {
		t.Errorf("summary bounded length = %d, want %d", n, maxRunFailureTextRunes)
	}
}

func TestRunFailureTextRedactsSecrets(t *testing.T) {
	githubToken := "ghp_" + strings.Repeat("a", 30)
	openAIKey := "sk-" + strings.Repeat("b", 30)
	stripeLive := "sk_" + "live_" + strings.Repeat("d", 24)
	stripeRestricted := "rk_" + "test_" + strings.Repeat("e", 24)
	jwt := strings.Join([]string{"eyJhbGciOiJIUzI1NiJ9", "eyJzdWIiOiIxMjM0NTY3ODkwIn0", "abcdefghijklmnop"}, ".")
	cases := []struct {
		name string
		msg  string
		gone []string
		want string
	}{
		{"github token", "request rejected for " + githubToken, []string{githubToken}, "request rejected for [redacted]"},
		{"github pat", "bad token github_pat_" + strings.Repeat("c", 30), []string{"github_pat_" + strings.Repeat("c", 30)}, "bad token [redacted]"},
		{"openai key", "invalid key " + openAIKey, []string{openAIKey}, "invalid key [redacted]"},
		{"openai project key", "invalid key sk-proj-abcdefghijklmnopqrstuvwxyz012345", []string{"sk-proj-abcdefghijklmnopqrstuvwxyz012345"}, "invalid key [redacted]"},
		{"anthropic key", "bad key sk-ant-api03-abcdefghijklmnopqrstuvwxyz", []string{"sk-ant-api03-abcdefghijklmnopqrstuvwxyz"}, "bad key [redacted]"},
		{"slack token", "slack said xoxb-1234567890-abcdef", []string{"xoxb-1234567890-abcdef"}, "slack said [redacted]"},
		{"bearer", "Authorization: Bearer abcdefghij.token-123", []string{"abcdefghij.token-123"}, "Authorization: [redacted]"},
		{"query token", "callback https://x.test/cb?token=secretvalue&state=ok", []string{"secretvalue"}, "callback https://x.test/cb?token=[redacted]&state=ok"},
		{"query api_key", "https://x.test/cb?API_KEY=abc123&x=1", []string{"abc123"}, "https://x.test/cb?API_KEY=[redacted]&x=1"},
		{"stripe live key", "billing said " + stripeLive, []string{stripeLive}, "billing said [redacted]"},
		{"stripe restricted key", "billing said " + stripeRestricted, []string{stripeRestricted}, "billing said [redacted]"},
		{"jwt", "bearer " + jwt, []string{jwt}, "bearer [redacted]"},
	}
	for _, c := range cases {
		got := runFailureText("failed", strPtr(c.msg), nil, nil)
		for _, secret := range c.gone {
			if strings.Contains(got, secret) {
				t.Errorf("%s: secret %q leaked: %q", c.name, secret, got)
			}
		}
		if got != c.want {
			t.Errorf("%s: runFailureText = %q, want %q", c.name, got, c.want)
		}
	}
}
