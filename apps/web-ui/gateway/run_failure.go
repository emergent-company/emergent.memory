package main

import (
	"regexp"
	"strings"
)

// maxRunFailureTextRunes bounds the rendered failure line. A failure message can
// embed a whole upstream response, so the surfaces cap it rather than letting it
// blow out a card or a one-line rail row. The ellipsis is part of the bound.
const maxRunFailureTextRunes = 500

// Secret shapes scrubbed from failure text before it reaches a surface. These
// are deliberately generic: a provider error is where a leaked credential tends
// to surface, and rendering the message must never repeat the secret.
var (
	runFailureGitHubTokenRe   = regexp.MustCompile(`gh[pousr]_[A-Za-z0-9]{20,}`)
	runFailureGitHubPATRe     = regexp.MustCompile(`github_pat_[A-Za-z0-9_]{20,}`)
	runFailureOpenAIKeyRe     = regexp.MustCompile(`sk-[A-Za-z0-9]{20,}`)
	runFailureSlackTokenRe    = regexp.MustCompile(`xox[baprs]-[A-Za-z0-9-]{10,}`)
	runFailureBearerRe        = regexp.MustCompile(`(?i)bearer\s+[A-Za-z0-9._-]{10,}`)
	runFailureQuerySecretRe   = regexp.MustCompile(`(?i)([?&](?:access_token|api_key|apikey|password|secret|token)=)[^&\s#]*`)
	runFailureSecretReplacers = []*regexp.Regexp{
		runFailureGitHubTokenRe,
		runFailureGitHubPATRe,
		runFailureOpenAIKeyRe,
		runFailureSlackTokenRe,
		runFailureBearerRe,
	}
)

// runStatusFailed reports whether a run status represents a failure. The legacy
// "error" spelling is accepted alongside the canonical "failed"; every other
// status — cancelled above all — is not a failure, because its own status
// already tells the operator what happened.
func runStatusFailed(status string) bool {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "failed", "error":
		return true
	default:
		return false
	}
}

// runFailureText resolves the human-readable reason a run failed. It returns ""
// unless the run's status is a failure: a cancelled or completed run must never
// render "This run failed". The explicit error message wins when present; a
// summary-recorded error is the next resort (the work preview already trusts
// that field), and only then a failure class. The result is redacted, collapsed
// to one line, and bounded.
func runFailureText(status string, errorMessage *string, failureClass *string, summary map[string]any) string {
	if !runStatusFailed(status) {
		return ""
	}
	if errorMessage != nil {
		if msg := strings.TrimSpace(*errorMessage); msg != "" {
			return boundRunFailureText(msg)
		}
	}
	if summary != nil {
		if msg, ok := summary["error"].(string); ok {
			if msg = strings.TrimSpace(msg); msg != "" {
				return boundRunFailureText(msg)
			}
		}
	}
	if failureClass != nil {
		if class := strings.TrimSpace(*failureClass); class != "" {
			return humanFailureClass(class)
		}
	}
	return ""
}

// humanFailureClass turns a machine failure class into copy that tells the
// operator what to do next. Only mapped classes and the class token itself ever
// reach the UI — never user content.
func humanFailureClass(class string) string {
	switch strings.ToLower(strings.TrimSpace(class)) {
	case "deterministic":
		return "The agent hit an unrecoverable error"
	case "retryable":
		return "Transient error; retry the run"
	case "human":
		return "Needs human review"
	case "quota":
		return "Provider quota exceeded"
	case "capability":
		return "Agent lacks the required capability"
	case "protocol":
		return "Run ended without completing its work"
	case "timeout":
		return "Run timed out"
	case "budget":
		return "Project budget exhausted"
	case "", "unknown":
		return "Run failed"
	default:
		return humanRunToken(class)
	}
}

// boundRunFailureText redacts secrets, collapses the message to a single line,
// and caps it at maxRunFailureTextRunes. Redaction runs before truncation so a
// cut can never leave half a credential visible.
func boundRunFailureText(text string) string {
	text = redactRunFailureText(text)
	text = strings.Join(strings.Fields(text), " ")
	runes := []rune(text)
	if len(runes) <= maxRunFailureTextRunes {
		return text
	}
	return string(runes[:maxRunFailureTextRunes-1]) + "…"
}

// redactRunFailureText replaces common credential shapes and secret-named query
// parameter values with [redacted]. The query-parameter rule is limited to URL
// `?`/`&` pairs so ordinary prose mentioning "token" is left alone.
func redactRunFailureText(text string) string {
	for _, re := range runFailureSecretReplacers {
		text = re.ReplaceAllString(text, "[redacted]")
	}
	return runFailureQuerySecretRe.ReplaceAllString(text, "${1}[redacted]")
}
