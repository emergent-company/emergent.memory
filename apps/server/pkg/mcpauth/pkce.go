// Package mcpauth implements the generic OAuth 2.1 client needed to talk to a
// remote MCP server protected by OAuth 2.1 with dynamic client registration
// (RFC 7591) and PKCE (RFC 7636). It is intentionally provider-agnostic: it
// discovers the protected-resource and authorization-server metadata, registers
// a public client, and exchanges/refreshes tokens. Provider wiring (Trale) lives
// in the sources domain; this package stays generic so the MCP registry can
// adopt it later without depending on any single vendor.
package mcpauth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
)

// NewCodeVerifier returns a URL-safe PKCE code verifier (RFC 7636: 43-128
// unreserved characters; here 43 from 32 random bytes).
func NewCodeVerifier() (string, error) { return randToken(32) }

// NewState returns a URL-safe random state value for CSRF protection.
func NewState() (string, error) { return randToken(32) }

// CodeChallengeS256 returns the S256 PKCE challenge for a verifier.
func CodeChallengeS256(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

func randToken(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("mcpauth: random token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
