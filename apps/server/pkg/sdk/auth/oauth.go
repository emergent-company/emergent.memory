package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// OAuthProvider implements Provider for OAuth 2.0 device flow authentication.
type OAuthProvider struct {
	mu          sync.RWMutex
	oidcConfig  *OIDCConfig
	credentials *Credentials
	clientID    string
	credsPath   string
}

// NewOAuthProvider creates a new OAuth provider.
// If credentials exist at credsPath and are valid, they will be loaded.
// Otherwise, device flow must be initiated manually.
func NewOAuthProvider(oidcConfig *OIDCConfig, clientID, credsPath string) *OAuthProvider {
	// Expand tilde in path
	if strings.HasPrefix(credsPath, "~/") {
		home, _ := os.UserHomeDir()
		credsPath = filepath.Join(home, credsPath[2:])
	}

	provider := &OAuthProvider{
		oidcConfig: oidcConfig,
		clientID:   clientID,
		credsPath:  credsPath,
	}

	// Try to load existing credentials
	if creds, err := LoadCredentials(credsPath); err == nil {
		provider.credentials = creds
	}

	return provider
}

// Authenticate adds the Bearer token to the request.
func (p *OAuthProvider) Authenticate(req *http.Request) error {
	p.mu.RLock()
	defer p.mu.RUnlock()

	if p.credentials == nil {
		return fmt.Errorf("no credentials available - please run device flow authentication")
	}

	if p.credentials.IsExpired() {
		return fmt.Errorf("credentials expired - please refresh or re-authenticate")
	}

	req.Header.Set("Authorization", "Bearer "+p.credentials.AccessToken)
	return nil
}

// Refresh refreshes the access token using the refresh token.
func (p *OAuthProvider) Refresh(ctx context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.credentials == nil || p.credentials.RefreshToken == "" {
		return fmt.Errorf("no refresh token available")
	}

	data := url.Values{}
	data.Set("grant_type", "refresh_token")
	data.Set("refresh_token", p.credentials.RefreshToken)
	data.Set("client_id", p.clientID)

	req, err := http.NewRequestWithContext(ctx, "POST", p.oidcConfig.TokenEndpoint, strings.NewReader(data.Encode()))
	if err != nil {
		return fmt.Errorf("failed to create refresh request: %w", err)
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send refresh request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read response body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		var errResp tokenErrorResponse
		if err := json.Unmarshal(body, &errResp); err == nil {
			return fmt.Errorf("refresh failed: %s - %s", errResp.Error, errResp.ErrorDescription)
		}
		return fmt.Errorf("refresh failed with status %d: %s", resp.StatusCode, string(body))
	}

	var tokenResp TokenResponse
	if err := json.Unmarshal(body, &tokenResp); err != nil {
		return fmt.Errorf("failed to parse token response: %w", err)
	}

	// Update credentials
	expiresAt := time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second)
	p.credentials = &Credentials{
		AccessToken:  tokenResp.AccessToken,
		RefreshToken: tokenResp.RefreshToken,
		ExpiresAt:    expiresAt,
		IssuerURL:    p.oidcConfig.Issuer,
	}

	// Save updated credentials
	if err := SaveCredentials(p.credentials, p.credsPath); err != nil {
		return fmt.Errorf("failed to save credentials: %w", err)
	}

	return nil
}

// DeviceCodeResponse represents the response from device authorization endpoint.
type DeviceCodeResponse struct {
	DeviceCode              string `json:"device_code"`
	UserCode                string `json:"user_code"`
	VerificationURI         string `json:"verification_uri"`
	VerificationURIComplete string `json:"verification_uri_complete"`
	ExpiresIn               int    `json:"expires_in"`
	Interval                int    `json:"interval"`
}

// TokenResponse represents the response from token endpoint.
type TokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int    `json:"expires_in"`
	TokenType    string `json:"token_type"`
	IDToken      string `json:"id_token,omitempty"`
}

type tokenErrorResponse struct {
	Error            string `json:"error"`
	ErrorDescription string `json:"error_description"`
}

// InitiateDeviceFlow starts the OAuth device flow and returns instructions for the user.
func (p *OAuthProvider) InitiateDeviceFlow(ctx context.Context) (*DeviceCodeResponse, error) {
	data := url.Values{}
	data.Set("client_id", p.clientID)
	data.Set("scope", "openid profile email")

	req, err := http.NewRequestWithContext(ctx, "POST", p.oidcConfig.DeviceAuthorizationEndpoint, strings.NewReader(data.Encode()))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send device code request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("device authorization endpoint returned status %d: %s", resp.StatusCode, string(body))
	}

	var deviceResp DeviceCodeResponse
	if err := json.Unmarshal(body, &deviceResp); err != nil {
		return nil, fmt.Errorf("failed to parse device code response: %w", err)
	}

	return &deviceResp, nil
}

// PollForToken polls the token endpoint until the user completes authorization.
func (p *OAuthProvider) PollForToken(ctx context.Context, deviceCode string, interval, expiresIn int) error {
	pollInterval := time.Duration(interval) * time.Second
	timeout := time.After(time.Duration(expiresIn) * time.Second)
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()

		case <-timeout:
			return fmt.Errorf("authentication timeout: user did not complete authorization within %d seconds", expiresIn)

		case <-ticker.C:
			token, err := p.attemptTokenRequest(ctx, deviceCode)
			if err != nil {
				if isRetryableError(err) {
					if strings.Contains(err.Error(), "slow_down") {
						ticker.Reset(pollInterval + (5 * time.Second))
					}
					continue
				}
				return err
			}

			// Successfully got token
			p.mu.Lock()
			expiresAt := time.Now().Add(time.Duration(token.ExpiresIn) * time.Second)
			p.credentials = &Credentials{
				AccessToken:  token.AccessToken,
				RefreshToken: token.RefreshToken,
				ExpiresAt:    expiresAt,
				IssuerURL:    p.oidcConfig.Issuer,
			}
			p.mu.Unlock()

			// Save credentials
			if err := SaveCredentials(p.credentials, p.credsPath); err != nil {
				return fmt.Errorf("failed to save credentials: %w", err)
			}

			return nil
		}
	}
}

func (p *OAuthProvider) attemptTokenRequest(ctx context.Context, deviceCode string) (*TokenResponse, error) {
	data := url.Values{}
	data.Set("grant_type", "urn:ietf:params:oauth:grant-type:device_code")
	data.Set("device_code", deviceCode)
	data.Set("client_id", p.clientID)

	req, err := http.NewRequestWithContext(ctx, "POST", p.oidcConfig.TokenEndpoint, strings.NewReader(data.Encode()))
	if err != nil {
		return nil, fmt.Errorf("failed to create token request: %w", err)
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send token request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		var errResp tokenErrorResponse
		if err := json.Unmarshal(body, &errResp); err == nil {
			return nil, fmt.Errorf("token request failed: %s - %s", errResp.Error, errResp.ErrorDescription)
		}
		return nil, fmt.Errorf("token request failed with status %d: %s", resp.StatusCode, string(body))
	}

	var tokenResp TokenResponse
	if err := json.Unmarshal(body, &tokenResp); err != nil {
		return nil, fmt.Errorf("failed to parse token response: %w", err)
	}

	return &tokenResp, nil
}

func isRetryableError(err error) bool {
	if err == nil {
		return false
	}

	errMsg := err.Error()
	return strings.Contains(errMsg, "authorization_pending") ||
		strings.Contains(errMsg, "slow_down")
}
