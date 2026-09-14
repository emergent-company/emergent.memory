package account

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	sdkauth "github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/auth"

	"github.com/emergent-company/memory.web-ui/connector/internal/secretstore"
)

// PendingLoginTTL is how long an unfinished PKCE login stays valid.
const PendingLoginTTL = 10 * time.Minute

// PKCE sign-in errors. CompletePKCE returns these so callers can map them to
// clear messages and exit codes.
var (
	// ErrUnknownLogin means the login id has no pending record (unknown,
	// cancelled, expired-and-pruned, or already completed).
	ErrUnknownLogin = errors.New("account: unknown login")
	// ErrExpiredLogin means the pending login exceeded PendingLoginTTL.
	ErrExpiredLogin = errors.New("account: expired login")
	// ErrStateMismatch means the callback state did not match the pending login.
	ErrStateMismatch = errors.New("account: state mismatch")
)

// PendingLogin is the public half of a started PKCE login. It deliberately does
// NOT carry the PKCE code verifier: that stays only in the 0600 pending record.
type PendingLogin struct {
	LoginID      string    `json:"login_id"`
	AuthorizeURL string    `json:"authorize_url"`
	State        string    `json:"state"`
	ExpiresAt    time.Time `json:"expires_at"`
}

// pendingLogin is the persisted one-shot PKCE record. It is stored 0600 under
// <baseDir>/pending/<login_id>.json and deleted on completion, cancellation, or
// expiry.
type pendingLogin struct {
	LoginID      string    `json:"login_id"`
	State        string    `json:"state"`
	CodeVerifier string    `json:"code_verifier"`
	RedirectURI  string    `json:"redirect_uri"`
	Issuer       string    `json:"issuer"`
	ClientID     string    `json:"client_id"`
	CreatedAt    time.Time `json:"created_at"`
	ExpiresAt    time.Time `json:"expires_at"`
}

// PKCEOptions tunes StartPKCE for callers that bring their own OAuth client,
// such as the native macOS app.
type PKCEOptions struct {
	// ClientID overrides the package default ClientID when non-empty.
	ClientID string
	// Issuer, when set, is used directly and skips GET /api/auth/issuer
	// discovery (serverURL may then be empty).
	Issuer string
}

// StartPKCE begins an authorization-code + PKCE login. It resolves the OIDC
// endpoints (from opts.Issuer when provided, otherwise by discovering the
// server's issuer), generates state and a code verifier/challenge (S256),
// persists a one-shot pending record, and returns the authorize URL plus the
// identifiers the app needs to drive the browser and callback. The verifier is
// never returned.
func (m *Manager) StartPKCE(ctx context.Context, serverURL, redirectURI string, opts PKCEOptions) (*PendingLogin, error) {
	clientID := strings.TrimSpace(opts.ClientID)
	if clientID == "" {
		clientID = m.clientID
	}
	if clientID == "" {
		return nil, errors.New("account: client id is required")
	}
	if strings.TrimSpace(redirectURI) == "" {
		return nil, errors.New("account: redirect URI is required")
	}

	now := m.now()
	m.pruneExpiredPending(now)

	issuer := strings.TrimSpace(opts.Issuer)
	if issuer == "" {
		if strings.TrimSpace(serverURL) == "" {
			return nil, errors.New("account: server URL is required")
		}
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
	if oidc.AuthorizationEndpoint == "" {
		return nil, errors.New("account: OIDC config is missing authorization_endpoint")
	}

	loginID, err := randomToken(16)
	if err != nil {
		return nil, fmt.Errorf("account: generate login id: %w", err)
	}
	state, err := randomToken(16)
	if err != nil {
		return nil, fmt.Errorf("account: generate state: %w", err)
	}
	verifier, err := randomToken(32)
	if err != nil {
		return nil, fmt.Errorf("account: generate PKCE verifier: %w", err)
	}

	rec := pendingLogin{
		LoginID:      loginID,
		State:        state,
		CodeVerifier: verifier,
		RedirectURI:  redirectURI,
		Issuer:       issuer,
		ClientID:     clientID,
		CreatedAt:    now,
		ExpiresAt:    now.Add(PendingLoginTTL),
	}
	b, err := json.MarshalIndent(rec, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("account: marshal pending login: %w", err)
	}
	if err := m.pendingStore().Save(pendingFileName(loginID), b); err != nil {
		return nil, err
	}

	authorizeURL, err := buildAuthorizeURL(oidc.AuthorizationEndpoint, clientID, redirectURI, m.scopes, state, pkceChallenge(verifier))
	if err != nil {
		return nil, err
	}
	return &PendingLogin{
		LoginID:      loginID,
		AuthorizeURL: authorizeURL,
		State:        state,
		ExpiresAt:    rec.ExpiresAt,
	}, nil
}

// CompletePKCE finishes a PKCE login: it validates state, exchanges the code
// with the stored verifier, persists the session for serverURL, marks it
// active, and deletes the pending record (single use).
//
// A mismatched state invalidates the pending record (a stale or tampered
// callback cannot be replayed with the correct state afterwards).
func (m *Manager) CompletePKCE(ctx context.Context, serverURL, loginID, code, state string) (*Session, error) {
	if strings.TrimSpace(serverURL) == "" {
		return nil, errors.New("account: server URL is required")
	}
	rec, err := m.loadPending(loginID)
	if err != nil {
		return nil, err
	}

	if !rec.ExpiresAt.IsZero() && m.now().After(rec.ExpiresAt) {
		_ = m.deletePending(loginID)
		return nil, fmt.Errorf("%w: %s", ErrExpiredLogin, loginID)
	}
	if rec.State != state {
		_ = m.deletePending(loginID)
		return nil, fmt.Errorf("%w: %s", ErrStateMismatch, loginID)
	}

	oidc, err := m.deps.DiscoverOIDC(rec.Issuer)
	if err != nil {
		return nil, fmt.Errorf("account: discover OIDC at %s: %w", rec.Issuer, err)
	}
	clientID := rec.ClientID
	if clientID == "" {
		clientID = m.clientID
	}
	creds, err := m.deps.ExchangeCode(ctx, oidc, clientID, code, rec.CodeVerifier, rec.RedirectURI)
	if err != nil {
		return nil, fmt.Errorf("account: exchange authorization code: %w", err)
	}

	// Single use: drop the record as soon as the code is exchanged so it can
	// never be replayed, even if the persistence below fails.
	_ = m.deletePending(loginID)

	sess := &Session{
		ServerURL:    serverURL,
		IssuerURL:    rec.Issuer,
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

// CancelPKCE discards a pending login. It is best-effort: an unknown login id
// is not an error.
func (m *Manager) CancelPKCE(loginID string) error {
	return m.deletePending(loginID)
}

func (m *Manager) pendingDir() string { return filepath.Join(m.baseDir, "pending") }

func (m *Manager) pendingStore() secretstore.Store { return secretstore.New(m.pendingDir()) }

func (m *Manager) now() time.Time {
	if m.deps.Now != nil {
		return m.deps.Now()
	}
	return time.Now()
}

func pendingFileName(loginID string) string { return loginID + ".json" }

// validLoginID restricts ids to the URL-safe alphabet we generate, so a
// caller-supplied id can never escape the pending directory.
func validLoginID(loginID string) bool {
	if loginID == "" || len(loginID) > 128 {
		return false
	}
	for _, r := range loginID {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
		default:
			return false
		}
	}
	return true
}

func (m *Manager) loadPending(loginID string) (*pendingLogin, error) {
	if !validLoginID(loginID) {
		return nil, fmt.Errorf("%w: %s", ErrUnknownLogin, loginID)
	}
	b, err := m.pendingStore().Load(pendingFileName(loginID))
	if err != nil {
		return nil, err
	}
	if b == nil {
		return nil, fmt.Errorf("%w: %s", ErrUnknownLogin, loginID)
	}
	rec := &pendingLogin{}
	if err := json.Unmarshal(b, rec); err != nil {
		return nil, fmt.Errorf("account: parse pending login: %w", err)
	}
	return rec, nil
}

func (m *Manager) deletePending(loginID string) error {
	if !validLoginID(loginID) {
		return nil
	}
	return m.pendingStore().Delete(pendingFileName(loginID))
}

// pruneExpiredPending removes expired pending records. Errors are ignored:
// pruning is a housekeeping best-effort on the StartPKCE path.
func (m *Manager) pruneExpiredPending(now time.Time) {
	entries, err := os.ReadDir(m.pendingDir())
	if err != nil {
		return
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		path := filepath.Join(m.pendingDir(), e.Name())
		b, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var rec pendingLogin
		if json.Unmarshal(b, &rec) != nil {
			continue
		}
		if !rec.ExpiresAt.IsZero() && now.After(rec.ExpiresAt) {
			_ = os.Remove(path)
		}
	}
}

// randomToken returns n cryptographically random bytes, base64url-encoded.
func randomToken(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// pkceChallenge returns the S256 code challenge for a verifier.
func pkceChallenge(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

// buildAuthorizeURL assembles the authorization request URL.
func buildAuthorizeURL(endpoint, clientID, redirectURI string, scopes []string, state, challenge string) (string, error) {
	u, err := url.Parse(endpoint)
	if err != nil {
		return "", fmt.Errorf("account: parse authorization endpoint: %w", err)
	}
	q := u.Query()
	q.Set("response_type", "code")
	q.Set("client_id", clientID)
	q.Set("redirect_uri", redirectURI)
	q.Set("scope", strings.Join(scopes, " "))
	q.Set("state", state)
	q.Set("code_challenge", challenge)
	q.Set("code_challenge_method", "S256")
	u.RawQuery = q.Encode()
	return u.String(), nil
}

// exchangeCode is the production authorization-code token exchange.
func exchangeCode(ctx context.Context, oidc *sdkauth.OIDCConfig, clientID, code, codeVerifier, redirectURI string) (*sdkauth.Credentials, error) {
	if oidc == nil || oidc.TokenEndpoint == "" {
		return nil, errors.New("account: OIDC config is missing token_endpoint")
	}
	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("code", code)
	form.Set("code_verifier", codeVerifier)
	form.Set("client_id", clientID)
	form.Set("redirect_uri", redirectURI)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, oidc.TokenEndpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, fmt.Errorf("account: create token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := (&http.Client{Timeout: httpTimeout}).Do(req)
	if err != nil {
		return nil, fmt.Errorf("account: send token request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("account: token endpoint returned status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var tok struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int    `json:"expires_in"`
		TokenType    string `json:"token_type"`
		IDToken      string `json:"id_token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tok); err != nil {
		return nil, fmt.Errorf("account: parse token response: %w", err)
	}
	return &sdkauth.Credentials{
		AccessToken:  tok.AccessToken,
		RefreshToken: tok.RefreshToken,
		ExpiresAt:    time.Now().Add(time.Duration(tok.ExpiresIn) * time.Second),
		IssuerURL:    oidc.Issuer,
	}, nil
}
