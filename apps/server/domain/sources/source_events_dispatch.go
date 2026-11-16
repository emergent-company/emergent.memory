package sources

import (
	"context"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/emergent-company/emergent.memory/domain/agents"
	"github.com/emergent-company/emergent.memory/domain/githubapp"
	"github.com/emergent-company/emergent.memory/domain/graph"
	"github.com/emergent-company/emergent.memory/internal/config"
	"github.com/emergent-company/emergent.memory/pkg/logger"
)

// Source event sentinel errors.
var (
	ErrSourceAppNotFound           = errors.New("sources: source app not found")
	ErrSourceEventInvalidSignature = errors.New("sources: invalid webhook signature")
	ErrSourceEventMalformed        = errors.New("sources: malformed webhook payload")
	ErrSourceEventNoCredentials    = errors.New("sources: source app credentials are not readable")
)

const asyncSourceEventTimeout = 2 * time.Minute

// SourceEventDispatcher verifies provider webhooks and dispatches matching
// source triggers to agent runs.
type SourceEventDispatcher struct {
	repo        *Repository
	agents      *agents.Repository
	key         []byte
	apiBaseURL  string
	minter      *githubapp.TokenMinter
	newClient   func(baseURL, token string) *githubapp.Client
	workObjects SourceWorkObjectStore
	objectTypes ObjectTypeProvisioner
	reviewGates *ReviewGateService
	log         *slog.Logger
}

// SetObjectTypes installs the schema-pack provisioner so an issue event can
// ensure GitHubIssue exists on a source created before the type was added.
func (d *SourceEventDispatcher) SetObjectTypes(p ObjectTypeProvisioner) {
	d.objectTypes = p
}

// SetReviewGateService installs the server-owned review aggregation service.
func (d *SourceEventDispatcher) SetReviewGateService(g *ReviewGateService) {
	d.reviewGates = g
}

// SourceWorkObjectStore is the graph upsert surface the workflow dispatcher
// needs. A source without a workflow keeps the direct trigger path.
type SourceWorkObjectStore interface {
	CreateOrUpdate(ctx context.Context, projectID uuid.UUID, req *graph.CreateGraphObjectRequest, actorID *uuid.UUID) (*graph.GraphObjectResponse, bool, error)
	FindHeadObjectByTypeAndKey(ctx context.Context, projectID, objType, key string) (*graph.GraphObject, error)
}

// WireSourceWorkObjectStore injects the graph service into the dispatcher.
func WireSourceWorkObjectStore(d *SourceEventDispatcher, store SourceWorkObjectStore) {
	if d != nil {
		d.workObjects = store
	}
}

// NewSourceEventDispatcher builds the dispatcher from the deployment config.
func NewSourceEventDispatcher(repo *Repository, agentsRepo *agents.Repository, cfg *config.Config, log *slog.Logger) *SourceEventDispatcher {
	keyHex, baseURL := "", ""
	if cfg != nil {
		keyHex = cfg.LLMProvider.EncryptionKey
		baseURL = cfg.GitHubSource.APIBaseURL
	}
	key, err := hex.DecodeString(keyHex)
	if err != nil && log != nil {
		log.Error("sources: invalid LLM_ENCRYPTION_KEY; source app credentials are unreadable", "error", err)
		key = nil
	}
	return &SourceEventDispatcher{
		repo:       repo,
		agents:     agentsRepo,
		key:        key,
		apiBaseURL: baseURL,
		minter:     githubapp.NewTokenMinter(baseURL, nil),
		newClient: func(baseURL, token string) *githubapp.Client {
			return githubapp.NewClient(baseURL, token, nil)
		},
		log: log.With(logger.Scope("sources.events")),
	}
}

// VerifiedSourceEvent is an authenticated, parsed delivery ready to process.
type VerifiedSourceEvent struct {
	Event    string
	Delivery string
	App      *SourceApp
	Payload  *githubapp.WebhookPayload
}

// Verify resolves the App, verifies the signature, and parses the payload. It
// never trusts the body before the App's secret is verified.
func (d *SourceEventDispatcher) Verify(ctx context.Context, webhookKey uuid.UUID, event, delivery, signature string, body []byte) (*VerifiedSourceEvent, error) {
	app, err := d.repo.FindSourceAppByWebhookKey(ctx, webhookKey)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrSourceAppNotFound
		}
		return nil, err
	}
	if len(d.key) == 0 {
		return nil, ErrSourceEventNoCredentials
	}
	secret, err := OpenSourceBlob(d.key, app.WebhookSecretEncrypted)
	if err != nil {
		return nil, ErrSourceEventNoCredentials
	}
	if !githubapp.VerifyWebhook(string(secret), body, signature) {
		return nil, ErrSourceEventInvalidSignature
	}
	if strings.TrimSpace(delivery) == "" {
		return nil, fmt.Errorf("%w: missing delivery id", ErrSourceEventMalformed)
	}
	payload, err := githubapp.ParseWebhook(body)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrSourceEventMalformed, err)
	}
	return &VerifiedSourceEvent{Event: event, Delivery: delivery, App: app, Payload: payload}, nil
}

// Process routes a verified delivery. It is safe to run after the inbound
// request has returned 202.
func (d *SourceEventDispatcher) Process(ctx context.Context, v *VerifiedSourceEvent) error {
	switch v.Event {
	case "pull_request":
		return d.handlePullRequest(ctx, v)
	case "issues":
		return d.handleIssues(ctx, v)
	case "installation":
		return d.handleInstallation(ctx, v)
	case "installation_repositories":
		return d.handleInstallationRepositories(ctx, v)
	default:
		return nil
	}
}

// ProcessAsync processes a verified delivery on a detached, bounded context.
func (d *SourceEventDispatcher) ProcessAsync(v *VerifiedSourceEvent) {
	ctx, cancel := context.WithTimeout(context.Background(), asyncSourceEventTimeout)
	defer cancel()
	if err := d.Process(ctx, v); err != nil && d.log != nil {
		d.log.Warn("source event processing failed", "event", v.Event, "delivery", v.Delivery, "error", err)
	}
}

// HandleWebhook is the synchronous verify+process path.
func (d *SourceEventDispatcher) HandleWebhook(ctx context.Context, webhookKey uuid.UUID, event, delivery, signature string, body []byte) error {
	v, err := d.Verify(ctx, webhookKey, event, delivery, signature, body)
	if err != nil || v == nil {
		return err
	}
	return d.Process(ctx, v)
}

func (d *SourceEventDispatcher) handlePullRequest(ctx context.Context, v *VerifiedSourceEvent) error {
	p := v.Payload
	repoName := p.Repository.FullName
	if p.AuthoredBy(v.App.Slug) {
		return nil
	}
	sources, err := d.repo.ListSourcesByAppAndInstallation(ctx, v.App.ID, strconv.FormatInt(p.Installation.ID, 10))
	if err != nil {
		return err
	}
	allowed := make([]*Source, 0, len(sources))
	for _, src := range sources {
		if repoAllowed(src.Config, repoName) {
			allowed = append(allowed, src)
		}
	}
	if len(allowed) == 0 {
		return nil
	}

	subject := fmt.Sprintf("%s#%d", repoName, p.PullNumber())
	if p.Action == "closed" || p.Action == "converted_to_draft" {
		for _, src := range allowed {
			linkState := "closed"
			if p.Action == "converted_to_draft" {
				linkState = "draft"
			}
			link, err := d.ensureIssuePRLink(ctx, src, p, &githubapp.PullRequestState{
				HeadSHA: p.PullRequest.Head.SHA, State: linkState,
			}, subject)
			if err != nil {
				return err
			}
			if link != nil && link.State == "merged" && link.ClosesIssue {
				token, terr := d.mintToken(ctx, v.App, p.Installation.ID, repoName)
				if terr == nil {
					if cerr := d.newClient(d.apiBaseURL, token).CloseIssue(ctx, repoName, link.IssueNumber); cerr != nil && d.log != nil {
						d.log.Warn("failed to close merged issue", "issue", link.IssueNumber, "repo", repoName, "error", cerr)
					}
				}
			}
			if handled, err := d.processWorkflowEvent(ctx, src, nil, &githubapp.PullRequestState{
				HeadSHA: p.PullRequest.Head.SHA, State: "closed",
			}, p, subject); err != nil {
				return err
			} else if handled {
				continue
			}
			runs, err := d.repo.CancelSourceSubjectClaims(ctx, src.ProjectID, src.ID, subject)
			if err != nil {
				return err
			}
			d.cancelRuns(ctx, runs)
		}
		return nil
	}
	if p.ShouldSkipPullRequest() {
		return nil
	}
	if !p.IsRelevantPullRequestAction() && p.Action != "labeled" && p.Action != "unlabeled" {
		return nil
	}

	// One installation token scoped to the event repo is enough for every source.
	token, err := d.mintToken(ctx, v.App, p.Installation.ID, repoName)
	if err != nil {
		return err
	}
	state, err := d.newClient(d.apiBaseURL, token).GetPullRequest(ctx, repoName, p.PullNumber())
	if err != nil {
		return err
	}
	if state.Draft || state.Merged || (state.State != "" && state.State != "open") {
		return nil
	}

	for _, src := range allowed {
		if err := d.dispatchPullRequestSource(ctx, v, src, subject, state); err != nil {
			return err
		}
	}
	return nil
}

func (d *SourceEventDispatcher) dispatchPullRequestSource(ctx context.Context, v *VerifiedSourceEvent, src *Source, subject string, state *githubapp.PullRequestState) error {
	p := v.Payload
	repoName := p.Repository.FullName
	if _, err := d.ensureIssuePRLink(ctx, src, p, state, subject); err != nil {
		return err
	}
	ev := &SourceEvent{
		ProjectID:   src.ProjectID,
		SourceID:    src.ID,
		DeliveryID:  v.Delivery,
		EventType:   "pull_request",
		Action:      p.Action,
		SubjectType: "pull_request",
		SubjectRef:  subject,
		HeadSHA:     p.PullRequest.Head.SHA,
		Payload: map[string]any{
			"repo":     repoName,
			"number":   p.PullNumber(),
			"title":    p.PullRequest.Title,
			"headSha":  p.PullRequest.Head.SHA,
			"baseSha":  p.PullRequest.Base.SHA,
			"baseRef":  p.PullRequest.Base.Ref,
			"headRef":  p.PullRequest.Head.Ref,
			"action":   p.Action,
			"delivery": v.Delivery,
		},
	}
	inserted, err := d.repo.InsertSourceEvent(ctx, ev)
	if err != nil {
		return err
	}
	if !inserted {
		return nil
	}
	if err := d.processPullRequestSource(ctx, v, src, subject, state, ev); err != nil {
		// Leave no row behind: a redelivery must be able to retry.
		_ = d.repo.DeleteSourceEvent(ctx, src.ProjectID, ev.ID)
		return err
	}
	return nil
}

// ensureReviewSubject emits the provider-owned review subject and snapshots the
// reviewer agents that match it. Reviewer selection is agent-driven; this
// function never writes a reviewer list into the source.
func (d *SourceEventDispatcher) ensureReviewSubject(ctx context.Context, v *VerifiedSourceEvent, src *Source, p *githubapp.WebhookPayload, state *githubapp.PullRequestState, subject string) error {
	if d.workObjects == nil || p == nil || src == nil {
		return nil
	}
	repoName := p.Repository.FullName
	headSHA := p.PullRequest.Head.SHA
	if state != nil && state.HeadSHA != "" {
		headSHA = state.HeadSHA
	}
	labels := make([]string, 0)
	if state != nil && len(state.Labels) > 0 {
		labels = append(labels, state.Labels...)
	} else {
		for _, label := range p.PullRequest.Labels {
			if name := strings.TrimSpace(label.Name); name != "" {
				labels = append(labels, name)
			}
		}
	}
	title := p.PullRequest.Title
	if title == "" {
		title = subject
	}
	key := fmt.Sprintf("github:%s:%s#%d", src.ID.String(), repoName, p.PullNumber())
	props := map[string]any{
		"repo": repoName, "pr_number": p.PullNumber(), "title": title,
		"head_sha": headSHA, "base_sha": p.PullRequest.Base.SHA,
		"state": p.PullRequest.State, "labels": labels, "status": "open",
	}
	resp, _, err := d.workObjects.CreateOrUpdate(ctx, src.ProjectID, &graph.CreateGraphObjectRequest{
		Type: "GitHubPullRequest", Key: &key, Name: &title, Properties: props,
	}, nil)
	if err != nil {
		return fmt.Errorf("sources: upsert review subject: %w", err)
	}
	if d.reviewGates != nil && resp != nil {
		gate, err := d.reviewGates.Ensure(ctx, src.ProjectID, "GitHubPullRequest", subject, headSHA, props)
		if err != nil {
			return fmt.Errorf("sources: ensure review gate: %w", err)
		}
		if gate != nil && gate.Status == ReviewGatePending && v != nil && v.App != nil {
			if token, terr := d.mintToken(ctx, v.App, p.Installation.ID, repoName); terr == nil {
				_ = d.newClient(d.apiBaseURL, token).CreateCheck(ctx, repoName, headSHA, "reviewed", "in_progress", "", "reviewed: pending", "Waiting for all required reviewers.")
			}
		}
	}
	return nil
}

func (d *SourceEventDispatcher) processPullRequestSource(ctx context.Context, v *VerifiedSourceEvent, src *Source, subject string, state *githubapp.PullRequestState, ev *SourceEvent) error {
	p := v.Payload
	if err := d.ensureReviewSubject(ctx, v, src, p, state, subject); err != nil {
		d.markEvent(ctx, src.ProjectID, ev.ID, SourceEventFailed, nil, err)
		return err
	}
	if p.Action == "labeled" || p.Action == "unlabeled" {
		d.markEvent(ctx, src.ProjectID, ev.ID, SourceEventDispatched, nil, nil)
		return nil
	}
	handled, err := d.processWorkflowEvent(ctx, src, ev, state, p, subject)
	if err != nil {
		return err
	}
	if handled {
		return nil
	}
	if _, err := d.ensureIssuePRLink(ctx, src, p, state, subject); err != nil {
		return err
	}
	claim, superseded, err := d.repo.ClaimSourceSubject(ctx, src.ProjectID, src.ID, subject, state.HeadSHA, p.PullRequest.Head.SHA)
	if errors.Is(err, ErrSourceEventStaleHead) {
		d.markEvent(ctx, src.ProjectID, ev.ID, SourceEventSkipped, nil, nil)
		return nil
	}
	if err != nil {
		d.markEvent(ctx, src.ProjectID, ev.ID, SourceEventFailed, nil, err)
		return err
	}
	d.cancelRuns(ctx, superseded)
	if claim.RunID != nil {
		d.markEvent(ctx, src.ProjectID, ev.ID, SourceEventDispatched, claim.RunID, nil)
		return nil
	}

	trigger, err := d.matchingTrigger(ctx, src.ProjectID, src.ID, "pull_request", p.Action)
	if err != nil {
		d.markEvent(ctx, src.ProjectID, ev.ID, SourceEventFailed, nil, err)
		return err
	}
	if trigger == nil {
		d.markEvent(ctx, src.ProjectID, ev.ID, SourceEventSkipped, nil, nil)
		return nil
	}

	repoName := p.Repository.FullName
	message := fmt.Sprintf("Handle github_app pull_request %s (%s) for %s at %s", p.Action, subject, repoName, state.HeadSHA)
	metadata := map[string]any{
		"sourceId":       src.ID.String(),
		"provider":       ProviderTypeGitHubApp,
		"eventType":      "pull_request",
		"subjectType":    "pull_request",
		"subjectRef":     subject,
		"repo":           repoName,
		"prNumber":       p.PullNumber(),
		"headSha":        state.HeadSHA,
		"baseSha":        p.PullRequest.Base.SHA,
		"baseRef":        state.BaseRef,
		"headRef":        state.HeadRef,
		"installationId": p.Installation.ID,
		"deliveryId":     v.Delivery,
	}
	opts := agents.CreateRunQueuedOptions{
		Queue:           defaultSourceQueue(trigger.Queue),
		TriggerMessage:  &message,
		TriggerMetadata: metadata,
		Invocation:      agents.NormalizeInvocation(trigger.Invocation),
		// Source runs are external: PR authors can open/synchronize a PR. The
		// review tools get a narrow verified-source column instead of blanket
		// internal trust.
		TrustedInternal: false,
		SourceSubject: &agents.SourceSubject{
			SourceID: src.ID.String(), Repo: repoName, Kind: "pull_request", PRNumber: p.PullNumber(), HeadSHA: state.HeadSHA,
		},
	}
	triggerID := trigger.ID.String()
	opts.SourceTriggerID = &triggerID
	if opts.Invocation != nil {
		opts.Priority = opts.Invocation.Priority
	}
	run, err := d.agents.CreateRunQueued(ctx, trigger.AgentID.String(), 1, opts)
	if err != nil {
		_, _ = d.repo.SetSourceEventClaimState(ctx, claim.ProjectID, claim.ID, SourceClaimPending)
		d.markEvent(ctx, src.ProjectID, ev.ID, SourceEventFailed, nil, err)
		return fmt.Errorf("sources: enqueue source trigger: %w", err)
	}
	runID, err := uuid.Parse(run.ID)
	if err != nil {
		d.markEvent(ctx, src.ProjectID, ev.ID, SourceEventFailed, nil, err)
		return err
	}
	attached, err := d.repo.SetSourceEventClaimRun(ctx, claim.ProjectID, claim.ID, runID, SourceClaimRunning)
	if err != nil {
		d.markEvent(ctx, src.ProjectID, ev.ID, SourceEventFailed, nil, err)
		return err
	}
	if !attached {
		_, _ = d.agents.RequestRunCancellation(ctx, run.ID)
		d.markEvent(ctx, src.ProjectID, ev.ID, SourceEventSkipped, nil, nil)
		return nil
	}
	d.markEvent(ctx, src.ProjectID, ev.ID, SourceEventDispatched, &runID, nil)
	return nil
}

var prClosingIssueRe = regexp.MustCompile(`(?i)(?:closes|fixes|resolves)\s+#(\d+)`)
var prAnyIssueRefRe = regexp.MustCompile(`(?i)(?:refs|references)\s+#(\d+)`)
var prBranchIssueRe = regexp.MustCompile(`(?:^|/)(\d+)[-_]`)
var prTitleIssueRe = regexp.MustCompile(`\(#(\d+)\)`)

func issueReferenceForPullRequest(p *githubapp.WebhookPayload) (int, bool) {
	if p == nil {
		return 0, false
	}
	if m := prClosingIssueRe.FindStringSubmatch(p.PullRequest.Body); len(m) == 2 {
		n, _ := strconv.Atoi(m[1])
		if n > 0 {
			return n, true
		}
	}
	if m := prAnyIssueRefRe.FindStringSubmatch(p.PullRequest.Body); len(m) == 2 {
		n, _ := strconv.Atoi(m[1])
		if n > 0 {
			return n, false
		}
	}
	if m := prBranchIssueRe.FindStringSubmatch(p.PullRequest.Head.Ref); len(m) == 2 {
		n, _ := strconv.Atoi(m[1])
		if n > 0 {
			return n, false
		}
	}
	if m := prTitleIssueRe.FindStringSubmatch(p.PullRequest.Title); len(m) == 2 {
		n, _ := strconv.Atoi(m[1])
		if n > 0 {
			return n, false
		}
	}
	return 0, false
}

func issueNumberForPullRequest(p *githubapp.WebhookPayload) int {
	n, _ := issueReferenceForPullRequest(p)
	return n
}

// ensureIssuePRLink resolves the coding issue referenced by a PR and records a
// server-owned link. The link is source-independent, so the review source can
// resolve the issue even though the coding source owns the item.
func (d *SourceEventDispatcher) ensureIssuePRLink(ctx context.Context, src *Source, p *githubapp.WebhookPayload, state *githubapp.PullRequestState, _ string) (*SourceIssuePRLink, error) {
	if d.workObjects == nil || p == nil || src == nil {
		return nil, nil
	}
	if d.objectTypes != nil {
		if err := d.objectTypes.EnsureObjectTypes(ctx, src.ProjectID.String(), (&githubAppProvider{}).ObjectTypes()); err != nil {
			return nil, fmt.Errorf("sources: ensure GitHub issue/pr object types: %w", err)
		}
	}
	repoName := p.Repository.FullName
	issueNumber, closesIssue := issueReferenceForPullRequest(p)
	if issueNumber <= 0 {
		return nil, nil
	}
	candidates, err := d.repo.FindGitHubAppSourcesForRepo(ctx, src.ProjectID, repoName)
	if err != nil {
		return nil, err
	}
	var issue *graph.GraphObject
	var issueSourceID uuid.UUID
	for i := range candidates {
		key := fmt.Sprintf("github:%s:%s#%d", candidates[i].ID, repoName, issueNumber)
		found, ferr := d.workObjects.FindHeadObjectByTypeAndKey(ctx, src.ProjectID.String(), "GitHubIssue", key)
		if ferr != nil {
			return nil, ferr
		}
		if found != nil {
			issue = found
			issueSourceID = candidates[i].ID
			break
		}
	}
	if issue == nil {
		return nil, nil
	}
	prNumber := p.PullNumber()
	merged := p.PullRequest.Merged || (state != nil && state.State == "closed" && p.PullRequest.Merged)
	draft := p.Action == "converted_to_draft"
	closed := p.Action == "closed" || (state != nil && state.State == "closed" && !merged)
	linkState := p.PullRequest.State
	if linkState == "" {
		linkState = "open"
	}
	issueStatus := "review"
	if merged && closesIssue {
		linkState = "merged"
		issueStatus = "done"
	} else if closed {
		linkState = "closed"
		issueStatus = "cancelled"
	} else if draft {
		linkState = "draft"
	}
	branch := p.PullRequest.Head.Ref
	headSHA := p.PullRequest.Head.SHA
	if state != nil && state.HeadSHA != "" {
		headSHA = state.HeadSHA
	}
	prURL := p.PullRequest.HTMLURL
	if prURL == "" {
		prURL = fmt.Sprintf("https://github.com/%s/pull/%d", repoName, prNumber)
	}
	link := &SourceIssuePRLink{
		ProjectID: src.ProjectID, Repo: repoName, PRNumber: prNumber,
		IssueCanonicalID: issue.CanonicalID, IssueNumber: issueNumber, IssueSourceID: issueSourceID,
		PRURL: prURL, Branch: branch, HeadSHA: headSHA, State: linkState, ClosesIssue: closesIssue,
	}
	if err := d.repo.UpsertSourceIssuePRLink(ctx, link); err != nil {
		return nil, err
	}
	issueProps := make(map[string]any, len(issue.Properties)+4)
	for k, v := range issue.Properties {
		issueProps[k] = v
	}
	issueProps["pr_number"] = prNumber
	issueProps["pr_url"] = prURL
	issueProps["pr_branch"] = branch
	issueProps["pr_head_sha"] = headSHA
	if issue.Status != nil && (*issue.Status == "done" || *issue.Status == "cancelled") && issueStatus == "review" {
		issueStatus = *issue.Status
	}
	issueProps["status"] = issueStatus
	issueKey := fmt.Sprintf("github:%s:%s#%d", issueSourceID, repoName, issueNumber)
	if _, _, err := d.workObjects.CreateOrUpdate(ctx, src.ProjectID, &graph.CreateGraphObjectRequest{
		Type: "GitHubIssue", Key: &issueKey, Name: issue.Name, Status: &issueStatus, Properties: issueProps,
	}, nil); err != nil {
		return nil, err
	}
	return link, nil

}

func (d *SourceEventDispatcher) handleIssues(ctx context.Context, v *VerifiedSourceEvent) error {
	p := v.Payload
	if p == nil || !p.IsIssue() || p.AuthoredByIssue(v.App.Slug) || p.AuthoredBySender(v.App.Slug) {
		return nil
	}
	repoName := p.Repository.FullName
	sources, err := d.repo.ListSourcesByAppAndInstallation(ctx, v.App.ID, strconv.FormatInt(p.Installation.ID, 10))
	if err != nil {
		return err
	}
	for _, src := range sources {
		if !repoAllowed(src.Config, repoName) {
			continue
		}
		if d.objectTypes != nil {
			if err := d.objectTypes.EnsureObjectTypes(ctx, src.ProjectID.String(), (&githubAppProvider{}).ObjectTypes()); err != nil {
				return fmt.Errorf("sources: ensure GitHub issue object types: %w", err)
			}
		}
		if err := d.processIssueSource(ctx, v, src); err != nil {
			return err
		}
	}
	return nil
}

const (
	issueClaimStatusLabel  = "status: in-progress"
	issueClaimBlockedLabel = "status: blocked"
	issueClaimManagerLabel = "manager: memory-coding"
)

func issueClaimLabelStyle(name string) (color, description string) {
	if name == issueClaimManagerLabel {
		return "5319e7", "Memory coding issue claim manager"
	}
	return "fbca04", "Issue claimed by an agent"
}

// issueClaimResult describes the live GitHub claim state and the labels the
// board item should mirror. configured=false means the source has no
// assignee_login, so the legacy no-claim path is preserved.
type issueClaimResult struct {
	configured bool
	ours       bool
	foreign    bool
	labels     []string
}

func sourceAssigneeLogin(src *Source) string {
	if src == nil || src.Config == nil {
		return ""
	}
	login, _ := src.Config["assignee_login"].(string)
	return strings.TrimSpace(login)
}

func issueLabelNames(issue *githubapp.IssueState) []string {
	if issue == nil {
		return nil
	}
	out := make([]string, 0, len(issue.Labels))
	for _, label := range issue.Labels {
		if name := strings.TrimSpace(label.Name); name != "" {
			out = append(out, name)
		}
	}
	return out
}

func hasIssueLabel(labels []string, want string) bool {
	for _, label := range labels {
		if strings.EqualFold(strings.TrimSpace(label), want) {
			return true
		}
	}
	return false
}

func mergeIssueLabels(labels []string, extra ...string) []string {
	out := append([]string(nil), labels...)
	for _, label := range extra {
		if !hasIssueLabel(out, label) {
			out = append(out, label)
		}
	}
	return out
}

func hasIssueAssignee(issue *githubapp.IssueState, login string) bool {
	for _, assignee := range issue.Assignees {
		if strings.EqualFold(assignee.Login, login) {
			return true
		}
	}
	return false
}

// issueClaimConflict separates our own live claim from a peer claim. A peer
// claim is any manager label other than memory-coding, a lifecycle claim with
// no manager (legacy), a blocked status, or an assignee other than the
// configured responsible user.
func issueClaimConflict(issue *githubapp.IssueState, responsible string) (ours, foreign bool) {
	labels := issueLabelNames(issue)
	manager := ""
	for _, label := range labels {
		if strings.HasPrefix(strings.ToLower(label), "manager: ") {
			manager = strings.TrimSpace(label[len("manager: "):])
			break
		}
	}
	inProgress := hasIssueLabel(labels, issueClaimStatusLabel)
	blocked := hasIssueLabel(labels, issueClaimBlockedLabel)

	if blocked {
		return false, true
	}
	if manager != "" && !strings.EqualFold(manager, "memory-coding") {
		return false, true
	}
	if inProgress && !strings.EqualFold(manager, "memory-coding") {
		return false, true
	}
	if responsible == "" {
		if len(issue.Assignees) > 0 {
			return false, true
		}
	} else {
		for _, assignee := range issue.Assignees {
			if assignee.Login != "" && !strings.EqualFold(assignee.Login, responsible) {
				return false, true
			}
		}
	}
	return inProgress && strings.EqualFold(manager, "memory-coding"), false
}

func (d *SourceEventDispatcher) claimIssue(ctx context.Context, v *VerifiedSourceEvent, src *Source, repoName, agentName string) (issueClaimResult, error) {
	login := sourceAssigneeLogin(src)
	if login == "" {
		return issueClaimResult{}, nil
	}
	token, err := d.mintToken(ctx, v.App, v.Payload.Installation.ID, repoName)
	if err != nil {
		return issueClaimResult{}, fmt.Errorf("sources: mint issue-claim token: %w", err)
	}
	client := d.newClient(d.apiBaseURL, token)
	issue, err := client.GetIssue(ctx, repoName, v.Payload.Issue.Number)
	if err != nil {
		return issueClaimResult{}, fmt.Errorf("sources: read issue claim state: %w", err)
	}
	result := issueClaimResult{configured: true, labels: issueLabelNames(issue)}
	ours, foreign := issueClaimConflict(issue, login)
	if foreign {
		result.foreign = true
		return result, nil
	}
	result.ours = ours

	missing := make([]string, 0, 2)
	if !hasIssueLabel(result.labels, issueClaimStatusLabel) {
		missing = append(missing, issueClaimStatusLabel)
	}
	if !hasIssueLabel(result.labels, issueClaimManagerLabel) {
		missing = append(missing, issueClaimManagerLabel)
	}
	needAssignee := !hasIssueAssignee(issue, login)

	// GitHub silently ignores unknown labels on an issue write. Create the
	// claim labels first, then assign, then verify the add response.
	for _, label := range missing {
		color, description := issueClaimLabelStyle(label)
		if err := client.EnsureIssueLabel(ctx, repoName, label, color, description); err != nil {
			return result, fmt.Errorf("sources: ensure issue claim label %q: %w", label, err)
		}
	}
	if needAssignee {
		if err := client.AddIssueAssignees(ctx, repoName, v.Payload.Issue.Number, []string{login}); err != nil {
			return result, fmt.Errorf("sources: assign responsible issue owner %q: %w", login, err)
		}
	}
	if len(missing) > 0 {
		added, err := client.AddIssueLabels(ctx, repoName, v.Payload.Issue.Number, missing)
		if err == nil {
			for _, label := range missing {
				if !hasIssueLabel(added, label) {
					err = fmt.Errorf("label %q was not applied", label)
					break
				}
			}
		}
		if err != nil {
			for _, label := range missing {
				_ = client.RemoveIssueLabel(ctx, repoName, v.Payload.Issue.Number, label)
			}
			if needAssignee {
				_ = client.RemoveIssueAssignees(ctx, repoName, v.Payload.Issue.Number, []string{login})
			}
			return result, fmt.Errorf("sources: add issue claim labels: %w", err)
		}
		result.labels = mergeIssueLabels(result.labels, missing...)
	}
	if !ours {
		body := fmt.Sprintf("Claimed by Memory coding agent `%s` for responsible owner @%s. The agent will open a PR that references this issue.", agentName, login)
		if err := client.CreateIssueComment(ctx, repoName, v.Payload.Issue.Number, body); err != nil && d.log != nil {
			d.log.Warn("failed to post issue claim receipt", "repo", repoName, "issue", v.Payload.Issue.Number, "error", err)
		}
	}
	return result, nil
}

func (d *SourceEventDispatcher) processIssueSource(ctx context.Context, v *VerifiedSourceEvent, src *Source) error {
	p := v.Payload
	issue := p.Issue
	repoName := p.Repository.FullName
	subject := fmt.Sprintf("%s#%d", repoName, issue.Number)
	labels := make([]any, 0, len(issue.Labels))
	for _, label := range issue.Labels {
		if name := strings.TrimSpace(label.Name); name != "" {
			labels = append(labels, name)
		}
	}
	payload := map[string]any{
		"repo": repoName, "number": issue.Number, "title": issue.Title,
		"state": issue.State, "url": issue.HTMLURL, "updatedAt": issue.UpdatedAt,
		"labels": labels, "action": p.Action, "delivery": v.Delivery,
	}
	ev := &SourceEvent{
		ProjectID: src.ProjectID, SourceID: src.ID, DeliveryID: v.Delivery,
		EventType: "issues", Action: p.Action, SubjectType: "issue", SubjectRef: subject,
		Payload: payload,
	}
	inserted, err := d.repo.InsertSourceEvent(ctx, ev)
	if err != nil {
		return err
	}
	if !inserted {
		return nil
	}
	if d.workObjects == nil {
		err := fmt.Errorf("sources: issue source has no work-object store")
		d.markEvent(ctx, src.ProjectID, ev.ID, SourceEventFailed, nil, err)
		return err
	}

	trigger, err := d.matchingTrigger(ctx, src.ProjectID, src.ID, "issues", p.Action)
	if err != nil {
		d.markEvent(ctx, src.ProjectID, ev.ID, SourceEventFailed, nil, err)
		return err
	}
	agentName := ""
	if trigger != nil {
		agentName = d.triggerAgentName(ctx, src.ProjectID, trigger.AgentID)
	}
	closed := p.Action == "closed" || p.Action == "deleted" || strings.EqualFold(issue.State, "closed")
	key := fmt.Sprintf("github:%s:%s#%d", src.ID.String(), repoName, issue.Number)
	title := issue.Title
	if title == "" {
		title = subject
	}
	existing, err := d.workObjects.FindHeadObjectByTypeAndKey(ctx, src.ProjectID.String(), "GitHubIssue", key)
	if err != nil {
		d.markEvent(ctx, src.ProjectID, ev.ID, SourceEventFailed, nil, err)
		return err
	}
	status := "ready"
	assignee := ""
	if existing != nil {
		if existing.Status != nil {
			status = *existing.Status
		}
		if existing.Assignee != nil {
			assignee = *existing.Assignee
		}
	}
	wantStart := trigger != nil && !closed && p.IsRelevantIssueAction() &&
		(existing == nil || existing.Status == nil || status == "ready" ||
			(p.Action == "reopened" && (status == "cancelled" || status == "done")))
	if wantStart && agentName == "" {
		err := fmt.Errorf("sources: issue trigger %s references an agent that cannot be resolved", trigger.ID)
		d.markEvent(ctx, src.ProjectID, ev.ID, SourceEventFailed, nil, err)
		return err
	}
	checkClaim := trigger != nil && !closed && (wantStart || (existing != nil && (status == "ready" || status == "in_progress")))
	claim := issueClaimResult{}
	if checkClaim {
		claim, err = d.claimIssue(ctx, v, src, repoName, agentName)
		if err != nil {
			d.markEvent(ctx, src.ProjectID, ev.ID, SourceEventFailed, nil, err)
			return err
		}
		if claim.foreign {
			if existing == nil {
				d.markEvent(ctx, src.ProjectID, ev.ID, SourceEventSkipped, nil, nil)
				return nil
			}
			status = "blocked"
			assignee = ""
		}
	}
	if closed {
		if status != "done" {
			status = "cancelled"
		}
		assignee = ""
	} else if claim.foreign {
		status = "blocked"
		assignee = ""
	} else if trigger != nil && p.IsRelevantIssueAction() {
		switch {
		case existing == nil || existing.Status == nil:
			status = "ready"
			assignee = agentName
		case status == "ready":
			assignee = agentName
		case p.Action == "reopened" && (status == "cancelled" || status == "done"):
			status = "ready"
			assignee = agentName
		}
		if assignee == "" {
			err := fmt.Errorf("sources: issue trigger %s references an agent that cannot be resolved", trigger.ID)
			d.markEvent(ctx, src.ProjectID, ev.ID, SourceEventFailed, nil, err)
			return err
		}
	}
	boardLabels := labels
	if claim.configured {
		boardLabels = make([]any, 0, len(claim.labels))
		for _, label := range claim.labels {
			boardLabels = append(boardLabels, label)
		}
	}
	props := map[string]any{
		"repo": repoName, "issue_number": issue.Number, "title": title,
		"body": truncateUTF8(issue.Body, 16000), "state": issue.State,
		"labels": boardLabels, "url": issue.HTMLURL, "source_id": src.ID.String(), "status": status,
	}
	if claim.foreign {
		props["claim_conflict"] = true
	}
	if trigger != nil && !closed && assignee != "" {
		// Only the server-owned trigger id is stored. The invocation (including
		// the credential request) is re-resolved from kb.source_triggers by the
		// board, so a writable graph property can never mint a write token.
		props["source_trigger_id"] = trigger.ID.String()
	}
	var assigneePtr *string
	if assignee != "" {
		assigneePtr = &assignee
	}
	resp, _, err := d.workObjects.CreateOrUpdate(ctx, src.ProjectID, &graph.CreateGraphObjectRequest{
		Type: "GitHubIssue", Key: &key, Name: &title, Status: &status,
		Assignee: assigneePtr, Properties: props,
	}, nil)
	if err != nil {
		d.markEvent(ctx, src.ProjectID, ev.ID, SourceEventFailed, nil, err)
		return err
	}
	if (closed || claim.foreign) && resp != nil {
		d.cancelSubjectRuns(ctx, resp.CanonicalID.String())
	}
	state := SourceEventDispatched
	if trigger == nil || closed || claim.foreign {
		state = SourceEventSkipped
	}
	d.markEvent(ctx, src.ProjectID, ev.ID, state, nil, nil)
	return nil
}

// triggerAgentName resolves the runtime agent's display name for a board item
// assignee. Board routing matches assignees by name.
func (d *SourceEventDispatcher) triggerAgentName(ctx context.Context, projectID uuid.UUID, agentID uuid.UUID) string {
	if d.agents == nil || agentID == uuid.Nil {
		return ""
	}
	pid := projectID.String()
	agent, err := d.agents.FindByID(ctx, agentID.String(), &pid)
	if err != nil || agent == nil {
		return ""
	}
	return agent.Name
}

func (d *SourceEventDispatcher) handleInstallation(ctx context.Context, v *VerifiedSourceEvent) error {
	p := v.Payload
	instKey := strconv.FormatInt(p.Installation.ID, 10)
	sources, err := d.repo.ListSourcesByAppAndInstallation(ctx, v.App.ID, instKey)
	if err != nil {
		return err
	}
	switch p.Action {
	case "deleted", "suspend":
		for _, src := range sources {
			runs, err := d.repo.CancelAllSourceClaims(ctx, src.ProjectID, src.ID)
			if err != nil {
				return err
			}
			d.cancelRuns(ctx, runs)
		}
		return d.repo.DisableSourcesByInstallation(ctx, v.App.ID, instKey)
	case "created", "new_permissions_accepted", "unsuspend":
		if len(p.Repositories) == 0 {
			return nil
		}
		for _, src := range sources {
			if err := d.repo.UpdateSourceRepos(ctx, src.ID, webhookRepoNames(p.Repositories)); err != nil {
				return err
			}
		}
		return nil
	default:
		return nil
	}
}

func (d *SourceEventDispatcher) handleInstallationRepositories(ctx context.Context, v *VerifiedSourceEvent) error {
	p := v.Payload
	sources, err := d.repo.ListSourcesByAppAndInstallation(ctx, v.App.ID, strconv.FormatInt(p.Installation.ID, 10))
	if err != nil {
		return err
	}
	for _, src := range sources {
		repos := applySourceRepoDelta(sourceRepoList(src.Config), p.RepositoriesAdded, p.RepositoriesRemoved)
		if err := d.repo.UpdateSourceRepos(ctx, src.ID, repos); err != nil {
			return err
		}
		for _, removed := range p.RepositoriesRemoved {
			if removed.FullName == "" {
				continue
			}
			runs, err := d.repo.CancelSourceClaimsForRepo(ctx, src.ProjectID, src.ID, removed.FullName)
			if err != nil {
				return err
			}
			d.cancelRuns(ctx, runs)
		}
	}
	return nil
}

func (d *SourceEventDispatcher) matchingTrigger(ctx context.Context, projectID, sourceID uuid.UUID, eventType, action string) (*SourceTrigger, error) {
	triggers, err := d.repo.ListEnabledTriggersForSource(ctx, projectID, sourceID)
	if err != nil {
		return nil, err
	}
	for _, trigger := range triggers {
		if trigger.EventType != eventType {
			continue
		}
		if triggerAllowsAction(trigger.EventFilter, action) {
			return trigger, nil
		}
	}
	return nil, nil
}

func (d *SourceEventDispatcher) mintToken(ctx context.Context, app *SourceApp, installationID int64, repoName string) (string, error) {
	if len(d.key) == 0 {
		return "", ErrSourceEventNoCredentials
	}
	privateKey, err := OpenSourceBlob(d.key, app.PrivateKeyEncrypted)
	if err != nil {
		return "", ErrSourceEventNoCredentials
	}
	return d.minter.MintInstallationToken(ctx, app.AppID, privateKey, installationID, []string{repoName},
		map[string]string{
			"contents":      "read",
			"pull_requests": "write",
			"issues":        "write",
			"checks":        "write",
		})
}

func (d *SourceEventDispatcher) markEvent(ctx context.Context, projectID, id uuid.UUID, state string, runID *uuid.UUID, cause error) {
	var msg *string
	if cause != nil {
		s := cause.Error()
		msg = &s
	}
	if err := d.repo.SetSourceEventState(ctx, projectID, id, state, runID, msg); err != nil && d.log != nil {
		d.log.Warn("failed to record source event state", "event", id, "error", err)
	}
}

func (d *SourceEventDispatcher) cancelRuns(ctx context.Context, runIDs []uuid.UUID) {
	for _, id := range runIDs {
		if _, err := d.agents.RequestRunCancellation(ctx, id.String()); err != nil && d.log != nil {
			d.log.Warn("failed to cancel superseded source run", "runId", id, "error", err)
		}
	}
}

func triggerAllowsAction(filter map[string]any, action string) bool {
	if len(filter) == 0 {
		return true
	}
	raw, ok := filter["actions"]
	if !ok {
		return true
	}
	switch v := raw.(type) {
	case []any:
		for _, item := range v {
			if fmt.Sprint(item) == action {
				return true
			}
		}
	case []string:
		for _, item := range v {
			if item == action {
				return true
			}
		}
	}
	return false
}

func repoAllowed(cfg map[string]any, repoName string) bool {
	for _, repo := range sourceRepoList(cfg) {
		if strings.EqualFold(repo, repoName) {
			return true
		}
	}
	return false
}

func sourceRepoList(cfg map[string]any) []string {
	if cfg == nil {
		return nil
	}
	switch v := cfg["repos"].(type) {
	case []string:
		return v
	case []any:
		out := make([]string, 0, len(v))
		for _, item := range v {
			if s := strings.TrimSpace(fmt.Sprint(item)); s != "" {
				out = append(out, s)
			}
		}
		return out
	default:
		return nil
	}
}

func webhookRepoNames(repos []githubapp.WebhookRepo) []string {
	out := make([]string, 0, len(repos))
	for _, repo := range repos {
		if repo.FullName != "" {
			out = append(out, repo.FullName)
		}
	}
	return normalizeSourceRepos(out)
}

func applySourceRepoDelta(current []string, added, removed []githubapp.WebhookRepo) []string {
	set := map[string]string{}
	for _, repo := range current {
		set[strings.ToLower(repo)] = repo
	}
	for _, repo := range added {
		if repo.FullName != "" {
			set[strings.ToLower(repo.FullName)] = repo.FullName
		}
	}
	for _, repo := range removed {
		delete(set, strings.ToLower(repo.FullName))
	}
	out := make([]string, 0, len(set))
	for _, repo := range set {
		out = append(out, repo)
	}
	return normalizeSourceRepos(out)
}

func normalizeSourceRepos(repos []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(repos))
	for _, repo := range repos {
		repo = strings.TrimSpace(repo)
		if repo == "" {
			continue
		}
		key := strings.ToLower(repo)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, repo)
	}
	return out
}

func defaultSourceQueue(queue string) string {
	if strings.TrimSpace(queue) == "" {
		return "default"
	}
	return strings.TrimSpace(queue)
}

// processWorkflowEvent emits parent/lane work items for a source that has a
// workflow configured. The board reconciler, not this dispatcher, starts runs.
func (d *SourceEventDispatcher) processWorkflowEvent(ctx context.Context, src *Source, ev *SourceEvent, state *githubapp.PullRequestState, p *githubapp.WebhookPayload, subject string) (bool, error) {
	wf, err := d.repo.GetSourceWorkflow(ctx, src.ProjectID, src.ID)
	if err != nil || wf == nil || !wf.Enabled {
		return false, err
	}
	if d.workObjects == nil {
		return false, fmt.Errorf("sources: workflow source has no work-object store")
	}
	var cfg SourceWorkflowConfig
	raw, _ := json.Marshal(wf.Config)
	if err := json.Unmarshal(raw, &cfg); err != nil || len(cfg.Reviewers) == 0 {
		return false, fmt.Errorf("sources: workflow config is invalid: %w", err)
	}
	repoName := p.Repository.FullName
	parentKey := fmt.Sprintf("github:%s:%s#%d", src.ID.String(), repoName, p.PullNumber())
	title := p.PullRequest.Title
	if title == "" {
		title = subject
	}
	parentStatus := "open"
	laneStatus := "ready"
	closing := p.Action == "closed" || p.Action == "converted_to_draft" || p.PullRequest.State == "closed"
	reopening := p.Action == "reopened" || p.Action == "ready_for_review"
	if closing {
		parentStatus = "closed"
		laneStatus = "cancelled"
	}
	parentProps := map[string]any{
		"repo": repoName, "pr_number": p.PullNumber(), "head_sha": state.HeadSHA,
		"base_sha": p.PullRequest.Base.SHA, "title": title, "state": p.PullRequest.State, "status": parentStatus,
	}
	if _, _, err := d.workObjects.CreateOrUpdate(ctx, src.ProjectID, &graph.CreateGraphObjectRequest{
		Type: "GitHubPullRequest", Key: &parentKey, Name: &title, Status: &parentStatus, Properties: parentProps,
	}, nil); err != nil {
		if ev != nil {
			d.markEvent(ctx, src.ProjectID, ev.ID, SourceEventFailed, nil, err)
		}
		return true, err
	}
	for _, reviewer := range cfg.Reviewers {
		laneKey := parentKey + ":" + reviewer.Lane
		laneName := title + " · " + reviewer.Lane
		status := laneStatus
		existing, ferr := d.workObjects.FindHeadObjectByTypeAndKey(ctx, src.ProjectID.String(), "GitHubReviewLane", laneKey)
		if ferr != nil {
			if ev != nil {
				d.markEvent(ctx, src.ProjectID, ev.ID, SourceEventFailed, nil, ferr)
			}
			return true, ferr
		}
		if existing != nil && existing.Properties != nil {
			oldHead, _ := existing.Properties["head_sha"].(string)
			if oldHead == state.HeadSHA {
				if closing {
					// Close/draft always ends the live lane, even on the same head.
					d.cancelSubjectRuns(ctx, existing.CanonicalID.String())
					status = "cancelled"
				} else if reopening && existing.Status != nil && *existing.Status == "cancelled" {
					status = "ready"
				} else {
					// Same head replay: never reset a lane that is running,
					// blocked, reviewed or done.
					if existing.Status != nil {
						status = *existing.Status
					}
				}
			} else if oldHead != "" {
				d.cancelSubjectRuns(ctx, existing.CanonicalID.String())
			}
		}
		laneProps := map[string]any{
			"lane": reviewer.Lane, "agent": reviewer.Agent, "source_id": src.ID.String(), "repo": repoName, "pr_number": p.PullNumber(),
			"head_sha": state.HeadSHA, "base_sha": p.PullRequest.Base.SHA, "title": title,
			"state": p.PullRequest.State, "status": status,
		}
		assignee := reviewer.Agent
		if _, _, err := d.workObjects.CreateOrUpdate(ctx, src.ProjectID, &graph.CreateGraphObjectRequest{
			Type: "GitHubReviewLane", Key: &laneKey, Name: &laneName, Status: &status,
			Assignee: &assignee, Properties: laneProps,
		}, nil); err != nil {
			if ev != nil {
				d.markEvent(ctx, src.ProjectID, ev.ID, SourceEventFailed, nil, err)
			}
			return true, err
		}
	}
	if ev != nil {
		d.markEvent(ctx, src.ProjectID, ev.ID, SourceEventDispatched, nil, nil)
	}
	return true, nil
}

func (d *SourceEventDispatcher) cancelSubjectRuns(ctx context.Context, canonicalID string) {
	if d.agents == nil || canonicalID == "" {
		return
	}
	runs, err := d.agents.FindLiveRunsForSubject(ctx, canonicalID)
	if err != nil {
		return
	}
	for _, run := range runs {
		if run != nil {
			_, _ = d.agents.RequestRunCancellation(ctx, run.ID)
		}
	}
}
