package main

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/labstack/echo/v4"
)

// accountSession is one signed-in Memory account held in the in-memory
// registry (design D2). It is the inactive-account counterpart to the active
// sessionClaims carried in the signed cookie.
type accountSession struct {
	Sub               string // Zitadel subject — stable account id (dedup/switch key)
	Name              string
	Email             string
	Picture           string
	AvatarOverrideURL string // memory avatar override (see sessionClaims)
	AccessToken       string
	RefreshToken      string
	IDToken           string // retained for RP-initiated logout (id_token_hint)
	ActiveProjectID   string // the account's remembered active project
	OrgID             string
	ExpiresAt         int64 // unix seconds
}

// accountRegistry holds inactive account sessions keyed by browser install id.
// It is in-memory only (lost on gateway restart; the active account survives
// in its cookie) and guarded by a mutex for concurrent requests.
type accountRegistry struct {
	mu       sync.Mutex
	accounts map[string][]accountSession // install id -> inactive accounts
}

func newAccountRegistry() *accountRegistry {
	return &accountRegistry{accounts: map[string][]accountSession{}}
}

// reg returns the server's account registry, or nil when unset (bare test
// servers); the registry methods are nil-safe.
func (s *Server) reg() *accountRegistry {
	return s.registry
}

// list returns the inactive accounts for an install id (nil-safe).
func (r *accountRegistry) list(installID string) []accountSession {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]accountSession, len(r.accounts[installID]))
	copy(out, r.accounts[installID])
	return out
}

// put inserts or replaces (by sub) an account for an install id (nil-safe).
func (r *accountRegistry) put(installID string, a accountSession) {
	if r == nil || a.Sub == "" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	list := r.accounts[installID]
	for i := range list {
		if list[i].Sub == a.Sub {
			list[i] = a
			r.accounts[installID] = list
			return
		}
	}
	r.accounts[installID] = append(list, a)
}

// remove deletes an account by sub, dropping the install key when empty
// (nil-safe).
func (r *accountRegistry) remove(installID, sub string) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	list := r.accounts[installID]
	out := list[:0]
	for _, a := range list {
		if a.Sub != sub {
			out = append(out, a)
		}
	}
	if len(out) == 0 {
		delete(r.accounts, installID)
	} else {
		r.accounts[installID] = out
	}
}

// get returns an account by sub (nil-safe).
func (r *accountRegistry) get(installID, sub string) (accountSession, bool) {
	if r == nil {
		return accountSession{}, false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, a := range r.accounts[installID] {
		if a.Sub == sub {
			return a, true
		}
	}
	return accountSession{}, false
}

// sessionToAccount maps active session claims to a registry entry.
func sessionToAccount(c sessionClaims) accountSession {
	return accountSession{
		Sub:               c.Sub,
		Name:              c.Name,
		Email:             c.Email,
		Picture:           c.Picture,
		AvatarOverrideURL: c.AvatarOverrideURL,
		AccessToken:       c.AccessToken,
		RefreshToken:      c.RefreshToken,
		IDToken:           c.IDToken,
		ActiveProjectID:   c.ActiveProjectID,
		OrgID:             c.OrgID,
		ExpiresAt:         c.ExpiresAt,
	}
}

// accountToSession maps a registry entry back to session claims.
func accountToSession(a accountSession) sessionClaims {
	return sessionClaims{
		AccessToken:       a.AccessToken,
		RefreshToken:      a.RefreshToken,
		IDToken:           a.IDToken,
		ActiveProjectID:   a.ActiveProjectID,
		OrgID:             a.OrgID,
		Name:              a.Name,
		Email:             a.Email,
		Picture:           a.Picture,
		AvatarOverrideURL: a.AvatarOverrideURL,
		Sub:               a.Sub,
		ExpiresAt:         a.ExpiresAt,
	}
}

// backfillAccountEmail fills a cached account's Email from its Memory profile
// when the IdP token lacked one (mirror of the active-account fallback in
// ui.go). The profile is fetched with the account's OWN access token — callers
// run in a request whose session context may belong to a different account
// (OIDC select_account callback), so the ambient token is not trustworthy
// here. Failures are swallowed: the menu row just keeps no email line.
func (s *Server) backfillAccountEmail(ctx context.Context, a *accountSession) {
	if a == nil || a.Email != "" || a.AccessToken == "" {
		return
	}
	mem := NewMemoryClient(s.cfg.MemoryURL, a.AccessToken, s.cfg.MemoryProjectID)
	if p, err := mem.GetProfile(ctx); err == nil && p != nil && p.Email != "" {
		a.Email = p.Email
	}
}

// registerAccount reconciles the in-memory registry after a successful OIDC
// callback (design D1/D4): it ensures the browser has a stable install id and,
// for an "add another account" flow, rotates the prior active account into the
// registry and drops the newly-authenticated account (if already cached) so no
// account is ever listed twice.
func (s *Server) registerAccount(c echo.Context, flow *oauthStateClaims, claims sessionClaims) {
	if claims.Sub == "" {
		return // legacy IdP without a sub: nothing to key multi-account on
	}
	installID, err := s.ensureInstallID(c)
	if err != nil || installID == "" {
		return
	}
	if flow.Prompt == "select_account" {
		// Move the current active account into the registry, unless the newly
		// authenticated account is the same one (a re-add → plain switch).
		if old, oerr := s.currentSession(c); oerr == nil && old.Sub != "" && old.Sub != claims.Sub {
			acc := sessionToAccount(*old)
			s.backfillAccountEmail(c.Request().Context(), &acc)
			s.reg().put(installID, acc)
		}
		// The new account is now active: drop any stale cached copy.
		s.reg().remove(installID, claims.Sub)
	}
}

// authSwitch switches the active account to a previously signed-in account
// (POST /auth/switch, form field "sub"). It rotates the current active account
// into the registry, pops the target out, lazily refreshes the target's access
// token when needed, and re-issues the session cookie (design D4).
func (s *Server) authSwitch(c echo.Context) error {
	if s.cfg.AuthMode != "session" {
		return c.Redirect(http.StatusFound, "/")
	}
	sub := strings.TrimSpace(c.FormValue("sub"))
	if sub == "" {
		return c.Redirect(http.StatusFound, "/")
	}
	installID, ok := s.readInstallID(c)
	if !ok {
		return c.Redirect(http.StatusFound, "/auth/login")
	}
	target, found := s.reg().get(installID, sub)
	if !found {
		return c.Redirect(http.StatusFound, "/auth/login")
	}
	// Rotate the current active account into the registry.
	if cur, err := s.currentSession(c); err == nil && cur.Sub != "" && cur.Sub != target.Sub {
		acc := sessionToAccount(*cur)
		s.backfillAccountEmail(c.Request().Context(), &acc)
		s.reg().put(installID, acc)
	}
	// The target becomes active: drop it from the registry.
	s.reg().remove(installID, sub)

	claims := accountToSession(target)
	// Lazily refresh a stale target token before making it active.
	if claims.RefreshToken != "" && claims.ExpiresAt <= time.Now().Add(5*time.Minute).Unix() {
		if refreshed, err := s.refreshSessionClaims(c, &claims); err == nil {
			s.setSessionCookie(c, *refreshed)
			return c.Redirect(http.StatusFound, "/")
		}
	}
	s.setSessionCookie(c, claims)
	return c.Redirect(http.StatusFound, "/")
}
