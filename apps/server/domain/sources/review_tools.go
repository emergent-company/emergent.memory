package sources

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"sort"
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

	// A commit's diff is served in whole-file patches, paged by byte offset: a
	// release promotion is one commit carrying every change since the last
	// promotion (137 files / 632 KB of patch at v0.103.0), and one response that
	// large cannot be held — the review agent's own context cut it at ~122 KB and
	// it then (correctly) refused to approve a diff it had only partly read.
	//
	// The per-file patch is therefore no longer truncated (the old 6000-byte
	// bound is what cut ~43% of that promotion mid-hunk); the page is, and the
	// page ceiling stays below the model boundary that cuts every tool result
	// (agents.DefaultToolResultMaxBytes, 128 KiB). Every response states
	// total_bytes and next_offset at the top, so an agent reads on from where it
	// stopped instead of retrying the same call and seeing the same cut.
	//
	// These are the *requested* page size. The response carries a file index and
	// JSON-escapes the patch text, so a page is additionally bounded by what
	// actually fits in the tool-result envelope (diffEnvelopeBudget): see the
	// sizing loop in executePRDiff.
	maxDiffPageBytes     = 100000
	defaultDiffPageBytes = 80000

	// maxDiffIndexBytes and maxIndexFiles bound the file index that heads every
	// page. GitHub serves at most 300 files for a commit, so the index covers a
	// real change in full; the byte cap keeps a page of pathological paths from
	// eating the patch budget.
	maxDiffIndexBytes = 16000
	maxIndexFiles     = 300

	// maxDiffMessageBytes clips the commit message that heads every page. That
	// text comes from the commit object and is therefore attacker-controlled on
	// a fork PR, and unlike the patch it cannot be shrunk by paging: one long
	// message would otherwise push every page over the tool-result boundary, so
	// the reader would be shown the message and none of the change.
	maxDiffMessageBytes = 2000

	// diffEnvelopeMargin is headroom under a deployment's per-tool result cap for
	// the envelope's own keys ({"ok":true,"result":…}) and their escaping.
	diffEnvelopeMargin = 2048

	// defaultDiffEnvelopeBudget is the fallback ceiling for one diff response when
	// the handler was built without a resolved cap (unit tests, an unbounded
	// deployment): the compiled default boundary, less the margin above. The
	// production budget is resolved per deployment — see
	// ReviewToolHandler.resultBudget.
	defaultDiffEnvelopeBudget = agents.DefaultToolResultMaxBytes - diffEnvelopeMargin
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

// diffPageKey names one byte range of repo@sha's patch text that this run was
// served. A diff larger than one page is read across several calls, so the
// aggregate diffEvidenceKey may only be recorded once these ranges tile the
// whole text — an agent that read the first page and the last page has not read
// the change in between, and must not be able to approve it.
func diffPageKey(repo, sha string, start, end int) string {
	return fmt.Sprintf("githubapp:diffpage:%s@%s:%d:%d", strings.ToLower(repo), sha, start, end)
}

// diffFullyRead reports whether the ranges recorded for repo@sha cover [0,total)
// with no gap, ignoring overlaps and out-of-order reads.
func diffFullyRead(ctx context.Context, repo, sha string, total int) bool {
	if total <= 0 {
		return false
	}
	prefix := fmt.Sprintf("githubapp:diffpage:%s@%s:", strings.ToLower(repo), sha)
	type span struct{ start, end int }
	spans := make([]span, 0, 8)
	for _, key := range mcp.RunEvidenceKeys(ctx, prefix) {
		parts := strings.SplitN(strings.TrimPrefix(key, prefix), ":", 2)
		if len(parts) != 2 {
			continue
		}
		start, startErr := strconv.Atoi(parts[0])
		end, endErr := strconv.Atoi(parts[1])
		if startErr != nil || endErr != nil {
			continue
		}
		spans = append(spans, span{start: start, end: end})
	}
	sort.Slice(spans, func(i, j int) bool { return spans[i].start < spans[j].start })
	covered := 0
	for _, s := range spans {
		if s.start > covered {
			return false
		}
		if s.end > covered {
			covered = s.end
		}
		if covered >= total {
			return true
		}
	}
	return covered >= total
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
	// resultBudget is the byte ceiling for one github-pr-diff response as the
	// model sees it, resolved from the deployment's effective per-tool result cap
	// at construction (0 → defaultDiffEnvelopeBudget). See diffPageBudget.
	resultBudget int
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
	budget := 0
	if cfg != nil {
		keyHex = cfg.LLMProvider.EncryptionKey
		baseURL = cfg.GitHubSource.APIBaseURL
		budget = diffPageBudget(agents.MaxToolResultBytesFor(cfg.MCP, toolPRDiff))
	}
	key, err := hex.DecodeString(keyHex)
	if err != nil {
		if log != nil {
			log.Error("sources: invalid LLM_ENCRYPTION_KEY; review tool source app credentials are unreadable", "error", err)
		}
		key = nil
	}
	return &ReviewToolHandler{
		repo:         repo,
		links:        repo,
		key:          key,
		apiBaseURL:   baseURL,
		minter:       githubapp.NewTokenMinter(baseURL, nil),
		resultBudget: budget,
		newClient: func(baseURL, token string) *githubapp.Client {
			return githubapp.NewClient(baseURL, token, nil)
		},
	}
}

// diffPageBudget turns a deployment's effective per-tool result cap into the
// ceiling for one diff response. A negative cap disables per-result bounding
// outright, the same way it does for the executor, so the page may use the
// requested size; a zero cap means the default boundary, which is also what the
// handler falls back to.
//
// A positive budget is never allowed to reach those two sentinels: a cap at or
// below the envelope margin leaves no room for a page, and the safe answer is
// the smallest positive budget (which fails closed in executePRDiff), not a
// negative one that would read as "unbounded" and serve a page the model
// boundary would cut.
func diffPageBudget(capBytes int) int {
	if capBytes < 0 {
		return -1
	}
	if capBytes == 0 {
		capBytes = agents.DefaultToolResultMaxBytes
	}
	if budget := capBytes - diffEnvelopeMargin; budget > 0 {
		return budget
	}
	return 1
}

// pageBudget is the ceiling executePRDiff must fit one response into, or 0 when
// the deployment bounds nothing.
func (h *ReviewToolHandler) pageBudget() int {
	switch {
	case h.resultBudget < 0:
		return 0
	case h.resultBudget > 0:
		return h.resultBudget
	default:
		return defaultDiffEnvelopeBudget
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
		offset, _ := intArg(args, "offset")
		limit, _ := intArg(args, "limit")
		return h.executePRDiff(ctx, client, repo, stringArg(args, "head_sha"), stringArg(args, "path"), offset, limit)
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
		return fmt.Errorf("github app tool: repo %q does not match the triggering subject %q", repo, subject.Repo)
	}
	if head := stringArg(args, "head_sha"); head != "" && head != subject.HeadSHA {
		return fmt.Errorf("github app tool: head_sha %q does not match the triggering subject %q", head, subject.HeadSHA)
	}
	if toolName == toolReviewSubmit {
		if number, ok := intArg(args, "number"); ok && number != subject.PRNumber {
			return fmt.Errorf("github app tool: PR number %d does not match the triggering subject #%d", number, subject.PRNumber)
		}
	}
	return nil
}

// executePRDiff is moved from domain/githubapp/tools.go: it validates the commit
// SHA, records run-scoped evidence for a non-empty diff, and serves the diff in
// whole-file patches, paged by byte offset.
//
// A promotion-sized commit cannot be served in one response — the review agent's
// context cut the v0.103.0 promotion at ~122 KB of a 632 KB diff and it then
// refused to approve a change it had only partly read (#1022). Every response
// therefore carries the full file index plus a slice of the patch text, with
// total_bytes and next_offset, so the agent continues from where it stopped
// instead of retrying the same call and seeing the same cut.
//
// pathFilter narrows the payload to files whose name contains it. A filtered read
// records no evidence: the review-submit rule is that the run read the change it
// approves, and one narrowed path proves nothing about the rest of it.
func (h *ReviewToolHandler) executePRDiff(ctx context.Context, client *githubapp.Client, repo, headSHA, pathFilter string, offset, limit int) (*mcp.ToolResult, error) {
	if headSHA == "" {
		return nil, apperror.NewValidation("github app tool: head_sha is required")
	}
	if !validCommitSHA(headSHA) {
		return nil, apperror.NewValidationf("github app tool: head_sha %q is not a commit SHA (40 lowercase hex)", headSHA)
	}
	diff, err := client.GetCommit(ctx, repo, headSHA)
	if err != nil {
		return nil, err
	}
	pathFilter = strings.TrimSpace(pathFilter)
	files := diff.Files
	if pathFilter != "" {
		files = make([]githubapp.CommitFile, 0, len(diff.Files))
		for _, file := range diff.Files {
			if strings.Contains(file.Filename, pathFilter) {
				files = append(files, file)
			}
		}
	}
	// The patch body is assembled for the whole (possibly filtered) selection so
	// an offset always means the same byte of the same text, whatever page the
	// caller asked for.
	var body strings.Builder
	for _, file := range files {
		fmt.Fprintf(&body, "--- %s (%s, +%d/-%d)\n%s\n\n", file.Filename, file.Status, file.Additions, file.Deletions, file.Patch)
	}
	patches := body.String()

	if offset < 0 {
		offset = 0
	}
	if offset > len(patches) {
		return nil, apperror.NewValidationf("github app tool: offset %d is past the end of the diff (%d bytes)", offset, len(patches))
	}
	// Snap a mid-rune offset back to the start of its character. A caller that
	// computes an offset arithmetically would otherwise get a page that begins
	// with a broken UTF-8 sequence, or (when the backing-off end lands on the
	// same byte) a zero-length page that reports next_offset == offset and never
	// advances.
	for offset > 0 && offset < len(patches) && !utf8.RuneStart(patches[offset]) {
		offset--
	}

	// The page is bounded twice: by the requested byte count, and by what actually
	// fits in the model's tool-result envelope. The second bound has to be measured
	// on the rendered response — the index rides along and JSON escaping inflates
	// the patch text (every newline becomes \n, every quote \") — and it has to be
	// applied *before* the range is recorded as read, or the evidence would name
	// bytes the model context silently cut away.
	end := pageEnd(patches, offset, diffPage(limit))
	text := diffPageText(diff, files, pathFilter, patches, offset, end)
	budget := h.pageBudget()
	if budget > 0 {
		for guard := 0; guard < 8; guard++ {
			over := diffEnvelopeBytes(text) - budget
			if over <= 0 {
				break
			}
			shrunk := end - over
			if floor := offset + runeWidth(patches, offset); shrunk < floor {
				shrunk = floor
			}
			if shrunk >= end {
				break
			}
			end = pageEnd(patches, offset, shrunk-offset)
			text = diffPageText(diff, files, pathFilter, patches, offset, end)
		}
		// Fail closed. If even the smallest page — a clipped message, a bounded
		// index, one character of patch — does not fit the deployment's bound, the
		// model would be shown a tail-cut response, so no page can be handed over
		// and no range may be recorded as read. Serving it anyway is exactly the
		// "approved bytes nobody saw" failure this paging exists to close.
		if over := diffEnvelopeBytes(text) - budget; over > 0 {
			return nil, apperror.NewValidationf("github app tool: one diff page cannot fit this deployment's tool-result bound (page budget %d bytes, %d over); raise MCP_TOOL_RESULT_MAX_BYTES or the github-pr-diff override, or read narrowly with path=<substring> (a narrowed read is not review evidence)", budget, over)
		}
	}

	// Evidence is the whole patch text of this commit, not one page of it: a
	// prompt-injected run must not be able to approve a 632 KB promotion after
	// reading 80 KB of it. Pages accumulate; the aggregate fact is recorded only
	// once the pages tile [0,len(patches)).
	evidenced := false
	if pathFilter == "" && len(files) > 0 {
		mcp.MarkRunEvidence(ctx, diffPageKey(repo, diff.SHA, offset, end))
		if diffFullyRead(ctx, repo, diff.SHA, len(patches)) {
			mcp.MarkRunEvidence(ctx, diffEvidenceKey(repo, diff.SHA))
			evidenced = true
		}
	}

	return &mcp.ToolResult{
		Content: []mcp.ContentBlock{{Type: "text", Text: text}},
		StructuredContent: map[string]any{
			"sha":         diff.SHA,
			"files":       len(diff.Files),
			"returned":    len(files),
			"path":        pathFilter,
			"total_bytes": len(patches),
			"offset":      offset,
			"next_offset": nextOffset(end, len(patches)),
			"evidence":    evidenced,
		},
	}, nil
}

// diffPageText renders one page of a commit diff: the page declaration and the
// file index first — the model boundary truncates a tool result tail-first (see
// agents.DefaultToolResultMaxBytes), so anything a reader must not miss sits at
// the top — then the patch slice [offset,end).
func diffPageText(diff *githubapp.CommitDiff, files []githubapp.CommitFile, pathFilter, patches string, offset, end int) string {
	message := strings.TrimSpace(diff.Message)
	if len(message) > maxDiffMessageBytes {
		message = truncateUTF8(message, maxDiffMessageBytes) + "\n… (commit message truncated)"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "commit %s\n%s\n\n", diff.SHA, message)
	fmt.Fprintf(&b, "files: %d", len(files))
	if len(files) != len(diff.Files) {
		fmt.Fprintf(&b, " of %d", len(diff.Files))
	}
	if pathFilter != "" {
		fmt.Fprintf(&b, " (path filter %q; a filtered read is not review evidence)", pathFilter)
	}
	fmt.Fprintf(&b, ", patch bytes: %d\n", len(patches))
	fmt.Fprintf(&b, "page: bytes %d..%d of %d", offset, end, len(patches))
	if end < len(patches) {
		fmt.Fprintf(&b, " — call again with offset=%d for the rest\n", end)
	} else {
		b.WriteString(" — end of diff\n")
	}
	b.WriteString("--- index ---\n")
	indexStart := b.Len()
	indexed := 0
	for _, file := range files {
		if indexed >= maxIndexFiles || b.Len()-indexStart >= maxDiffIndexBytes {
			break
		}
		note := ""
		if file.Patch == "" && file.Additions+file.Deletions > 0 {
			// GitHub omits the patch body for binary and oversized files: say so,
			// so "no hunk text" is never read as "no change".
			note = "  (no patch body from the API)"
		}
		fmt.Fprintf(&b, "%s\t+%d/-%d\t%dB\t%s%s\n", file.Status, file.Additions, file.Deletions, len(file.Patch), file.Filename, note)
		indexed++
	}
	if indexed < len(files) {
		fmt.Fprintf(&b, "… and %d more files (index capped at %d files / %d bytes; the patch text below covers the whole selection)\n",
			len(files)-indexed, maxIndexFiles, maxDiffIndexBytes)
	}
	fmt.Fprintf(&b, "--- patches [%d,%d) of %d bytes ---\n", offset, end, len(patches))
	b.WriteString(patches[offset:end])
	if end < len(patches) {
		fmt.Fprintf(&b, "\n\n[page: bytes %d..%d of %d; next_offset=%d]\n", offset, end, len(patches), end)
	} else {
		fmt.Fprintf(&b, "\n\n[end of diff: %d bytes]\n", len(patches))
	}
	return b.String()
}

// diffEnvelopeBytes is the size of a page as the model sees it: the executor
// hands a tool result over as {"ok":true,"result":<text>}, which agents bounds
// at agents.DefaultToolResultMaxBytes.
func diffEnvelopeBytes(text string) int {
	encoded, err := json.Marshal(map[string]any{"ok": true, "result": text})
	if err != nil {
		return len(text)
	}
	return len(encoded)
}

// runeWidth is the byte length of the character at offset (1 for an invalid
// byte, 0 past the end) — the smallest slice a page may carry and still advance.
func runeWidth(s string, offset int) int {
	if offset >= len(s) {
		return 0
	}
	if _, size := utf8.DecodeRuneInString(s[offset:]); size > 0 {
		return size
	}
	return 1
}

// nextOffset is the offset to read on from, or 0 when this page reached the end.
func nextOffset(end, total int) int {
	if end >= total {
		return 0
	}
	return end
}

// diffPage bounds one requested page: the hard ceiling an explicit limit may
// reach and the default when the caller names none. Both stay under the model
// boundary that cuts every tool result (agents.DefaultToolResultMaxBytes, 128 KiB
// / MCP_TOOL_RESULT_MAX_BYTES), and executePRDiff additionally fits the rendered
// response into diffEnvelopeBudget, so a page arrives whole and the trailing page
// declaration is never the part that gets cut.
func diffPage(limit int) int {
	if limit <= 0 {
		return defaultDiffPageBytes
	}
	if limit > maxDiffPageBytes {
		return maxDiffPageBytes
	}
	return limit
}

// pageEnd returns the exclusive end of a page starting at offset, backed off to a
// UTF-8 rune boundary so a multi-byte character is never split across pages.
func pageEnd(s string, offset, limit int) int {
	end := offset + limit
	if end >= len(s) {
		return len(s)
	}
	for end > offset && !utf8.RuneStart(s[end]) {
		end--
	}
	// A limit smaller than the character at offset would back off to an empty
	// page, whose next_offset repeats the offset and never advances — an agent
	// following it would re-request the same page forever. Serve the whole
	// character instead: a page is never empty while bytes remain.
	if end == offset && offset < len(s) {
		end = offset + runeWidth(s, offset)
		if end > len(s) {
			end = len(s)
		}
	}
	return end
}

// executeReviewSubmit is moved verbatim from domain/githubapp/tools.go: it
// enforces the live-head binding and the same-run evidence rule, then posts the
// review, the reviewed check and the merge-guard marker with byte-exact text.
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
		return nil, fmt.Errorf("github app tool: PR head is %s, not the reviewed %s; re-review the new head", state.HeadSHA, headSHA)
	}
	// Same-run binding: the verdict may only name a commit whose non-empty diff
	// this run read through github-pr-diff — in full, page by page. A prompt
	// injection in PR text cannot make the agent approve a change it never
	// fetched (and an empty diff has nothing to approve). Fail closed when the run
	// carries no evidence, as a validation error: the caller has to be told how to
	// finish, and an internal error hides the instruction behind a 500.
	if !mcp.RunHasEvidence(ctx, diffEvidenceKey(repo, headSHA)) {
		return nil, apperror.NewValidationf("github app tool: %s@%s was not read in full by this run; call github-pr-diff for the exact reviewed commit and continue with offset=<next_offset> on each page until it reports the end of the diff, then submit", repo, headSHA)
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
			return nil, fmt.Errorf("review gate: %w", gateErr)
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
