// Package project manages the connector's project selection and project-scoped
// tokens. It is bound to the account base dir so it shares the same per-server
// layout as internal/account. API calls are injectable through Deps, keeping
// tests offline.
package project

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/emergent-company/emergent.memory/apps/connector.linux/internal/account"
	"github.com/emergent-company/emergent.memory/apps/connector.linux/internal/memoryapi"
	"github.com/emergent-company/emergent.memory/apps/connector.linux/internal/secretstore"
)

// Project is the connector-owned project type (an alias for the API layer's
// type, so callers can use one name everywhere).
type Project = memoryapi.Project

// ErrNotSignedIn is returned when an operation needs an account session and
// none is stored for the server.
var ErrNotSignedIn = errors.New("project: not signed in")

// TokenScopes are the least-privilege read set requested for connector tokens.
//
// The connector used to request the coarse data:read umbrella, which is retired
// from token creation (the API now rejects it with `invalid scope: data:read`),
// so the umbrella is spelled out in the fine-grained scopes it covered for an
// MCP client: documents (document-list/document-get), search
// (search-hybrid/search-semantic), graph (entity-search), journal and schema.
// The remaining data:read implications (chunks:read, search:read,
// graph:search:read, extraction:read, tasks:*, user-activity:*,
// notifications:*) are mintable, but the connector does not need them for its
// MCP tool set, so it keeps the narrower least-privilege set.
var TokenScopes = []string{
	"documents:read",
	"search",
	"graph:read",
	"journal:read",
	"schema:read",
}

// Sessions supplies the stored account session for a server. *account.Manager
// implements it.
type Sessions interface {
	SessionFor(serverURL string) (*account.Session, error)
}

// TokenSource supplies a usable account access token, refreshing it when the
// stored one is expired or about to expire. *account.Manager implements it, and
// a Sessions implementation that also satisfies this interface is used
// automatically.
type TokenSource interface {
	AccessToken(ctx context.Context, serverURL string, minTTL time.Duration) (string, error)
}

// TokenRefresher forces a refresh of the stored session, ignoring the stored
// expiry. *account.Manager implements it. A 401 from the API is the one signal
// that the stored expiry was optimistic, so a retry needs a forced refresh
// rather than the proximity check TokenSource applies.
type TokenRefresher interface {
	Refresh(ctx context.Context, serverURL string) (*account.Session, error)
}

// accessTokenRefreshWindow is how long before expiry a project operation
// refreshes proactively. It is deliberately generous: the cost of an
// unnecessary refresh is one token-endpoint round trip, the cost of a stale
// token is a 401 the user sees.
const accessTokenRefreshWindow = 5 * time.Minute

// Deps are the injectable API operations behind the Manager. Every field
// defaults to a real SDK-backed implementation (DefaultDeps); tests replace
// them to stay offline.
type Deps struct {
	// ListProjects lists projects visible to the account access token.
	ListProjects func(ctx context.Context, serverURL, accessToken string) ([]memoryapi.Project, error)
	// ListTokens lists the API tokens registered for a project.
	ListTokens func(ctx context.Context, serverURL, accessToken, projectID string) ([]memoryapi.Token, error)
	// CreateToken mints a project-scoped token with the given name/scopes.
	CreateToken func(ctx context.Context, serverURL, accessToken, projectID, name string, scopes []string) (memoryapi.CreatedToken, error)
	// RevokeToken revokes a project token by id.
	RevokeToken func(ctx context.Context, serverURL, accessToken, projectID, tokenID string) error
}

// DefaultDeps returns the production SDK-backed implementations.
func DefaultDeps() Deps {
	return Deps{
		ListProjects: func(ctx context.Context, serverURL, accessToken string) ([]memoryapi.Project, error) {
			c, err := memoryapi.NewBearerClient(serverURL, accessToken)
			if err != nil {
				return nil, err
			}
			return c.ListProjects(ctx)
		},
		ListTokens: func(ctx context.Context, serverURL, accessToken, projectID string) ([]memoryapi.Token, error) {
			c, err := memoryapi.NewBearerClient(serverURL, accessToken)
			if err != nil {
				return nil, err
			}
			return c.ListTokens(ctx, projectID)
		},
		CreateToken: func(ctx context.Context, serverURL, accessToken, projectID, name string, scopes []string) (memoryapi.CreatedToken, error) {
			c, err := memoryapi.NewBearerClient(serverURL, accessToken)
			if err != nil {
				return memoryapi.CreatedToken{}, err
			}
			return c.CreateToken(ctx, projectID, name, scopes)
		},
		RevokeToken: func(ctx context.Context, serverURL, accessToken, projectID, tokenID string) error {
			c, err := memoryapi.NewBearerClient(serverURL, accessToken)
			if err != nil {
				return err
			}
			return c.RevokeToken(ctx, projectID, tokenID)
		},
	}
}

// Manager stores per-account project selection and tokens under baseDir.
type Manager struct {
	baseDir  string
	sessions Sessions
	deps     Deps
	// tokens refreshes the account access token before project operations, and
	// refresher forces one after a 401. Both are optional (a Sessions
	// implementation that does not provide them falls back to the stored
	// token), which keeps the offline test fakes simple.
	tokens    TokenSource
	refresher TokenRefresher
	// mu serializes EnsureToken minting so a second concurrent caller reuses
	// the token the first one just stored instead of racing into a 409+revoke.
	mu sync.Mutex
}

// NewManager returns a Manager backed by baseDir, reading sessions from an
// account store in the same location and using production API calls.
func NewManager(baseDir string) *Manager {
	accounts := account.NewManager(baseDir)
	return NewManagerWithDeps(baseDir, accounts, DefaultDeps())
}

// NewManagerWithDeps returns a Manager with injected session and API
// dependencies. It is the test seam for the project operations. When sessions
// also implements TokenSource/TokenRefresher — as *account.Manager does — the
// Manager refreshes through it.
func NewManagerWithDeps(baseDir string, sessions Sessions, deps Deps) *Manager {
	m := &Manager{baseDir: baseDir, sessions: sessions, deps: deps}
	if ts, ok := sessions.(TokenSource); ok {
		m.tokens = ts
	}
	if tr, ok := sessions.(TokenRefresher); ok {
		m.refresher = tr
	}
	return m
}

func (m *Manager) projectsDir() string { return filepath.Join(m.baseDir, "projects") }

func (m *Manager) tokensDir() string { return filepath.Join(m.baseDir, "tokens") }

// accountKey is the per-account file stem, matching account.FileNameFor.
func accountKey(serverURL string) string {
	return strings.TrimSuffix(account.FileNameFor(serverURL), ".json")
}

func (m *Manager) projectPath(serverURL string) string {
	return filepath.Join(m.projectsDir(), accountKey(serverURL)+".json")
}

func tokenFileName(serverURL, projectID string) string {
	return accountKey(serverURL) + "-" + safeKey(projectID) + ".json"
}

// safeKey maps an arbitrary id to a filesystem-safe stem.
func safeKey(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '.', r == '-', r == '_':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	name := b.String()
	if name == "" {
		name = "default"
	}
	return name
}

// accessToken returns a usable account access token for serverURL, refreshing
// it when the stored one is expired or about to expire, or ErrNotSignedIn when
// there is no session.
func (m *Manager) accessToken(ctx context.Context, serverURL string) (string, error) {
	if m.tokens != nil {
		tok, err := m.tokens.AccessToken(ctx, serverURL, accessTokenRefreshWindow)
		switch {
		case err == nil && tok != "":
			return tok, nil
		case err != nil && !errors.Is(err, account.ErrNoSession):
			return "", err
		}
	}
	sess, err := m.sessions.SessionFor(serverURL)
	if err != nil {
		return "", err
	}
	if sess == nil || sess.AccessToken == "" {
		return "", ErrNotSignedIn
	}
	return sess.AccessToken, nil
}

// withToken runs op with a usable account access token and retries once after a
// forced refresh when the API answers 401. A 401 means the token was rejected
// although the stored expiry said it was fine (revoked, or a clock that ran
// ahead), which only a refresh can fix; without the retry every such rejection
// surfaced as "signed out" until the user signed in again.
func (m *Manager) withToken(ctx context.Context, serverURL string, op func(accessToken string) error) error {
	token, err := m.accessToken(ctx, serverURL)
	if err != nil {
		return err
	}
	err = op(token)
	if err == nil || !memoryapi.IsUnauthorized(err) || m.refresher == nil {
		return err
	}
	if _, rerr := m.refresher.Refresh(ctx, serverURL); rerr != nil {
		// The refresh itself was rejected: the session genuinely needs a new
		// sign-in, so surface the original rejection with its cause.
		return err
	}
	token, terr := m.accessToken(ctx, serverURL)
	if terr != nil {
		return err
	}
	return op(token)
}

// List returns the projects visible to the signed-in account for serverURL.
func (m *Manager) List(ctx context.Context, serverURL string) ([]Project, error) {
	var out []Project
	err := m.withToken(ctx, serverURL, func(token string) error {
		var lerr error
		out, lerr = m.deps.ListProjects(ctx, serverURL, token)
		return lerr
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// activeRecord is the persisted active-project selection for one account.
type activeRecord struct {
	ServerURL string `json:"server_url"`
	ProjectID string `json:"project_id"`
	Name      string `json:"name,omitempty"`
}

// Active returns the active project for serverURL, if one is set.
func (m *Manager) Active(serverURL string) (Project, bool) {
	b, err := os.ReadFile(m.projectPath(serverURL))
	if err != nil {
		return Project{}, false
	}
	var rec activeRecord
	if err := json.Unmarshal(b, &rec); err != nil || rec.ProjectID == "" {
		return Project{}, false
	}
	return Project{ID: rec.ProjectID, Name: rec.Name}, true
}

// SetActive persists the active project for serverURL (0600, atomic).
func (m *Manager) SetActive(serverURL, projectID, name string) error {
	if projectID == "" {
		return errors.New("project: project id is required")
	}
	b, err := json.MarshalIndent(activeRecord{ServerURL: serverURL, ProjectID: projectID, Name: name}, "", "  ")
	if err != nil {
		return fmt.Errorf("project: marshal active project: %w", err)
	}
	return secretstore.New(m.projectsDir()).Save(accountKey(serverURL)+".json", b)
}

// tokenRecord is a stored project token.
type tokenRecord struct {
	ServerURL string   `json:"server_url"`
	ProjectID string   `json:"project_id"`
	TokenID   string   `json:"token_id,omitempty"`
	Token     string   `json:"token"`
	Name      string   `json:"name,omitempty"`
	Scopes    []string `json:"scopes,omitempty"`
	CreatedAt string   `json:"created_at,omitempty"`
}

func (m *Manager) loadToken(serverURL, projectID string) (tokenRecord, bool) {
	b, err := os.ReadFile(filepath.Join(m.tokensDir(), tokenFileName(serverURL, projectID)))
	if err != nil {
		return tokenRecord{}, false
	}
	var rec tokenRecord
	if err := json.Unmarshal(b, &rec); err != nil || rec.Token == "" {
		return tokenRecord{}, false
	}
	return rec, true
}

func (m *Manager) saveToken(serverURL, projectID string, rec tokenRecord) error {
	b, err := json.MarshalIndent(rec, "", "  ")
	if err != nil {
		return fmt.Errorf("project: marshal token: %w", err)
	}
	return secretstore.New(m.tokensDir()).Save(tokenFileName(serverURL, projectID), b)
}

// EnsureToken returns the connector's project token for projectID, reusing a
// stored one when present and otherwise minting a least-privilege token
// (scopes TokenScopes) named TokenName(projectID).
func (m *Manager) EnsureToken(ctx context.Context, serverURL, projectID string) (string, error) {
	if projectID == "" {
		return "", errors.New("project: project id is required")
	}
	// Serialize minting per Manager: two concurrent callers for the same
	// project would otherwise both miss the local record, race into a 409, and
	// one could revoke the other's freshly minted token. Locking here lets the
	// second caller reuse the token the first one stored.
	m.mu.Lock()
	defer m.mu.Unlock()

	if rec, ok := m.loadToken(serverURL, projectID); ok {
		return rec.Token, nil
	}
	name := m.TokenName(projectID)
	var rec tokenRecord
	err := m.withToken(ctx, serverURL, func(accessToken string) error {
		created, cerr := m.deps.CreateToken(ctx, serverURL, accessToken, projectID, name, append([]string(nil), TokenScopes...))
		if cerr != nil {
			if !memoryapi.IsTokenNameExists(cerr) {
				return fmt.Errorf("project: mint token for %s: %w", projectID, cerr)
			}
			// A token with this project-scoped name already exists on the
			// server but no local record is stored (e.g. it was cleared on
			// sign-out). Revoke the conflicting token and re-mint once.
			if rerr := m.revokeConflictingTokens(ctx, serverURL, accessToken, projectID, name); rerr != nil {
				return fmt.Errorf("project: mint token for %s: %w", projectID, rerr)
			}
			created, cerr = m.deps.CreateToken(ctx, serverURL, accessToken, projectID, name, append([]string(nil), TokenScopes...))
			if cerr != nil {
				return fmt.Errorf("project: mint token for %s: %w", projectID, cerr)
			}
		}
		rec = tokenRecord{
			ServerURL: serverURL,
			ProjectID: projectID,
			TokenID:   created.ID,
			Token:     created.Token,
			Name:      created.Name,
			Scopes:    created.Scopes,
			CreatedAt: created.CreatedAt,
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	if err := m.saveToken(serverURL, projectID, rec); err != nil {
		return "", err
	}
	// A fresh token was just minted (the reuse path returned early), so it is
	// safe to clean up any superseded legacy credential still active in this
	// project — never an in-use token.
	if accessToken, terr := m.accessToken(ctx, serverURL); terr == nil {
		m.revokeLegacyTokens(ctx, serverURL, accessToken, projectID, m.legacyTokenName())
	}
	return rec.Token, nil
}

// revokeConflictingTokens revokes the caller's own active project tokens named
// name so the name can be reused. The plaintext secret of an existing token is
// not recoverable, so a conflicting token that this connector no longer holds
// must be replaced. Only the caller's own (OwnedByCaller) active (not
// IsRevoked) tokens for this project with a matching name are touched; another
// user's same-name token is never revoked.
func (m *Manager) revokeConflictingTokens(ctx context.Context, serverURL, accessToken, projectID, name string) error {
	tokens, err := m.deps.ListTokens(ctx, serverURL, accessToken, projectID)
	if err != nil {
		return err
	}
	for _, t := range tokens {
		if t.IsRevoked || !t.OwnedByCaller || t.Name != name {
			continue
		}
		if err := m.deps.RevokeToken(ctx, serverURL, accessToken, projectID, t.ID); err != nil {
			return err
		}
	}
	return nil
}

// revokeLegacyTokens best-effort revokes the caller's own active tokens named
// legacyName for this project. An upgraded connector must not leave the old
// connector-<hostname> credential live in that project once it mints a
// project-scoped token. Errors are ignored: cleanup is a courtesy and the
// superseded credential is no longer used locally.
func (m *Manager) revokeLegacyTokens(ctx context.Context, serverURL, accessToken, projectID, legacyName string) {
	if m.deps.ListTokens == nil || m.deps.RevokeToken == nil {
		return
	}
	tokens, err := m.deps.ListTokens(ctx, serverURL, accessToken, projectID)
	if err != nil {
		return
	}
	for _, t := range tokens {
		if t.IsRevoked || !t.OwnedByCaller || t.Name != legacyName {
			continue
		}
		_ = m.deps.RevokeToken(ctx, serverURL, accessToken, projectID, t.ID)
	}
}

// Revoke revokes the stored project token (best-effort) and deletes it
// locally. A missing token is not an error.
func (m *Manager) Revoke(ctx context.Context, serverURL, projectID string) error {
	if rec, ok := m.loadToken(serverURL, projectID); ok {
		if accessToken, err := m.accessToken(ctx, serverURL); err == nil && rec.TokenID != "" {
			// Best-effort: the local copy is removed even if the server call
			// fails (the token may already be gone or the session expired).
			_ = m.deps.RevokeToken(ctx, serverURL, accessToken, projectID, rec.TokenID)
		}
	}
	return secretstore.New(m.tokensDir()).Delete(tokenFileName(serverURL, projectID))
}

// ClearAccountTokens removes every locally stored project token for serverURL.
// It is used on sign-out so a later login re-mints rather than reusing stale
// credentials.
func (m *Manager) ClearAccountTokens(serverURL string) error {
	prefix := accountKey(serverURL) + "-"
	entries, err := os.ReadDir(m.tokensDir())
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("project: list tokens: %w", err)
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasPrefix(e.Name(), prefix) {
			continue
		}
		if err := os.Remove(filepath.Join(m.tokensDir(), e.Name())); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("project: remove token %s: %w", e.Name(), err)
		}
	}
	return nil
}

// TokenName is the name given to connector-minted project tokens:
// connector-<hostname>-<projectID>. The project id suffix keeps the name
// unique across projects: the server enforces UNIQUE (user_id, name) over all
// of a user's active tokens, so a bare connector-<hostname> would collide when
// the same host connects a second project.
func (m *Manager) TokenName(projectID string) string {
	host := hostName()
	if projectID == "" {
		return "connector-" + host
	}
	return "connector-" + host + "-" + projectID
}

// legacyTokenName returns the pre-project-scoping connector token name
// (connector-<hostname>) that older connector builds used.
func (m *Manager) legacyTokenName() string {
	return "connector-" + hostName()
}

// hostName returns the connector host's hostname, falling back to "unknown".
func hostName() string {
	host, err := os.Hostname()
	if err != nil || host == "" {
		return "unknown"
	}
	return host
}
