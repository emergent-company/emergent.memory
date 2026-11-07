package skills

import (
	"fmt"
	"regexp"
	"strings"
)

// Lint severities.
const (
	LintSeverityError   = "error"
	LintSeverityWarning = "warning"
)

// LintFinding is one advisory result from the skill linter or security scan.
type LintFinding struct {
	Severity string `json:"severity"`
	Code     string `json:"code"`
	Message  string `json:"message"`
}

// LintResult aggregates findings. Errors block a write or an auto-apply;
// warnings are surfaced to the reviewer and recorded on the review row.
type LintResult struct {
	Findings []LintFinding `json:"findings,omitempty"`
	Errors   int           `json:"errors"`
	Warnings int           `json:"warnings"`
}

// Add appends a finding and bumps the matching counter.
func (r *LintResult) Add(severity, code, message string) {
	r.Findings = append(r.Findings, LintFinding{Severity: severity, Code: code, Message: message})
	if severity == LintSeverityError {
		r.Errors++
	} else {
		r.Warnings++
	}
}

// Passed reports whether no error-severity finding was recorded.
func (r LintResult) Passed() bool { return r.Errors == 0 }

// HasWarnings reports whether any warning was recorded.
func (r LintResult) HasWarnings() bool { return r.Warnings > 0 }

var (
	datePattern      = regexp.MustCompile(`\b(19|20)\d{2}-\d{2}-\d{2}\b`)
	issueRefPattern  = regexp.MustCompile(`(^|\s)#\d{2,}\b`)
	prRefPattern     = regexp.MustCompile(`(?i)\b(PR|pull request)\s*#?\d+\b`)
	placeholderRegex = regexp.MustCompile(`(?i)\b(TODO|TBD|FIXME|XXX|PLACEHOLDER)\b`)
	absPathPattern   = regexp.MustCompile(`(^|\s)(/root/|/Users/|/home/[a-z]+/|C:\\Users\\)`)
	chatQuotePattern = regexp.MustCompile(`(?i)\b(as (we )?(discussed|said) (in|on) (the )?(chat|slack|call)|quoted (message|conversation))\b`)

	// Security scan patterns.
	privateKeyPattern = regexp.MustCompile(`-----BEGIN (?:[A-Z ]+ )?PRIVATE KEY-----`)
	awsKeyPattern     = regexp.MustCompile(`\bAKIA[0-9A-Z]{16}\b`)
	githubTokenRegex  = regexp.MustCompile(`\bgh[pousr]_[A-Za-z0-9]{20,}\b`)
	openAIKeyRegex    = regexp.MustCompile(`\bsk-[A-Za-z0-9_-]{20,}\b`)
	slackTokenRegex   = regexp.MustCompile(`\bxox[baprs]-[A-Za-z0-9-]{10,}\b`)
	secretAssignRegex = regexp.MustCompile(`(?i)\b(password|passwd|secret|api[_-]?key|access[_-]?token|auth[_-]?token)\s*[:=]\s*["']?[A-Za-z0-9/+_=.-]{8,}`)
	bearerRegex       = regexp.MustCompile(`(?i)authorization:\s*bearer\s+[A-Za-z0-9._-]{10,}`)
	ignoredInstrRegex = regexp.MustCompile(`(?i)ignore\s+(all\s+|any\s+)?(previous|prior|above)\s+instructions`)
	exfiltrateRegex   = regexp.MustCompile(`(?i)\bexfiltrat(e|ion)\b`)
	curlPipeRegex     = regexp.MustCompile(`(?i)\b(curl|wget)\b[^\n|]{0,200}\|\s*(ba)?sh\b`)
	revealPromptRegex = regexp.MustCompile(`(?i)\b(reveal|print|dump)\b[^\n]{0,40}\bsystem prompt\b`)
	base64BlobRegex   = regexp.MustCompile(`[A-Za-z0-9+/]{200,}={0,2}`)
)

// LintSkill runs the deterministic, advisory linter on a candidate skill.
// maxBytes <= 0 falls back to DefaultMaxContentSize.
func LintSkill(name, description, content string, maxBytes int) LintResult {
	var r LintResult
	if maxBytes <= 0 {
		maxBytes = DefaultMaxContentSize
	}

	if err := ValidateSkillName(name); err != nil {
		r.Add(LintSeverityError, "invalid_name", err.Error())
	}
	if strings.TrimSpace(description) == "" {
		r.Add(LintSeverityError, "missing_description", "description is required")
	} else if len(description) > 500 {
		r.Add(LintSeverityWarning, "long_description", "description should stay under 500 characters")
	}
	if strings.TrimSpace(content) == "" {
		r.Add(LintSeverityError, "empty_content", "content is required")
		return r
	}
	if len(content) > maxBytes {
		r.Add(LintSeverityError, "content_too_large",
			fmt.Sprintf("content is %d bytes, cap is %d", len(content), maxBytes))
	}

	if !strings.HasPrefix(strings.TrimSpace(content), "#") {
		r.Add(LintSeverityWarning, "missing_heading", "start the skill with a Markdown heading")
	}
	if !strings.Contains(content, "\n## ") {
		r.Add(LintSeverityWarning, "missing_sections", "break the skill into ## sections a future agent can follow")
	}
	if placeholderRegex.MatchString(content) {
		r.Add(LintSeverityWarning, "placeholder_text", "remove TODO/TBD/FIXME placeholders before publishing")
	}
	if datePattern.MatchString(content) {
		r.Add(LintSeverityWarning, "contains_dates", "prefer durable lessons over dates that go stale")
	}
	if issueRefPattern.MatchString(content) || prRefPattern.MatchString(content) {
		r.Add(LintSeverityWarning, "contains_ticket_refs", "replace PR/issue numbers with the general rule they taught")
	}
	if chatQuotePattern.MatchString(content) {
		r.Add(LintSeverityWarning, "quoted_chat", "summarise the lesson instead of quoting the conversation")
	}
	if absPathPattern.MatchString(content) {
		r.Add(LintSeverityWarning, "absolute_path", "avoid machine-specific absolute paths in a portable skill")
	}

	return r
}

// ScanSkill runs the deterministic security scan. An error-severity finding
// blocks agent-initiated writes and always blocks auto-apply.
func ScanSkill(name, content string) LintResult {
	var r LintResult
	add := func(code, message string) { r.Add(LintSeverityError, code, message) }

	if privateKeyPattern.MatchString(content) {
		add("private_key", "content contains a private key block")
	}
	if awsKeyPattern.MatchString(content) {
		add("aws_access_key", "content looks like it contains an AWS access key")
	}
	if githubTokenRegex.MatchString(content) {
		add("github_token", "content looks like it contains a GitHub token")
	}
	if openAIKeyRegex.MatchString(content) {
		add("api_key", "content looks like it contains an API key")
	}
	if slackTokenRegex.MatchString(content) {
		add("slack_token", "content looks like it contains a Slack token")
	}
	if bearerRegex.MatchString(content) {
		add("bearer_token", "content contains an Authorization: Bearer header")
	}
	if secretAssignRegex.MatchString(content) {
		add("secret_assignment", "content assigns what looks like a secret or credential")
	}
	if ignoredInstrRegex.MatchString(content) {
		add("prompt_injection", "content asks a reader to ignore earlier instructions")
	}
	if exfiltrateRegex.MatchString(content) {
		add("exfiltration", "content talks about exfiltrating data")
	}
	if revealPromptRegex.MatchString(content) {
		r.Add(LintSeverityWarning, "prompt_disclosure", "content asks to reveal a system prompt")
	}
	if curlPipeRegex.MatchString(content) {
		r.Add(LintSeverityWarning, "pipe_to_shell", "content pipes a download straight into a shell")
	}
	if base64BlobRegex.MatchString(content) {
		r.Add(LintSeverityWarning, "base64_blob", "content contains a long opaque blob")
	}

	return r
}
