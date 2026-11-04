package auth

import (
	"context"

	"github.com/google/uuid"

	"github.com/emergent-company/emergent.memory/pkg/apperror"
)

// Context keys for storing auth data in standard context.Context.
// These allow downstream service layers to access auth information
// without requiring an echo.Context dependency.
type (
	userCtxKey       struct{}
	projectIDCtxKey  struct{}
	orgIDCtxKey      struct{}
	rawTokenCtxKey   struct{}
	namespaceCtxKey  struct{}
	actorCtxKey      struct{}
	credentialCtxKey struct{}
	runIDsCtxKey     struct{}

	// Reaction-agent work-status exemption. A reaction agent owns the board
	// status of the object that triggered its run, but agents are otherwise
	// forbidden from writing board work status directly. The executor stamps the
	// run's subject object here, and the MCP entity-update path marks a single
	// write as allowed only when the target is that subject.
	agentWorkStatusSubjectCtxKey struct{}
	agentWorkStatusAllowedCtxKey struct{}
)

// actorValue carries the acting actor for provenance attribution. It is stamped
// on the context at run/endpoint boundaries (agent run, per-agent MCP endpoint,
// extraction worker) and read by downstream graph mutations to attribute writes
// to the actual writer rather than the HTTP principal.
type actorValue struct {
	ActorType string
	ActorID   *uuid.UUID
}

// ContextWithUser returns a new context with the AuthUser embedded.
func ContextWithUser(ctx context.Context, user *AuthUser) context.Context {
	return context.WithValue(ctx, userCtxKey{}, user)
}

// UserFromContext extracts the AuthUser from a standard context.Context.
// Returns nil if no user is present.
func UserFromContext(ctx context.Context) *AuthUser {
	if user, ok := ctx.Value(userCtxKey{}).(*AuthUser); ok {
		return user
	}
	return nil
}

// RequireUser returns the authenticated user embedded in ctx, or
// ErrUnauthorized when no user is present or the user ID is empty. It is the
// error-returning counterpart to MustGetUser for authorization checks that run
// in the service layer (which receives a context.Context, not an echo.Context).
// Callers that need a domain-specific message should re-wrap the returned error.
func RequireUser(ctx context.Context) (*AuthUser, error) {
	user := UserFromContext(ctx)
	if user == nil || user.ID == "" {
		return nil, apperror.ErrUnauthorized
	}
	return user, nil
}

// ContextWithProjectID returns a new context with the project ID embedded.
func ContextWithProjectID(ctx context.Context, projectID string) context.Context {
	return context.WithValue(ctx, projectIDCtxKey{}, projectID)
}

// UserIDFromContext returns the authenticated user's profile id, or "" when the
// context carries no user. Use it where a handler needs the id as a value and
// UserFromContext's nil-ness is not otherwise interesting — e.g. recording who
// uploaded a document (#2260).
func UserIDFromContext(ctx context.Context) string {
	if user := UserFromContext(ctx); user != nil {
		return user.ID
	}
	return ""
}

// ProjectIDFromContext extracts the project ID from a standard context.Context.
// Returns empty string if no project ID is present.
func ProjectIDFromContext(ctx context.Context) string {
	if id, ok := ctx.Value(projectIDCtxKey{}).(string); ok {
		return id
	}
	return ""
}

// ContextWithOrgID returns a new context with the organization ID embedded.
func ContextWithOrgID(ctx context.Context, orgID string) context.Context {
	return context.WithValue(ctx, orgIDCtxKey{}, orgID)
}

// OrgIDFromContext extracts the organization ID from a standard context.Context.
// Returns empty string if no organization ID is present.
func OrgIDFromContext(ctx context.Context) string {
	if id, ok := ctx.Value(orgIDCtxKey{}).(string); ok {
		return id
	}
	return ""
}

// ContextWithNamespace returns a new context with the graph namespace embedded.
func ContextWithNamespace(ctx context.Context, namespace string) context.Context {
	return context.WithValue(ctx, namespaceCtxKey{}, namespace)
}

// NamespaceFromContext extracts the graph namespace from a standard context.Context.
// Returns empty string if no namespace is present.
func NamespaceFromContext(ctx context.Context) string {
	if ns, ok := ctx.Value(namespaceCtxKey{}).(string); ok {
		return ns
	}
	return ""
}

// WithActor stamps the acting actor on the context. Downstream graph mutations
// read this (via ActorFromContext) to attribute writes to the actual writer —
// `agent` for an agent run's tool writes, `system` for background/extraction —
// overriding the fallback user actor that HTTP handlers pass explicitly.
//
// An empty actorType is the "clear" form: it shadows any actor stamped by a
// parent context (e.g. a delegated run) so ActorFromContext reports no actor and
// the mutation falls back to its normal user/system attribution.
func WithActor(ctx context.Context, actorType string, actorID *uuid.UUID) context.Context {
	return context.WithValue(ctx, actorCtxKey{}, actorValue{ActorType: actorType, ActorID: actorID})
}

// ActorFromContext extracts the stamped actor from a standard context.Context.
// The bool is false when no actor was stamped — including the "clear" form
// (WithActor with an empty actorType), which shadows a parent's actor.
func ActorFromContext(ctx context.Context) (actorType string, actorID *uuid.UUID, ok bool) {
	if a, ok := ctx.Value(actorCtxKey{}).(actorValue); ok && a.ActorType != "" {
		return a.ActorType, a.ActorID, true
	}
	return "", nil, false
}

// credentialValue carries the credential that authenticated a request, as
// recorded in a write stamp (#2376). Kind is never empty when stamped.
type credentialValue struct {
	Kind string
	ID   *uuid.UUID
}

// runIDsValue carries the agent run that a write happened inside, and the root
// of its delegation chain.
type runIDsValue struct {
	RunID     *uuid.UUID
	RootRunID *uuid.UUID
}

// ContextWithCredential stamps the credential that authenticated the request.
// Graph writes read it (via CredentialFromContext) to record which key or link
// produced the row. An empty kind is normalised to CredentialNone so a write
// stamp can never be silently absent.
func ContextWithCredential(ctx context.Context, kind string, id *uuid.UUID) context.Context {
	if kind == "" {
		kind = CredentialNone
	}
	return context.WithValue(ctx, credentialCtxKey{}, credentialValue{Kind: kind, ID: id})
}

// CredentialFromContext returns the stamped credential, or CredentialNone when
// nothing was stamped (background work, internal calls). The bool is false when
// no credential was stamped, which callers use to distinguish "background" from
// "explicitly unauthenticated" only if they need to.
func CredentialFromContext(ctx context.Context) (kind string, id *uuid.UUID, ok bool) {
	if c, ok := ctx.Value(credentialCtxKey{}).(credentialValue); ok && c.Kind != "" {
		return c.Kind, c.ID, true
	}
	return CredentialNone, nil, false
}

// ContextWithRunIDs stamps the agent run (and the root of its delegation chain)
// that a write happens inside. Stamped at the run boundary alongside WithActor,
// so the actor pair keeps its meaning (agent definition id) and the run is
// recorded separately.
func ContextWithRunIDs(ctx context.Context, runID, rootRunID *uuid.UUID) context.Context {
	return context.WithValue(ctx, runIDsCtxKey{}, runIDsValue{RunID: runID, RootRunID: rootRunID})
}

// RunIDsFromContext returns the stamped run ids, or (nil, nil) outside a run.
func RunIDsFromContext(ctx context.Context) (runID, rootRunID *uuid.UUID) {
	if r, ok := ctx.Value(runIDsCtxKey{}).(runIDsValue); ok {
		return r.RunID, r.RootRunID
	}
	return nil, nil
}

// WithAgentWorkStatusSubject stamps the canonical id of a reaction agent's
// subject object. The MCP entity-update path uses it to scope the work-status
// exemption to the object that triggered the run, never to other objects.
func WithAgentWorkStatusSubject(ctx context.Context, subjectID string) context.Context {
	return context.WithValue(ctx, agentWorkStatusSubjectCtxKey{}, subjectID)
}

// AgentWorkStatusSubjectFromContext returns the reaction run's subject canonical
// id, or "" when the run is not a reaction run.
func AgentWorkStatusSubjectFromContext(ctx context.Context) string {
	id, _ := ctx.Value(agentWorkStatusSubjectCtxKey{}).(string)
	return id
}

// WithAgentWorkStatusAllowed marks a single graph write as allowed to set work
// status. Only the MCP entity-update path sets it, after confirming the target
// object is the reaction run's subject. It is never read from client input.
func WithAgentWorkStatusAllowed(ctx context.Context) context.Context {
	return context.WithValue(ctx, agentWorkStatusAllowedCtxKey{}, true)
}

// AgentWorkStatusAllowedFromContext reports whether the current write carries
// the scoped reaction-agent work-status exemption.
func AgentWorkStatusAllowedFromContext(ctx context.Context) bool {
	allowed, _ := ctx.Value(agentWorkStatusAllowedCtxKey{}).(bool)
	return allowed
}

// ContextWithRawToken returns a new context with the raw bearer/API token embedded.
// This allows downstream code (e.g. internal HTTP loopback calls) to forward the
// original credential without re-extracting it from HTTP headers.
func ContextWithRawToken(ctx context.Context, token string) context.Context {
	return context.WithValue(ctx, rawTokenCtxKey{}, token)
}

// RawTokenFromContext extracts the raw bearer/API token from a standard context.Context.
// Returns empty string if no token is present.
func RawTokenFromContext(ctx context.Context) string {
	if token, ok := ctx.Value(rawTokenCtxKey{}).(string); ok {
		return token
	}
	return ""
}

// InjectAuthContext enriches the given context with user, project ID, and org ID
// from the provided AuthUser. This is a convenience function for injecting all
// auth-related values at once.
func InjectAuthContext(ctx context.Context, user *AuthUser) context.Context {
	ctx = ContextWithUser(ctx, user)
	if user.ProjectID != "" {
		ctx = ContextWithProjectID(ctx, user.ProjectID)
	}
	if user.OrgID != "" {
		ctx = ContextWithOrgID(ctx, user.OrgID)
	}
	// Stamp the credential that authenticated the request so graph writes can
	// record it (#2376). An emt_* API token is the credential; a share-link token
	// is distinguishable by its share scope; a human session has none.
	ctx = ContextWithCredential(ctx, CredentialForUser(user), tokenUUID(user.APITokenID))
	return ctx
}

// Credential kinds recorded in a write stamp. Every write resolves to one of
// these; CredentialNone means no credential was identifiable (background work,
// internal calls) and is stored explicitly rather than as an absent value.
const (
	CredentialAPIKey  = "api_key"
	CredentialMCPLink = "mcp_link"
	CredentialSession = "session"
	CredentialNone    = "none"
)

// CredentialForUser maps an authenticated principal to the credential kind that
// should be recorded for a write it performs.
func CredentialForUser(user *AuthUser) string {
	if user == nil {
		return CredentialNone
	}
	if user.APITokenID != "" {
		// A per-agent MCP endpoint key is minted only by the agent MCP share
		// lifecycle and carries mcp:agent-call; its tool calls are what write
		// graph rows on behalf of an agent, so it is worth distinguishing from a
		// user's project API key even though both are API tokens.
		//
		// A public agent-share chat link (share:agent-chat) is also an API token,
		// but it is read-only apart from chat: it stays CredentialAPIKey, and
		// credential_id still identifies the exact token. Do not map it here —
		// that would label ordinary share traffic as MCP.
		if hasScope(user.Scopes, agentCallScope) {
			return CredentialMCPLink
		}
		return CredentialAPIKey
	}
	return CredentialSession
}

// tokenUUID parses an API token id, returning nil when it is absent or not a
// UUID. A malformed id must not fail the request; it simply leaves the stamp's
// credential id empty rather than inventing one.
func tokenUUID(tokenID string) *uuid.UUID {
	if tokenID == "" {
		return nil
	}
	id, err := uuid.Parse(tokenID)
	if err != nil {
		return nil
	}
	return &id
}
