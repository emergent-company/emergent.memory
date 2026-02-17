package githubapp

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"path"
	"time"
)

// Service handles business logic for GitHub App integration.
type Service struct {
	store        *Store
	crypto       *Crypto
	tokenService *TokenService
	log          *slog.Logger
	httpClient   *http.Client
}

// NewService creates a new GitHub App service.
func NewService(store *Store, crypto *Crypto, tokenService *TokenService, log *slog.Logger) *Service {
	return &Service{
		store:        store,
		crypto:       crypto,
		tokenService: tokenService,
		log:          log.With("component", "githubapp-service"),
		httpClient:   &http.Client{Timeout: 30 * time.Second},
	}
}

// VerifyWebhookSignature verifies the X-Hub-Signature-256 header against the stored webhook secret.
func (s *Service) VerifyWebhookSignature(ctx context.Context, signature string, body []byte) error {
	config, err := s.store.Get(ctx)
	if err != nil {
		return fmt.Errorf("failed to get GitHub App config: %w", err)
	}
	if config == nil {
		return fmt.Errorf("GitHub App not configured")
	}

	if len(config.WebhookSecretEncrypted) == 0 {
		return fmt.Errorf("webhook secret not configured")
	}

	secret, err := s.crypto.Decrypt(config.WebhookSecretEncrypted)
	if err != nil {
		return fmt.Errorf("failed to decrypt webhook secret: %w", err)
	}

	return verifyHMACSignature(secret, signature, body)
}

// verifyHMACSignature verifies an HMAC-SHA256 signature against a secret and body.
// The signature must be in the format "sha256=<hex-encoded-hmac>".
func verifyHMACSignature(secret []byte, signature string, body []byte) error {
	mac := hmac.New(sha256.New, secret)
	mac.Write(body)
	expectedMAC := mac.Sum(nil)
	expectedSignature := "sha256=" + hex.EncodeToString(expectedMAC)

	if !hmac.Equal([]byte(signature), []byte(expectedSignature)) {
		return fmt.Errorf("invalid signature")
	}

	return nil
}

// GetStatus returns the current GitHub App connection status.
func (s *Service) GetStatus(ctx context.Context) (*StatusResponse, error) {
	config, err := s.store.Get(ctx)
	if err != nil {
		return nil, err
	}

	if config == nil {
		return &StatusResponse{Connected: false}, nil
	}

	return &StatusResponse{
		Connected:       true,
		AppID:           config.AppID,
		AppSlug:         config.AppSlug,
		InstallationID:  config.InstallationID,
		InstallationOrg: config.InstallationOrg,
		ConnectedBy:     config.OwnerID,
		ConnectedAt:     config.CreatedAt.Format(time.RFC3339),
	}, nil
}

// GenerateManifestURL creates a GitHub App manifest and returns the redirect URL.
func (s *Service) GenerateManifestURL(callbackURL string) (string, error) {
	// Build webhook URL by replacing the last path segment with "webhook"
	hookURL, err := url.Parse(callbackURL)
	if err != nil {
		return "", fmt.Errorf("invalid callback URL: %w", err)
	}
	hookURL.Path = path.Join(path.Dir(hookURL.Path), "webhook")

	manifest := map[string]any{
		"name":         "Emergent",
		"url":          "https://emergent.sh",
		"redirect_url": callbackURL,
		"hook_attributes": map[string]any{
			"url":    hookURL.String(),
			"active": true,
		},
		"default_permissions": map[string]string{
			"contents": "write",
		},
		"default_events": []string{
			"installation",
		},
		"public": false,
	}

	manifestJSON, err := json.Marshal(manifest)
	if err != nil {
		return "", fmt.Errorf("failed to marshal manifest: %w", err)
	}

	manifestURL := fmt.Sprintf(
		"https://github.com/settings/apps/new?manifest=%s",
		url.QueryEscape(string(manifestJSON)),
	)

	return manifestURL, nil
}

// HandleCallback exchanges a temporary code for GitHub App credentials.
func (s *Service) HandleCallback(ctx context.Context, code string, ownerID string) error {
	// Exchange code for credentials via GitHub API
	apiURL := fmt.Sprintf("%s/app-manifests/%s/conversions", githubAPIBaseURL, code)

	req, err := http.NewRequestWithContext(ctx, "POST", apiURL, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("GitHub API request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read GitHub response body: %w", err)
	}

	if resp.StatusCode != http.StatusCreated {
		return fmt.Errorf("GitHub API returned %d: %s", resp.StatusCode, string(body))
	}

	var conversion ManifestConversionResponse
	if err := json.Unmarshal(body, &conversion); err != nil {
		return fmt.Errorf("failed to parse conversion response: %w", err)
	}

	// Encrypt credentials
	privateKeyEnc, err := s.crypto.EncryptString(conversion.PEM)
	if err != nil {
		return fmt.Errorf("failed to encrypt private key: %w", err)
	}

	var webhookSecretEnc []byte
	if conversion.WebhookSecret != "" {
		webhookSecretEnc, err = s.crypto.EncryptString(conversion.WebhookSecret)
		if err != nil {
			return fmt.Errorf("failed to encrypt webhook secret: %w", err)
		}
	}

	var clientSecretEnc []byte
	if conversion.ClientSecret != "" {
		clientSecretEnc, err = s.crypto.EncryptString(conversion.ClientSecret)
		if err != nil {
			return fmt.Errorf("failed to encrypt client secret: %w", err)
		}
	}

	// Delete any existing config first (singleton)
	if _, err := s.store.Delete(ctx); err != nil {
		s.log.Warn("failed to delete existing GitHub App config", "error", err)
	}

	// Store the new config
	config := &GitHubAppConfig{
		AppID:                  conversion.ID,
		AppSlug:                conversion.Slug,
		PrivateKeyEncrypted:    privateKeyEnc,
		WebhookSecretEncrypted: webhookSecretEnc,
		ClientID:               conversion.ClientID,
		ClientSecretEncrypted:  clientSecretEnc,
		OwnerID:                ownerID,
	}

	_, err = s.store.Create(ctx, config)
	if err != nil {
		return fmt.Errorf("failed to store GitHub App config: %w", err)
	}

	s.log.Info("GitHub App connected via manifest flow",
		"app_id", conversion.ID,
		"app_slug", conversion.Slug,
	)

	return nil
}

// Disconnect removes all GitHub App credentials.
func (s *Service) Disconnect(ctx context.Context) error {
	deleted, err := s.store.Delete(ctx)
	if err != nil {
		return err
	}
	if !deleted {
		return nil // Nothing to disconnect
	}
	s.log.Info("GitHub App disconnected")
	return nil
}

// HandleInstallation records a GitHub App installation from a webhook event.
func (s *Service) HandleInstallation(ctx context.Context, appID int64, installationID int64, org string) error {
	return s.store.UpdateInstallation(ctx, appID, installationID, org)
}

// CLISetup configures a GitHub App from CLI-provided credentials.
func (s *Service) CLISetup(ctx context.Context, req *CLISetupRequest, ownerID string) error {
	// Validate credentials by generating a test token
	privateKeyEnc, err := s.crypto.EncryptString(req.PrivateKeyPEM)
	if err != nil {
		return fmt.Errorf("failed to encrypt private key: %w", err)
	}

	// Build temporary config for validation
	installID := req.InstallationID
	testConfig := &GitHubAppConfig{
		AppID:               req.AppID,
		PrivateKeyEncrypted: privateKeyEnc,
		InstallationID:      &installID,
	}

	// Validate by generating a token
	_, err = s.tokenService.GetInstallationToken(testConfig)
	if err != nil {
		return fmt.Errorf("credential validation failed: %w", err)
	}

	// Delete any existing config first
	if _, err := s.store.Delete(ctx); err != nil {
		s.log.Warn("failed to delete existing GitHub App config", "error", err)
	}

	// Store the new config
	config := &GitHubAppConfig{
		AppID:               req.AppID,
		PrivateKeyEncrypted: privateKeyEnc,
		InstallationID:      &installID,
		OwnerID:             ownerID,
	}

	_, err = s.store.Create(ctx, config)
	if err != nil {
		return fmt.Errorf("failed to store GitHub App config: %w", err)
	}

	s.log.Info("GitHub App configured via CLI",
		"app_id", req.AppID,
		"installation_id", req.InstallationID,
	)

	return nil
}

// GetInstallationToken returns a valid installation access token.
// This is the primary entry point for other services (checkout, git push).
func (s *Service) GetInstallationToken(ctx context.Context) (string, error) {
	config, err := s.store.Get(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to get GitHub App config: %w", err)
	}
	if config == nil {
		return "", fmt.Errorf("GitHub App not configured — connect GitHub in Settings > Integrations")
	}

	return s.tokenService.GetInstallationToken(config)
}

// GetConfig returns the current GitHub App configuration (for bot identity etc.).
func (s *Service) GetConfig(ctx context.Context) (*GitHubAppConfig, error) {
	return s.store.Get(ctx)
}
