package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// sessionClaims is the stateless session payload carried in the signed session
// cookie (design D2). ExpiresAt is a unix-seconds expiry. Name/Email/Picture
// are the signed-in user's identity (from the ID token) for the UI.
type sessionClaims struct {
	AccessToken     string `json:"access_token"`
	RefreshToken    string `json:"refresh_token"`
	ActiveProjectID string `json:"active_project_id"`
	// IDToken is the OIDC id_token from the token exchange, retained so
	// RP-initiated logout can pass it as id_token_hint to Zitadel's
	// end_session_endpoint (see authLogout). It is omitted from the signed
	// cookie when doing so would exceed the browser's per-cookie size limit
	// (see setSessionCookie), in which case logout falls back to client_id.
	IDToken string `json:"id_token,omitempty"`
	OrgID   string `json:"org_id,omitempty"` // active org (drives X-Org-ID)
	Name    string `json:"name,omitempty"`
	Email   string `json:"email,omitempty"`
	Picture string `json:"picture,omitempty"`
	Sub     string `json:"sub,omitempty"` // Zitadel subject — stable account id
	// AvatarOverrideURL is the gateway-relative memory avatar URL
	// (/api/user/avatar?v=<key>) when the user uploaded a profile photo; it
	// wins over the IdP picture when both are present.
	AvatarOverrideURL string `json:"avatar_override_url,omitempty"`
	ExpiresAt         int64  `json:"exp"` // unix seconds
}

const (
	// sessionCookieName is the signed session cookie. HttpOnly + SameSite=Lax.
	sessionCookieName = "memory_session"
	// oauthStateCookieName is the short-lived signed OAuth flow state cookie
	// (state/code_verifier/nonce carried across the Zitadel redirect).
	oauthStateCookieName = "memory_oauth"
)

// signCookieValue returns base64url(payload) + "." +
// base64url(HMAC-SHA256(secret, payload)).
func signCookieValue(secret string, payload []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write(payload)
	return base64.RawURLEncoding.EncodeToString(payload) + "." +
		base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

// verifyCookieValue checks the cookie's HMAC signature and returns the raw
// payload bytes. It rejects malformed values and tampering.
func verifyCookieValue(secret, value string) ([]byte, error) {
	if value == "" {
		return nil, errSessionMalformed
	}
	parts := strings.Split(value, ".")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return nil, errSessionMalformed
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return nil, errSessionMalformed
	}
	sig, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, errSessionMalformed
	}
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write(payload)
	want := mac.Sum(nil)
	if !hmac.Equal(sig, want) {
		return nil, errSessionTampered
	}
	return payload, nil
}

// issueSession signs claims with the given secret and returns the cookie value.
// Format: base64url(payloadJSON) + "." + base64url(HMAC-SHA256(secret, payload)).
func issueSession(secret string, claims sessionClaims, now time.Time) (string, error) {
	payload, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}
	return signCookieValue(secret, payload), nil
}

var (
	errSessionMalformed = errors.New("session: malformed cookie value")
	errSessionTampered  = errors.New("session: invalid signature")
	errSessionExpired   = errors.New("session: expired")
	errSessionEmpty     = errors.New("session: empty claims")
)

// verifySessionSignature verifies the cookie's HMAC signature and decodes the
// claims WITHOUT checking expiry or claim presence. It is the lenient path the
// token-refresh flow uses to recover a refresh token (and identity) from an
// expired-but-authentic cookie.
func verifySessionSignature(secret, value string) (*sessionClaims, error) {
	payload, err := verifyCookieValue(secret, value)
	if err != nil {
		return nil, err
	}
	var claims sessionClaims
	if err := json.Unmarshal(payload, &claims); err != nil {
		return nil, fmt.Errorf("session: %w", err)
	}
	return &claims, nil
}

// verifySession validates the cookie value's signature and expiry and returns
// the claims. It rejects tampering (wrong signature), malformed values,
// expired cookies, and empty/absent claims.
func verifySession(secret, value string, now time.Time) (*sessionClaims, error) {
	claims, err := verifySessionSignature(secret, value)
	if err != nil {
		return nil, err
	}
	if claims.AccessToken == "" && claims.RefreshToken == "" && claims.ActiveProjectID == "" {
		return nil, errSessionEmpty
	}
	if claims.ExpiresAt <= now.Unix() {
		return nil, errSessionExpired
	}
	return claims, nil
}
