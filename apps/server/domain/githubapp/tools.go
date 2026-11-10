package githubapp

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/emergent-company/emergent.memory/domain/mcp"
)

const (
	toolPRDiff       = "github-pr-diff"
	toolReviewSubmit = "github-review-submit"

	// maxPatchBytes bounds each file patch and the whole diff result so a large
	// commit cannot blow up the agent context.
	maxPatchBytes = 6000
	maxDiffBytes  = 200000
)

// commitSHARe pins head_sha to a commit id before it reaches a URL path or the
// review API. A ref name ("main") must never be accepted: the diff and the
// review are bound to one immutable commit, not to whatever a branch points at
// when the request lands.
var commitSHARe = regexp.MustCompile(`^[0-9a-f]{40}$`)

func validCommitSHA(sha string) bool {
	return commitSHARe.MatchString(sha)
}

// diffEvidenceKey names the run-scoped fact "github-pr-diff returned a non-empty
// diff for repo@sha". executeReviewSubmit requires it so a review verdict can
// only be posted for a commit this run actually read.
func diffEvidenceKey(repo, sha string) string {
	return "githubapp:diff:" + strings.ToLower(repo) + "@" + sha
}

// truncateUTF8 cuts s to at most max bytes without splitting a UTF-8 sequence.
func truncateUTF8(s string, max int) string {
	if len(s) <= max {
		return s
	}
	for max > 0 && !utf8.RuneStart(s[max]) {
		max--
	}
	return s[:max]
}

// HandlesGitHubAppTool reports whether name belongs to this handler.
func (s *Service) HandlesGitHubAppTool(name string) bool {
	return name == toolPRDiff || name == toolReviewSubmit
}

// ExecuteGitHubAppTool runs a review tool with the project's installation token.
func (s *Service) ExecuteGitHubAppTool(ctx context.Context, projectID, toolName string, args map[string]any) (*mcp.ToolResult, error) {
	projectUUID, err := uuid.Parse(projectID)
	if err != nil {
		return nil, fmt.Errorf("github app tool: invalid project id")
	}
	repo := stringArg(args, "repo")
	if repo == "" {
		return nil, fmt.Errorf("github app tool: repo is required")
	}
	inst, err := s.repo.FindEnabledInstallationForRepo(ctx, projectUUID, repo)
	if err != nil {
		return nil, fmt.Errorf("github app tool: no installation for %s", repo)
	}
	app, err := s.repo.FindAppByID(ctx, inst.AppPK)
	if err != nil {
		return nil, fmt.Errorf("github app tool: installation is not readable")
	}
	token, err := s.mintToken(ctx, app, inst, repo)
	if err != nil {
		return nil, err
	}
	client := s.newClient(s.apiBaseURL, token)

	switch toolName {
	case toolPRDiff:
		return s.executePRDiff(ctx, client, repo, stringArg(args, "head_sha"))
	case toolReviewSubmit:
		return s.executeReviewSubmit(ctx, client, repo, args)
	default:
		return nil, fmt.Errorf("github app tool: unsupported tool %q", toolName)
	}
}

func (s *Service) executePRDiff(ctx context.Context, client *Client, repo, headSHA string) (*mcp.ToolResult, error) {
	if headSHA == "" {
		return nil, fmt.Errorf("github app tool: head_sha is required")
	}
	if !validCommitSHA(headSHA) {
		return nil, fmt.Errorf("github app tool: head_sha %q is not a commit SHA (7-40 lowercase hex)", headSHA)
	}
	diff, err := client.GetCommit(ctx, repo, headSHA)
	if err != nil {
		return nil, err
	}
	// Record that this run saw a real (non-empty) diff for this commit. The
	// review submit tool requires this evidence so a prompt-injected run cannot
	// post a verdict without reading the change it approves.
	if len(diff.Files) > 0 {
		mcp.MarkRunEvidence(ctx, diffEvidenceKey(repo, diff.SHA))
	}
	var b strings.Builder
	fmt.Fprintf(&b, "commit %s\n%s\n\n", diff.SHA, strings.TrimSpace(diff.Message))
	for _, file := range diff.Files {
		patch := file.Patch
		if len(patch) > maxPatchBytes {
			patch = truncateUTF8(patch, maxPatchBytes) + "\n… patch truncated"
		}
		fmt.Fprintf(&b, "--- %s (%s, +%d/-%d)\n%s\n\n", file.Filename, file.Status, file.Additions, file.Deletions, patch)
		if b.Len() >= maxDiffBytes {
			b.WriteString("… diff truncated\n")
			break
		}
	}
	text := b.String()
	if len(text) > maxDiffBytes {
		text = truncateUTF8(text, maxDiffBytes) + "\n… diff truncated"
	}
	return &mcp.ToolResult{
		Content: []mcp.ContentBlock{{Type: "text", Text: text}},
		StructuredContent: map[string]any{
			"sha":   diff.SHA,
			"files": len(diff.Files),
		},
	}, nil
}

func (s *Service) executeReviewSubmit(ctx context.Context, client *Client, repo string, args map[string]any) (*mcp.ToolResult, error) {
	number, ok := intArg(args, "number")
	if !ok || number <= 0 {
		return nil, fmt.Errorf("github app tool: number is required")
	}
	headSHA := stringArg(args, "head_sha")
	verdict := strings.ToUpper(strings.TrimSpace(stringArg(args, "verdict")))
	summary := strings.TrimSpace(stringArg(args, "summary"))
	if headSHA == "" || summary == "" {
		return nil, fmt.Errorf("github app tool: head_sha and summary are required")
	}
	if !validCommitSHA(headSHA) {
		return nil, fmt.Errorf("github app tool: head_sha %q is not a commit SHA (7-40 lowercase hex)", headSHA)
	}
	var event, conclusion, checkTitle string
	switch verdict {
	case "MERGE":
		event, conclusion, checkTitle = "APPROVE", "success", "reviewed: approved"
	case "CHANGES_REQUESTED":
		event, conclusion, checkTitle = "REQUEST_CHANGES", "failure", "reviewed: changes requested"
	default:
		return nil, fmt.Errorf("github app tool: verdict must be MERGE or CHANGES_REQUESTED")
	}

	state, err := client.GetPullRequest(ctx, repo, number)
	if err != nil {
		return nil, err
	}
	if state.HeadSHA != headSHA {
		return nil, fmt.Errorf("github app tool: PR head is %s, not the reviewed %s; re-review the new head", state.HeadSHA, headSHA)
	}
	// Same-run binding: the verdict may only name a commit whose non-empty diff
	// this run read through github-pr-diff. A prompt injection in PR text cannot
	// make the agent approve a change it never fetched (and an empty diff has
	// nothing to approve). Fail closed when the run carries no evidence.
	if !mcp.RunHasEvidence(ctx, diffEvidenceKey(repo, headSHA)) {
		return nil, fmt.Errorf("github app tool: no non-empty github-pr-diff result for %s@%s in this run; fetch the diff for the exact reviewed commit before submitting", repo, headSHA)
	}

	if err := client.CreateReview(ctx, repo, number, headSHA, event, summary); err != nil {
		return nil, err
	}
	if err := client.CreateReviewedCheck(ctx, repo, headSHA, conclusion, checkTitle, summary); err != nil {
		return nil, err
	}
	marker := fmt.Sprintf("Reviewed-SHA: %s\nVerdict: %s\n\n%s", headSHA, verdict, summary)
	if err := client.CreateIssueComment(ctx, repo, number, marker); err != nil {
		return nil, err
	}
	return &mcp.ToolResult{
		Content: []mcp.ContentBlock{{Type: "text", Text: fmt.Sprintf("%s posted for %s#%d at %s", verdict, repo, number, headSHA)}},
		StructuredContent: map[string]any{
			"repo":       repo,
			"number":     number,
			"headSha":    headSHA,
			"verdict":    verdict,
			"event":      event,
			"conclusion": conclusion,
		},
	}, nil
}

func stringArg(args map[string]any, key string) string {
	if v, ok := args[key].(string); ok {
		return strings.TrimSpace(v)
	}
	return ""
}

func intArg(args map[string]any, key string) (int, bool) {
	switch v := args[key].(type) {
	case float64:
		return int(v), true
	case int:
		return v, true
	case int64:
		return int(v), true
	case json.Number:
		n, err := v.Int64()
		return int(n), err == nil
	case string:
		n, err := strconv.Atoi(v)
		return n, err == nil
	default:
		return 0, false
	}
}
