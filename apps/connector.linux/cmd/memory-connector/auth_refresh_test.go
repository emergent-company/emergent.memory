package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	sdkauth "github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/auth"

	"github.com/emergent-company/emergent.memory/apps/connector.linux/internal/account"
)

// refreshDeps returns offline deps whose refresh yields a fresh, rotated pair.
func refreshDeps(t *testing.T) account.Deps {
	t.Helper()
	return account.Deps{
		DiscoverOIDC: func(issuer string) (*sdkauth.OIDCConfig, error) {
			return &sdkauth.OIDCConfig{Issuer: issuer, TokenEndpoint: "https://issuer.test/token"}, nil
		},
		RefreshToken: func(context.Context, *sdkauth.OIDCConfig, string, string) (*sdkauth.Credentials, error) {
			return &sdkauth.Credentials{
				AccessToken:  "access-renewed",
				RefreshToken: "refresh-2",
				ExpiresAt:    time.Now().Add(12 * time.Hour),
			}, nil
		},
	}
}

func TestAuthRefreshJSONReportsRenewedSession(t *testing.T) {
	dir := t.TempDir()
	serverURL := "https://memory.example.test"
	configPath := writeAuthConfig(t, dir, serverURL)
	seedAccessSession(t, configPath, serverURL, &account.Session{
		ServerURL:    serverURL,
		IssuerURL:    "https://issuer.test",
		AccessToken:  "access-stale",
		RefreshToken: "refresh-1",
		ExpiresAt:    time.Now().Add(-time.Minute),
	})
	withAccessTokenManager(t, refreshDeps(t))

	code, stdout, stderr := runAuthCapture("refresh", "--config", configPath, "--json")
	if code != 0 {
		t.Fatalf("code = %d, want 0 (stderr: %s)", code, stderr)
	}
	var doc authRefreshDoc
	if err := json.Unmarshal([]byte(stdout), &doc); err != nil {
		t.Fatalf("decode %q: %v", stdout, err)
	}
	if !doc.Refreshed {
		t.Error("refreshed = false, want true for an expired session")
	}
	if !doc.CanRefresh {
		t.Error("can_refresh = false, want true (the rotated refresh token is stored)")
	}
	if doc.NeedsReauth {
		t.Error("needs_reauth = true after a successful refresh")
	}
	if doc.ExpiresAt == "" {
		t.Error("expires_at is empty, want the new expiry")
	}
	// The refresh token must never appear in the document.
	if strings.Contains(stdout, "refresh-1") || strings.Contains(stdout, "refresh-2") {
		t.Errorf("stdout leaks a refresh token: %q", stdout)
	}
}

func TestAuthRefreshSkipsAFreshToken(t *testing.T) {
	dir := t.TempDir()
	serverURL := "https://memory.example.test"
	configPath := writeAuthConfig(t, dir, serverURL)
	seedAccessSession(t, configPath, serverURL, &account.Session{
		ServerURL:    serverURL,
		IssuerURL:    "https://issuer.test",
		AccessToken:  "access-fresh",
		RefreshToken: "refresh-1",
		ExpiresAt:    time.Now().Add(2 * time.Hour),
	})
	deps := refreshDeps(t)
	var refreshes int
	deps.RefreshToken = func(context.Context, *sdkauth.OIDCConfig, string, string) (*sdkauth.Credentials, error) {
		refreshes++
		return nil, errors.New("must not refresh")
	}
	withAccessTokenManager(t, deps)

	code, stdout, _ := runAuthCapture("refresh", "--config", configPath, "--json")
	if code != 0 {
		t.Fatalf("code = %d, want 0", code)
	}
	var doc authRefreshDoc
	if err := json.Unmarshal([]byte(stdout), &doc); err != nil {
		t.Fatalf("decode %q: %v", stdout, err)
	}
	if doc.Refreshed {
		t.Error("refreshed = true for a token outside the window")
	}
	if refreshes != 0 {
		t.Errorf("refresh token endpoint called %d times, want 0", refreshes)
	}
}

func TestAuthRefreshRejectionKeepsTheSession(t *testing.T) {
	dir := t.TempDir()
	serverURL := "https://memory.example.test"
	configPath := writeAuthConfig(t, dir, serverURL)
	seedAccessSession(t, configPath, serverURL, &account.Session{
		ServerURL:    serverURL,
		IssuerURL:    "https://issuer.test",
		AccessToken:  "access-stale",
		RefreshToken: "refresh-1",
		ExpiresAt:    time.Now().Add(-time.Minute),
	})
	deps := refreshDeps(t)
	deps.RefreshToken = func(context.Context, *sdkauth.OIDCConfig, string, string) (*sdkauth.Credentials, error) {
		// Mirrors what the SDK's OAuthProvider returns: the provider's code is
		// carried in both fields, and in the message the CLI prints on stderr
		// (which is what the macOS app's failure classifier reads).
		return nil, &sdkauth.RefreshError{
			StatusCode: http.StatusBadRequest,
			Code:       "invalid_grant",
			Err:        errors.New("refresh failed: invalid_grant - refresh token is invalid"),
		}
	}
	withAccessTokenManager(t, deps)

	code, stdout, stderr := runAuthCapture("refresh", "--config", configPath, "--json")
	if code != 1 {
		t.Fatalf("code = %d, want 1", code)
	}
	if stdout != "" {
		t.Errorf("stdout = %q, want empty on a rejected refresh", stdout)
	}
	if !strings.Contains(stderr, "invalid_grant") {
		t.Errorf("stderr = %q, want the provider's error code", stderr)
	}

	m := account.NewManager(account.BaseDirForConfig(configPath))
	sess, err := m.SessionFor(serverURL)
	if err != nil || sess == nil {
		t.Fatalf("SessionFor = (%v, %v), want the session retained", sess, err)
	}
	if sess.RefreshToken != "refresh-1" {
		t.Errorf("refresh token = %q, want refresh-1 kept", sess.RefreshToken)
	}
	if !sess.NeedsReauth {
		t.Error("NeedsReauth = false after a rejected refresh, want true")
	}
}

func TestAuthStatusReportsRefreshFields(t *testing.T) {
	dir := t.TempDir()
	serverURL := "https://memory.example.test"
	configPath := writeAuthConfig(t, dir, serverURL)
	seedAccessSession(t, configPath, serverURL, &account.Session{
		ServerURL:    serverURL,
		IssuerURL:    "https://issuer.test",
		AccessToken:  "access-stale",
		RefreshToken: "refresh-1",
		ExpiresAt:    time.Now().Add(-time.Minute),
		NeedsReauth:  true,
	})
	withAccessTokenManager(t, refreshDeps(t))

	code, stdout, stderr := runAuthCapture("status", "--config", configPath, "--json")
	if code != 0 {
		t.Fatalf("code = %d, want 0 (stderr: %s)", code, stderr)
	}
	var doc struct {
		SignedIn    bool `json:"signed_in"`
		Expired     bool `json:"expired"`
		CanRefresh  bool `json:"can_refresh"`
		NeedsReauth bool `json:"needs_reauth"`
	}
	if err := json.Unmarshal([]byte(stdout), &doc); err != nil {
		t.Fatalf("decode %q: %v", stdout, err)
	}
	// `signed_in` keeps its meaning: a session is stored. The two new fields
	// tell a client whether the access token is renewable and whether the
	// provider has already rejected the refresh token.
	if !doc.SignedIn {
		t.Error("signed_in = false, want true (a session is stored)")
	}
	if !doc.Expired {
		t.Error("expired = false, want true for an expired access token")
	}
	if !doc.CanRefresh {
		t.Error("can_refresh = false, want true (a refresh token is stored)")
	}
	if !doc.NeedsReauth {
		t.Error("needs_reauth = false, want true for a rejected session")
	}
}

// TestAuthStatusFromSessionCarriesRefreshFields covers the one document shape
// several commands share: `auth complete` and `auth import` build it too, so a
// session must look the same whichever command printed it.
func TestAuthStatusFromSessionCarriesRefreshFields(t *testing.T) {
	sess := &account.Session{
		ServerURL:    "https://memory.example.test",
		IssuerURL:    "https://issuer.test",
		AccessToken:  "access-1",
		RefreshToken: "refresh-1",
		ExpiresAt:    time.Now().Add(time.Hour),
		NeedsReauth:  true,
		UserEmail:    "a@example.test",
	}
	doc := authStatusFromSession(sess.ServerURL, sess)
	if !doc.SignedIn {
		t.Error("signed_in = false, want true")
	}
	if !doc.CanRefresh {
		t.Error("can_refresh = false, want true (a refresh token is stored)")
	}
	if !doc.NeedsReauth {
		t.Error("needs_reauth = false, want true (the session recorded a rejection)")
	}
	if doc.Expired {
		t.Error("expired = true, want false for a token an hour from now")
	}

	// No refresh token: not renewable, and nothing was rejected.
	bare := &account.Session{ServerURL: sess.ServerURL, AccessToken: "access-1"}
	bareDoc := authStatusFromSession(bare.ServerURL, bare)
	if bareDoc.CanRefresh || bareDoc.NeedsReauth {
		t.Errorf("doc = %+v, want can_refresh=false and needs_reauth=false", bareDoc)
	}

	// Absent session: the zero document, never a signed-in one.
	if empty := authStatusFromSession("", nil); empty.SignedIn || empty.CanRefresh || empty.NeedsReauth {
		t.Errorf("doc = %+v, want the zero status", empty)
	}
}

func TestAuthStatusWithoutSessionReportsNoRefresh(t *testing.T) {
	dir := t.TempDir()
	serverURL := "https://memory.example.test"
	configPath := writeAuthConfig(t, dir, serverURL)
	withAccessTokenManager(t, refreshDeps(t))

	code, stdout, stderr := runAuthCapture("status", "--config", configPath, "--json")
	if code != 0 {
		t.Fatalf("code = %d, want 0 (stderr: %s)", code, stderr)
	}
	var doc struct {
		SignedIn   bool `json:"signed_in"`
		CanRefresh bool `json:"can_refresh"`
	}
	if err := json.Unmarshal([]byte(stdout), &doc); err != nil {
		t.Fatalf("decode %q: %v", stdout, err)
	}
	if doc.SignedIn || doc.CanRefresh {
		t.Errorf("status = %+v, want signed_in=false and can_refresh=false", doc)
	}
}
