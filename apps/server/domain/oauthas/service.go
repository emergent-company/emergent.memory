package oauthas

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"log/slog"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/emergent-company/emergent.memory/domain/apitoken"
	"github.com/emergent-company/emergent.memory/internal/config"
	"github.com/emergent-company/emergent.memory/pkg/apperror"
	"github.com/emergent-company/emergent.memory/pkg/logger"
)

const (
	// authorizationCodeTTL is the code lifetime mandated by the design.
	authorizationCodeTTL = 5 * time.Minute
	// maxRedirectURIsPerClient is the registration cap. It must stay above the
	// number Google's Gemini connector sends: Gemini registers a sandbox/test/
	// production triple for both the /r/ and /a/ redirect shapes, i.e. six URIs
	// in one request, and a lower cap rejects the registration outright (#390).
	maxRedirectURIsPerClient = 10
	// tokenTypeBearer is the only token type this AS issues.
	tokenTypeBearer = "Bearer"
)

// SupportedScopes is the complete scope allowlist advertised in metadata. A
// client can never widen beyond this set, and an authorization code stores only
// a validated subset.
var SupportedScopes = []string{"graph:read", "graph:write", "schema:read", "projects:read", "chat:use"}

var supportedScopeSet = func() map[string]bool {
	m := make(map[string]bool, len(SupportedScopes))
	for _, s := range SupportedScopes {
		m[s] = true
	}
	return m
}()

// OAuthError is an RFC 6749 / RFC 7591 shaped failure. Status is the HTTP
// status the handler emits.
type OAuthError struct {
	Code        string
	Description string
	Status      int
}

func (e *OAuthError) Error() string {
	if e.Description == "" {
		return e.Code
	}
	return e.Code + ": " + e.Description
}

func newOAuthError(status int, code, description string) *OAuthError {
	return &OAuthError{Code: code, Description: description, Status: status}
}

func errInvalidRequest(description string) *OAuthError {
	return newOAuthError(400, "invalid_request", description)
}

func errInvalidClient(description string) *OAuthError {
	return newOAuthError(401, "invalid_client", description)
}

func errInvalidGrant(description string) *OAuthError {
	return newOAuthError(400, "invalid_grant", description)
}

func errInvalidScope(description string) *OAuthError {
	return newOAuthError(400, "invalid_scope", description)
}

func errAccessDenied(description string) *OAuthError {
	return newOAuthError(403, "access_denied", description)
}

func errServer(description string) *OAuthError {
	return newOAuthError(500, "server_error", description)
}

// tokenMinter is the subset of apitoken.Service this domain uses. The concrete
// service is injected in production; a fake is used in tests.
type tokenMinter interface {
	Create(ctx context.Context, projectID, userID, name string, scopes []string) (*apitoken.CreateApiTokenResponseDTO, error)
	Revoke(ctx context.Context, tokenID, projectID, userID string) error
}

// tokenLookup resolves a raw API token hash to its row for RFC 7009
// revocation. *apitoken.Repository implements it.
type tokenLookup interface {
	FindByHash(ctx context.Context, tokenHash string) (*apitoken.ApiToken, error)
}

// Service implements the OAuth 2.1 authorization-server business logic.
type Service struct {
	repo      Store
	tokens    tokenMinter
	tokenRepo tokenLookup
	cfg       config.OAuthASConfig
	log       *slog.Logger
	now       func() time.Time
}

// NewService creates the OAuth authorization-server service.
func NewService(repo *Repository, tokens *apitoken.Service, tokenRepo *apitoken.Repository, cfg *config.Config, log *slog.Logger) *Service {
	s := &Service{
		repo:      repo,
		tokens:    tokens,
		tokenRepo: tokenRepo,
		log:       log.With(logger.Scope("oauthas.svc")),
		now:       time.Now,
	}
	if cfg != nil {
		s.cfg = cfg.OAuthAS
	}
	return s
}

// RegisterClient validates and persists a public client (RFC 7591).
func (s *Service) RegisterClient(ctx context.Context, req ClientRegistrationRequest) (*ClientRegistrationResponse, error) {
	// client_name is optional in RFC 7591; some clients (Google's Gemini
	// connector among them) omit it. Give them a stable display name instead of
	// failing the registration.
	name := strings.TrimSpace(req.ClientName)
	if name == "" {
		name = "MCP client"
	}
	if len(name) > 255 {
		return nil, newOAuthError(400, "invalid_client_metadata", "client_name must be at most 255 characters")
	}

	redirectURIs, oerr := s.validateRedirectURIs(req.RedirectURIs)
	if oerr != nil {
		return nil, oerr
	}

	// This authorization server mints no client secret: clients are public and
	// authenticate with PKCE. A client that asks for client_secret_basic/post
	// (RFC 7591's default) is registered as a public client rather than
	// rejected, so clients that default to a confidential method can still
	// connect; the token endpoint ignores any Authorization header they send.
	authMethod := strings.TrimSpace(req.TokenEndpointAuthMethod)
	if authMethod == "" {
		authMethod = "none"
	}
	if authMethod != "none" {
		s.log.Info("oauth client registration requested a confidential auth method; registering as public",
			"requested_method", authMethod, "client_name", name)
		authMethod = "none"
	}

	// Refresh tokens are not issued: an OAuth-minted token is a non-expiring
	// project API token, so there is nothing to refresh. A client that asks for
	// the refresh_token grant gets authorization_code only rather than a failed
	// registration (RFC 7591 lets the server grant a subset of the request).
	grantTypes := req.GrantTypes
	if len(grantTypes) == 0 {
		grantTypes = []string{"authorization_code"}
	}
	grantedGrants := make([]string, 0, 1)
	for _, gt := range grantTypes {
		if gt == "authorization_code" {
			grantedGrants = append(grantedGrants, gt)
		}
	}
	if len(grantedGrants) == 0 {
		grantedGrants = []string{"authorization_code"}
	}
	grantTypes = grantedGrants

	// Unknown scopes are dropped instead of failing: clients commonly request
	// offline_access (a no-op here) or vendor-specific scopes alongside the
	// ones this resource advertises.
	scopes := clampSupportedScopes(splitScopeString(req.Scope))

	clientID, err := randomString(32)
	if err != nil {
		return nil, errServer("failed to generate client_id")
	}

	now := s.now().UTC()
	client := &OAuthClient{
		ClientID:                clientID,
		ClientName:              name,
		RedirectURIs:            redirectURIs,
		GrantTypes:              grantTypes,
		TokenEndpointAuthMethod: authMethod,
		Scope:                   strings.Join(scopes, " "),
		CreatedAt:               now,
	}
	if err := s.repo.CreateClient(ctx, client); err != nil {
		s.log.Error("failed to persist oauth client", logger.Error(err))
		return nil, errServer("failed to register client")
	}

	return &ClientRegistrationResponse{
		ClientID:                clientID,
		ClientIDIssuedAt:        now.Unix(),
		ClientName:              name,
		RedirectURIs:            redirectURIs,
		TokenEndpointAuthMethod: authMethod,
		GrantTypes:              grantTypes,
		Scope:                   client.Scope,
	}, nil
}

// AuthorizeContext validates the client/redirect pair and returns the consent
// bootstrap payload. Projects are limited to those the caller can write to.
func (s *Service) AuthorizeContext(ctx context.Context, userID, clientID, redirectURI string) (*AuthorizeContextResponse, error) {
	client, _, oerr := s.validateClientRedirect(ctx, clientID, redirectURI)
	if oerr != nil {
		return nil, oerr
	}

	projects, err := s.repo.ListWritableProjects(ctx, userID)
	if err != nil {
		s.log.Error("failed to list writable projects", logger.Error(err))
		return nil, errServer("failed to list projects")
	}
	for i := range projects {
		projects[i].Slug = slugify(projects[i].Name)
	}

	return &AuthorizeContextResponse{
		ClientID:        client.ClientID,
		ClientName:      client.ClientName,
		RedirectURI:     redirectURI,
		ScopesSupported: append([]string(nil), SupportedScopes...),
		Projects:        projects,
	}, nil
}

// Authorize issues a single-use authorization code after consent. It re-checks
// that the caller still has write access to the chosen project.
func (s *Service) Authorize(ctx context.Context, userID string, req AuthorizeRequest) (*AuthorizeResponse, error) {
	client, parsedRedirect, oerr := s.validateClientRedirect(ctx, req.ClientID, req.RedirectURI)
	if oerr != nil {
		return nil, oerr
	}

	if _, err := uuid.Parse(req.ProjectID); err != nil {
		return nil, errInvalidRequest("project_id must be a valid UUID")
	}

	scopes, oerr := validateScopeList(req.Scopes)
	if oerr != nil {
		return nil, oerr
	}
	if len(scopes) == 0 {
		return nil, errInvalidScope("at least one scope is required")
	}
	if oerr := validateAgainstClientScope(client, scopes); oerr != nil {
		return nil, oerr
	}

	allowed, err := s.repo.CanWriteProject(ctx, req.ProjectID, userID)
	if err != nil {
		s.log.Error("failed to check project write access", logger.Error(err))
		return nil, errServer("failed to check project access")
	}
	if !allowed {
		return nil, errAccessDenied("you do not have write access to this project")
	}

	challenge := strings.TrimSpace(req.CodeChallenge)
	if challenge == "" {
		return nil, errInvalidRequest("code_challenge is required (PKCE S256)")
	}
	method := req.CodeChallengeMethod
	if method == "" {
		method = "S256"
	}
	if method != "S256" {
		return nil, errInvalidRequest("code_challenge_method must be S256")
	}

	code, err := randomString(32)
	if err != nil {
		return nil, errServer("failed to generate authorization code")
	}

	now := s.now().UTC()
	row := &OAuthAuthorizationCode{
		CodeHash:            hashCode(code),
		ClientID:            client.ClientID,
		UserID:              userID,
		ProjectID:           req.ProjectID,
		Scopes:              scopes,
		RedirectURI:         req.RedirectURI,
		CodeChallenge:       challenge,
		CodeChallengeMethod: method,
		ExpiresAt:           now.Add(authorizationCodeTTL),
		CreatedAt:           now,
	}
	if err := s.repo.CreateCode(ctx, row); err != nil {
		s.log.Error("failed to persist authorization code", logger.Error(err))
		return nil, errServer("failed to issue authorization code")
	}

	// Preserve any existing query parameters on the redirect URI and append the
	// one-time code (plus state when supplied). The code is never logged.
	q := parsedRedirect.Query()
	q.Set("code", code)
	if req.State != "" {
		q.Set("state", req.State)
	}
	parsedRedirect.RawQuery = q.Encode()

	return &AuthorizeResponse{RedirectURL: parsedRedirect.String()}, nil
}

// ExchangeCode redeems an authorization code for a project-scoped emt_ token.
func (s *Service) ExchangeCode(ctx context.Context, req TokenRequest) (*TokenResponse, error) {
	if req.GrantType != "" && req.GrantType != "authorization_code" {
		return nil, newOAuthError(400, "unsupported_grant_type", "only authorization_code is supported")
	}
	if req.ClientID == "" {
		return nil, errInvalidRequest("client_id is required")
	}
	client := s.lookupActiveClient(ctx, req.ClientID)
	if client == nil {
		return nil, errInvalidClient("unknown or revoked client")
	}
	if req.Code == "" {
		return nil, errInvalidRequest("code is required")
	}
	if req.RedirectURI == "" {
		return nil, errInvalidRequest("redirect_uri is required")
	}
	if req.CodeVerifier == "" {
		return nil, errInvalidRequest("code_verifier is required")
	}

	code, err := s.repo.GetCodeByHash(ctx, hashCode(req.Code))
	if err != nil {
		s.log.Error("failed to look up authorization code", logger.Error(err))
		return nil, errServer("failed to exchange code")
	}
	if code == nil {
		return nil, errInvalidGrant("invalid authorization code")
	}
	if code.ClientID != req.ClientID {
		return nil, errInvalidGrant("invalid authorization code")
	}
	if code.RedirectURI != req.RedirectURI {
		return nil, errInvalidGrant("redirect_uri does not match the authorization request")
	}
	if code.ConsumedAt != nil {
		return nil, errInvalidGrant("authorization code already used")
	}
	now := s.now().UTC()
	if !now.Before(code.ExpiresAt) {
		return nil, errInvalidGrant("authorization code expired")
	}
	if code.CodeChallengeMethod != "S256" {
		return nil, errInvalidGrant("unsupported code_challenge_method")
	}
	if !verifyPKCE(req.CodeVerifier, code.CodeChallenge) {
		return nil, errInvalidGrant("PKCE verification failed")
	}

	scopes, oerr := validateScopeList(code.Scopes)
	if oerr != nil {
		return nil, errInvalidScope(oerr.Description)
	}
	if oerr := validateAgainstClientScope(client, scopes); oerr != nil {
		return nil, oerr
	}

	// Re-check write membership: the user may have been removed from the
	// project in the few minutes between consent and redemption.
	allowed, err := s.repo.CanWriteProject(ctx, code.ProjectID, code.UserID)
	if err != nil {
		s.log.Error("failed to check project write access", logger.Error(err))
		return nil, errServer("failed to exchange code")
	}
	if !allowed {
		return nil, errInvalidGrant("user no longer has write access to the project")
	}

	// Single-use: exactly one concurrent exchange wins. The predicate also
	// pins client_id + redirect_uri so a bad actor cannot burn another client's
	// code.
	consumed, err := s.repo.ConsumeCode(ctx, code.CodeHash, req.ClientID, req.RedirectURI, now)
	if err != nil {
		s.log.Error("failed to consume authorization code", logger.Error(err))
		return nil, errServer("failed to exchange code")
	}
	if !consumed {
		return nil, errInvalidGrant("authorization code already used or expired")
	}

	token, err := s.mintToken(ctx, code.ProjectID, code.UserID, client.ClientName, scopes)
	if err != nil {
		s.log.Error("failed to mint oauth api token",
			slog.String("client_id", client.ClientID),
			slog.String("project_id", code.ProjectID),
			logger.Error(err))
		return nil, errServer("failed to issue access token")
	}

	grant := &OAuthGrant{
		ClientID:  client.ClientID,
		UserID:    code.UserID,
		ProjectID: code.ProjectID,
		Scopes:    scopes,
		TokenID:   token.ID,
		CreatedAt: now,
	}
	if err := s.repo.CreateGrant(ctx, grant); err != nil {
		// Do not leave an unlinked token behind.
		_ = s.revokeAPIToken(ctx, token.ID, code.ProjectID, code.UserID)
		s.log.Error("failed to persist oauth grant", logger.Error(err))
		return nil, errServer("failed to record grant")
	}

	if err := s.repo.TouchClient(ctx, client.ClientID, now); err != nil {
		s.log.Warn("failed to update oauth client last_used_at", logger.Error(err))
	}

	projectName, err := s.repo.GetProjectName(ctx, code.ProjectID)
	if err != nil {
		s.log.Warn("failed to resolve project name for token response", logger.Error(err))
	}

	return &TokenResponse{
		AccessToken: token.Token,
		TokenType:   tokenTypeBearer,
		Scope:       strings.Join(scopes, " "),
		ProjectID:   code.ProjectID,
		ProjectName: projectName,
	}, nil
}

// ListGrants returns the caller's OAuth grants.
func (s *Service) ListGrants(ctx context.Context, userID string) ([]GrantDTO, error) {
	rows, err := s.repo.ListGrantsByUser(ctx, userID)
	if err != nil {
		s.log.Error("failed to list oauth grants", logger.Error(err))
		return nil, errServer("failed to list grants")
	}
	out := make([]GrantDTO, 0, len(rows))
	for _, row := range rows {
		scopes := row.Scopes
		if scopes == nil {
			scopes = []string{}
		}
		out = append(out, GrantDTO{
			ID:          row.ID,
			ClientID:    row.ClientID,
			ClientName:  row.ClientName,
			ProjectID:   row.ProjectID,
			ProjectName: row.ProjectName,
			Scopes:      scopes,
			CreatedAt:   row.CreatedAt,
			LastUsedAt:  row.LastUsedAt,
			RevokedAt:   row.RevokedAt,
		})
	}
	return out, nil
}

// RevokeGrant revokes one of the caller's grants and the underlying API token.
func (s *Service) RevokeGrant(ctx context.Context, userID, grantID string) error {
	if _, err := uuid.Parse(grantID); err != nil {
		return newOAuthError(404, "not_found", "grant not found")
	}
	grant, err := s.repo.GetGrantByIDForUser(ctx, grantID, userID)
	if err != nil {
		s.log.Error("failed to load oauth grant", logger.Error(err))
		return errServer("failed to revoke grant")
	}
	if grant == nil {
		return newOAuthError(404, "not_found", "grant not found")
	}

	if _, err := s.repo.RevokeGrant(ctx, grantID, userID, s.now().UTC()); err != nil {
		s.log.Error("failed to revoke oauth grant", logger.Error(err))
		return errServer("failed to revoke grant")
	}
	// Always attempt the token revocation, including when the grant was already
	// marked revoked: token revocation is idempotent, and this makes a retry
	// self-heal if a previous attempt revoked the grant but failed to revoke the
	// token.
	if err := s.revokeAPIToken(ctx, grant.TokenID, grant.ProjectID, grant.UserID); err != nil {
		s.log.Error("grant revoked but api token revocation failed",
			slog.String("grant_id", grantID),
			slog.String("token_id", grant.TokenID),
			logger.Error(err))
		return errServer("grant revoked but token revocation failed")
	}
	return nil
}

// RevokeToken implements RFC 7009 token revocation. Unknown tokens are
// accepted silently (HTTP 200) so the endpoint is not a token oracle.
func (s *Service) RevokeToken(ctx context.Context, clientID, rawToken string) error {
	if clientID == "" {
		return errInvalidRequest("client_id is required")
	}
	if rawToken == "" {
		return errInvalidRequest("token is required")
	}
	if s.lookupClient(ctx, clientID) == nil {
		return errInvalidClient("unknown client")
	}

	token, err := s.tokenRepo.FindByHash(ctx, hashCode(rawToken))
	if err != nil {
		s.log.Error("failed to look up api token for revocation", logger.Error(err))
		return errServer("failed to revoke token")
	}
	if token == nil {
		return nil
	}

	grant, err := s.repo.GetGrantByTokenIDAndClient(ctx, token.ID, clientID)
	if err != nil {
		s.log.Error("failed to look up oauth grant for revocation", logger.Error(err))
		return errServer("failed to revoke token")
	}
	if grant == nil {
		return nil
	}
	if _, err := s.repo.RevokeGrantByTokenID(ctx, token.ID, clientID, s.now().UTC()); err != nil {
		s.log.Error("failed to revoke oauth grant", logger.Error(err))
		return errServer("failed to revoke token")
	}

	projectID, userID := grant.ProjectID, grant.UserID
	if token.ProjectID != nil {
		projectID = *token.ProjectID
	}
	if token.UserID != nil {
		userID = *token.UserID
	}
	if err := s.revokeAPIToken(ctx, token.ID, projectID, userID); err != nil {
		s.log.Error("failed to revoke api token",
			slog.String("token_id", token.ID),
			logger.Error(err))
		return errServer("failed to revoke token")
	}
	return nil
}

// validateClientRedirect loads an active client and requires an exact redirect
// URI match. No prefix or wildcard matching is ever performed.
func (s *Service) validateClientRedirect(ctx context.Context, clientID, redirectURI string) (*OAuthClient, *url.URL, *OAuthError) {
	if clientID == "" {
		return nil, nil, errInvalidRequest("client_id is required")
	}
	client := s.lookupActiveClient(ctx, clientID)
	if client == nil {
		return nil, nil, errInvalidClient("unknown or revoked client")
	}
	if redirectURI == "" {
		return nil, nil, errInvalidRequest("redirect_uri is required")
	}
	parsed, err := url.Parse(redirectURI)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return nil, nil, errInvalidRequest("redirect_uri must be an absolute URI")
	}
	for _, registered := range client.RedirectURIs {
		if registered == redirectURI {
			return client, parsed, nil
		}
	}
	return nil, nil, errInvalidRequest("redirect_uri is not registered for this client")
}

func (s *Service) lookupActiveClient(ctx context.Context, clientID string) *OAuthClient {
	client, err := s.repo.GetClient(ctx, clientID)
	if err != nil {
		s.log.Error("failed to load oauth client", logger.Error(err))
		return nil
	}
	if client == nil || client.RevokedAt != nil {
		return nil
	}
	return client
}

func (s *Service) lookupClient(ctx context.Context, clientID string) *OAuthClient {
	client, err := s.repo.GetClient(ctx, clientID)
	if err != nil {
		s.log.Error("failed to load oauth client", logger.Error(err))
		return nil
	}
	return client
}

func (s *Service) validateRedirectURIs(uris []string) ([]string, *OAuthError) {
	if len(uris) == 0 {
		return nil, newOAuthError(400, "invalid_redirect_uri", "redirect_uris is required")
	}
	if len(uris) > maxRedirectURIsPerClient {
		return nil, newOAuthError(400, "invalid_redirect_uri", "at most 10 redirect_uris are allowed")
	}

	allowedHosts := s.allowedRedirectHosts()
	out := make([]string, 0, len(uris))
	for _, raw := range uris {
		parsed, err := url.Parse(raw)
		if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
			return nil, newOAuthError(400, "invalid_redirect_uri", "each redirect_uri must be an absolute http or https URI")
		}
		if parsed.Fragment != "" || parsed.User != nil {
			return nil, newOAuthError(400, "invalid_redirect_uri", "redirect_uri must not contain a fragment or userinfo")
		}
		if len(allowedHosts) > 0 {
			host := strings.ToLower(parsed.Hostname())
			if !allowedHosts[host] {
				return nil, newOAuthError(400, "invalid_redirect_uri", "redirect_uri host is not allowed")
			}
		}
		out = append(out, raw)
	}
	return out, nil
}

func (s *Service) allowedRedirectHosts() map[string]bool {
	parts := strings.Split(s.cfg.AllowedRedirectHosts, ",")
	hosts := make(map[string]bool, len(parts))
	for _, p := range parts {
		h := strings.ToLower(strings.TrimSpace(p))
		if h != "" {
			hosts[h] = true
		}
	}
	return hosts
}

func (s *Service) mintToken(ctx context.Context, projectID, userID, clientName string, scopes []string) (*apitoken.CreateApiTokenResponseDTO, error) {
	name := "oauth: " + clientName
	token, err := s.tokens.Create(ctx, projectID, userID, name, scopes)
	if err == nil {
		return token, nil
	}

	// apitoken enforces uniqueness on (user_id, name) for active tokens. A user
	// may authorize the same client more than once (e.g. two projects), so fall
	// back to a suffixed display name instead of failing the exchange.
	var appErr *apperror.Error
	if errors.As(err, &appErr) && appErr.Code == "token_name_exists" {
		suffix, suffixErr := randomString(6)
		if suffixErr == nil {
			if token, retryErr := s.tokens.Create(ctx, projectID, userID, name+" ("+suffix+")", scopes); retryErr == nil {
				return token, nil
			} else {
				return nil, retryErr
			}
		}
	}
	return nil, err
}

// revokeAPIToken revokes the minted token, treating "already revoked" and
// "not found" as success so revocation is idempotent.
func (s *Service) revokeAPIToken(ctx context.Context, tokenID, projectID, userID string) error {
	if s.tokens == nil {
		return nil
	}
	err := s.tokens.Revoke(ctx, tokenID, projectID, userID)
	if err == nil {
		return nil
	}
	var appErr *apperror.Error
	if errors.As(err, &appErr) {
		if appErr.Code == "token_already_revoked" || appErr.HTTPStatus == 404 {
			return nil
		}
	}
	return err
}

// validateScopeList rejects an empty entry or any scope outside the allowlist,
// and de-duplicates while preserving order.
func validateScopeList(scopes []string) ([]string, *OAuthError) {
	seen := make(map[string]bool, len(scopes))
	out := make([]string, 0, len(scopes))
	for _, raw := range scopes {
		scope := strings.TrimSpace(raw)
		if scope == "" {
			return nil, errInvalidScope("empty scope")
		}
		if !supportedScopeSet[scope] {
			return nil, errInvalidScope("unsupported scope: " + scope)
		}
		if seen[scope] {
			continue
		}
		seen[scope] = true
		out = append(out, scope)
	}
	return out, nil
}

func splitScopeString(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	return strings.Fields(raw)
}

// clampSupportedScopes keeps the requested scopes this server understands and
// drops the rest, preserving order and de-duplicating. An empty result is legal
// (it places no restriction beyond the server allowlist). Used where a client's
// scope list must not fail the request - registration, and the consent page's
// rendering of a client's scope request.
func clampSupportedScopes(requested []string) []string {
	out := make([]string, 0, len(requested))
	seen := make(map[string]bool, len(requested))
	for _, raw := range requested {
		scope := strings.TrimSpace(raw)
		if scope == "" || !supportedScopeSet[scope] || seen[scope] {
			continue
		}
		seen[scope] = true
		out = append(out, scope)
	}
	return out
}

// validateAgainstClientScope narrows the requested scopes to the set the client
// registered at DCR time. An empty registered scope places no extra restriction
// beyond the server allowlist.
func validateAgainstClientScope(client *OAuthClient, scopes []string) *OAuthError {
	if client == nil || strings.TrimSpace(client.Scope) == "" {
		return nil
	}
	allowed := make(map[string]bool)
	for _, s := range strings.Fields(client.Scope) {
		allowed[s] = true
	}
	for _, s := range scopes {
		if !allowed[s] {
			return errInvalidScope("scope not registered for this client: " + s)
		}
	}
	return nil
}

// verifyPKCE checks base64url(sha256(verifier)) against the stored challenge in
// constant time.
func verifyPKCE(verifier, challenge string) bool {
	sum := sha256.Sum256([]byte(verifier))
	computed := base64.RawURLEncoding.EncodeToString(sum[:])
	return subtle.ConstantTimeCompare([]byte(computed), []byte(challenge)) == 1
}

// hashCode returns the SHA-256 hex digest used at rest for codes and API
// tokens (mirrors apitoken's token hashing).
func hashCode(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

// randomString returns n random bytes encoded as unpadded base64url.
func randomString(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// slugify derives a URL-safe display slug from a project name. kb.projects has
// no persisted slug column, so this is presentation-only.
func slugify(name string) string {
	var b strings.Builder
	prevDash := false
	for _, r := range strings.ToLower(strings.TrimSpace(name)) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			prevDash = false
			continue
		}
		if !prevDash && b.Len() > 0 {
			b.WriteByte('-')
			prevDash = true
		}
	}
	slug := strings.Trim(b.String(), "-")
	if slug == "" {
		return "project"
	}
	return slug
}
