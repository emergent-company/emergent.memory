package feedback

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func fixedIssueInput() issueInput {
	return issueInput{
		Submission: Submission{
			Message:        "It broke\n\nWhen I called the tool it returned an error.",
			Category:       "bug",
			Severity:       "high",
			IncludeContext: true,
			Source:         SourceMCP,
			Client:         "Claude Desktop | 1.0",
			UserID:         "user-1",
			UserEmail:      "ada@example.com",
			ProjectID:      "project-1",
		},
		ProjectName:   "Docs | Demo",
		OrgID:         "org-1",
		Environment:   "production",
		ServerVersion: "1.2.3",
		ServerCommit:  "abc1234",
		Label:         "feedback",
		SubmittedAt:   time.Date(2026, 11, 8, 12, 34, 56, 0, time.UTC),
	}
}

func TestBuildIssueTitleUsesFirstLine(t *testing.T) {
	payload := buildIssue(fixedIssueInput())
	require.Equal(t, "[feedback] It broke", payload.Title)
	require.Equal(t, []string{"feedback"}, payload.Labels)
}

func TestBuildIssueBodyCarriesWhoAndWhere(t *testing.T) {
	payload := buildIssue(fixedIssueInput())
	body := payload.Body

	require.Contains(t, body, "ada@example.com")
	require.Contains(t, body, "`user-1`")
	require.Contains(t, body, "Docs \\| Demo")
	require.Contains(t, body, "`project-1`")
	require.Contains(t, body, "`org-1`")
	require.Contains(t, body, "`production`")
	require.Contains(t, body, "`1.2.3` (`abc1234`)")
	require.Contains(t, body, "MCP tool `feedback-submit`")
	require.Contains(t, body, "Claude Desktop \\| 1.0")
	require.Contains(t, body, "2026-11-08T12:34:56Z")
}

func TestBuildIssueWithoutContextDropsTechnicalRows(t *testing.T) {
	in := fixedIssueInput()
	in.Submission.IncludeContext = false
	body := buildIssue(in).Body

	require.Contains(t, body, "ada@example.com")
	require.NotContains(t, body, "`project-1`")
	require.NotContains(t, body, "Docs \\| Demo")
	require.NotContains(t, body, "`production`")
	require.NotContains(t, body, "Claude Desktop")
	require.NotContains(t, body, "abc1234")
}

func TestBuildIssueNoLabel(t *testing.T) {
	in := fixedIssueInput()
	in.Label = ""
	require.Empty(t, buildIssue(in).Labels)
}

func TestBuildIssueTruncatesTitle(t *testing.T) {
	in := fixedIssueInput()
	in.Submission.Message = strings.Repeat("a", 200)
	title := buildIssue(in).Title
	require.LessOrEqual(t, len([]rune(strings.TrimPrefix(title, "[feedback] "))), 73)
}

func TestSanitizeMessageRemovesControlCharacters(t *testing.T) {
	got := sanitizeMessage("a\x00b\rc\nd\te")
	require.Equal(t, "ab\nc\nd\te", got)
	require.NotContains(t, got, "\x00")
}

func TestSummarizeFallsBackWhenEmpty(t *testing.T) {
	require.Equal(t, "user feedback", summarize("\n\n"))
}

func TestBuildIssueEscapesPipesInIdentifiers(t *testing.T) {
	in := fixedIssueInput()
	in.Submission.UserID = "user|1"
	in.Submission.ProjectID = "project|1"
	in.OrgID = "org|1"

	body := buildIssue(in).Body

	// A pipe ends the table cell even inside a code span, so an identifier that
	// carries one is escaped rather than left to split the row.
	require.Contains(t, body, "`user\\|1`")
	require.Contains(t, body, "`project\\|1`")
	require.Contains(t, body, "`org\\|1`")
}
