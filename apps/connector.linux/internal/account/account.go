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
	"syscall"
	"time"

	sdkauth "github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/auth"

	"github.com/emergent-company/emergent.memory/apps/connector.linux/internal/config"
	"github.com/emergent-company/emergent.memory/apps/connector.linux/internal/secretstore"
)

// ClientID is the public OAuth client id used for device-flow sign-in. It is
// shared with the Memory CLI.
const ClientID = "362800068257972227"

// Scopes are the OAuth scopes requested during device flow. offline_access is
// required to receive a refresh token.
var Scopes = []string{"openid", "profile", "email", "offline_access"}

// httpTimeout bounds each account network call.
const httpTimeout = 15 * time.Second

// Cross-process refresh serialization. Zitadel refresh tokens are single-use,
// so two processes refreshing at once means one wins and the other is rejected;
// the lock makes the loser reuse the winner's token instead.
const (
	refreshLockTimeout = 20 * time.Second
	refreshLockPoll    = 50 * time.Millisecond
	refreshLockSuffix  = ".refresh.lock"
)

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
	// NeedsReauth marks a session whose refresh token the provider rejected
	// (expired, revoked, or rotated away by a process that lost the refresh
	// race). The tokens are deliberately KEPT: deleting them turns a
	// recoverable rejection into a forced interactive login, and a silent
	// re-auth (`prompt=none`) cannot run without the stored client/issuer.
	NeedsReauth bool `json:"needs_reauth,omitempty"`
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

// UpdateSession runs mutate over the stored session for serverURL and saves the
// result, holding the same cross-process refresh lock (and in-process mutex)
// that Refresh takes for the whole read-modify-write. A caller that changes the
// session outside a refresh must use this instead of SessionFor + Save:
// refresh tokens are single-use, so a refresh that rotates the token between an
// unlocked read and its Save would be overwritten, leaving a session that is
// stale by construction.
//
// When no session is stored the mutator starts from an empty document (with
// ServerURL set), so a caller can create one; Save creates the file. A mutator
// error is returned (wrapped) and nothing is written. The saved, mutated
// document is returned. mutate must not retain the pointer.
func (m *Manager) UpdateSession(serverURL string, mutate func(*Session) error) (*Session, error) {
	if mutate == nil {
		return nil, errors.New("account: nil session mutator")
	}
	// Same order as Refresh: the cross-process lock first, then refreshMu.
	unlock, err := m.lockRefresh(serverURL)
	if err != nil {
		return nil, err
	}
	defer unlock()

	m.refreshMu.Lock()
	defer m.refreshMu.Unlock()

	sess, err := m.SessionFor(serverURL)
	if err != nil {
		return nil, err
	}
	if sess == nil {
		sess = &Session{ServerURL: serverURL}
	}
	if err := mutate(sess); err != nil {
		return nil, fmt.Errorf("account: update session for %s: %w", serverURL, err)
	}
	if err := m.Save(serverURL, sess); err != nil {
		return nil, err
	}
	return sess, nil
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
// refresh that is rejected as an authentication error (the refresh token was
// rejected with invalid_grant/invalid_token or a 400/401 response) keeps the
// stored session and marks it NeedsReauth, so a caller can attempt a silent
// re-auth instead of forcing an interactive sign-in; transient failures
// (network, timeout, parse, 5xx) leave the stored session untouched. Refreshes
// are serialized per Manager and across processes.
func (m *Manager) Refresh(ctx context.Context, serverURL string) (*Session, error) {
	// Serialize across processes as well: a single-use rotation raced by two
	// `memory-connector` processes rejects the loser, so every refresh path
	// takes the same advisory lock.
	unlock, err := m.lockRefresh(serverURL)
	if err != nil {
		return nil, err
	}
	defer unlock()
	return m.refreshLocked(ctx, serverURL)
}

// refreshLocked performs the refresh. Callers must hold refreshMu and the
// cross-process refresh lock.
func (m *Manager) refreshLocked(ctx context.Context, serverURL string) (*Session, error) {
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
		if isAuthRejection(err) && m.refreshTokenStillCurrent(serverURL, sess.RefreshToken) {
			// Do NOT delete the session. The stored refresh token is the only
			// thing that can recover this account without user interaction, and
			// a deletion turns a rejection into a forced interactive sign-in
			// (the historical behaviour, and the reason a single-use rotation
			// race looked like "signed out unexpectedly"). Mark it so callers
			// can offer a silent re-auth instead.
			if !sess.NeedsReauth {
				sess.NeedsReauth = true
				_ = m.Save(serverURL, sess)
			}
		}
		return nil, fmt.Errorf("account: refresh %s: %w", serverURL, err)
	}

	sess.AccessToken = creds.AccessToken
	sess.NeedsReauth = false
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

// AccessToken returns a usable access token for serverURL, refreshing it when
// the stored one is missing or expires within minTTL.
//
// Refresh is serialized across PROCESSES as well as goroutines. Zitadel refresh
// tokens are single-use and rotate on every refresh, and the macOS app spawns a
// separate `memory-connector` process for each token read, so concurrent
// refreshes used to race: one process rotated the token and every loser was
// rejected with invalid_grant (which the app surfaced as "signed out"). Inside
// the advisory lock the session is re-read, so a process that waited returns the
// winner's fresh token instead of refreshing again.
//
// A session with no refresh token still returns its access token: the caller's
// API call will decide whether it is still good.
func (m *Manager) AccessToken(ctx context.Context, serverURL string, minTTL time.Duration) (string, error) {
	sess, err := m.SessionFor(serverURL)
	if err != nil {
		return "", err
	}
	if sess == nil {
		return "", fmt.Errorf("%w for %s", ErrNoSession, serverURL)
	}
	if !needsRefresh(sess, minTTL) {
		return sess.AccessToken, nil
	}
	if sess.RefreshToken == "" {
		if sess.AccessToken != "" {
			return sess.AccessToken, nil
		}
		return "", fmt.Errorf("%w for %s", ErrNoSession, serverURL)
	}

	unlock, err := m.lockRefresh(serverURL)
	if err != nil {
		return "", err
	}
	defer unlock()

	// Another process may have refreshed while this one waited for the lock.
	if sess, err = m.SessionFor(serverURL); err != nil {
		return "", err
	}
	if sess == nil {
		return "", fmt.Errorf("%w for %s", ErrNoSession, serverURL)
	}
	if !needsRefresh(sess, minTTL) {
		return sess.AccessToken, nil
	}
	refreshed, err := m.refreshLocked(ctx, serverURL)
	if err != nil {
		return "", err
	}
	return refreshed.AccessToken, nil
}

// needsRefresh reports whether the access token must be renewed before use:
// missing, or expiring within minTTL. A session with no recorded expiry is
// treated as usable — there is nothing to compare against, and a stale token
// then fails at the API call, which retries after a refresh.
func needsRefresh(s *Session, minTTL time.Duration) bool {
	if s == nil || s.AccessToken == "" {
		return true
	}
	if s.ExpiresAt.IsZero() {
		return false
	}
	return time.Now().Add(minTTL).After(s.ExpiresAt)
}

// lockRefresh takes the cross-process advisory lock that serializes refresh for
// one server, waiting up to refreshLockTimeout. The returned function releases
// it; it is safe to call exactly once (defer unlock()).
func (m *Manager) lockRefresh(serverURL string) (func(), error) {
	dir := m.accountsDir()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("account: create accounts dir: %w", err)
	}
	path := filepath.Join(dir, "."+strings.TrimSuffix(accountFileName(serverURL), ".json")+refreshLockSuffix)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("account: open refresh lock: %w", err)
	}
	deadline := time.Now().Add(refreshLockTimeout)
	for {
		err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			break
		}
		if !errors.Is(err, syscall.EWOULDBLOCK) && !errors.Is(err, syscall.EAGAIN) {
			_ = f.Close()
			return nil, fmt.Errorf("account: lock refresh for %s: %w", serverURL, err)
		}
		if time.Now().After(deadline) {
			_ = f.Close()
			return nil, fmt.Errorf("account: timed out after %s waiting for the refresh lock of %s", refreshLockTimeout, serverURL)
		}
		time.Sleep(refreshLockPoll)
	}
	return func() {
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		_ = f.Close()
	}, nil
}

// isAuthRejection reports whether a refresh error is an authentication
// rejection (the token endpoint rejected the refresh token) rather than a
// transient failure. Only such rejections mark the session as needing re-auth.
func isAuthRejection(err error) bool {
	var re *sdkauth.RefreshError
	if !errors.As(err, &re) {
		return false
	}
	switch re.Code {
	case "invalid_grant", "invalid_token":
		// The provider's own words for "this refresh token is dead".
		return true
	case "":
		// No OAuth error code: fall back to the status. A 5xx is transient and
		// deliberately lands here as false.
		return re.StatusCode == http.StatusBadRequest || re.StatusCode == http.StatusUnauthorized
	}
	// A coded error that is not about the grant itself (`invalid_request`,
	// `invalid_client`, `unsupported_grant_type`) is a configuration problem: a
	// new sign-in would fail the same way, so it must not be reported as
	// "needs re-auth" or it would loop the user through a useless login.
	return false
}

// refreshTokenStillCurrent reports whether the stored session still holds the
// refresh token this Refresh just attempted. A cross-process race can rotate a
// single-use refresh token between the initial read and the token call: the
// winner persists the rotated token, after which the loser's original token is
// rejected with invalid_grant. In that case the stored session is valid again
// and must not be cleared.
func (m *Manager) refreshTokenStillCurrent(serverURL, attempted string) bool {
	cur, err := m.SessionFor(serverURL)
	if err != nil || cur == nil {
		// Cannot verify. Treat the attempted token as current: a rejection is
		// then attributed to it and the session is kept (marked needs_reauth)
		// rather than discarded — the token is the one recovery path that
		// needs no user.
		return true
	}
	return cur.RefreshToken == attempted
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
