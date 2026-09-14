// Package account stores per-server sign-in sessions for the connector and
// implements the OAuth device-flow login behind `memory-connector auth`.
//
// Each server gets its own 0600 session file under <baseDir>/accounts/, with an
// `active` pointer naming the most recently used server. Network operations are
// injectable through Deps so tests can exercise the whole flow without a
// browser or a live OIDC provider. Credentials are kept by the connector and
// never written to the Memory CLI's ~/.memory store.
package account

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	sdkauth "github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/auth"

	"github.com/emergent-company/memory.web-ui/connector/internal/config"
	"github.com/emergent-company/memory.web-ui/connector/internal/secretstore"
)

// ClientID is the public OAuth client id used for device-flow sign-in. It is
// shared with the Memory CLI.
const ClientID = "362800068257972227"

// Scopes are the OAuth scopes requested during device flow. offline_access is
// required to receive a refresh token.
var Scopes = []string{"openid", "profile", "email", "offline_access"}

// httpTimeout bounds each account network call.
const httpTimeout = 15 * time.Second

// ErrNoSession indicates there is no stored session for the requested server.
var ErrNoSession = errors.New("account: no stored session")

// Session is a signed-in account for one Memory server. Its JSON shape matches
// the SDK/CLI Credentials document (plus server_url), so a CLI credentials
// file can be imported unchanged.
type Session struct {
	ServerURL    string    `json:"server_url,omitempty"`
	IssuerURL    string    `json:"issuer_url,omitempty"`
	ClientID     string    `json:"client_id,omitempty"`
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token,omitempty"`
	ExpiresAt    time.Time `json:"expires_at"`
	UserEmail    string    `json:"user_email,omitempty"`
	UserID       string    `json:"user_id,omitempty"`
}

// Expired reports whether the access token is past its expiry.
func (s *Session) Expired() bool {
	if s == nil || s.ExpiresAt.IsZero() {
		return true
	}
	return time.Now().After(s.ExpiresAt)
}

// DeviceCode is the device-authorization response.
type DeviceCode struct {
	DeviceCode              string `json:"device_code"`
	UserCode                string `json:"user_code"`
	VerificationURI         string `json:"verification_uri"`
	VerificationURIComplete string `json:"verification_uri_complete"`
	ExpiresIn               int    `json:"expires_in"`
	Interval                int    `json:"interval"`
}

// Identity is the subset of GET /api/auth/me the connector uses.
type Identity struct {
	UserID string `json:"user_id"`
	Email  string `json:"email"`
	Type   string `json:"type"`
}

// Deps are the injectable operations behind Login and Refresh. Every field
// defaults to a real network implementation (DefaultDeps); tests replace the
// fields they need to keep the flow offline and deterministic.
type Deps struct {
	// DiscoverIssuer fetches GET {serverURL}/api/auth/issuer and returns the
	// OIDC issuer URL.
	DiscoverIssuer func(ctx context.Context, serverURL string) (string, error)
	// DiscoverOIDC resolves OIDC endpoints from an issuer.
	DiscoverOIDC func(issuer string) (*sdkauth.OIDCConfig, error)
	// RequestDeviceCode POSTs the device-authorization request with the given
	// scopes and returns the user/device codes.
	RequestDeviceCode func(ctx context.Context, oidc *sdkauth.OIDCConfig, clientID string, scopes []string) (*DeviceCode, error)
	// PollToken waits for approval via the SDK provider, which persists the
	// resulting credentials to credsPath, and returns them.
	PollToken func(ctx context.Context, oidc *sdkauth.OIDCConfig, clientID, credsPath, deviceCode string, interval, expiresIn int) (*sdkauth.Credentials, error)
	// RefreshToken refreshes stored credentials through the SDK provider.
	RefreshToken func(ctx context.Context, oidc *sdkauth.OIDCConfig, clientID, credsPath string) (*sdkauth.Credentials, error)
	// FetchIdentity calls GET {serverURL}/api/auth/me with the access token.
	FetchIdentity func(ctx context.Context, serverURL, accessToken string) (Identity, error)
	// ExchangeCode exchanges an OAuth authorization code (PKCE) for tokens.
	ExchangeCode func(ctx context.Context, oidc *sdkauth.OIDCConfig, clientID, code, codeVerifier, redirectURI string) (*sdkauth.Credentials, error)
	// Now returns the current time; injectable for deterministic expiry tests.
	Now func() time.Time
}

// DefaultDeps returns the production network implementations.
func DefaultDeps() Deps {
	return Deps{
		DiscoverIssuer:    discoverIssuer,
		DiscoverOIDC:      sdkauth.DiscoverOIDC,
		RequestDeviceCode: requestDeviceCode,
		PollToken:         pollToken,
		RefreshToken:      refreshToken,
		FetchIdentity:     fetchIdentity,
		ExchangeCode:      exchangeCode,
		Now:               time.Now,
	}
}

// Manager persists sessions under baseDir and drives login/refresh.
type Manager struct {
	baseDir   string
	clientID  string
	scopes    []string
	deps      Deps
	refreshMu sync.Mutex
}

// NewManager returns a Manager backed by baseDir using the production network
// operations.
func NewManager(baseDir string) *Manager {
	return NewManagerWithDeps(baseDir, DefaultDeps())
}

// NewManagerWithDeps returns a Manager with injected network operations. It is
// the test seam for the device flow.
func NewManagerWithDeps(baseDir string, deps Deps) *Manager {
	return &Manager{
		baseDir:  baseDir,
		clientID: ClientID,
		scopes:   append([]string(nil), Scopes...),
		deps:     deps,
	}
}

// BaseDirForConfig returns the account store directory for a config file path:
// a "memory-connector" directory next to the config file.
func BaseDirForConfig(configPath string) string {
	return filepath.Join(filepath.Dir(configPath), "memory-connector")
}

// DefaultBaseDir returns the account store directory for the default config
// location.
func DefaultBaseDir() string {
	return BaseDirForConfig(config.DefaultConfigPath())
}

func (m *Manager) accountsDir() string { return filepath.Join(m.baseDir, "accounts") }

func (m *Manager) sessionPath(serverURL string) string {
	return filepath.Join(m.accountsDir(), accountFileName(serverURL))
}

func (m *Manager) store() secretstore.Store { return secretstore.New(m.accountsDir()) }

func (m *Manager) baseStore() secretstore.Store { return secretstore.New(m.baseDir) }

// accountFileName maps a server URL to a deterministic, filesystem-safe file
// name based on its host.
func accountFileName(serverURL string) string {
	host := serverURL
	if u, err := url.Parse(serverURL); err == nil && u.Host != "" {
		host = u.Host
	}
	var b strings.Builder
	for _, r := range host {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '.', r == '-':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	name := b.String()
	if name == "" {
		name = "default"
	}
	return name + ".json"
}

// FileNameFor returns the deterministic, filesystem-safe file name used for
// serverURL's session file. Callers that key other per-account state (for
// example the project store) can reuse it to line up with the account store.
func FileNameFor(serverURL string) string { return accountFileName(serverURL) }

// Save writes the session for serverURL atomically (0600 file, 0700 dir).
func (m *Manager) Save(serverURL string, s *Session) error {
	if s == nil {
		return errors.New("account: nil session")
	}
	s.ServerURL = serverURL
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Errorf("account: marshal session for %s: %w", serverURL, err)
	}
	return m.store().Save(accountFileName(serverURL), b)
}

// SessionFor returns the stored session for serverURL, or (nil, nil) when none
// is stored.
func (m *Manager) SessionFor(serverURL string) (*Session, error) {
	b, err := m.store().Load(accountFileName(serverURL))
	if err != nil {
		return nil, err
	}
	if b == nil {
		return nil, nil
	}
	s := &Session{}
	if err := json.Unmarshal(b, s); err != nil {
		return nil, fmt.Errorf("account: parse session for %s: %w", serverURL, err)
	}
	if s.ServerURL == "" {
		s.ServerURL = serverURL
	}
	return s, nil
}

// Active returns the last server signed in to, if any.
func (m *Manager) Active() (string, bool) {
	b, err := os.ReadFile(filepath.Join(m.baseDir, "active"))
	if err != nil {
		return "", false
	}
	serverURL := strings.TrimSpace(string(b))
	if serverURL == "" {
		return "", false
	}
	return serverURL, true
}

// SetActive records serverURL as the active account.
func (m *Manager) SetActive(serverURL string) error {
	if err := m.baseStore().Save("active", []byte(serverURL)); err != nil {
		return fmt.Errorf("account: set active %s: %w", serverURL, err)
	}
	return nil
}

// Logout removes the stored session for serverURL (idempotent). If it was the
// active account, the active pointer is cleared.
func (m *Manager) Logout(serverURL string) error {
	if err := m.store().Delete(accountFileName(serverURL)); err != nil {
		return err
	}
	if active, ok := m.Active(); ok && active == serverURL {
		if err := m.baseStore().Delete("active"); err != nil {
			return err
		}
	}
	return nil
}

// LogoutAll removes every stored session and the active pointer.
func (m *Manager) LogoutAll() error {
	entries, err := os.ReadDir(m.accountsDir())
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("account: list sessions: %w", err)
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if err := os.Remove(filepath.Join(m.accountsDir(), e.Name())); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("account: remove session %s: %w", e.Name(), err)
		}
	}
	if err := m.baseStore().Delete("active"); err != nil {
		return err
	}
	return nil
}

// LoginOptions tunes Login for callers that bring their own OAuth client or
// issuer, such as the native macOS app or a dev environment whose registered
// client differs from the built-in default.
type LoginOptions struct {
	// ClientID overrides the manager default (the package ClientID) when
	// non-empty.
	ClientID string
	// Issuer, when set, is used directly and skips GET /api/auth/issuer
	// discovery.
	Issuer string
}

// Login runs the OAuth device flow for serverURL with the default client and
// issuer discovery. It delegates to LoginWithOptions with zero options.
func (m *Manager) Login(ctx context.Context, serverURL string, out io.Writer) (*Session, error) {
	return m.LoginWithOptions(ctx, serverURL, LoginOptions{}, out)
}

// LoginWithOptions runs the OAuth device flow for serverURL, persists the
// resulting session (including the effective client id), marks it active, and
// returns it. Verification instructions are written to out (when non-nil).
//
// The effective client id is opts.ClientID when non-empty, else the manager
// default; an empty effective client id is an error. The effective issuer is
// opts.Issuer when non-empty (skipping /api/auth/issuer discovery), else the
// issuer discovered for serverURL.
func (m *Manager) LoginWithOptions(ctx context.Context, serverURL string, opts LoginOptions, out io.Writer) (*Session, error) {
	if strings.TrimSpace(serverURL) == "" {
		return nil, errors.New("account: server URL is required")
	}

	clientID := strings.TrimSpace(opts.ClientID)
	if clientID == "" {
		clientID = m.clientID
	}
	if clientID == "" {
		return nil, errors.New("account: client id is required")
	}

	issuer := strings.TrimSpace(opts.Issuer)
	if issuer == "" {
		var err error
		issuer, err = m.deps.DiscoverIssuer(ctx, serverURL)
		if err != nil {
			return nil, fmt.Errorf("account: discover issuer for %s: %w", serverURL, err)
		}
	}
	oidc, err := m.deps.DiscoverOIDC(issuer)
	if err != nil {
		return nil, fmt.Errorf("account: discover OIDC at %s: %w", issuer, err)
	}

	device, err := m.deps.RequestDeviceCode(ctx, oidc, clientID, m.scopes)
	if err != nil {
		return nil, fmt.Errorf("account: request device code: %w", err)
	}
	if out != nil {
		verifyURL := device.VerificationURIComplete
		if verifyURL == "" {
			verifyURL = device.VerificationURI
		}
		_, _ = fmt.Fprintf(out, "To sign in, visit %s\n", verifyURL)
		_, _ = fmt.Fprintf(out, "and enter code: %s\n", device.UserCode)
		_, _ = fmt.Fprintln(out, "Waiting for authorization...")
	}

	credsPath := m.sessionPath(serverURL)
	creds, err := m.deps.PollToken(ctx, oidc, clientID, credsPath, device.DeviceCode, device.Interval, device.ExpiresIn)
	if err != nil {
		return nil, fmt.Errorf("account: authorization failed: %w", err)
	}

	sess := &Session{
		ServerURL:    serverURL,
		IssuerURL:    issuer,
		ClientID:     clientID,
		AccessToken:  creds.AccessToken,
		RefreshToken: creds.RefreshToken,
		ExpiresAt:    creds.ExpiresAt,
	}
	// Identity confirmation is best-effort: a valid token with no /me details
	// still signs the user in.
	if id, idErr := m.deps.FetchIdentity(ctx, serverURL, creds.AccessToken); idErr == nil {
		sess.UserEmail = id.Email
		sess.UserID = id.UserID
	}
	if err := m.Save(serverURL, sess); err != nil {
		return nil, err
	}
	if err := m.SetActive(serverURL); err != nil {
		return nil, err
	}
	return sess, nil
}

// Refresh refreshes the stored session for serverURL via the SDK provider. A
// failed refresh clears the stored session so callers can prompt for a new
// sign-in. Refreshes are serialized per Manager.
func (m *Manager) Refresh(ctx context.Context, serverURL string) (*Session, error) {
	m.refreshMu.Lock()
	defer m.refreshMu.Unlock()

	sess, err := m.SessionFor(serverURL)
	if err != nil {
		return nil, err
	}
	if sess == nil {
		return nil, fmt.Errorf("%w for %s", ErrNoSession, serverURL)
	}
	if sess.RefreshToken == "" {
		return nil, fmt.Errorf("account: no refresh token for %s", serverURL)
	}

	oidc, err := m.oidcConfigFor(ctx, serverURL, sess)
	if err != nil {
		return nil, err
	}
	creds, err := m.deps.RefreshToken(ctx, oidc, m.effectiveClientID(sess.ClientID), m.sessionPath(serverURL))
	if err != nil {
		_ = m.Logout(serverURL)
		return nil, fmt.Errorf("account: refresh %s: %w", serverURL, err)
	}

	sess.AccessToken = creds.AccessToken
	if creds.RefreshToken != "" {
		sess.RefreshToken = creds.RefreshToken
	}
	sess.ExpiresAt = creds.ExpiresAt
	if sess.IssuerURL == "" {
		sess.IssuerURL = oidc.Issuer
	}
	if err := m.Save(serverURL, sess); err != nil {
		return nil, err
	}
	return sess, nil
}

// effectiveClientID returns persisted when non-empty, else the manager default,
// else the package ClientID. Sessions written before client_id was persisted
// carry no id and fall back to the default.
func (m *Manager) effectiveClientID(persisted string) string {
	if id := strings.TrimSpace(persisted); id != "" {
		return id
	}
	if id := strings.TrimSpace(m.clientID); id != "" {
		return id
	}
	return ClientID
}

func (m *Manager) oidcConfigFor(ctx context.Context, serverURL string, sess *Session) (*sdkauth.OIDCConfig, error) {
	issuer := ""
	if sess != nil {
		issuer = sess.IssuerURL
	}
	if issuer == "" {
		var err error
		issuer, err = m.deps.DiscoverIssuer(ctx, serverURL)
		if err != nil {
			return nil, fmt.Errorf("account: discover issuer for %s: %w", serverURL, err)
		}
	}
	oidc, err := m.deps.DiscoverOIDC(issuer)
	if err != nil {
		return nil, fmt.Errorf("account: discover OIDC at %s: %w", issuer, err)
	}
	return oidc, nil
}

// ImportCLISession reads a Memory CLI credentials file (~/.memory/credentials
// .json) into a Session. It never writes to or modifies the CLI file.
func ImportCLISession(path string) (*Session, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("account: read CLI credentials %s: %w", path, err)
	}
	var creds sdkauth.Credentials
	if err := json.Unmarshal(b, &creds); err != nil {
		return nil, fmt.Errorf("account: parse CLI credentials %s: %w", path, err)
	}
	return &Session{
		IssuerURL:    creds.IssuerURL,
		AccessToken:  creds.AccessToken,
		RefreshToken: creds.RefreshToken,
		ExpiresAt:    creds.ExpiresAt,
		UserEmail:    creds.UserEmail,
	}, nil
}

func discoverIssuer(ctx context.Context, serverURL string) (string, error) {
	endpoint := strings.TrimRight(serverURL, "/") + "/api/auth/issuer"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return "", fmt.Errorf("create issuer request: %w", err)
	}
	resp, err := (&http.Client{Timeout: httpTimeout}).Do(req)
	if err != nil {
		return "", fmt.Errorf("reach server at %s: %w", serverURL, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("issuer endpoint returned status %d", resp.StatusCode)
	}
	var body struct {
		Issuer     string `json:"issuer"`
		Standalone bool   `json:"standalone"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return "", fmt.Errorf("parse issuer response: %w", err)
	}
	if body.Standalone {
		return "", errors.New("server is in standalone mode and does not support OAuth sign-in")
	}
	if body.Issuer == "" {
		return "", errors.New("server did not return an OIDC issuer URL")
	}
	return body.Issuer, nil
}

func requestDeviceCode(ctx context.Context, oidc *sdkauth.OIDCConfig, clientID string, scopes []string) (*DeviceCode, error) {
	if oidc == nil || oidc.DeviceAuthorizationEndpoint == "" {
		return nil, errors.New("OIDC config is missing device_authorization_endpoint")
	}
	form := url.Values{}
	form.Set("client_id", clientID)
	form.Set("scope", strings.Join(scopes, " "))
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, oidc.DeviceAuthorizationEndpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, fmt.Errorf("create device request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := (&http.Client{Timeout: httpTimeout}).Do(req)
	if err != nil {
		return nil, fmt.Errorf("send device request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("device authorization endpoint returned status %d", resp.StatusCode)
	}
	var device DeviceCode
	if err := json.NewDecoder(resp.Body).Decode(&device); err != nil {
		return nil, fmt.Errorf("parse device response: %w", err)
	}
	return &device, nil
}

func pollToken(ctx context.Context, oidc *sdkauth.OIDCConfig, clientID, credsPath, deviceCode string, interval, expiresIn int) (*sdkauth.Credentials, error) {
	if interval <= 0 {
		interval = 5
	}
	provider := sdkauth.NewOAuthProvider(oidc, clientID, credsPath)
	if err := provider.PollForToken(ctx, deviceCode, interval, expiresIn); err != nil {
		return nil, err
	}
	creds, err := sdkauth.LoadCredentials(credsPath)
	if err != nil {
		return nil, fmt.Errorf("load credentials after poll: %w", err)
	}
	return creds, nil
}

func refreshToken(ctx context.Context, oidc *sdkauth.OIDCConfig, clientID, credsPath string) (*sdkauth.Credentials, error) {
	provider := sdkauth.NewOAuthProvider(oidc, clientID, credsPath)
	if err := provider.Refresh(ctx); err != nil {
		return nil, err
	}
	creds, err := sdkauth.LoadCredentials(credsPath)
	if err != nil {
		return nil, fmt.Errorf("load credentials after refresh: %w", err)
	}
	return creds, nil
}

func fetchIdentity(ctx context.Context, serverURL, accessToken string) (Identity, error) {
	endpoint := strings.TrimRight(serverURL, "/") + "/api/auth/me"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return Identity{}, fmt.Errorf("create identity request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	resp, err := (&http.Client{Timeout: httpTimeout}).Do(req)
	if err != nil {
		return Identity{}, fmt.Errorf("fetch identity: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return Identity{}, fmt.Errorf("identity endpoint returned status %d", resp.StatusCode)
	}
	var id Identity
	if err := json.NewDecoder(resp.Body).Decode(&id); err != nil {
		return Identity{}, fmt.Errorf("parse identity response: %w", err)
	}
	return id, nil
}
