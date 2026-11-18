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

	// maxDiffMessageBytes bounds the commit message that commit mode carries in
	// its result header. The message is attacker-controlled on a fork pull
	// request and is charged against the same text budget as the patch, so an
	// unbounded message is rendered in full while the change itself is cut --
	// the reader would see only the message and none of the diff (#1037).
	maxDiffMessageBytes = 2000

	// maxDiffTitleBytes bounds the pull request title carried in the diff header.
	// The title is author-controlled and charged against the same text budget as
	// the patch (#1037 review).
	maxDiffTitleBytes = 300

	// githubCommitFileListCap is the maximum number of changed files GitHub's
	// commit endpoint lists; a commit at the cap may hide more files.
	githubCommitFileListCap = 300
	// githubPRFileListCap mirrors githubapp.ListPullRequestFiles' pagination bound
	// (30 pages of 100). A listing at the cap may have more files GitHub never
	// returned, so the pull request diff cannot be certified complete.
	githubPRFileListCap = 3000
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

// diffFileEvidenceKey names the run-scoped fact "github-pr-diff rendered this
// changed file of repo#number@sha in full". A payload too large for one result is
// finished with the tool's path/files arguments, and executeReviewSubmit accepts
// the per-file union once every changed file has been read in full (#1006).
func diffFileEvidenceKey(repo string, number int, sha, filename string) string {
	return fmt.Sprintf("githubapp:prdiff-file:%s#%d@%s:%s", strings.ToLower(repo), number, sha, filename)
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
	// maxResultBytes is the agent executor's per-result model-context bound. A diff
	// bigger than this would reach the model cut while this run had already
	// recorded evidence for it, so the tool renders below the bound instead (#1006).
	maxResultBytes int
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
	maxResultBytes := 0
	if cfg != nil {
		keyHex = cfg.LLMProvider.EncryptionKey
		baseURL = cfg.GitHubSource.APIBaseURL
		maxResultBytes = agents.ToolResultMaxBytesFor(cfg.MCP, toolPRDiff)
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
		log:            log,
		maxResultBytes: maxResultBytes,
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
	selection, err := diffSelection(args)
	if err != nil {
		return nil, err
	}
	number, ok := intArg(args, "number")
	if !ok || number <= 0 {
		if subject, hasSubject := mcp.RunSubjectFromContext(ctx); hasSubject &&
			strings.EqualFold(subject.Repo, repo) && subject.HeadSHA == headSHA && subject.PRNumber > 0 {
			number, ok = subject.PRNumber, true
		}
	}
	if ok && number > 0 {
		return h.executePullRequestDiff(ctx, client, projectID, repo, number, headSHA, selection)
	}
	return h.executeCommitDiff(ctx, client, repo, headSHA, selection)
}

// executePullRequestDiff reads GET /pulls/{number}/files, the diff of the
// reviewed object. A non-empty result is the evidence a verdict is allowed to
// name. An empty result is a genuinely unapprovable head: the tool returns a
// validation error (never an internal one) and moves the review gate to a
// terminal state instead of leaving it pending forever.
//
// The rendered result is bounded and truncation is machine-readable
// (truncated/truncatedFiles/complete). A truncated read records no whole-diff
// evidence, so it cannot authorize github-review-submit (#1006); the agent
// finishes a large change with the path/files selection, which records per-file
// evidence, and submit accepts the union once every changed file is read in full.
func (h *ReviewToolHandler) executePullRequestDiff(ctx context.Context, client *githubapp.Client, projectID, repo string, number int, headSHA string, selection map[string]struct{}) (*mcp.ToolResult, error) {
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

	title := strings.TrimSpace(state.Title)
	header := fmt.Sprintf("pull request %s#%d at %s", repo, number, headSHA)
	if title != "" {
		// The title is author-controlled and lands in model-visible text, so it is
		// neutralised like a filename and bounded so it cannot forge a marker line
		// or spend the diff budget (#1037 review).
		header += "\n" + truncateUTF8(sanitizeDiffPath(title), maxDiffTitleBytes)
	}
	rendered, result, err := h.renderBounded(ctx, headSHA, header, files, selection, len(files) >= githubPRFileListCap)
	if err != nil {
		return nil, err
	}
	for _, filename := range rendered.completeFiles {
		mcp.MarkRunEvidence(ctx, diffFileEvidenceKey(repo, number, headSHA, filename))
	}
	// Record that this run saw the whole diff for this pull request. A truncated
	// or filtered read records per-file evidence at most, so submit can still be
	// reached once the run reads the remaining files, but never on a half-read
	// diff.
	if rendered.complete {
		mcp.MarkRunEvidence(ctx, diffEvidenceKey(repo, number, headSHA))
	}
	return result, nil
}

// executeCommitDiff serves one commit's first-parent diff for context. It never
// records review evidence: the verified object is the pull request, and a merge
// commit's first-parent diff can be empty while the PR is large. An empty
// commit diff is a validation error, not a successful empty tool result.
func (h *ReviewToolHandler) executeCommitDiff(ctx context.Context, client *githubapp.Client, repo, headSHA string, selection map[string]struct{}) (*mcp.ToolResult, error) {
	diff, err := client.GetCommit(ctx, repo, headSHA)
	if err != nil {
		return nil, err
	}
	if len(diff.Files) == 0 {
		return nil, apperror.NewValidationf("github app tool: commit %s has an empty diff (its tree matches its first parent); pass the pull request number to read the reviewed pull request's diff",
			headSHA)
	}
	header := fmt.Sprintf("commit %s\n%s", diff.SHA, truncateUTF8(strings.TrimSpace(diff.Message), maxDiffMessageBytes))
	_, result, err := h.renderBounded(ctx, diff.SHA, header, diff.Files, selection, len(diff.Files) >= githubCommitFileListCap)
	if err != nil {
		return nil, err
	}
	return result, nil
}

// renderBounded renders files under this tool's caps and, when the executor's
// per-result bound is known, shrinks the text until the marshalled result fits
// it. The executor bounds a tool result before the model sees it, so a payload
// that passes this tool's caps but exceeds that bound would reach the model cut
// while this run had already recorded evidence for it -- the same
// truncated-verdict failure through a different cap (#1006). Re-rendering
// recomputes which files are complete, so evidence is only recorded for content
// the model received in full.
func (h *ReviewToolHandler) renderBounded(ctx context.Context, sha, header string, files []githubapp.CommitFile, selection map[string]struct{}, listingCapped bool) (*diffRender, *mcp.ToolResult, error) {
	textBudget := h.diffTextBudget()
	rendered, err := renderDiffFiles(sha, header, files, selection, textBudget, listingCapped)
	if err != nil {
		return nil, nil, err
	}
	result := diffToolResult(sha, len(files), selection, rendered)
	if h.maxResultBytes <= 0 {
		return rendered, result, nil
	}
	for attempt := 0; attempt < 4; attempt++ {
		raw, marshalErr := json.Marshal(result)
		if marshalErr != nil || len(raw) <= h.maxResultBytes {
			break
		}
		textBudget -= len(raw) - h.maxResultBytes + 512
		if textBudget < 512 {
			textBudget = 512
		}
		next, rerr := renderDiffFiles(sha, header, files, selection, textBudget, listingCapped)
		if rerr != nil {
			break
		}
		rendered, result = next, diffToolResult(sha, len(files), selection, next)
	}
	if raw, marshalErr := json.Marshal(result); marshalErr != nil || len(raw) > h.maxResultBytes {
		// The configured bound is too small to carry even the envelope. Never
		// claim a complete read (or record evidence) for a payload the model
		// cannot receive whole; the truncation flags stay honest instead.
		rendered.truncated = true
		rendered.complete = false
		rendered.truncatedFiles = append(rendered.truncatedFiles, rendered.completeFiles...)
		rendered.completeFiles = nil
		result = diffToolResult(sha, len(files), selection, rendered)
	}
	return rendered, result, nil
}

// diffTextBudget is the largest diff text this tool emits. It never exceeds
// maxDiffBytes and, when the executor's per-result bound is known, starts below
// it so the result envelope fits; renderBounded shrinks further and measures the
// marshalled result, since JSON escaping varies by content.
func (h *ReviewToolHandler) diffTextBudget() int {
	if h.maxResultBytes > 0 && h.maxResultBytes < maxDiffBytes {
		return h.maxResultBytes
	}
	return maxDiffBytes
}

// diffToolResult builds the agent-facing result from one render. truncatedFiles
// is always a slice so the schema type stays stable (an empty list, not null).
func diffToolResult(sha string, fileCount int, selection map[string]struct{}, rendered *diffRender) *mcp.ToolResult {
	truncatedFiles := rendered.truncatedFiles
	if truncatedFiles == nil {
		truncatedFiles = []string{}
	}
	withheldFiles := rendered.withheldFiles
	if withheldFiles == nil {
		withheldFiles = []string{}
	}
	return &mcp.ToolResult{
		Content: []mcp.ContentBlock{{Type: "text", Text: rendered.text}},
		StructuredContent: map[string]any{
			"sha":            sha,
			"files":          fileCount,
			"rendered":       rendered.rendered,
			"complete":       rendered.complete,
			"truncated":      rendered.truncated,
			"truncatedFiles": truncatedFiles,
			"withheldFiles":  withheldFiles,
			"partial":        selection != nil,
		},
	}
}

// diffSelection parses the optional path/files arguments. A selection makes the
// tool render only the named changed files, at the whole-result budget instead
// of the per-file one: that is how an agent reads past a truncation. Matching is
// exact (a leading ./ or / is ignored); a miss is a validation error naming the
// changed files rather than a silently empty diff.
func diffSelection(args map[string]any) (map[string]struct{}, error) {
	if args == nil {
		return nil, nil
	}
	selection := make(map[string]struct{})
	add := func(path string) {
		if path = normalizeDiffPath(path); path != "" {
			selection[path] = struct{}{}
		}
	}
	add(stringArg(args, "path"))
	switch files := args["files"].(type) {
	case nil:
	case []any:
		for _, value := range files {
			if path, ok := value.(string); ok {
				add(path)
			}
		}
	case []string:
		for _, path := range files {
			add(path)
		}
	default:
		return nil, apperror.NewValidation("github app tool: files must be an array of changed file paths")
	}
	if len(selection) == 0 {
		return nil, nil
	}
	return selection, nil
}

// normalizeDiffPath trims a path as GitHub reports it. Agents sometimes prefix a
// repository-relative path with ./ or /.
func normalizeDiffPath(path string) string {
	path = strings.TrimSpace(path)
	path = strings.TrimPrefix(path, "./")
	return strings.TrimPrefix(path, "/")
}

// sanitizeDiffPath neutralises control characters in a PR-controlled filename
// before it is interpolated into model-visible tool text, so a crafted filename
// cannot forge lines (for example a fake Reviewed-SHA marker) in the diff output.
// Structured content keeps the raw name; JSON escaping covers it there.
func sanitizeDiffPath(path string) string {
	return strings.Map(func(r rune) rune {
		if r == '\t' || r == '\n' || r == '\r' || r < 0x20 || r == 0x7f {
			return '?'
		}
		// Unicode line/paragraph separators and bidi/format controls are not
		// visible in a terminal but some renderers and tokenizers treat them as
		// line breaks or direction overrides, so a crafted name could still
		// reshape the model-visible text.
		switch r {
		case 0x200e, 0x200f, 0x2028, 0x2029:
			return '?'
		}
		if (r >= 0x202a && r <= 0x202e) || (r >= 0x2066 && r <= 0x2069) {
			return '?'
		}
		return r
	}, path)
}

// patchWithheld reports that GitHub returned no patch for a file whose content a
// reviewer cannot otherwise see. The API omits the patch for a binary file and
// for a diff over its patch-size limit. It also omits it for a pure path move or
// a mode-only change, which carry no line diff. A binary file reports
// additions/deletions of 0 (line counts are undefined for it), so those counts
// cannot separate it from a mode-only change; only a rename/copy with no changed
// lines is provably content-free. Everything else fails closed: a file whose
// bytes were never returned must never count as read (#1006).
func patchWithheld(file githubapp.CommitFile) bool {
	if file.Patch != "" {
		return false
	}
	switch file.Status {
	case "renamed", "copied":
		// A pure path move changed no content. A rename that also changed content
		// carries a patch, or at least a non-zero line count (a renamed binary that
		// also changed bytes reports 0/0 like a mode-only edit, so it is the one
		// residual ambiguity and is accepted only because nothing distinguishes it
		// from a content-free move in the API).
		return file.Additions != 0 || file.Deletions != 0
	default:
		return true
	}
}

// diffRender is the rendered, bounded form of one diff.
type diffRender struct {
	text string
	// rendered is the number of changed files whose header reached the payload.
	rendered int
	// truncated reports that the payload was cut (a patch or the whole result).
	truncated bool
	// truncatedFiles names every changed file whose returned content is
	// incomplete: cut mid-patch or not rendered at all. It never names a file
	// excluded by a path/files selection.
	truncatedFiles []string
	// withheldFiles names every changed file no re-read can produce: GitHub
	// withheld its patch (binary, mode-only, an empty file, or a diff over its
	// patch limit), or the patch alone exceeds the payload budget. It is reported
	// to the reader but never counted as read, and it does not block a verdict: a
	// single binary asset must not make every PR containing it unapprovable
	// (#1037 review).
	withheldFiles []string
	// completeFiles names the files whose full patch is in the payload.
	completeFiles []string
	// complete reports that this one call rendered every changed file in full,
	// which is the only thing that authorizes a verdict without per-file reads.
	complete bool
}

type diffSegment struct {
	filename string
	text     string
	complete bool
	// withheld reports that GitHub returned no patch for a file whose content the
	// reviewer cannot see (binary, a diff over the API's patch limit, or a
	// mode-only change). Re-reading the file cannot recover it, so it must never
	// count as read.
	withheld bool
}

// renderDiffFiles renders the changed files under the payload caps. textBudget
// is the hard ceiling on the returned text (already resolved against the
// executor's per-result bound). It is pure: the caller records evidence from
// completeFiles/complete, so a segment cut by the final size cap is never
// reported as fully read.
func renderDiffFiles(sha, header string, files []githubapp.CommitFile, selection map[string]struct{}, textBudget int, fileListCapped bool) (*diffRender, error) {
	if selection != nil {
		present := make(map[string]struct{}, len(files))
		for _, file := range files {
			present[file.Filename] = struct{}{}
		}
		for path := range selection {
			if _, ok := present[path]; !ok {
				return nil, apperror.NewValidationf("github app tool: path %q is not among the changed files of %s (%s)", path, sha, summarizedFiles(files))
			}
		}
	}
	if textBudget < 1 {
		textBudget = 1
	}
	patchBudget := maxPatchBytes
	if selection != nil {
		// A file-filtered read spends the whole-result budget on the selected
		// files: the caller asked for them precisely because the unfiltered
		// payload was cut.
		patchBudget = textBudget
	}
	if patchBudget > textBudget {
		patchBudget = textBudget
	}
	prefix := header + "\n\n"
	if fileListCapped {
		// Put the warning at the top so the size cap cannot cut it.
		prefix += fmt.Sprintf("… warning: the change lists %d changed files (GitHub's listing cap); files beyond it were not returned.\n\n", len(files))
	}
	segments := make([]diffSegment, 0, len(files))
	total := len(prefix)
	for _, file := range files {
		if selection != nil {
			if _, ok := selection[file.Filename]; !ok {
				continue
			}
		}
		patch := file.Patch
		complete := true
		withheld := patchWithheld(file)
		if withheld {
			// GitHub omits the patch for binary files, for diffs over its patch-size
			// limit, and for mode-only changes. A file that changed but whose content
			// was never returned cannot count as read (#1006).
			complete = false
		} else if len(patch) > patchBudget {
			patch = truncateUTF8(patch, patchBudget)
			complete = false
			// Under a path/files selection the patch budget is the whole-result
			// budget, so a file that still does not fit can never be rendered by any
			// re-read. Unfiltered, the per-file cap is what cut it and a selection
			// can still produce it.
			withheld = selection != nil
		}
		name := sanitizeDiffPath(file.Filename)
		var b strings.Builder
		fmt.Fprintf(&b, "--- %s (%s, +%d/-%d)\n%s\n\n", name, file.Status, file.Additions, file.Deletions, patch)
		switch {
		case withheld:
			fmt.Fprintf(&b, "… patch unavailable for %s: GitHub withheld it (binary, over its patch limit, or mode-only); treat the change as not fully reviewed\n\n", name)
		case !complete && selection != nil:
			// Re-reading the same file cannot help: it alone exceeds the
			// payload budget, so say so instead of inviting a loop.
			fmt.Fprintf(&b, "… patch truncated for %s: this file alone exceeds the payload limit; treat the change as not fully reviewed\n\n", name)
		case !complete:
			fmt.Fprintf(&b, "… patch truncated for %s: re-fetch this file with github-pr-diff path=%q\n\n", name, name)
		}
		segments = append(segments, diffSegment{filename: file.Filename, text: b.String(), complete: complete, withheld: withheld})
		total += b.Len()
		if total >= textBudget {
			break
		}
	}

	var b strings.Builder
	b.WriteString(prefix)
	ends := make([]int, len(segments))
	for i, segment := range segments {
		b.WriteString(segment.text)
		ends[i] = b.Len()
	}
	text := b.String()
	kept := len(text)
	if kept > textBudget {
		trimmed := truncateUTF8(text, textBudget)
		text = trimmed + "\n… diff truncated\n"
		// The rune-boundary trim can drop a byte or two below textBudget; compare
		// segment ends against what was actually kept, not the nominal cap.
		kept = len(trimmed)
	}

	result := &diffRender{text: text, rendered: len(segments)}
	truncatedFiles := make([]string, 0)
	withheldFiles := make([]string, 0)
	rendered := make(map[string]struct{}, len(segments))
	for i, segment := range segments {
		rendered[segment.filename] = struct{}{}
		if segment.complete && ends[i] <= kept {
			result.completeFiles = append(result.completeFiles, segment.filename)
			continue
		}
		if segment.withheld {
			withheldFiles = append(withheldFiles, segment.filename)
			continue
		}
		result.truncated = true
		truncatedFiles = append(truncatedFiles, segment.filename)
	}
	for _, file := range files {
		if selection != nil {
			if _, ok := selection[file.Filename]; !ok {
				continue
			}
		}
		if _, ok := rendered[file.Filename]; !ok {
			// Cut by the whole-result budget before its header was written.
			result.truncated = true
			truncatedFiles = append(truncatedFiles, file.Filename)
		}
	}
	if fileListCapped {
		// The API listed at most this many files; anything beyond is invisible to
		// the tool, so no call can certify the diff complete.
		result.truncated = true
	}
	result.truncatedFiles = truncatedFiles
	result.withheldFiles = withheldFiles
	result.complete = selection == nil && !result.truncated && len(withheldFiles) == 0 && len(files) > 0
	return result, nil
}

// summarizedFiles renders changed file names for a validation error, bounded so
// a large change cannot turn the error into a payload of its own.
func summarizedFiles(files []githubapp.CommitFile) string {
	names := make([]string, 0, len(files))
	for _, file := range files {
		names = append(names, file.Filename)
	}
	return summarizedNames(names)
}

// summarizedNames renders up to 20 sanitized names, then a count of the rest.
// Names are PR-controlled, so they are neutralised before entering an error the
// model reads.
func summarizedNames(names []string) string {
	const limit = 20
	if len(names) == 0 {
		return "none"
	}
	render := func(in []string) string {
		out := make([]string, len(in))
		for i, name := range in {
			out[i] = sanitizeDiffPath(name)
		}
		return strings.Join(out, ", ")
	}
	if len(names) <= limit {
		return render(names)
	}
	return render(names[:limit]) + fmt.Sprintf(", … %d more", len(names)-limit)
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
	// Same-run binding: a MERGE verdict may only name a pull request head whose
	// non-empty diff this run read in full through github-pr-diff. A prompt
	// injection in PR text cannot make the agent approve a change it never
	// fetched. Evidence is keyed on (repo, number, head SHA) because the reviewed
	// object is the pull request, not one commit's first-parent diff (#1022).
	//
	// CHANGES_REQUESTED is deliberately exempt. Requiring full evidence for a
	// rejection turns a review into a permanent block: a diff containing a file
	// GitHub will not return (binary, over its patch limit) can never be read in
	// full, so the agent could neither approve nor reject, and the gate hung
	// in_progress exactly as #1022 did (#1037). A rejection cannot authorize
	// anything -- it moves the gate to its terminal changes_requested state --
	// so the fail-closed rule belongs on the approving verdict alone.
	if verdict == "MERGE" && !mcp.RunHasEvidence(ctx, diffEvidenceKey(repo, number, headSHA)) {
		gaps, readErr := h.requireCompleteDiffRead(ctx, client, projectID, repo, number, headSHA)
		if readErr != nil {
			return nil, readErr
		}
		summary = withDiffGaps(summary, gaps)
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
			if _, ok := errors.AsType[*apperror.Error](gateErr); ok {
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

// requireCompleteDiffRead is the finishing path for a diff too large for one
// result. It reads the pull request's changed-file list (the reviewed object),
// requires per-file evidence for every changed file whose content this API can
// produce, then marks the aggregate key so a later submit in the same run does
// not repeat the check.
//
// Files no re-read can produce -- GitHub withholds the patch for a binary file,
// a mode-only edit, an empty file and a diff over its patch limit, and a patch
// larger than the whole-result budget cannot be rendered by any selection -- are
// reported back as gaps and never count as read, but they do not refuse the
// verdict: one binary asset must not make every pull request that contains it
// unapprovable, which would be a release-blocking denial of service reachable by
// any contributor (#1037). The caller names the gap on the check and in the
// review. A genuinely empty pull request is still a terminal, unapprovable head:
// the gate is moved out of pending and the caller gets a validation error.
func (h *ReviewToolHandler) requireCompleteDiffRead(ctx context.Context, client *githubapp.Client, projectID, repo string, number int, headSHA string) ([]string, error) {
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
	if len(files) >= githubPRFileListCap {
		return nil, apperror.NewValidationf("github app tool: pull request %s#%d lists %d changed files, GitHub's listing cap (pagination bound); files beyond it cannot be read, so the diff cannot be certified complete", repo, number, len(files))
	}
	budget := h.diffTextBudget()
	unread := make([]string, 0)
	gaps := make([]string, 0)
	for _, file := range files {
		if mcp.RunHasEvidence(ctx, diffFileEvidenceKey(repo, number, headSHA, file.Filename)) {
			continue
		}
		if diffContentUnavailable(file, budget) {
			gaps = append(gaps, file.Filename)
			continue
		}
		unread = append(unread, file.Filename)
	}
	if len(unread) > 0 {
		return gaps, apperror.NewValidationf("github app tool: the github-pr-diff result for %s#%d@%s was truncated or partial; this run did not fully read %s. Re-fetch each of those files with github-pr-diff path=<file> (or files=[...]) and read every changed file before submitting", repo, number, headSHA, summarizedNames(unread))
	}
	mcp.MarkRunEvidence(ctx, diffEvidenceKey(repo, number, headSHA))
	return gaps, nil
}

// diffContentUnavailable reports whether no re-read can produce this file's
// content: GitHub withheld the patch (binary, mode-only, empty file, or a diff
// over its patch limit) or the patch alone exceeds this tool's whole-result
// budget. Such a file is a reported gap rather than a blocking one -- the API
// gives no way to tell a mode-only edit from a binary that changed bytes, so
// blocking on it would refuse every pull request carrying an image, a wasm
// fixture or a chmod (#1037).
func diffContentUnavailable(file githubapp.CommitFile, budget int) bool {
	return patchWithheld(file) || len(file.Patch) > budget
}

// withDiffGaps appends the files whose content GitHub did not return to the
// verdict text, so the approved check and the review body say what was not read
// instead of implying a complete review (#1037).
func withDiffGaps(summary string, gaps []string) string {
	if len(gaps) == 0 {
		return summary
	}
	return fmt.Sprintf("%s\n\nNot reviewable: %s (GitHub returned no content for these files -- binary, mode-only, empty, or over its patch limit -- so they were not read)", summary, summarizedNames(gaps))
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
		// minimumReviewers() caps the quorum at the configured reviewer count, so
		// an approved gate has every required reviewer's verdict; no separate
		// under-quorum check can fire here.
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
