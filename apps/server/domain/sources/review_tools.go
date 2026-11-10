package sources

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/emergent-company/emergent.memory/domain/githubapp"
	"github.com/emergent-company/emergent.memory/domain/mcp"
	"github.com/emergent-company/emergent.memory/internal/config"
)

const (
	// toolPRDiff and toolReviewSubmit are agent-facing names pinned by the
	// static catalog, agentcompat's tool namer and the security-review
	// blueprint. They must not change when the executor moves (#579).
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

// ReviewToolHandler executes the two agent review tools against a
// github_app source: it resolves (project, repo) to a source through the
// kb.sources repo allowlist, reads the App key from kb.source_apps, mints an
// installation token, and then runs the table-independent diff/submit logic.
//
// It implements mcp.GitHubAppToolHandler so the tools keep the same catalog
// names, AgentOnly property and TrustedInternal gate they had under
// domain/githubapp (#579).
type ReviewToolHandler struct {
	repo       *Repository
	key        []byte // raw AES-256 key bytes (LLM_ENCRYPTION_KEY)
	apiBaseURL string
	minter     *githubapp.TokenMinter
	newClient  func(baseURL, token string) *githubapp.Client
}

// NewReviewToolHandler builds the handler from the deployment config. The
// credential key is the same LLM_ENCRYPTION_KEY the source dispatcher uses to
// open kb.source_apps blobs, not the removed GITHUB_APP_ENCRYPTION_KEY.
func NewReviewToolHandler(repo *Repository, cfg *config.Config, log *slog.Logger) *ReviewToolHandler {
	keyHex, baseURL := "", ""
	if cfg != nil {
		keyHex = cfg.LLMProvider.EncryptionKey
		baseURL = cfg.GitHubSource.APIBaseURL
	}
	key, err := hex.DecodeString(keyHex)
	if err != nil {
		if log != nil {
			log.Error("sources: invalid LLM_ENCRYPTION_KEY; review tool source app credentials are unreadable", "error", err)
		}
		key = nil
	}
	return &ReviewToolHandler{
		repo:       repo,
		key:        key,
		apiBaseURL: baseURL,
		minter:     githubapp.NewTokenMinter(baseURL, nil),
		newClient: func(baseURL, token string) *githubapp.Client {
			return githubapp.NewClient(baseURL, token, nil)
		},
	}
}

// HandlesGitHubAppTool reports whether name belongs to this handler.
func (h *ReviewToolHandler) HandlesGitHubAppTool(name string) bool {
	return name == toolPRDiff || name == toolReviewSubmit
}

// ExecuteGitHubAppTool resolves the repo's github_app source, mints a token
// scoped to that repo, and runs the review tool.
//
// Run metadata: the mcp dispatch context carries the run id but not the run's
// TriggerMetadata, so the run's sourceId is not reachable here. Resolution is
// therefore the (project, repo) allowlist lookup, and it fails closed on zero
// or multiple candidates instead of guessing (#579, infra #1111).
func (h *ReviewToolHandler) ExecuteGitHubAppTool(ctx context.Context, projectID, toolName string, args map[string]any) (*mcp.ToolResult, error) {
	if !h.HandlesGitHubAppTool(toolName) {
		return nil, fmt.Errorf("sources: unsupported review tool %q", toolName)
	}
	projectUUID, err := uuid.Parse(projectID)
	if err != nil {
		return nil, fmt.Errorf("sources: review tool: invalid project id")
	}
	repo := stringArg(args, "repo")
	if repo == "" {
		return nil, fmt.Errorf("sources: review tool: repo is required")
	}
	src, app, err := h.resolveSource(ctx, projectUUID, repo)
	if err != nil {
		return nil, err
	}
	installationID, err := strconv.ParseInt(*src.ExternalAccountID, 10, 64)
	if err != nil || installationID <= 0 {
		return nil, fmt.Errorf("sources: github_app source %s has a non-numeric installation id %q; cannot mint a token",
			src.ID, *src.ExternalAccountID)
	}
	if len(h.key) == 0 {
		return nil, ErrSourceEventNoCredentials
	}
	privateKey, err := OpenSourceBlob(h.key, app.PrivateKeyEncrypted)
	if err != nil {
		return nil, ErrSourceEventNoCredentials
	}
	token, err := h.minter.MintInstallationToken(ctx, app.AppID, privateKey, installationID, []string{repo},
		map[string]string{
			"contents":      "read",
			"pull_requests": "write",
			"issues":        "write",
			"checks":        "write",
		})
	if err != nil {
		return nil, err
	}
	client := h.newClient(h.apiBaseURL, token)

	switch toolName {
	case toolPRDiff:
		return h.executePRDiff(ctx, client, repo, stringArg(args, "head_sha"))
	case toolReviewSubmit:
		return h.executeReviewSubmit(ctx, client, repo, args)
	default:
		return nil, fmt.Errorf("sources: unsupported review tool %q", toolName)
	}
}

// resolveSource maps a repo to its single github_app source. Exactly one
// allowlist match is required: zero is "no source", more than one is ambiguous
// because the deployment may run two Apps against the same repo. Both fail
// closed with an error that names what was found.
func (h *ReviewToolHandler) resolveSource(ctx context.Context, projectID uuid.UUID, repo string) (*Source, *SourceApp, error) {
	candidates, err := h.repo.FindGitHubAppSourcesForRepo(ctx, projectID, repo)
	if err != nil {
		return nil, nil, fmt.Errorf("sources: resolve github_app source for %s: %w", repo, err)
	}
	switch len(candidates) {
	case 0:
		return nil, nil, fmt.Errorf("sources: no github_app source allows repo %s in this project", repo)
	case 1:
	default:
		return nil, nil, fmt.Errorf("sources: repo %s is allowlisted by %d github_app sources in this project (%s); refusing to guess",
			repo, len(candidates), namedSourceCandidates(candidates))
	}
	src := candidates[0]
	if src.AppID == nil {
		return nil, nil, fmt.Errorf("sources: github_app source %s has no app", src.ID)
	}
	if src.ExternalAccountID == nil || strings.TrimSpace(*src.ExternalAccountID) == "" {
		return nil, nil, fmt.Errorf("sources: github_app source %s has no installation id", src.ID)
	}
	app, err := h.repo.FindSourceAppByID(ctx, *src.AppID)
	if err != nil {
		return nil, nil, fmt.Errorf("sources: github_app source %s references an unreadable app", src.ID)
	}
	return &src, app, nil
}

// namedSourceCandidates renders the ambiguity error's source ids/names.
func namedSourceCandidates(candidates []Source) string {
	parts := make([]string, 0, len(candidates))
	for _, src := range candidates {
		if name := strings.TrimSpace(src.DisplayName); name != "" {
			parts = append(parts, fmt.Sprintf("%s (%s)", src.ID, name))
			continue
		}
		parts = append(parts, src.ID.String())
	}
	return strings.Join(parts, ", ")
}

// executePRDiff is moved verbatim from domain/githubapp/tools.go: it validates
// the commit SHA, records run-scoped evidence for a non-empty diff, and
// truncates large payloads.
func (h *ReviewToolHandler) executePRDiff(ctx context.Context, client *githubapp.Client, repo, headSHA string) (*mcp.ToolResult, error) {
	if headSHA == "" {
		return nil, fmt.Errorf("github app tool: head_sha is required")
	}
	if !validCommitSHA(headSHA) {
		return nil, fmt.Errorf("github app tool: head_sha %q is not a commit SHA (40 lowercase hex)", headSHA)
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

// executeReviewSubmit is moved verbatim from domain/githubapp/tools.go: it
// enforces the live-head binding and the same-run evidence rule, then posts the
// review, the reviewed check and the merge-guard marker with byte-exact text.
func (h *ReviewToolHandler) executeReviewSubmit(ctx context.Context, client *githubapp.Client, repo string, args map[string]any) (*mcp.ToolResult, error) {
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
		return nil, fmt.Errorf("github app tool: head_sha %q is not a commit SHA (40 lowercase hex)", headSHA)
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
