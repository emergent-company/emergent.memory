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

	"github.com/emergent-company/memory.web-ui/connector/internal/account"
	"github.com/emergent-company/memory.web-ui/connector/internal/memoryapi"
	"github.com/emergent-company/memory.web-ui/connector/internal/secretstore"
)

// Project is the connector-owned project type (an alias for the API layer's
// type, so callers can use one name everywhere).
type Project = memoryapi.Project

// ErrNotSignedIn is returned when an operation needs an account session and
// none is stored for the server.
var ErrNotSignedIn = errors.New("project: not signed in")

// TokenScopes are the least-privilege scopes requested for connector tokens,
// matching the macOS app.
var TokenScopes = []string{"data:read"}

// Sessions supplies the stored account session for a server. *account.Manager
// implements it.
type Sessions interface {
	SessionFor(serverURL string) (*account.Session, error)
}

// Deps are the injectable API operations behind the Manager. Every field
// defaults to a real SDK-backed implementation (DefaultDeps); tests replace
// them to stay offline.
type Deps struct {
	// ListProjects lists projects visible to the account access token.
	ListProjects func(ctx context.Context, serverURL, accessToken string) ([]memoryapi.Project, error)
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
}

// NewManager returns a Manager backed by baseDir, reading sessions from an
// account store in the same location and using production API calls.
func NewManager(baseDir string) *Manager {
	return NewManagerWithDeps(baseDir, account.NewManager(baseDir), DefaultDeps())
}

// NewManagerWithDeps returns a Manager with injected session and API
// dependencies. It is the test seam for the project operations.
func NewManagerWithDeps(baseDir string, sessions Sessions, deps Deps) *Manager {
	return &Manager{baseDir: baseDir, sessions: sessions, deps: deps}
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

// accessToken returns the account access token for serverURL, or
// ErrNotSignedIn.
func (m *Manager) accessToken(serverURL string) (string, error) {
	sess, err := m.sessions.SessionFor(serverURL)
	if err != nil {
		return "", err
	}
	if sess == nil || sess.AccessToken == "" {
		return "", ErrNotSignedIn
	}
	return sess.AccessToken, nil
}

// List returns the projects visible to the signed-in account for serverURL.
func (m *Manager) List(ctx context.Context, serverURL string) ([]Project, error) {
	token, err := m.accessToken(serverURL)
	if err != nil {
		return nil, err
	}
	return m.deps.ListProjects(ctx, serverURL, token)
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
// (scopes TokenScopes) named TokenName().
func (m *Manager) EnsureToken(ctx context.Context, serverURL, projectID string) (string, error) {
	if projectID == "" {
		return "", errors.New("project: project id is required")
	}
	if rec, ok := m.loadToken(serverURL, projectID); ok {
		return rec.Token, nil
	}
	accessToken, err := m.accessToken(serverURL)
	if err != nil {
		return "", err
	}
	created, err := m.deps.CreateToken(ctx, serverURL, accessToken, projectID, m.TokenName(), append([]string(nil), TokenScopes...))
	if err != nil {
		return "", fmt.Errorf("project: mint token for %s: %w", projectID, err)
	}
	rec := tokenRecord{
		ServerURL: serverURL,
		ProjectID: projectID,
		TokenID:   created.ID,
		Token:     created.Token,
		Name:      created.Name,
		Scopes:    created.Scopes,
		CreatedAt: created.CreatedAt,
	}
	if err := m.saveToken(serverURL, projectID, rec); err != nil {
		return "", err
	}
	return rec.Token, nil
}

// Revoke revokes the stored project token (best-effort) and deletes it
// locally. A missing token is not an error.
func (m *Manager) Revoke(ctx context.Context, serverURL, projectID string) error {
	if rec, ok := m.loadToken(serverURL, projectID); ok {
		if accessToken, err := m.accessToken(serverURL); err == nil && rec.TokenID != "" {
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
// connector-<hostname>.
func (m *Manager) TokenName() string {
	host, err := os.Hostname()
	if err != nil || host == "" {
		host = "unknown"
	}
	return "connector-" + host
}
