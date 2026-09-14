package auth

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// RevokeToken revokes a single OAuth token via the OIDC revocation endpoint (RFC 7009).
// tokenTypeHint should be "access_token" or "refresh_token".
// Returns nil if revocation succeeds or the endpoint is not available (best-effort).
func RevokeToken(revocationEndpoint, clientID, token, tokenTypeHint string) error {
	if revocationEndpoint == "" {
		return fmt.Errorf("no revocation endpoint available")
	}
	if token == "" {
		return nil // nothing to revoke
	}

	data := url.Values{}
	data.Set("token", token)
	data.Set("token_type_hint", tokenTypeHint)
	data.Set("client_id", clientID)

	client := &http.Client{Timeout: 10 * time.Second}
	req, err := http.NewRequest("POST", revocationEndpoint, strings.NewReader(data.Encode()))
	if err != nil {
		return fmt.Errorf("failed to create revocation request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("revocation request failed: %w", err)
	}
	defer resp.Body.Close()

	// RFC 7009: 200 OK means the token was revoked (or was already invalid).
	// Any non-2xx is an error.
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("revocation endpoint returned status %d", resp.StatusCode)
	}

	return nil
}

// RevokeCredentials attempts to revoke all tokens in the given credentials
// using the OIDC revocation endpoint discovered from the stored issuer URL.
// It revokes the refresh token first (higher-value target), then the access token.
// Returns a list of warnings for any failures — callers should log these but not block.
func RevokeCredentials(creds *Credentials, clientID string) []string {
	var warnings []string

	if creds.IssuerURL == "" {
		return append(warnings, "no issuer URL in credentials, skipping token revocation")
	}

	oidcConfig, err := DiscoverOIDC(creds.IssuerURL)
	if err != nil {
		return append(warnings, fmt.Sprintf("failed to discover OIDC config: %v", err))
	}

	if oidcConfig.RevocationEndpoint == "" {
		return append(warnings, "OIDC provider does not expose a revocation endpoint, skipping token revocation")
	}

	// Revoke refresh token first (higher value target)
	if creds.RefreshToken != "" {
		if err := RevokeToken(oidcConfig.RevocationEndpoint, clientID, creds.RefreshToken, "refresh_token"); err != nil {
			warnings = append(warnings, fmt.Sprintf("failed to revoke refresh token: %v", err))
		}
	}

	// Revoke access token
	if creds.AccessToken != "" {
		if err := RevokeToken(oidcConfig.RevocationEndpoint, clientID, creds.AccessToken, "access_token"); err != nil {
			warnings = append(warnings, fmt.Sprintf("failed to revoke access token: %v", err))
		}
	}

	return warnings
}
