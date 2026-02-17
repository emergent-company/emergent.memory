package githubapp

import (
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const (
	// tokenCacheDuration is how long to cache installation tokens (55 min, 5 min safety margin before 1h expiry).
	tokenCacheDuration = 55 * time.Minute

	// githubAPIBaseURL is the base URL for GitHub API calls.
	githubAPIBaseURL = "https://api.github.com"
)

// cachedToken holds an in-memory cached installation access token.
type cachedToken struct {
	token     string
	expiresAt time.Time
}

// TokenService generates and caches GitHub App installation access tokens.
type TokenService struct {
	store  *Store
	crypto *Crypto
	log    *slog.Logger

	mu    sync.RWMutex
	cache map[int64]*cachedToken // installationID -> cached token

	// httpClient is used for GitHub API calls (injectable for testing).
	httpClient *http.Client
}

// NewTokenService creates a new token service.
func NewTokenService(store *Store, crypto *Crypto, log *slog.Logger) *TokenService {
	return &TokenService{
		store:      store,
		crypto:     crypto,
		log:        log.With("component", "github-token-service"),
		cache:      make(map[int64]*cachedToken),
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}
}

// GetInstallationToken returns a valid installation access token, using cache when possible.
// Returns the token string and any error.
func (ts *TokenService) GetInstallationToken(config *GitHubAppConfig) (string, error) {
	if config.InstallationID == nil || *config.InstallationID == 0 {
		return "", fmt.Errorf("GitHub App not installed — connect GitHub in Settings > Integrations")
	}

	installID := *config.InstallationID

	// Check cache
	ts.mu.RLock()
	if cached, ok := ts.cache[installID]; ok {
		if time.Now().Before(cached.expiresAt) {
			ts.mu.RUnlock()
			return cached.token, nil
		}
	}
	ts.mu.RUnlock()

	// Generate fresh token
	token, err := ts.generateInstallationToken(config)
	if err != nil {
		return "", err
	}

	// Cache it
	ts.mu.Lock()
	ts.cache[installID] = &cachedToken{
		token:     token,
		expiresAt: time.Now().Add(tokenCacheDuration),
	}
	ts.mu.Unlock()

	ts.log.Info("generated new installation access token",
		"installation_id", installID,
		"app_id", config.AppID,
	)

	return token, nil
}

// InvalidateCache removes cached tokens for a given installation.
func (ts *TokenService) InvalidateCache(installationID int64) {
	ts.mu.Lock()
	delete(ts.cache, installationID)
	ts.mu.Unlock()
}

// generateInstallationToken creates a new installation access token via GitHub API.
// Steps: 1) Decrypt PEM, 2) Sign JWT, 3) Exchange JWT for installation token.
func (ts *TokenService) generateInstallationToken(config *GitHubAppConfig) (string, error) {
	// Decrypt PEM
	pemData, err := ts.crypto.Decrypt(config.PrivateKeyEncrypted)
	if err != nil {
		return "", fmt.Errorf("failed to decrypt private key: %w", err)
	}

	// Sign JWT
	jwtToken, err := ts.signJWT(config.AppID, pemData)
	if err != nil {
		return "", fmt.Errorf("failed to sign JWT: %w", err)
	}

	// Exchange JWT for installation access token
	installToken, err := ts.exchangeForInstallationToken(jwtToken, *config.InstallationID)
	if err != nil {
		return "", fmt.Errorf("failed to exchange JWT for installation token: %w", err)
	}

	return installToken, nil
}

// signJWT creates a signed JWT for GitHub App authentication.
// The JWT is valid for 10 minutes (GitHub's maximum).
func (ts *TokenService) signJWT(appID int64, pemData []byte) (string, error) {
	block, _ := pem.Decode(pemData)
	if block == nil {
		return "", fmt.Errorf("failed to decode PEM block")
	}

	key, err := x509.ParsePKCS1PrivateKey(block.Bytes)
	if err != nil {
		// Try PKCS8 format
		pkcs8Key, err2 := x509.ParsePKCS8PrivateKey(block.Bytes)
		if err2 != nil {
			return "", fmt.Errorf("failed to parse private key (PKCS1: %v, PKCS8: %v)", err, err2)
		}
		var ok bool
		key, ok = pkcs8Key.(*rsa.PrivateKey)
		if !ok {
			return "", fmt.Errorf("PKCS8 key is not RSA")
		}
	}

	now := time.Now()
	claims := jwt.RegisteredClaims{
		IssuedAt:  jwt.NewNumericDate(now.Add(-60 * time.Second)), // 60 second clock drift allowance
		ExpiresAt: jwt.NewNumericDate(now.Add(10 * time.Minute)),  // Max 10 min for GitHub
		Issuer:    fmt.Sprintf("%d", appID),
	}

	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	signedToken, err := token.SignedString(key)
	if err != nil {
		return "", fmt.Errorf("failed to sign JWT: %w", err)
	}

	return signedToken, nil
}

// exchangeForInstallationToken exchanges a JWT for an installation access token via GitHub API.
func (ts *TokenService) exchangeForInstallationToken(jwtToken string, installationID int64) (string, error) {
	url := fmt.Sprintf("%s/app/installations/%d/access_tokens", githubAPIBaseURL, installationID)

	req, err := http.NewRequest("POST", url, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+jwtToken)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	resp, err := ts.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("GitHub API request failed: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusCreated {
		return "", fmt.Errorf("GitHub API returned %d: %s", resp.StatusCode, string(body))
	}

	var tokenResp InstallationTokenResponse
	if err := json.Unmarshal(body, &tokenResp); err != nil {
		return "", fmt.Errorf("failed to parse token response: %w", err)
	}

	return tokenResp.Token, nil
}

// BotCommitIdentity returns the git user.name and user.email for the GitHub App bot.
func BotCommitIdentity(appID int64) (name string, email string) {
	name = "emergent-app[bot]"
	email = fmt.Sprintf("%d+emergent-app[bot]@users.noreply.github.com", appID)
	return
}

// DefaultCommitIdentity returns the default git identity when no GitHub App is configured.
func DefaultCommitIdentity() (name string, email string) {
	return "Emergent Agent", "agent@emergent.local"
}
