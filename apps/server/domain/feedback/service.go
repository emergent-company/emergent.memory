// Package feedback implements user- and agent-authored feedback intake.
//
// Feedback submitted through POST /api/feedback or the feedback-submit MCP tool
// is filed as a GitHub issue in the repository configured by
// FEEDBACK_GITHUB_REPO. The submitter's identity comes from the authenticated
// session, never from the request body, so an issue always names who filed it.
//
// The feature is deliberately optional: an empty FEEDBACK_GITHUB_TOKEN or
// FEEDBACK_GITHUB_REPO disables it, and there is no built-in destination — the
// operator of a deployment is the only one who can say where its users' feedback
// should go. That is the expected state on a self-hosted deployment, which has no
// Memory team to receive the issue.
package feedback

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/uptrace/bun"

	"github.com/emergent-company/emergent.memory/internal/config"
	"github.com/emergent-company/emergent.memory/internal/version"
	"github.com/emergent-company/emergent.memory/pkg/logger"
)

const (
	// DefaultRateLimitPerHour bounds successful submissions per user. Agents
	// retry; a human does not need a higher ceiling.
	DefaultRateLimitPerHour = 5

	// maxMessageBytes bounds the user-authored message. GitHub's issue body cap
	// is 64 KiB and the surrounding context is small, so 16 KiB leaves ample
	// room for a detailed report without letting one call consume the budget.
	maxMessageBytes = 16 * 1024

	// dedupeWindow suppresses an identical submission from the same user, which
	// is what a retrying agent produces.
	dedupeWindow = 10 * time.Minute
)

// Categories accepted from a submitter. Unknown values are refused rather than
// silently dropped so a caller can correct the call.
var validCategories = map[string]struct{}{
	"bug":       {},
	"feature":   {},
	"usability": {},
	"docs":      {},
	"other":     {},
}

// Severities accepted from a submitter. Empty means "not specified".
var validSeverities = map[string]struct{}{
	"":       {},
	"low":    {},
	"medium": {},
	"high":   {},
}

// ErrNotConfigured reports that this deployment has no feedback destination.
var ErrNotConfigured = errors.New("feedback is not enabled on this deployment")

// ValidationError is a caller-correctable input problem.
type ValidationError struct {
	Message string
}

func (e *ValidationError) Error() string { return e.Message }

// RateLimitError reports that the caller exceeded the per-user submission
// budget. RetryAfter is the time until the oldest hit leaves the window.
type RateLimitError struct {
	RetryAfter time.Duration
}

func (e *RateLimitError) Error() string {
	return fmt.Sprintf("feedback rate limit reached; retry in %s", e.RetryAfter.Round(time.Second))
}

// Source identifies the surface the feedback was submitted from.
type Source string

const (
	SourceAPI Source = "api"
	SourceMCP Source = "mcp"
)

// Submission is one piece of feedback plus the trusted identity the server
// attached to it. Identity fields are populated by the handler/tool from the
// authentication context; they are never read from the request body.
type Submission struct {
	Message string
	// Category is one of bug, feature, usability, docs, other. Empty defaults
	// to "other".
	Category string
	// Severity is one of low, medium, high. Empty means unspecified.
	Severity string
	// IncludeContext attaches project, environment and client details. The
	// submitter's account identity is always attached regardless.
	IncludeContext bool

	Source Source
	// Client is the calling client (User-Agent for REST, the MCP client name
	// when known).
	Client string

	UserID    string
	UserEmail string

	ProjectID string
	OrgID     string
}

// Result is the issue a submission produced.
type Result struct {
	IssueNumber int
	IssueURL    string
	// Duplicate is true when an identical submission within the dedupe window
	// returned the issue created by the first call.
	Duplicate bool
}

// Service files feedback as GitHub issues.
type Service struct {
	cfg    config.FeedbackConfig
	env    string
	db     bun.IDB
	log    *slog.Logger
	client issueCreator
	guard  *submissionGuard
	now    func() time.Time
}

// NewService builds the feedback service. A missing token disables the feature
// without failing startup; a malformed repo/token configuration logs a warning
// and disables it, because feedback must never take the API down.
func NewService(cfg *config.Config, db bun.IDB, log *slog.Logger) *Service {
	if log == nil {
		log = slog.Default()
	}
	s := &Service{
		db:  db,
		log: log.With(logger.Scope("feedback")),
		now: time.Now,
	}
	if cfg != nil {
		s.cfg = cfg.Feedback
		s.env = cfg.Environment
	}
	if s.cfg.RateLimitPerHour <= 0 {
		s.cfg.RateLimitPerHour = DefaultRateLimitPerHour
	}
	s.guard = newSubmissionGuard(s.cfg.RateLimitPerHour, dedupeWindow, s.now)

	if !s.cfg.Enabled || strings.TrimSpace(s.cfg.GitHubToken) == "" || strings.TrimSpace(s.cfg.Repo) == "" {
		return s
	}
	client, err := newGitHubClient(s.cfg)
	if err != nil {
		s.log.Warn("feedback disabled: invalid GitHub configuration", logger.Error(err))
		return s
	}
	s.client = client
	return s
}

// Enabled reports whether feedback has a working destination. The REST handler
// and the MCP tool both fail with ErrNotConfigured when it does not.
func (s *Service) Enabled() bool {
	return s != nil && s.client != nil
}

// Submit validates, rate-limits and files one submission.
func (s *Service) Submit(ctx context.Context, sub Submission) (*Result, error) {
	if !s.Enabled() {
		return nil, ErrNotConfigured
	}

	if err := s.validate(&sub); err != nil {
		return nil, err
	}

	identity := sub.UserID
	if identity == "" {
		identity = sub.UserEmail
	}
	if identity == "" {
		// RequireAuth guarantees a user, but the service is also reachable from
		// the MCP agent-run path; fail closed rather than filing anonymous
		// feedback.
		return nil, &ValidationError{Message: "feedback requires an authenticated user"}
	}

	dedupeKey := submissionFingerprint(sub)
	if dup := s.guard.lookupDuplicate(identity, dedupeKey); dup != nil {
		return &Result{IssueNumber: dup.IssueNumber, IssueURL: dup.IssueURL, Duplicate: true}, nil
	}
	if retryAfter := s.guard.reserve(identity); retryAfter > 0 {
		return nil, &RateLimitError{RetryAfter: retryAfter}
	}

	projectName, projectOrgID := s.projectMeta(ctx, sub.ProjectID)
	orgID := sub.OrgID
	if orgID == "" {
		orgID = projectOrgID
	}

	payload := buildIssue(issueInput{
		Submission:    sub,
		ProjectName:   projectName,
		OrgID:         orgID,
		Environment:   s.env,
		ServerVersion: version.Version,
		ServerCommit:  version.GitCommit,
		Label:         s.cfg.Label,
		SubmittedAt:   s.now(),
	})

	issue, err := s.client.CreateIssue(ctx, payload.Title, payload.Body, payload.Labels)
	if err != nil {
		s.log.Warn("failed to file feedback issue", logger.Error(err))
		return nil, fmt.Errorf("file feedback issue: %w", err)
	}

	result := &Result{IssueNumber: issue.Number, IssueURL: issue.HTMLURL}
	s.guard.rememberResult(identity, dedupeKey, result)
	s.log.Info("feedback filed",
		slog.Int("issue_number", issue.Number),
		slog.String("source", string(sub.Source)),
		slog.String("project_id", sub.ProjectID),
	)
	return result, nil
}

// validate normalises and bounds the caller-controlled fields.
func (s *Service) validate(sub *Submission) error {
	sub.Message = strings.TrimSpace(sub.Message)
	if sub.Message == "" {
		return &ValidationError{Message: "message is required: describe what you were trying to do and what happened"}
	}
	if len(sub.Message) > maxMessageBytes {
		return &ValidationError{Message: fmt.Sprintf("message is %d bytes; the limit is %d", len(sub.Message), maxMessageBytes)}
	}

	sub.Category = strings.ToLower(strings.TrimSpace(sub.Category))
	if sub.Category == "" {
		sub.Category = "other"
	}
	if _, ok := validCategories[sub.Category]; !ok {
		return &ValidationError{Message: "category must be one of: bug, feature, usability, docs, other"}
	}

	sub.Severity = strings.ToLower(strings.TrimSpace(sub.Severity))
	if _, ok := validSeverities[sub.Severity]; !ok {
		return &ValidationError{Message: "severity must be one of: low, medium, high"}
	}

	if sub.Source == "" {
		sub.Source = SourceAPI
	}
	return nil
}

// projectMeta resolves the project name/organization for the issue context. A
// lookup failure is not fatal: the submission still names the project id.
func (s *Service) projectMeta(ctx context.Context, projectID string) (name, orgID string) {
	if s.db == nil || projectID == "" {
		return "", ""
	}
	var row struct {
		Name           string `bun:"name"`
		OrganizationID string `bun:"organization_id"`
	}
	err := s.db.NewSelect().
		TableExpr("kb.projects").
		ColumnExpr("name, organization_id").
		Where("id = ?", projectID).
		Scan(ctx, &row)
	if err != nil {
		if !errors.Is(err, sql.ErrNoRows) {
			s.log.Warn("feedback: project lookup failed",
				slog.String("project_id", projectID), logger.Error(err))
		}
		return "", ""
	}
	return row.Name, row.OrganizationID
}

// normaliseID keeps an identifier only when it is a UUID. The REST surface reads
// project and organization ids from request headers, so they are
// caller-controlled: a stray value must not be rendered into the issue or drive a
// lookup that logs a warning on every submission.
func normaliseID(id string) string {
	id = strings.TrimSpace(id)
	if id == "" {
		return ""
	}
	if _, err := uuid.Parse(id); err != nil {
		return ""
	}
	return id
}

// submissionFingerprint is the dedupe key: identity is added by the guard, so
// this covers the content a retrying caller repeats.
func submissionFingerprint(sub Submission) string {
	h := sha256.New()
	for _, part := range []string{
		sub.Message,
		sub.Category,
		sub.Severity,
		sub.ProjectID,
		strconv.FormatBool(sub.IncludeContext),
	} {
		h.Write([]byte(part))
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))
}
