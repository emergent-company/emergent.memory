package sources

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/emergent-company/emergent.memory/domain/agents"
	"github.com/emergent-company/emergent.memory/domain/githubapp"
	"github.com/emergent-company/emergent.memory/domain/graph"
	"github.com/emergent-company/emergent.memory/domain/mcp"
	"github.com/emergent-company/emergent.memory/domain/provider"
	"github.com/emergent-company/emergent.memory/internal/config"
	"github.com/emergent-company/emergent.memory/pkg/apperror"
	"github.com/emergent-company/emergent.memory/pkg/auth"
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
// diff for repo#number@sha". executeReviewSubmit requires it so a review verdict
// can only be posted for a pull request this run actually read.
//
// The reviewed object is the pull request (repo#number at a head SHA), not one
// commit's first-parent diff: a merge commit can have an empty first-parent diff
// while the pull request changes hundreds of files (#1022). The key carries the
// number so evidence read for PR #1 can never justify a verdict on PR #2.
func diffEvidenceKey(repo string, number int, sha string) string {
	return fmt.Sprintf("githubapp:prdiff:%s#%d@%s", strings.ToLower(repo), number, sha)
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
	repo        *Repository
	key         []byte // raw AES-256 key bytes (LLM_ENCRYPTION_KEY)
	apiBaseURL  string
	minter      *githubapp.TokenMinter
	newClient   func(baseURL, token string) *githubapp.Client
	workItems   WorkItemStatusWriter
	links       IssuePRLinkReader
	reworker    WorkItemReworker
	reviewGates *ReviewGateService
	agentRuns   *agents.Repository
	log         *slog.Logger
}

// WorkItemStatusWriter finalizes the board lane a review run was dispatched for.
type WorkItemStatusWriter interface {
	UpdateWorkObjectStatus(ctx context.Context, projectID, canonicalID, status, reviewStatus string) (bool, error)
}

// WorkItemReworker transitions a linked issue to revision and enqueues its
// assigned coding agent with the review feedback.
type WorkItemReworker interface {
	RequestWorkItemChanges(ctx context.Context, projectID, canonicalID, author, text string) error
}

// IssuePRLinkReader reads the server-owned issue/PR link.
type IssuePRLinkReader interface {
	FindSourceIssuePRLink(ctx context.Context, projectID uuid.UUID, repo string, prNumber int) (*SourceIssuePRLink, error)
}

// SetWorkItemStatusWriter injects the graph work-object writer.
func (h *ReviewToolHandler) SetWorkItemStatusWriter(w WorkItemStatusWriter) { h.workItems = w }

// SetWorkItemReworker injects the coding rework dispatcher.
func (h *ReviewToolHandler) SetWorkItemReworker(r WorkItemReworker) { h.reworker = r }

// SetReviewGateService injects the server-owned review aggregator.
func (h *ReviewToolHandler) SetReviewGateService(g *ReviewGateService) { h.reviewGates = g }

// SetAgentRuns injects the run reader used to identify the reviewer agent.
func (h *ReviewToolHandler) SetAgentRuns(r *agents.Repository) { h.agentRuns = r }

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
		links:      repo,
		key:        key,
		apiBaseURL: baseURL,
		minter:     githubapp.NewTokenMinter(baseURL, nil),
		newClient: func(baseURL, token string) *githubapp.Client {
			return githubapp.NewClient(baseURL, token, nil)
		},
		log: log,
	}
}

// HandlesGitHubAppTool reports whether name belongs to this handler.
func (h *ReviewToolHandler) HandlesGitHubAppTool(name string) bool {
	return name == toolPRDiff || name == toolReviewSubmit
}

// MintSourceCredential mints a short-lived installation token for a verified
// source run. It is the runtime half of AgentCredentialRef: the trigger names
// the source and permissions, and this method resolves the App key, validates
// the repo allowlist and mints the scoped token. The token is returned to the
// caller and never persisted.
func (h *ReviewToolHandler) MintSourceCredential(ctx context.Context, projectID, sourceID, repo string, permissions map[string]string) (string, error) {
	projectUUID, err := uuid.Parse(strings.TrimSpace(projectID))
	if err != nil {
		return "", apperror.NewValidation("sources: credential: invalid project id")
	}
	sourceUUID, err := uuid.Parse(strings.TrimSpace(sourceID))
	if err != nil || sourceUUID == uuid.Nil {
		return "", apperror.NewValidation("sources: credential: invalid source id")
	}
	repo = strings.TrimSpace(repo)
	if repo == "" {
		return "", apperror.NewValidation("sources: credential: repo is required")
	}
	src, app, err := h.resolveRunSource(ctx, projectUUID, repo, sourceUUID.String())
	if err != nil {
		return "", err
	}
	installationID, err := strconv.ParseInt(*src.ExternalAccountID, 10, 64)
	if err != nil || installationID <= 0 {
		return "", fmt.Errorf("sources: github_app source %s has a non-numeric installation id", src.ID)
	}
	if len(h.key) == 0 {
		return "", ErrSourceEventNoCredentials
	}
	privateKey, err := OpenSourceBlob(h.key, app.PrivateKeyEncrypted)
	if err != nil {
		return "", ErrSourceEventNoCredentials
	}
	perms, err := normalizeCredentialPermissions(permissions)
	if err != nil {
		return "", err
	}
	return h.minter.MintInstallationToken(ctx, app.AppID, privateKey, installationID, []string{repo}, perms)
}

func normalizeCredentialPermissions(in map[string]string) (map[string]string, error) {
	allowed := map[string]struct{}{
		"contents": {}, "pull_requests": {}, "issues": {}, "checks": {},
	}
	out := make(map[string]string, len(in))
	for name, value := range in {
		if _, ok := allowed[name]; !ok {
			return nil, apperror.NewValidationf("sources: credential permission %q is not allowed", name)
		}
		switch value {
		case "read", "write":
		default:
			return nil, apperror.NewValidationf("sources: credential permission %q must be read or write", name)
		}
		out[name] = value
	}
	if len(out) == 0 {
		out["contents"] = "read"
	}
	return out, nil
}

// ExecuteGitHubAppTool resolves the repo's github_app source, mints a token
// scoped to that repo, and runs the review tool.
//
// The run's sourceId, when the agent executor injected it from TriggerMetadata,
// takes precedence and is validated against this project and repo. Without it,
// resolution falls back to the (project, repo) allowlist and fails closed on
// zero or multiple candidates instead of guessing (#579).
func (h *ReviewToolHandler) ExecuteGitHubAppTool(ctx context.Context, projectID, toolName string, args map[string]any) (*mcp.ToolResult, error) {
	if !h.HandlesGitHubAppTool(toolName) {
		return nil, apperror.NewValidationf("sources: unsupported review tool %q", toolName)
	}
	projectUUID, err := uuid.Parse(projectID)
	if err != nil {
		return nil, apperror.NewValidation("sources: review tool: invalid project id")
	}
	repo := stringArg(args, "repo")
	if repo == "" {
		return nil, apperror.NewValidation("sources: review tool: repo is required")
	}
	if err := validateReviewSubject(ctx, toolName, repo, args); err != nil {
		return nil, err
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
		return h.executePRDiff(ctx, client, projectID, repo, args)
	case toolReviewSubmit:
		return h.executeReviewSubmit(ctx, client, projectID, repo, args)
	default:
		return nil, apperror.NewValidationf("sources: unsupported review tool %q", toolName)
	}
}

// resolveSource maps a repo to its github_app source. The run's sourceId wins
// when the dispatch context carries one; only without it does the project repo
// allowlist act as the fallback.
//
// The sourceId path never falls back to another source: a run whose own source
// cannot serve its repo is a misconfiguration, and guessing could post a review
// through the wrong App. The fallback requires exactly one allowlist match;
// zero or many fail closed naming what was found.
func (h *ReviewToolHandler) resolveSource(ctx context.Context, projectID uuid.UUID, repo string) (*Source, *SourceApp, error) {
	// Prefer the dispatcher-persisted subject's source over the run metadata
	// sourceId, which is model-settable on spawned runs.
	if subject, ok := mcp.RunSubjectFromContext(ctx); ok && subject.SourceID != "" {
		return h.resolveRunSource(ctx, projectID, repo, subject.SourceID)
	}
	if sourceID := mcp.RunSourceIDFromContext(ctx); sourceID != "" {
		return h.resolveRunSource(ctx, projectID, repo, sourceID)
	}
	candidates, err := h.repo.FindGitHubAppSourcesForRepo(ctx, projectID, repo)
	if err != nil {
		return nil, nil, fmt.Errorf("sources: resolve github_app source for %s: %w", repo, err)
	}
	switch len(candidates) {
	case 0:
		return nil, nil, apperror.NewValidationf("sources: no github_app source allows repo %s in this project", repo)
	case 1:
	default:
		return nil, nil, apperror.NewValidationf("sources: repo %s is allowlisted by %d github_app sources in this project (%s); refusing to guess",
			repo, len(candidates), namedSourceCandidates(candidates))
	}
	src := candidates[0]
	app, err := h.resolveSourceApp(ctx, &src)
	if err != nil {
		return nil, nil, err
	}
	return &src, app, nil
}

// resolveRunSource loads the run's own source inside the project and validates
// that it can serve repo. Every mismatch fails closed naming the source and the
// reason; there is deliberately no fallback to the allowlist scan.
func (h *ReviewToolHandler) resolveRunSource(ctx context.Context, projectID uuid.UUID, repo, sourceID string) (*Source, *SourceApp, error) {
	id, err := uuid.Parse(sourceID)
	if err != nil {
		return nil, nil, fmt.Errorf("sources: run sourceId %q is not a valid source id", sourceID)
	}
	src, err := h.repo.GetByID(ctx, id, projectID)
	if err != nil {
		return nil, nil, fmt.Errorf("sources: run sourceId %s is not a source in this project: %w", id, err)
	}
	if src.Provider != ProviderTypeGitHubApp {
		return nil, nil, fmt.Errorf("sources: run sourceId %s is a %s source, not github_app", sourceLabel(src), src.Provider)
	}
	if src.Status == SourceStatusDisabled {
		return nil, nil, fmt.Errorf("sources: run sourceId %s is disabled", sourceLabel(src))
	}
	if !repoAllowed(src.Config, repo) {
		return nil, nil, fmt.Errorf("sources: run sourceId %s does not allow repo %s", sourceLabel(src), repo)
	}
	app, err := h.resolveSourceApp(ctx, src)
	if err != nil {
		return nil, nil, err
	}
	return src, app, nil
}

// resolveSourceApp validates the source's App/installation binding and loads
// the sealed key row. The cause is wrapped so the operator log keeps the DB
// error while the agent-facing text stays readable.
func (h *ReviewToolHandler) resolveSourceApp(ctx context.Context, src *Source) (*SourceApp, error) {
	if src.AppID == nil {
		return nil, fmt.Errorf("sources: github_app source %s has no app", sourceLabel(src))
	}
	if src.ExternalAccountID == nil || strings.TrimSpace(*src.ExternalAccountID) == "" {
		return nil, fmt.Errorf("sources: github_app source %s has no installation id", sourceLabel(src))
	}
	app, err := h.repo.FindSourceAppByID(ctx, *src.AppID)
	if err != nil {
		return nil, fmt.Errorf("sources: github_app source %s references an unreadable app: %w", sourceLabel(src), err)
	}
	return app, nil
}

// sourceLabel renders a source id with its display name for diagnostics.
func sourceLabel(src *Source) string {
	if name := strings.TrimSpace(src.DisplayName); name != "" {
		return fmt.Sprintf("%s (%s)", src.ID, name)
	}
	return src.ID.String()
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

func validateReviewSubject(ctx context.Context, toolName, repo string, args map[string]any) error {
	subject, ok := mcp.RunSubjectFromContext(ctx)
	if !ok {
		return nil
	}
	if !strings.EqualFold(repo, subject.Repo) {
		return apperror.NewValidationf("github app tool: repo %q does not match the triggering subject %q", repo, subject.Repo)
	}
	if head := stringArg(args, "head_sha"); head != "" && head != subject.HeadSHA {
		return apperror.NewValidationf("github app tool: head_sha %q does not match the triggering subject %q", head, subject.HeadSHA)
	}
	if number, ok := intArg(args, "number"); ok && number != subject.PRNumber {
		return apperror.NewValidationf("github app tool: PR number %d does not match the triggering subject #%d", number, subject.PRNumber)
	}
	return nil
}

// executePRDiff reads the diff of the reviewed object. A pull request number
// (from the call or the run's verified subject) selects the pull request diff,
// which is what evidence is keyed on; without one it serves the commit's
// first-parent diff for context only (#1022).
func (h *ReviewToolHandler) executePRDiff(ctx context.Context, client *githubapp.Client, projectID, repo string, args map[string]any) (*mcp.ToolResult, error) {
	headSHA := stringArg(args, "head_sha")
	if headSHA == "" {
		return nil, apperror.NewValidation("github app tool: head_sha is required")
	}
	if !validCommitSHA(headSHA) {
		return nil, apperror.NewValidationf("github app tool: head_sha %q is not a commit SHA (40 lowercase hex)", headSHA)
	}
	number, ok := intArg(args, "number")
	if !ok || number <= 0 {
		if subject, hasSubject := mcp.RunSubjectFromContext(ctx); hasSubject &&
			strings.EqualFold(subject.Repo, repo) && subject.HeadSHA == headSHA && subject.PRNumber > 0 {
			number, ok = subject.PRNumber, true
		}
	}
	if ok && number > 0 {
		return h.executePullRequestDiff(ctx, client, projectID, repo, number, headSHA)
	}
	return h.executeCommitDiff(ctx, client, repo, headSHA)
}

// executePullRequestDiff reads GET /pulls/{number}/files, the diff of the
// reviewed object. A non-empty result is the evidence a verdict is allowed to
// name. An empty result is a genuinely unapprovable head: the tool returns a
// validation error (never an internal one) and moves the review gate to a
// terminal state instead of leaving it pending forever.
func (h *ReviewToolHandler) executePullRequestDiff(ctx context.Context, client *githubapp.Client, projectID, repo string, number int, headSHA string) (*mcp.ToolResult, error) {
	state, err := client.GetPullRequest(ctx, repo, number)
	if err != nil {
		return nil, err
	}
	if state.HeadSHA != headSHA {
		return nil, apperror.NewValidationf("github app tool: PR %s#%d head is %s, not the requested %s; fetch the diff for the live head",
			repo, number, state.HeadSHA, headSHA)
	}
	files, err := client.ListPullRequestFiles(ctx, repo, number)
	if err != nil {
		return nil, err
	}
	if len(files) == 0 {
		reason := fmt.Sprintf("pull request %s#%d has no changed files at head %s; there is nothing to review and no verdict can be recorded for this head",
			repo, number, headSHA)
		h.markHeadUnapprovable(ctx, client, projectID, repo, number, headSHA, reason)
		return nil, apperror.NewValidationf("github app tool: %s", reason)
	}
	mcp.MarkRunEvidence(ctx, diffEvidenceKey(repo, number, headSHA))

	title := strings.TrimSpace(state.Title)
	header := fmt.Sprintf("pull request %s#%d at %s", repo, number, headSHA)
	if title != "" {
		header += "\n" + title
	}
	return &mcp.ToolResult{
		Content: []mcp.ContentBlock{{Type: "text", Text: renderDiffFiles(header, files)}},
		StructuredContent: map[string]any{
			"sha":    headSHA,
			"number": number,
			"files":  len(files),
		},
	}, nil
}

// executeCommitDiff serves one commit's first-parent diff for context. It never
// records review evidence: the verified object is the pull request, and a merge
// commit's first-parent diff can be empty while the PR is large. An empty
// commit diff is a validation error, not a successful empty tool result.
func (h *ReviewToolHandler) executeCommitDiff(ctx context.Context, client *githubapp.Client, repo, headSHA string) (*mcp.ToolResult, error) {
	diff, err := client.GetCommit(ctx, repo, headSHA)
	if err != nil {
		return nil, err
	}
	if len(diff.Files) == 0 {
		return nil, apperror.NewValidationf("github app tool: commit %s has an empty diff (its tree matches its first parent); pass the pull request number to read the reviewed pull request's diff",
			headSHA)
	}
	header := fmt.Sprintf("commit %s\n%s", diff.SHA, strings.TrimSpace(diff.Message))
	return &mcp.ToolResult{
		Content: []mcp.ContentBlock{{Type: "text", Text: renderDiffFiles(header, diff.Files)}},
		StructuredContent: map[string]any{
			"sha":   diff.SHA,
			"files": len(diff.Files),
		},
	}, nil
}

// renderDiffFiles bounds each patch and the whole rendered result so a large
// pull request cannot blow up the agent context.
func renderDiffFiles(header string, files []githubapp.CommitFile) string {
	var b strings.Builder
	b.WriteString(header)
	b.WriteString("\n\n")
	for _, file := range files {
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
	return text
}

// markHeadUnapprovable moves the head's review gate to a terminal state and
// publishes a failing reviewed check, so a head with no reviewable diff cannot
// hang the required check in_progress forever (#1022). The gate update is best
// effort because the tool still owes the caller its validation error; a failure
// here is logged (and retried by the next webhook replay, which republishes the
// terminal state).
func (h *ReviewToolHandler) markHeadUnapprovable(ctx context.Context, client *githubapp.Client, projectID, repo string, number int, headSHA, reason string) {
	if h.reviewGates != nil {
		projectUUID, err := uuid.Parse(projectID)
		if err != nil {
			if h.log != nil {
				h.log.Warn("sources: cannot mark review gate unapprovable: invalid project id", "project_id", projectID, "error", err)
			}
		} else {
			subjectRef := fmt.Sprintf("%s#%d", repo, number)
			_, gateErr := h.reviewGates.MarkUnapprovable(ctx, projectUUID, "GitHubPullRequest", subjectRef, headSHA, reason)
			switch {
			case gateErr == nil:
			case errors.Is(gateErr, ErrReviewGateNotFound):
				// Legacy review sources with no parent gate keep the old path.
			default:
				if h.log != nil {
					h.log.Error("sources: mark review gate unapprovable",
						"subject", subjectRef, "head_sha", headSHA, "error", gateErr)
				}
			}
		}
	}
	if client == nil {
		return
	}
	if err := client.CreateReviewedCheck(ctx, repo, headSHA, "failure", "reviewed: unapprovable", reason); err != nil && h.log != nil {
		h.log.Error("sources: publish unapprovable reviewed check", "repo", repo, "head_sha", headSHA, "error", err)
	}
}

func (h *ReviewToolHandler) linkedIssue(ctx context.Context, projectID, repo string, number int) *SourceIssuePRLink {
	if h.links == nil || projectID == "" || repo == "" || number <= 0 {
		return nil
	}
	projectUUID, err := uuid.Parse(projectID)
	if err != nil {
		return nil
	}
	link, err := h.links.FindSourceIssuePRLink(ctx, projectUUID, repo, number)
	if err != nil {
		return nil
	}
	return link
}

// executeReviewSubmit enforces the live-head binding and the same-run evidence
// rule, then posts the review, the reviewed check and the merge-guard marker
// with byte-exact text.
func (h *ReviewToolHandler) executeReviewSubmit(ctx context.Context, client *githubapp.Client, projectID, repo string, args map[string]any) (*mcp.ToolResult, error) {
	number, ok := intArg(args, "number")
	if !ok || number <= 0 {
		return nil, apperror.NewValidation("github app tool: number is required")
	}
	headSHA := stringArg(args, "head_sha")
	verdict := strings.ToUpper(strings.TrimSpace(stringArg(args, "verdict")))
	summary := strings.TrimSpace(stringArg(args, "summary"))
	if headSHA == "" || summary == "" {
		return nil, apperror.NewValidation("github app tool: head_sha and summary are required")
	}
	if !validCommitSHA(headSHA) {
		return nil, apperror.NewValidationf("github app tool: head_sha %q is not a commit SHA (40 lowercase hex)", headSHA)
	}
	var event, conclusion, checkTitle string
	switch verdict {
	case "MERGE":
		event, conclusion, checkTitle = "APPROVE", "success", "reviewed: approved"
	case "CHANGES_REQUESTED":
		event, conclusion, checkTitle = "REQUEST_CHANGES", "failure", "reviewed: changes requested"
	default:
		return nil, apperror.NewValidation("github app tool: verdict must be MERGE or CHANGES_REQUESTED")
	}

	state, err := client.GetPullRequest(ctx, repo, number)
	if err != nil {
		return nil, err
	}
	if state.HeadSHA != headSHA {
		return nil, apperror.NewValidationf("github app tool: PR head is %s, not the reviewed %s; re-review the new head", state.HeadSHA, headSHA)
	}
	// Same-run binding: the verdict may only name a pull request head whose
	// non-empty diff this run read through github-pr-diff. A prompt injection in
	// PR text cannot make the agent approve a change it never fetched. Evidence
	// is keyed on (repo, number, head SHA) because the reviewed object is the
	// pull request, not one commit's first-parent diff (#1022).
	if !mcp.RunHasEvidence(ctx, diffEvidenceKey(repo, number, headSHA)) {
		reason := fmt.Sprintf("no non-empty github-pr-diff result for %s#%d@%s in this run; fetch the pull request diff for the exact reviewed head before submitting",
			repo, number, headSHA)
		// A genuinely empty pull request can never receive a verdict. Detect it
		// here too (not only in github-pr-diff) so a run that skipped the diff
		// cannot leave the gate pending forever.
		if files, ferr := client.ListPullRequestFiles(ctx, repo, number); ferr == nil && len(files) == 0 {
			reason = fmt.Sprintf("pull request %s#%d has no changed files at head %s; there is nothing to review and no verdict can be recorded for this head",
				repo, number, headSHA)
			h.markHeadUnapprovable(ctx, client, projectID, repo, number, headSHA, reason)
		}
		return nil, apperror.NewValidationf("github app tool: %s", reason)
	}

	if err := client.CreateReview(ctx, repo, number, headSHA, event, summary); err != nil {
		return nil, err
	}
	reviewerID, reviewerName := h.reviewerIdentity(ctx, projectID)
	legacy := true
	if h.reviewGates != nil && reviewerID != "" {
		gate, gateErr := h.reviewGates.RecordVerdict(ctx, uuid.MustParse(projectID), "GitHubPullRequest", fmt.Sprintf("%s#%d", repo, number), headSHA, reviewerID, reviewerName, verdict, summary)
		switch {
		case gateErr == nil && gate != nil:
			legacy = false
			if err := h.publishReviewGate(ctx, client, repo, headSHA, gate, summary); err != nil {
				return nil, err
			}
			switch gate.Status {
			case ReviewGateApproved:
				marker := fmt.Sprintf("Reviewed-SHA: %s\nVerdict: MERGE\n\n%s", headSHA, summary)
				if err := client.CreateIssueComment(ctx, repo, number, marker); err != nil {
					return nil, err
				}
			case ReviewGateChangesRequested:
				marker := fmt.Sprintf("Reviewed-SHA: %s\nVerdict: CHANGES_REQUESTED\n\n%s", headSHA, summary)
				_ = client.CreateIssueComment(ctx, repo, number, marker)
			}
		case errors.Is(gateErr, ErrReviewGateNotFound):
			// Legacy review sources with no parent gate keep the old single-lane path.
		case errors.Is(gateErr, ErrReviewerNotRequired) && gate != nil && len(gate.RequiredReviewers) == 0:
			// No reviewer agents configured for this subject; preserve the old path.
		default:
			// A gate that already holds a caller-visible validation refusal (for
			// example a terminal unapprovable head) is relayed as-is; only a
			// genuine server failure becomes an internal error.
			var validation *apperror.Error
			if errors.As(gateErr, &validation) {
				return nil, gateErr
			}
			return nil, apperror.NewInternal("github app tool: the review gate could not record the verdict", gateErr)
		}
	}
	if legacy {
		if err := client.CreateReviewedCheck(ctx, repo, headSHA, conclusion, checkTitle, summary); err != nil {
			return nil, err
		}
		marker := fmt.Sprintf("Reviewed-SHA: %s\nVerdict: %s\n\n%s", headSHA, verdict, summary)
		if err := client.CreateIssueComment(ctx, repo, number, marker); err != nil {
			return nil, err
		}
	}
	// A board-dispatched review lane is finalized from the verdict; a direct
	// trigger run has no work object and stays unchanged.
	if canonicalID := mcp.WorkObjectIDFromContext(ctx); canonicalID != "" && h.workItems != nil {
		laneStatus := "done"
		if verdict == "CHANGES_REQUESTED" {
			laneStatus = "revision"
		}
		_, _ = h.workItems.UpdateWorkObjectStatus(auth.WithActor(ctx, graph.ActorSystem, nil), projectID, canonicalID, laneStatus, "review")
	} else if link := h.linkedIssue(ctx, projectID, repo, number); link != nil && verdict == "CHANGES_REQUESTED" {
		issueCanonical := link.IssueCanonicalID.String()
		reworked := false
		if h.reworker != nil {
			reworked = h.reworker.RequestWorkItemChanges(ctx, projectID, issueCanonical, "review", summary) == nil
		}
		if !reworked && h.workItems != nil {
			_, _ = h.workItems.UpdateWorkObjectStatus(auth.WithActor(ctx, graph.ActorSystem, nil), projectID, issueCanonical, "revision", "review")
		}
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

func (h *ReviewToolHandler) reviewerIdentity(ctx context.Context, projectID string) (string, string) {
	if h.agentRuns == nil {
		return "", ""
	}
	runID := provider.RunIDFromContext(ctx)
	if runID == "" {
		return "", ""
	}
	run, err := h.agentRuns.FindRunByIDForProject(ctx, runID, projectID)
	if err != nil || run == nil || run.AgentID == "" {
		return "", ""
	}
	name := ""
	if agent, aerr := h.agentRuns.FindByID(ctx, run.AgentID, &projectID); aerr == nil && agent != nil {
		name = agent.Name
	}
	return run.AgentID, name
}

func (h *ReviewToolHandler) publishReviewGate(ctx context.Context, client *githubapp.Client, repo, headSHA string, gate *ReviewGate, summary string) error {
	if gate == nil {
		return client.CreateCheck(ctx, repo, headSHA, "reviewed", "in_progress", "", "reviewed: pending", "Waiting for all required reviewers.")
	}
	switch gate.Status {
	case ReviewGateApproved:
		// Defence in depth: the aggregate must never publish a green check for an
		// under-quorum reviewer set even if an older row or a direct caller
		// bypassed evaluate (#1005).
		if len(gate.RequiredReviewers) < gate.minimumReviewers() {
			return client.CreateCheck(ctx, repo, headSHA, "reviewed", "in_progress", "", "reviewed: pending", reviewGatePendingSummary(gate))
		}
		return client.CreateReviewedCheck(ctx, repo, headSHA, "success", "reviewed: all reviewers approved", summary)
	case ReviewGateChangesRequested:
		return client.CreateReviewedCheck(ctx, repo, headSHA, "failure", "reviewed: changes requested", summary)
	case ReviewGateUnapprovable:
		return client.CreateReviewedCheck(ctx, repo, headSHA, "failure", "reviewed: unapprovable", reviewGateUnapprovableSummary(gate))
	default:
		return client.CreateCheck(ctx, repo, headSHA, "reviewed", "in_progress", "", "reviewed: pending", reviewGatePendingSummary(gate))
	}
}

// reviewGatePendingSummary explains why the reviewed check is still pending. An
// under-configured gate names the missing independent reviewer instead of the
// generic wait text, so the operator can fix the agent configuration (#1005).
func reviewGatePendingSummary(gate *ReviewGate) string {
	if gate == nil {
		return "Waiting for all required reviewers."
	}
	required, minimum := len(gate.RequiredReviewers), gate.minimumReviewers()
	if required < minimum {
		return fmt.Sprintf("Waiting for %d independent reviewers: %d of %d configured.", minimum, required, minimum)
	}
	return "Waiting for all required reviewers."
}

// reviewGateUnapprovableSummary renders the terminal reason recorded on the
// gate so the failing reviewed check says why the head cannot be approved.
func reviewGateUnapprovableSummary(gate *ReviewGate) string {
	if gate != nil {
		if reason := strings.TrimSpace(gate.StatusReason); reason != "" {
			return reason
		}
	}
	return "No verdict can be recorded for this head."
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
