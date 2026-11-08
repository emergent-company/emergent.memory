package feedback

import (
	"fmt"
	"strings"
	"time"
	"unicode"
)

// issueInput is everything the issue renderer needs. It is a plain value so the
// rendering can be unit-tested without a database or HTTP server.
type issueInput struct {
	Submission    Submission
	ProjectName   string
	OrgID         string
	Environment   string
	ServerVersion string
	ServerCommit  string
	Label         string
	SubmittedAt   time.Time
}

// issuePayload is the JSON body sent to GitHub.
type issuePayload struct {
	Title  string
	Body   string
	Labels []string
}

// buildIssue renders the GitHub issue for one submission.
//
// The message is the user's own text and is rendered verbatim (after control
// characters are removed). Everything else is server-derived and safe to place
// in a markdown table after escaping.
func buildIssue(in issueInput) issuePayload {
	message := sanitizeMessage(in.Submission.Message)

	title := "[feedback] " + summarize(message)
	label := strings.TrimSpace(in.Label)

	return issuePayload{
		Title:  title,
		Body:   renderIssueBody(in, message),
		Labels: labelList(label),
	}
}

func labelList(label string) []string {
	if label == "" {
		return nil
	}
	return []string{label}
}

func renderIssueBody(in issueInput, message string) string {
	var b strings.Builder

	b.WriteString("## User feedback\n\n")
	b.WriteString(message)
	b.WriteString("\n\n---\n\n### Submission context\n\n")
	b.WriteString("| Field | Value |\n| --- | --- |\n")

	rows := [][2]string{
		{"Submitted by", submitterCell(in.Submission)},
		{"Category", codeCell(in.Submission.Category)},
		{"Severity", codeCell(defaultString(in.Submission.Severity, "unspecified"))},
		{"Surface", surfaceCell(in.Submission.Source)},
	}
	if in.Submission.IncludeContext {
		rows = append(rows,
			[2]string{"Project", projectCell(in.Submission.ProjectID, in.ProjectName)},
			[2]string{"Organization", codeCell(defaultString(in.OrgID, "unknown"))},
			[2]string{"Client", cell(defaultString(in.Submission.Client, "unknown"))},
			[2]string{"Environment", codeCell(defaultString(in.Environment, "unknown"))},
			[2]string{"Server", serverCell(in.ServerVersion, in.ServerCommit)},
		)
	}
	rows = append(rows, [2]string{"Submitted at", codeCell(in.SubmittedAt.UTC().Format(time.RFC3339))})

	for _, row := range rows {
		fmt.Fprintf(&b, "| %s | %s |\n", row[0], row[1])
	}

	b.WriteString("\n---\n_")
	b.WriteString("Filed automatically by Memory's feedback intake from an authenticated user or agent. ")
	b.WriteString("The message is user-authored; identity and context are attached by the server.\n")
	return b.String()
}

func submitterCell(sub Submission) string {
	if sub.UserEmail != "" && sub.UserID != "" {
		return fmt.Sprintf("%s (`%s`)", cell(sub.UserEmail), sub.UserID)
	}
	if sub.UserEmail != "" {
		return cell(sub.UserEmail)
	}
	if sub.UserID != "" {
		return fmt.Sprintf("`%s`", sub.UserID)
	}
	return "unknown"
}

func projectCell(projectID, name string) string {
	switch {
	case projectID != "" && name != "":
		return fmt.Sprintf("%s (`%s`)", cell(name), projectID)
	case projectID != "":
		return fmt.Sprintf("`%s`", projectID)
	case name != "":
		return cell(name)
	default:
		return "none (account-level submission)"
	}
}

func serverCell(version, commit string) string {
	switch {
	case version != "" && commit != "" && commit != "unknown":
		return fmt.Sprintf("`%s` (`%s`)", version, commit)
	case version != "":
		return fmt.Sprintf("`%s`", version)
	default:
		return "unknown"
	}
}

func surfaceCell(source Source) string {
	if source == SourceMCP {
		return "MCP tool `feedback-submit`"
	}
	return "REST API `POST /api/feedback`"
}

func codeCell(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "unknown"
	}
	return "`" + strings.ReplaceAll(value, "`", "'") + "`"
}

// cell makes a value safe for a single markdown table cell.
func cell(value string) string {
	value = strings.Join(strings.Fields(value), " ")
	value = strings.ReplaceAll(value, "|", `\|`)
	if value == "" {
		return "unknown"
	}
	return value
}

func defaultString(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

// sanitizeMessage drops control characters other than newline and tab, and
// normalises CRLF. It does not otherwise modify the user's text.
func sanitizeMessage(message string) string {
	message = strings.ReplaceAll(message, "\r\n", "\n")
	message = strings.ReplaceAll(message, "\r", "\n")
	return strings.Map(func(r rune) rune {
		if r == '\n' || r == '\t' {
			return r
		}
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, message)
}

// summarize returns the first non-empty line, bounded so the issue title stays
// readable in a list.
func summarize(message string) string {
	const maxRunes = 72
	for _, line := range strings.Split(message, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		runes := []rune(line)
		if len(runes) > maxRunes {
			return strings.TrimSpace(string(runes[:maxRunes])) + "…"
		}
		return line
	}
	return "user feedback"
}
