package agents

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/uptrace/bun"

	"github.com/emergent-company/emergent.memory/domain/apitoken"
	"github.com/emergent-company/emergent.memory/pkg/apperror"
)

// ============================================================================
// Interfaces (defined for testability; *Repository / *Handler / *AgentExecutor
// satisfy them)
// ============================================================================

// ShareRepo is the data-access surface the share service uses. *Repository
// satisfies it.
type ShareRepo interface {
	// links
	CreateShareLink(ctx context.Context, link *AgentShareLink) error
	ListShareLinksByProject(ctx context.Context, projectID string) ([]*AgentShareLink, error)
	GetShareLinkByID(ctx context.Context, linkID string, projectID *string) (*AgentShareLink, error)
	GetShareLinkByTokenID(ctx context.Context, apiTokenID string) (*AgentShareLink, error)
	UpdateShareLink(ctx context.Context, linkID, projectID, label string, config *ShareLinkConfig, expiresAt *time.Time) (bool, error)
	RevokeShareLink(ctx context.Context, linkID, projectID string) (bool, error)
	ReplaceShareLinkToken(ctx context.Context, tx bun.Tx, linkID, newTokenID string) error
	TouchShareLink(ctx context.Context, linkID string) error
	// sessions
	CreateShareSession(ctx context.Context, s *AgentShareSession) error
	CreateShareSessionIfUnderCap(ctx context.Context, s *AgentShareSession, maxActive int) (bool, error)
	GetShareSessionByID(ctx context.Context, sessionID, linkID, endUserRef string) (*AgentShareSession, error)
	ListShareSessionsByEndUser(ctx context.Context, linkID, endUserRef string, includeArchived bool) ([]*AgentShareSession, error)
	ListShareSessionsByProject(ctx context.Context, projectID string) ([]shareSessionProjectRow, error)
	GetShareSessionByProject(ctx context.Context, sessionID, projectID string) (*shareSessionProjectRow, error)
	CountActiveShareSessions(ctx context.Context, linkID, endUserRef string) (int, error)
	ArchiveShareSession(ctx context.Context, sessionID, linkID, endUserRef string) (bool, error)
	TouchShareSession(ctx context.Context, sessionID string) error
	FindShareSessionByRunAndEndUser(ctx context.Context, linkID, endUserRef, runID string) (*AgentShareSession, error)
	CountActiveShareRuns(ctx context.Context, linkID string) (int, error)
	CreateShareRunIfUnderLimit(ctx context.Context, linkID string, maxConcurrent int, opts CreateRunOptions) (*AgentRun, error)
	CountSessionToolApprovals(ctx context.Context, shareLinkID, acpSessionID string) (int, error)
	ReserveAndDecideShareApproval(ctx context.Context, shareLinkID, acpSessionID, questionID, decision, message, decidedBy string, maxApprovals int) (bool, error)
	ListPendingQuestionsForACPSession(ctx context.Context, acpSessionID string) ([]*AgentQuestion, error)
	// end users
	UpsertShareEndUser(ctx context.Context, u *AgentShareEndUser) error
	FindShareEndUser(ctx context.Context, linkID, endUserRef string) (*AgentShareEndUser, error)
	// usage
	IncrementShareUsage(ctx context.Context, linkID string, periodStart time.Time, messages int, tokens int64, costUSD float64) error
	ReserveShareBudget(ctx context.Context, linkID string, periodStart time.Time, maxMessages int, maxTokens int64, maxCostUSD float64, perTurnTokens int64, perTurnCostUSD float64) error
	SumShareUsageSince(ctx context.Context, linkID string, since time.Time) (*ShareUsageAggregate, error)
	ListShareUsage(ctx context.Context, linkID string, limit int) ([]*AgentShareUsage, error)
	// access log + reaper
	CreateShareAccessLog(ctx context.Context, linkID, endUserRef, ipHash, action string) error
	ListPausedShareRuns(ctx context.Context) ([]shareReapCandidate, error)
	// shared agent lookups
	FindDefinitionByID(ctx context.Context, id string, projectID *string) (*AgentDefinition, error)
	GetOrgIDByProjectID(ctx context.Context, projectID string) (string, error)
	FindByName(ctx context.Context, projectID, name string) (*Agent, error)
	Create(ctx context.Context, agent *Agent) error
	CreateACPSession(ctx context.Context, session *ACPSession) error
	FindQuestionByID(ctx context.Context, id string) (*AgentQuestion, error)
	GetRunTokenUsage(ctx context.Context, runID string) (*RunTokenUsage, error)
	GetConversationFullHistory(ctx context.Context, acpSessionID string) ([]*ConversationHistoryItem, error)
}

// QuestionResponder is the shared question-respond/resume helper. *Handler
// satisfies it.
type QuestionResponder interface {
	RespondToQuestion(ctx context.Context, p RespondParams) (*AgentQuestionDTO, error)
}

// shareTokenService is the narrow apitoken surface the share service uses.
// *apitoken.Service satisfies it; tests inject a fake.
type shareTokenService interface {
	CreateAgentChatShareToken(ctx context.Context, projectID, name string, expiresAt *time.Time) (*apitoken.CreateApiTokenResponseDTO, error)
	RevokeEphemeral(ctx context.Context, tokenID string)
	RegenerateWith(ctx context.Context, tokenID, projectID, userID string, after func(context.Context, bun.Tx, string) error) (*apitoken.CreateApiTokenResponseDTO, error)
	GetByID(ctx context.Context, tokenID, projectID string) (*apitoken.GetApiTokenResponseDTO, error)
	GetUserProjectRole(ctx context.Context, projectID, userID string) (string, error)
}

// ============================================================================
// Service
// ============================================================================

// ShareService implements the public keyed agent-share feature.
type ShareService struct {
	repo      ShareRepo
	apiTokens shareTokenService
	runner    agentRunner
	responder QuestionResponder
	refSecret string // HMAC key for gateway-minted end-user refs (SHARE_REF_SECRET)
	log       *slog.Logger
}

// NewShareService constructs a ShareService.
func NewShareService(repo ShareRepo, apiTokens shareTokenService, runner agentRunner, responder QuestionResponder, refSecret string, log *slog.Logger) *ShareService {
	if log == nil {
		log = slog.Default()
	}
	return &ShareService{
		repo:      repo,
		apiTokens: apiTokens,
		runner:    runner,
		responder: responder,
		refSecret: refSecret,
		log:       log.With("component", "share-service"),
	}
}

// ============================================================================
// Binding resolver
// ============================================================================

// ResolveLinkByTokenID resolves a share binding from an API token ID. Scope
// alone never authorizes: the token must resolve to a live link -> agent
// definition -> project. Missing -> 401; revoked/expired -> 410.
func (s *ShareService) ResolveLinkByTokenID(ctx context.Context, apiTokenID string) (*ShareLinkBinding, error) {
	if apiTokenID == "" {
		return nil, apperror.New(http.StatusUnauthorized, "unauthorized", "share token required")
	}
	link, err := s.repo.GetShareLinkByTokenID(ctx, apiTokenID)
	if err != nil {
		return nil, err
	}
	if link == nil {
		return nil, apperror.New(http.StatusUnauthorized, "unauthorized", "share link not found")
	}
	if link.IsRevoked() {
		return nil, apperror.New(410, "share_link_revoked", "share link is revoked")
	}
	if link.IsExpired(time.Now()) {
		return nil, apperror.New(410, "share_link_expired", "share link is expired")
	}

	def, err := s.repo.FindDefinitionByID(ctx, link.AgentDefinitionID, &link.ProjectID)
	if err != nil {
		return nil, err
	}
	if def == nil {
		return nil, apperror.New(410, "share_link_agent_gone", "share link agent no longer exists")
	}

	orgID, _ := s.repo.GetOrgIDByProjectID(ctx, link.ProjectID)

	return &ShareLinkBinding{
		Link:       link,
		Definition: def,
		ProjectID:  link.ProjectID,
		OrgID:      orgID,
		Config:     link.EffectiveConfig(),
	}, nil
}

// ============================================================================
// End-user identity (gateway-minted ref verification)
// ============================================================================

// header names for the signed end-user reference contract.
const (
	shareEndUserRefHeader    = "X-End-User-Ref"
	shareEndUserRefSigHeader = "X-End-User-Ref-Sig"
)

// VerifyEndUserRefSig verifies the gateway-minted end-user reference signature:
// X-End-User-Ref-Sig = base64url-nopad(HMAC-SHA256(key=SHARE_REF_SECRET,
// message=<exact end_user_ref>)). Missing/invalid -> 401; unconfigured secret
// fails closed -> 503.
func (s *ShareService) VerifyEndUserRefSig(ref, sig string) error {
	if ref == "" {
		return apperror.New(http.StatusUnauthorized, "unauthorized", "end-user ref header required")
	}
	if sig == "" {
		return apperror.New(http.StatusUnauthorized, "unauthorized", "end-user ref signature header required")
	}
	if s.refSecret == "" {
		return apperror.New(http.StatusServiceUnavailable, "share_ref_secret_unconfigured",
			"share end-user identity verification is not configured")
	}

	mac := hmac.New(sha256.New, []byte(s.refSecret))
	mac.Write([]byte(ref))
	expected := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))

	if !hmac.Equal([]byte(expected), []byte(sig)) {
		return apperror.New(http.StatusUnauthorized, "unauthorized", "invalid end-user ref signature")
	}
	return nil
}

// EnsureEndUserExists verifies that (share_link_id, end_user_ref) exists in
// kb.agent_share_end_users. The signature proves the gateway minted the ref; the
// DB row proves it belongs to THIS link. Missing -> 404 (existence never leaks).
func (s *ShareService) EnsureEndUserExists(ctx context.Context, linkID, endUserRef string) error {
	eu, err := s.repo.FindShareEndUser(ctx, linkID, endUserRef)
	if err != nil {
		return err
	}
	if eu == nil {
		return apperror.New(http.StatusNotFound, "not_found", "end user not found")
	}
	return nil
}

// ============================================================================
// Owner link management
// ============================================================================

// EnsureProjectAdmin returns nil when userID is a project admin, mirroring the
// MCP share precedent. Owner mutations must call this before acting, so an
// authenticated non-admin cannot manage links for a project they don't
// administer.
func (s *ShareService) EnsureProjectAdmin(ctx context.Context, projectID, userID string) error {
	role, err := s.apiTokens.GetUserProjectRole(ctx, projectID, userID)
	if err != nil {
		return err
	}
	if role != "project_admin" {
		return apperror.NewForbidden("project admin role required to manage share links")
	}
	return nil
}

// EnsureProjectMember returns nil when userID holds any membership role in
// projectID. Owner read endpoints call this for OAuth/session auth, because
// RequireProjectTokenScope and RequireAPITokenScopes are no-ops for OAuth
// sessions and the caller-supplied :projectId would otherwise be trusted —
// letting a member of one project read another project's shared sessions.
// API-token requests are already scoped by RequireProjectTokenScope and never
// pass a userID.
func (s *ShareService) EnsureProjectMember(ctx context.Context, projectID, userID string) error {
	role, err := s.apiTokens.GetUserProjectRole(ctx, projectID, userID)
	if err != nil {
		return err
	}
	if role == "" {
		return apperror.NewForbidden("project membership required to view share sessions")
	}
	return nil
}

// CreateLink mints a reserved-scope token and creates a share link bound to an
// agent definition.
func (s *ShareService) CreateLink(ctx context.Context, projectID, agentDefinitionID, label, createdBy string, in *ShareLinkConfigInput) (*ShareLinkDTO, error) {
	if err := s.EnsureProjectAdmin(ctx, projectID, createdBy); err != nil {
		return nil, err
	}
	if projectID == "" {
		return nil, apperror.NewBadRequest("projectId is required")
	}
	if agentDefinitionID == "" {
		return nil, apperror.NewBadRequest("agentDefinitionId is required")
	}
	if strings.TrimSpace(label) == "" {
		return nil, apperror.NewBadRequest("label is required")
	}

	cfg := in.Apply(nil)

	def, err := s.repo.FindDefinitionByID(ctx, agentDefinitionID, &projectID)
	if err != nil {
		return nil, err
	}
	if def == nil {
		return nil, apperror.NewNotFound("AgentDefinition", agentDefinitionID)
	}

	var expiresAt *time.Time
	if cfg.LinkExpiryDays > 0 {
		t := time.Now().Add(time.Duration(cfg.LinkExpiryDays) * 24 * time.Hour)
		expiresAt = &t
	}

	tokenName := shareTokenName(label)
	token, err := s.apiTokens.CreateAgentChatShareToken(ctx, projectID, tokenName, expiresAt)
	if err != nil {
		return nil, err
	}

	link := &AgentShareLink{
		ProjectID:         projectID,
		AgentDefinitionID: agentDefinitionID,
		APITokenID:        token.ID,
		Label:             label,
		Config:            cfg,
		ExpiresAt:         expiresAt,
	}
	if createdBy != "" {
		link.CreatedByUserID = &createdBy
	}

	if err := s.repo.CreateShareLink(ctx, link); err != nil {
		// Best-effort revoke the orphan token so a failed insert never strands a
		// live reserved-scope credential.
		s.apiTokens.RevokeEphemeral(ctx, token.ID)
		return nil, err
	}

	return s.linkDTO(link, token.Token, token.TokenPrefix), nil
}

// ListLinks lists a project's share links, optionally filtered to one agent
// definition.
func (s *ShareService) ListLinks(ctx context.Context, projectID, agentDefinitionID string) ([]*ShareLinkDTO, error) {
	links, err := s.repo.ListShareLinksByProject(ctx, projectID)
	if err != nil {
		return nil, err
	}
	out := make([]*ShareLinkDTO, 0, len(links))
	for _, l := range links {
		if agentDefinitionID != "" && l.AgentDefinitionID != agentDefinitionID {
			continue
		}
		out = append(out, s.linkDTO(l, "", ""))
	}
	return out, nil
}

// GetLink returns a link by ID (owner-scoped).
func (s *ShareService) GetLink(ctx context.Context, linkID, projectID string) (*ShareLinkDTO, error) {
	link, err := s.repo.GetShareLinkByID(ctx, linkID, &projectID)
	if err != nil {
		return nil, err
	}
	if link == nil {
		return nil, apperror.New(http.StatusNotFound, "not_found", "share link not found")
	}
	return s.linkDTO(link, "", ""), nil
}

// UpdateLink updates label + config + expiry of a non-revoked link.
func (s *ShareService) UpdateLink(ctx context.Context, linkID, projectID, userID, label string, in *ShareLinkConfigInput) (*ShareLinkDTO, error) {
	if err := s.EnsureProjectAdmin(ctx, projectID, userID); err != nil {
		return nil, err
	}
	link, err := s.repo.GetShareLinkByID(ctx, linkID, &projectID)
	if err != nil {
		return nil, err
	}
	if link == nil {
		return nil, apperror.New(http.StatusNotFound, "not_found", "share link not found")
	}
	if link.IsRevoked() {
		return nil, apperror.New(409, "share_link_revoked", "share link is revoked")
	}

	newLabel := link.Label
	if strings.TrimSpace(label) != "" {
		newLabel = label
	}
	cfg := in.Apply(link.EffectiveConfig())

	// Recompute expires_at from the (possibly changed) LinkExpiryDays so the
	// extend/expire/never-expire controls actually take effect on the row
	// ResolveLinkByTokenID authorizes against.
	var expiresAt *time.Time
	if cfg.LinkExpiryDays > 0 {
		t := time.Now().Add(time.Duration(cfg.LinkExpiryDays) * 24 * time.Hour)
		expiresAt = &t
	}

	updated, err := s.repo.UpdateShareLink(ctx, linkID, projectID, newLabel, cfg, expiresAt)
	if err != nil {
		return nil, err
	}
	if !updated {
		return nil, apperror.New(http.StatusNotFound, "not_found", "share link not found or revoked")
	}

	link.Label = newLabel
	link.Config = cfg
	link.ExpiresAt = expiresAt
	return s.linkDTO(link, "", ""), nil
}

// RevokeLink revokes a link and its bound credential. The share key (the bound
// core.api_tokens row) is revoked immediately so it is unusable right away,
// even though the link's own expires_at has not passed.
func (s *ShareService) RevokeLink(ctx context.Context, linkID, projectID, userID string) error {
	if err := s.EnsureProjectAdmin(ctx, projectID, userID); err != nil {
		return err
	}
	link, err := s.repo.GetShareLinkByID(ctx, linkID, &projectID)
	if err != nil {
		return err
	}
	if link == nil {
		return apperror.New(http.StatusNotFound, "not_found", "share link not found")
	}

	revoked, err := s.repo.RevokeShareLink(ctx, linkID, projectID)
	if err != nil {
		return err
	}
	if !revoked {
		return apperror.New(http.StatusNotFound, "not_found", "share link not found or already revoked")
	}

	// Revoke the bound credential so the share key is immediately unusable.
	s.apiTokens.RevokeEphemeral(ctx, link.APITokenID)
	return nil
}

// RotateLink atomically revokes the old token and binds a new one (link ID is
// preserved) via apitoken.RegenerateWith.
func (s *ShareService) RotateLink(ctx context.Context, linkID, projectID, userID string) (*ShareLinkDTO, error) {
	if err := s.EnsureProjectAdmin(ctx, projectID, userID); err != nil {
		return nil, err
	}
	link, err := s.repo.GetShareLinkByID(ctx, linkID, &projectID)
	if err != nil {
		return nil, err
	}
	if link == nil {
		return nil, apperror.New(http.StatusNotFound, "not_found", "share link not found")
	}
	if link.IsRevoked() {
		return nil, apperror.New(409, "share_link_revoked", "share link is revoked")
	}

	newToken, err := s.apiTokens.RegenerateWith(ctx, link.APITokenID, projectID, userID, func(ctx context.Context, tx bun.Tx, newTokenID string) error {
		return s.repo.ReplaceShareLinkToken(ctx, tx, linkID, newTokenID)
	})
	if err != nil {
		return nil, err
	}

	link, _ = s.repo.GetShareLinkByID(ctx, linkID, &projectID)
	if link == nil {
		return nil, apperror.New(http.StatusNotFound, "not_found", "share link not found")
	}
	return s.linkDTO(link, newToken.Token, newToken.TokenPrefix), nil
}

// RevealKey returns the plaintext share key (raw emt_* token) for an existing
// link, decrypted from core.api_tokens.token_encrypted. Returns a clear
// not_recoverable error when the token cannot be decrypted (never a partial or
// ambiguous value).
func (s *ShareService) RevealKey(ctx context.Context, linkID, projectID, userID string) (string, error) {
	if err := s.EnsureProjectAdmin(ctx, projectID, userID); err != nil {
		return "", err
	}
	link, err := s.repo.GetShareLinkByID(ctx, linkID, &projectID)
	if err != nil {
		return "", err
	}
	if link == nil {
		return "", apperror.New(http.StatusNotFound, "not_found", "share link not found")
	}

	result, err := s.apiTokens.GetByID(ctx, link.APITokenID, projectID)
	if err != nil {
		return "", err
	}
	if result == nil || result.Token == "" {
		return "", apperror.New(http.StatusUnprocessableEntity, "not_recoverable",
			"share key cannot be recovered (token was not stored encrypted)")
	}
	return result.Token, nil
}

// GetUsage returns a link's recent usage buckets.
func (s *ShareService) GetUsage(ctx context.Context, linkID, projectID string, limit int) ([]*ShareUsageDTO, error) {
	link, err := s.repo.GetShareLinkByID(ctx, linkID, &projectID)
	if err != nil {
		return nil, err
	}
	if link == nil {
		return nil, apperror.New(http.StatusNotFound, "not_found", "share link not found")
	}
	rows, err := s.repo.ListShareUsage(ctx, linkID, limit)
	if err != nil {
		return nil, err
	}
	out := make([]*ShareUsageDTO, 0, len(rows))
	for _, r := range rows {
		out = append(out, &ShareUsageDTO{
			LinkID:      r.LinkID,
			PeriodStart: r.PeriodStart,
			Messages:    r.Messages,
			Tokens:      r.Tokens,
			CostUSD:     r.CostUSD,
			UpdatedAt:   r.UpdatedAt,
		})
	}
	return out, nil
}

// PublicConfig returns the sanitized public config (never prompt/system/project).
func (s *ShareService) PublicConfig(binding *ShareLinkBinding) *SharePublicConfigDTO {
	cfg := binding.Config
	def := binding.Definition
	dto := &SharePublicConfigDTO{
		LinkID:                   binding.Link.ID,
		AgentName:                def.Name,
		RequireEmail:             cfg.RequireEmail,
		ShowSessionList:          cfg.ShowSessionList,
		MaxMessageChars:          cfg.MaxMessageChars,
		AllowEndUserApprovals:    cfg.AllowEndUserApprovals,
		SandboxEnabled:           cfg.SandboxEnabled,
		RetentionDays:            cfg.RetentionDays,
		BudgetMaxMessages:        cfg.BudgetMaxMessages,
		BudgetMaxTokens:          cfg.BudgetMaxTokens,
		BudgetMaxCostUSD:         cfg.BudgetMaxCostUSD,
		MaxActiveSessionsPerUser: cfg.MaxActiveSessionsPerUser,
		MaxConcurrentRuns:        cfg.MaxConcurrentRuns,
		WelcomeMessage:           cfg.WelcomeMessage,
	}
	if def.Description != nil {
		dto.AgentDescription = *def.Description
	}
	if def.Model != nil {
		dto.Model = def.Model.Name
	}
	dto.Icon, dto.Color = shareAgentIconColor(def.UIConfig)
	return dto
}

// shareAgentIconColor parses an agent definition's uiConfig JSON blob and
// returns its declared icon (a bare Lucide kebab name, or an emoji/glyph) and
// color (an arbitrary CSS color). It is tolerant of absent (nil/empty), null,
// and {} payloads, and ignores non-string values — mirroring the gateway's
// agentUIOf parsing so the public share surface renders the owner's configured
// appearance.
func shareAgentIconColor(raw json.RawMessage) (icon, color string) {
	if len(raw) == 0 {
		return "", ""
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return "", ""
	}
	icon, _ = m["icon"].(string)
	color, _ = m["color"].(string)
	return icon, color
}

// ============================================================================
// End-user sessions
// ============================================================================

// CreateSession creates a share session for an end user (enforcing the session
// cap and the require_email gate) and returns its DTO.
func (s *ShareService) CreateSession(ctx context.Context, binding *ShareLinkBinding, endUserRef, email, title string) (*ShareSessionDTO, error) {
	if endUserRef == "" {
		return nil, apperror.NewBadRequest("endUserRef is required")
	}
	cfg := binding.Config

	if cfg.RequireEmail && strings.TrimSpace(email) == "" {
		return nil, apperror.New(403, "share_email_required", "an email is required to start a session")
	}

	if err := s.upsertEndUser(ctx, binding.Link.ID, endUserRef, email); err != nil {
		return nil, err
	}

	agentName := binding.Definition.Name
	acp := &ACPSession{ProjectID: binding.ProjectID, AgentName: &agentName}
	if err := s.repo.CreateACPSession(ctx, acp); err != nil {
		return nil, err
	}

	now := time.Now()
	var titlePtr *string
	if strings.TrimSpace(title) != "" {
		t := title
		titlePtr = &t
	}
	sess := &AgentShareSession{
		ShareLinkID:    binding.Link.ID,
		ACPSessionID:   acp.ID,
		EndUserRef:     endUserRef,
		Title:          titlePtr,
		LastActivityAt: &now,
	}
	// Enforce the session cap atomically at insert (advisory lock + count), so
	// two concurrent first-session requests cannot both create a session past
	// the cap.
	created, err := s.repo.CreateShareSessionIfUnderCap(ctx, sess, cfg.MaxActiveSessionsPerUser)
	if err != nil {
		return nil, err
	}
	if !created {
		return nil, apperror.New(429, "share_session_limit", "session limit reached for this link")
	}
	return s.sessionDTO(sess), nil
}

// ListSessions lists an end user's sessions for the link. filter is
// active|all|archived (default active).
func (s *ShareService) ListSessions(ctx context.Context, binding *ShareLinkBinding, endUserRef, filter string) ([]*ShareSessionDTO, error) {
	if endUserRef == "" {
		return nil, apperror.NewBadRequest("endUserRef is required")
	}
	sessions, err := s.repo.ListShareSessionsByEndUser(ctx, binding.Link.ID, endUserRef, true)
	if err != nil {
		return nil, err
	}
	out := make([]*ShareSessionDTO, 0, len(sessions))
	for _, sess := range sessions {
		switch filter {
		case "archived":
			if !sess.IsArchived {
				continue
			}
		case "all":
			// include both
		default: // "active"
			if sess.IsArchived {
				continue
			}
		}
		out = append(out, s.sessionDTO(sess))
	}
	return out, nil
}

// GetSessionEntity returns the share-session entity belonging to (link, end
// user), for internal use by the streaming path.
func (s *ShareService) GetSessionEntity(ctx context.Context, binding *ShareLinkBinding, sessionID, endUserRef string) (*AgentShareSession, error) {
	sess, err := s.repo.GetShareSessionByID(ctx, sessionID, binding.Link.ID, endUserRef)
	if err != nil {
		return nil, err
	}
	if sess == nil {
		return nil, apperror.New(http.StatusNotFound, "not_found", "session not found")
	}
	return sess, nil
}

// GetSession returns one session belonging to (link, end user), including its
// message transcript (reusing the existing ACP-session history retrieval).
func (s *ShareService) GetSession(ctx context.Context, binding *ShareLinkBinding, sessionID, endUserRef string) (*ShareSessionDetailDTO, error) {
	sess, err := s.GetSessionEntity(ctx, binding, sessionID, endUserRef)
	if err != nil {
		return nil, err
	}
	messages, err := s.sessionTranscript(ctx, sess.ACPSessionID)
	if err != nil {
		return nil, err
	}
	return &ShareSessionDetailDTO{
		ID:         sess.ID,
		Title:      sess.Title,
		IsArchived: sess.IsArchived,
		Messages:   messages,
		CreatedAt:  sess.CreatedAt,
	}, nil
}

// sessionTranscript returns the plain user/assistant message history for an ACP
// session, derived from the existing run-message history (never a new store).
func (s *ShareService) sessionTranscript(ctx context.Context, acpSessionID string) ([]ShareTranscriptMessage, error) {
	items, err := s.repo.GetConversationFullHistory(ctx, acpSessionID)
	if err != nil {
		return nil, err
	}
	out := make([]ShareTranscriptMessage, 0, len(items))
	for _, it := range items {
		if it.Kind != "message" {
			continue
		}
		if it.Role != "user" && it.Role != "assistant" {
			continue
		}
		text := ""
		if it.Content != nil {
			if t, ok := it.Content["text"].(string); ok {
				text = t
			}
		}
		if text == "" {
			continue
		}
		out = append(out, ShareTranscriptMessage{Role: it.Role, Content: text})
	}
	return out, nil
}

// ListSessionsByProject returns a project's share sessions (across all of its
// links), newest activity first, mapped to the owner-facing DTO. When userID is
// non-empty (OAuth/session auth), the caller must be a project member.
func (s *ShareService) ListSessionsByProject(ctx context.Context, projectID, userID string) ([]*ShareOwnerSessionDTO, error) {
	if userID != "" {
		if err := s.EnsureProjectMember(ctx, projectID, userID); err != nil {
			return nil, err
		}
	}
	rows, err := s.repo.ListShareSessionsByProject(ctx, projectID)
	if err != nil {
		return nil, err
	}
	out := make([]*ShareOwnerSessionDTO, 0, len(rows))
	for _, row := range rows {
		out = append(out, s.ownerSessionDTO(row))
	}
	return out, nil
}

// GetSessionTranscriptByID returns the plain user/assistant transcript for a
// share session, scoped to a project. The session is loaded by id and its
// link's project_id must match projectID (404 otherwise), so the caller can
// never read another project's shared session. When userID is non-empty
// (OAuth/session auth), the caller must also be a project member.
func (s *ShareService) GetSessionTranscriptByID(ctx context.Context, projectID, sessionID, userID string) ([]ShareTranscriptMessage, error) {
	if userID != "" {
		if err := s.EnsureProjectMember(ctx, projectID, userID); err != nil {
			return nil, err
		}
	}
	row, err := s.repo.GetShareSessionByProject(ctx, sessionID, projectID)
	if err != nil {
		return nil, err
	}
	if row == nil {
		return nil, apperror.New(http.StatusNotFound, "not_found", "share session not found")
	}
	return s.sessionTranscript(ctx, row.ACPSessionID)
}

// ownerSessionDTO maps a project-scoped share-session row to its owner DTO,
// falling back to the agent name when the session has no explicit title.
func (s *ShareService) ownerSessionDTO(row shareSessionProjectRow) *ShareOwnerSessionDTO {
	title := ""
	if row.Title != nil {
		title = *row.Title
	}
	if title == "" {
		title = row.AgentName
	}
	return &ShareOwnerSessionDTO{
		ID:                row.ID,
		AgentDefinitionID: row.AgentDefinitionID,
		AgentName:         row.AgentName,
		Title:             title,
		ACPSessionID:      row.ACPSessionID,
		IsArchived:        row.IsArchived,
		CreatedAt:         row.CreatedAt,
		LastActivityAt:    row.LastActivityAt,
	}
}

// ArchiveSession archives one session.
func (s *ShareService) ArchiveSession(ctx context.Context, binding *ShareLinkBinding, sessionID, endUserRef string) error {
	archived, err := s.repo.ArchiveShareSession(ctx, sessionID, binding.Link.ID, endUserRef)
	if err != nil {
		return err
	}
	if !archived {
		return apperror.New(http.StatusNotFound, "not_found", "session not found or already archived")
	}
	return nil
}

// PendingQuestions returns pending questions for a session (or all of an end
// user's sessions when sessionID is empty).
func (s *ShareService) PendingQuestions(ctx context.Context, binding *ShareLinkBinding, endUserRef, sessionID string) ([]*AgentQuestionDTO, error) {
	if endUserRef == "" {
		return nil, apperror.NewBadRequest("endUserRef is required")
	}
	var questions []*AgentQuestion
	if sessionID != "" {
		sess, err := s.repo.GetShareSessionByID(ctx, sessionID, binding.Link.ID, endUserRef)
		if err != nil {
			return nil, err
		}
		if sess == nil {
			return nil, apperror.New(http.StatusNotFound, "not_found", "session not found")
		}
		questions, err = s.repo.ListPendingQuestionsForACPSession(ctx, sess.ACPSessionID)
		if err != nil {
			return nil, err
		}
	} else {
		sessions, err := s.repo.ListShareSessionsByEndUser(ctx, binding.Link.ID, endUserRef, false)
		if err != nil {
			return nil, err
		}
		for _, sess := range sessions {
			qs, err := s.repo.ListPendingQuestionsForACPSession(ctx, sess.ACPSessionID)
			if err != nil {
				return nil, err
			}
			questions = append(questions, qs...)
		}
	}

	out := make([]*AgentQuestionDTO, 0, len(questions))
	for _, q := range questions {
		out = append(out, q.ToDTO())
	}
	return out, nil
}

// ============================================================================
// Streaming
// ============================================================================

// StreamMessage runs one end-user message turn. It enforces message length,
// share budget, and concurrent-run caps, then runs the agent with sandbox-off
// (config-gated), an allowlist, and no owner credentials.
func (s *ShareService) StreamMessage(ctx context.Context, binding *ShareLinkBinding, session *AgentShareSession, message string, streamCallback StreamCallback) (*ExecuteResult, error) {
	cfg := binding.Config

	if cfg.MaxMessageChars > 0 && len(message) > cfg.MaxMessageChars {
		return nil, apperror.NewBadRequest(fmt.Sprintf("message exceeds %d characters", cfg.MaxMessageChars))
	}

	window := time.Duration(cfg.BudgetWindowSeconds) * time.Second
	if window <= 0 {
		window = 24 * time.Hour
	}
	periodStart := time.Now().UTC().Truncate(window)

	if err := s.reserveShareBudget(ctx, binding, periodStart); err != nil {
		return nil, err
	}

	req, err := s.buildShareExecuteRequest(ctx, binding, session, message, streamCallback)
	if err != nil {
		return nil, err
	}

	// Atomically reserve a concurrent-run slot and pre-create the run (status
	// working) under an advisory lock, so concurrent admissions for the link
	// cannot exceed the limit.
	maxSteps := shareMaxSteps(binding.Definition)
	req.MaxSteps = &maxSteps
	preRun, err := s.repo.CreateShareRunIfUnderLimit(ctx, binding.Link.ID, cfg.MaxConcurrentRuns, CreateRunOptions{
		AgentID:           req.Agent.ID,
		AgentDefinitionID: &req.AgentDefinition.ID,
		MaxSteps:          &maxSteps,
	})
	if err != nil {
		return nil, err
	}
	req.PreCreatedRun = preRun

	result, err := s.runner.Execute(ctx, req)
	// Bind teardown to this handler's lifetime as well. The executor already
	// tears the sandbox down before returning, so this is a defensive no-op for
	// the normal path, but it guarantees a share-link run can never leak a
	// container/volume if an early return or error path skipped the executor's
	// binding.
	if result != nil && result.Cleanup != nil {
		defer result.Cleanup()
	}
	if err != nil {
		return nil, err
	}

	if err := s.recordUsage(ctx, binding, periodStart, result); err != nil {
		s.log.Warn("failed to record share usage", slog.String("error", err.Error()))
	}
	_ = s.repo.TouchShareLink(ctx, binding.Link.ID)
	_ = s.repo.TouchShareSession(ctx, session.ID)
	return result, nil
}

// shareMaxSteps returns the effective max-steps for a share turn, matching the
// executor's default resolution (definition MaxSteps, else DefaultMaxStepsPerRun).
func shareMaxSteps(def *AgentDefinition) int {
	if def != nil && def.MaxSteps != nil && *def.MaxSteps > 0 {
		return *def.MaxSteps
	}
	return DefaultMaxStepsPerRun
}

// buildShareExecuteRequest constructs the ExecuteRequest for a share run. It is
// a pure helper so tests can assert sandbox-off + no owner credentials + deny
// list propagation.
func (s *ShareService) buildShareExecuteRequest(ctx context.Context, binding *ShareLinkBinding, session *AgentShareSession, message string, streamCallback StreamCallback) (ExecuteRequest, error) {
	cfg := binding.Config
	def := *binding.Definition // copy so we never mutate the cached definition
	if !cfg.SandboxEnabled {
		def.SandboxConfig = nil
	}

	agent, err := s.ensureShareAgent(ctx, binding)
	if err != nil {
		return ExecuteRequest{}, err
	}

	return ExecuteRequest{
		Agent:            agent,
		AgentDefinition:  &def,
		ProjectID:        binding.ProjectID,
		OrgID:            binding.OrgID,
		UserID:           "", // anonymous — never target notifications
		UserMessage:      message,
		StreamCallback:   streamCallback,
		ACPSessionID:     session.ACPSessionID,
		ShareLinkID:      binding.Link.ID,
		ShareToolDeny:    cfg.ComputeShareToolDeny(),
		DisableAuthMint:  true, // never mint owner credentials for anonymous users
		AuthToken:        "",   // never inject the owner's project credentials
		EphemeralTokenID: "",
	}, nil
}

// ensureShareAgent finds or creates the runtime agent backing the definition.
func (s *ShareService) ensureShareAgent(ctx context.Context, binding *ShareLinkBinding) (*Agent, error) {
	def := binding.Definition
	name := "Chat session for " + def.Name
	agent, err := s.repo.FindByName(ctx, def.ProjectID, name)
	if err != nil {
		return nil, err
	}
	if agent != nil {
		return agent, nil
	}
	agent = &Agent{
		ProjectID:    def.ProjectID,
		Name:         name,
		StrategyType: "chat-session:" + def.ID,
		CronSchedule: "0 0 * * *", // required by schema but ignored
		TriggerType:  "manual",
	}
	if err := s.repo.Create(ctx, agent); err != nil {
		return nil, err
	}
	return agent, nil
}

// reserveShareBudget atomically reserves one message slot plus a per-turn
// token/cost allowance, enforcing the rolling budget under a FOR UPDATE lock
// (see Repository.ReserveShareBudget), so concurrent turns for the same link
// cannot all pass a check-then-act gate and a link near its edge is denied
// before spending.
func (s *ShareService) reserveShareBudget(ctx context.Context, binding *ShareLinkBinding, periodStart time.Time) error {
	cfg := binding.Config
	perTurnTokens := cfg.BudgetPerTurnTokens
	if cfg.BudgetMaxTokens <= 0 {
		perTurnTokens = 0
	}
	perTurnCost := cfg.BudgetPerTurnCostUSD
	if cfg.BudgetMaxCostUSD <= 0 {
		perTurnCost = 0
	}
	return s.repo.ReserveShareBudget(ctx, binding.Link.ID, periodStart, cfg.BudgetMaxMessages, cfg.BudgetMaxTokens, cfg.BudgetMaxCostUSD, perTurnTokens, perTurnCost)
}

// recordUsage reconciles the actual token/cost usage against the per-turn
// allowance reserved up-front, adding only the delta (which may be negative to
// release unused allowance). The message slot was already reserved by
// reserveShareBudget.
func (s *ShareService) recordUsage(ctx context.Context, binding *ShareLinkBinding, periodStart time.Time, result *ExecuteResult) error {
	if result == nil {
		return nil
	}
	tokens := int64(0)
	cost := 0.0
	if usage, err := s.repo.GetRunTokenUsage(ctx, result.RunID); err == nil && usage != nil {
		tokens = usage.TotalInputTokens + usage.TotalOutputTokens
		cost = usage.EstimatedCostUSD
	}
	cfg := binding.Config
	perTurnTokens := cfg.BudgetPerTurnTokens
	if cfg.BudgetMaxTokens <= 0 {
		perTurnTokens = 0
	}
	perTurnCost := cfg.BudgetPerTurnCostUSD
	if cfg.BudgetMaxCostUSD <= 0 {
		perTurnCost = 0
	}
	return s.repo.IncrementShareUsage(ctx, binding.Link.ID, periodStart, 0, tokens-perTurnTokens, cost-perTurnCost)
}

// resumeSettled returns the OnRunSettled callback for a share approval. It
// records the resumed leg's actual token/cost usage against the link budget in
// the current rolling window. Without this, a visitor could cycle approve/resume
// to spend above the per-link budget, because recordUsage only reconciles the
// initial StreamMessage leg up to the first pause and never the resumed leg.
func (s *ShareService) resumeSettled(binding *ShareLinkBinding) func(*ExecuteResult) {
	return func(result *ExecuteResult) {
		if result == nil || result.RunID == "" {
			return
		}
		window := time.Duration(binding.Config.BudgetWindowSeconds) * time.Second
		if window <= 0 {
			window = 24 * time.Hour
		}
		periodStart := time.Now().UTC().Truncate(window)
		if err := s.recordResumeUsage(context.Background(), binding.Link.ID, periodStart, result.RunID); err != nil {
			s.log.Warn("failed to record share resume usage",
				slog.String("link_id", binding.Link.ID),
				slog.String("run_id", result.RunID),
				slog.String("error", err.Error()),
			)
		}
	}
}

// recordResumeUsage records the actual token/cost usage of a resumed run leg.
// Unlike recordUsage, no per-turn allowance was reserved for the resume leg, so
// the full actual usage is added (no reservation offset).
func (s *ShareService) recordResumeUsage(ctx context.Context, linkID string, periodStart time.Time, runID string) error {
	usage, err := s.repo.GetRunTokenUsage(ctx, runID)
	if err != nil || usage == nil {
		return nil // no usage recorded for this leg
	}
	tokens := usage.TotalInputTokens + usage.TotalOutputTokens
	cost := usage.EstimatedCostUSD
	return s.repo.IncrementShareUsage(ctx, linkID, periodStart, 0, tokens, cost)
}

// ============================================================================
// Approvals (end-user respond/deny)
// ============================================================================

// RespondToQuestion verifies question->run->acp_session->share_session
// ownership for the caller, then delegates to the shared respond/resume helper.
func (s *ShareService) RespondToQuestion(ctx context.Context, binding *ShareLinkBinding, sessionID, endUserRef, questionID, response, message, ipHash string) (*AgentQuestionDTO, error) {
	if endUserRef == "" {
		return nil, apperror.NewBadRequest("endUserRef is required")
	}
	// end_user_ref is written to kb.agent_questions.responded_by (a uuid column),
	// so it must be UUID-form. The gateway generates a UUID end-user reference.
	if _, err := uuid.Parse(endUserRef); err != nil {
		return nil, apperror.NewBadRequest("endUserRef must be a UUID")
	}
	if !binding.Config.AllowEndUserApprovals {
		return nil, apperror.NewForbidden("end-user approvals are disabled for this link")
	}

	session, err := s.repo.GetShareSessionByID(ctx, sessionID, binding.Link.ID, endUserRef)
	if err != nil {
		return nil, err
	}
	if session == nil {
		return nil, apperror.New(http.StatusNotFound, "not_found", "session not found")
	}
	if session.IsArchived {
		return nil, apperror.New(http.StatusConflict, "conflict", "session is archived")
	}

	question, err := s.repo.FindQuestionByID(ctx, questionID)
	if err != nil {
		return nil, apperror.NewInternal("failed to get question", err)
	}
	if question == nil {
		return nil, apperror.NewNotFound("AgentQuestion", questionID)
	}

	// Ownership: the question's run must map to one of the caller's sessions on
	// this link. A foreign end_user_ref can never answer another user's question.
	ss, err := s.repo.FindShareSessionByRunAndEndUser(ctx, binding.Link.ID, endUserRef, question.RunID)
	if err != nil {
		return nil, apperror.NewInternal("failed to verify session ownership", err)
	}
	if ss == nil || ss.ID != session.ID {
		return nil, apperror.New(http.StatusNotFound, "not_found", "question not found for this session")
	}

	_ = s.repo.CreateShareAccessLog(ctx, binding.Link.ID, endUserRef, ipHash, "approve")

	// The approval cap is enforced atomically inside the shared helper via
	// ReserveAndDecideShareApproval (advisory lock + conditional flip), so
	// concurrent approvals cannot exceed the per-session cap.
	return s.responder.RespondToQuestion(ctx, RespondParams{
		ProjectID:              binding.ProjectID,
		OrgID:                  binding.OrgID,
		RespondedBy:            endUserRef, // uuid-form end_user_ref
		UserID:                 "",         // no notifications for anonymous users
		QuestionID:             questionID,
		Response:               response,
		Message:                message,
		ShareLinkID:            binding.Link.ID,
		ShareToolDeny:          binding.Config.ComputeShareToolDeny(),
		DisableAuthMint:        true,
		MaxApprovalsPerSession: binding.Config.MaxApprovalsPerSession,
		ACPSessionID:           session.ACPSessionID,
		OnRunSettled:           s.resumeSettled(binding),
	})
}

// ============================================================================
// Helpers
// ============================================================================

func (s *ShareService) linkDTO(link *AgentShareLink, token, prefix string) *ShareLinkDTO {
	return &ShareLinkDTO{
		ID:                link.ID,
		ProjectID:         link.ProjectID,
		AgentDefinitionID: link.AgentDefinitionID,
		Label:             link.Label,
		Config:            link.EffectiveConfig(),
		APITokenPrefix:    prefix,
		Token:             token,
		CreatedAt:         link.CreatedAt,
		UpdatedAt:         link.UpdatedAt,
		LastUsedAt:        link.LastUsedAt,
		RevokedAt:         link.RevokedAt,
		ExpiresAt:         link.ExpiresAt,
	}
}

func (s *ShareService) sessionDTO(sess *AgentShareSession) *ShareSessionDTO {
	return &ShareSessionDTO{
		ID:             sess.ID,
		Title:          sess.Title,
		IsArchived:     sess.IsArchived,
		LastActivityAt: sess.LastActivityAt,
		CreatedAt:      sess.CreatedAt,
	}
}

func (s *ShareService) upsertEndUser(ctx context.Context, linkID, endUserRef, email string) error {
	now := time.Now()
	u := &AgentShareEndUser{
		ShareLinkID: linkID,
		EndUserRef:  endUserRef,
		LastSeenAt:  &now,
	}
	if strings.TrimSpace(email) != "" {
		e := email
		n := normalizeEmail(email)
		u.Email = &e
		u.EmailNormalized = &n
	}
	return s.repo.UpsertShareEndUser(ctx, u)
}

// shareTokenName builds a project-unique token name for a link label.
func shareTokenName(label string) string {
	sum := sha256.Sum256([]byte(label + ":" + time.Now().Format(time.RFC3339Nano)))
	return fmt.Sprintf("agent-share: %s (%s)", label, hex.EncodeToString(sum[:4]))
}

// LogAccess records a hashed-IP access event (best-effort audit logging).
func (s *ShareService) LogAccess(ctx context.Context, linkID, endUserRef, ipHash, action string) {
	if err := s.repo.CreateShareAccessLog(ctx, linkID, endUserRef, ipHash, action); err != nil {
		s.log.Warn("failed to log share access",
			slog.String("error", err.Error()),
		)
	}
}

// HashIP returns the SHA-256 hex digest of a client IP for audit logging.
func HashIP(ip string) string {
	sum := sha256.Sum256([]byte(ip))
	return hex.EncodeToString(sum[:])
}
