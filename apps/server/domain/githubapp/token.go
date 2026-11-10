package githubapp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	defaultGitHubAPIBase = "https://api.github.com"
	tokenRefreshSkew     = 5 * time.Minute
	maxTokenResponse     = 1 << 20
)

type cachedToken struct {
	token     string
	expiresAt time.Time
}

// TokenMinter mints and caches short-lived GitHub App installation tokens. It
// never logs or returns the App private key.
type TokenMinter struct {
	baseURL string
	client  *http.Client
	now     func() time.Time

	mu    sync.Mutex
	cache map[string]cachedToken
	// locks serializes minting per cache key so concurrent misses do not mint
	// several tokens for the same scope.
	locks map[string]*sync.Mutex
}

// NewTokenMinter builds a minter. Empty baseURL means github.com; a nil client
// gets a 15 s timeout.
func NewTokenMinter(baseURL string, client *http.Client) *TokenMinter {
	if strings.TrimSpace(baseURL) == "" {
		baseURL = defaultGitHubAPIBase
	}
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	return &TokenMinter{
		baseURL: strings.TrimRight(baseURL, "/"),
		client:  client,
		now:     time.Now,
		cache:   make(map[string]cachedToken),
		locks:   make(map[string]*sync.Mutex),
	}
}

// MintInstallationToken returns a token scoped to repos and permissions,
// reusing a cached token until shortly before expiry.
func (m *TokenMinter) MintInstallationToken(
	ctx context.Context,
	appID int64,
	privateKeyPEM []byte,
	installationID int64,
	repos []string,
	permissions map[string]string,
) (string, error) {
	// Least privilege is mandatory: an empty scope would mint a token for every
	// repository and every App permission.
	if len(repos) == 0 {
		return "", errors.New("githubapp: installation token requires an explicit repository list")
	}
	if len(permissions) == 0 {
		return "", errors.New("githubapp: installation token requires an explicit permission set")
	}

	key := tokenCacheKey(appID, installationID, repos, permissions)

	m.mu.Lock()
	if entry, ok := m.cache[key]; ok && entry.expiresAt.Sub(m.now()) > tokenRefreshSkew {
		m.mu.Unlock()
		return entry.token, nil
	}
	lock := m.locks[key]
	if lock == nil {
		lock = new(sync.Mutex)
		m.locks[key] = lock
	}
	m.mu.Unlock()

	lock.Lock()
	defer lock.Unlock()

	// Another caller may have minted while this one waited.
	m.mu.Lock()
	if entry, ok := m.cache[key]; ok && entry.expiresAt.Sub(m.now()) > tokenRefreshSkew {
		m.mu.Unlock()
		return entry.token, nil
	}
	m.mu.Unlock()

	jwt, err := SignAppJWT(appID, privateKeyPEM, m.now())
	if err != nil {
		return "", err
	}

	payload := map[string]any{}
	if len(repos) > 0 {
		payload["repositories"] = repos
	}
	if len(permissions) > 0 {
		payload["permissions"] = permissions
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("githubapp: marshal token request: %w", err)
	}

	url := fmt.Sprintf("%s/app/installations/%d/access_tokens", m.baseURL, installationID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("githubapp: build token request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+jwt)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("Content-Type", "application/json")

	resp, err := m.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("githubapp: mint installation token: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxTokenResponse))
	if err != nil {
		return "", fmt.Errorf("githubapp: read token response: %w", err)
	}
	if resp.StatusCode != http.StatusCreated {
		// Never echo the body: a proxy or GHES misconfiguration could return a
		// 200/201-shaped token body under a different status. Surface GitHub's
		// message field only.
		var apiErr struct {
			Message string `json:"message"`
		}
		_ = json.Unmarshal(raw, &apiErr)
		return "", fmt.Errorf("githubapp: mint installation token: status %d: %s", resp.StatusCode, truncate([]byte(apiErr.Message), 300))
	}

	var out struct {
		Token     string    `json:"token"`
		ExpiresAt time.Time `json:"expires_at"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return "", fmt.Errorf("githubapp: decode token response: %w", err)
	}
	if out.Token == "" {
		return "", fmt.Errorf("githubapp: token response had no token")
	}

	m.mu.Lock()
	m.cache[key] = cachedToken{token: out.Token, expiresAt: out.ExpiresAt}
	m.mu.Unlock()
	return out.Token, nil
}

// Invalidate drops cached tokens for an App installation.
func (m *TokenMinter) Invalidate(appID, installationID int64) {
	prefix := fmt.Sprintf("%d|%d|", appID, installationID)
	m.mu.Lock()
	defer m.mu.Unlock()
	for key := range m.cache {
		if strings.HasPrefix(key, prefix) {
			delete(m.cache, key)
		}
	}
}

func tokenCacheKey(appID, installationID int64, repos []string, permissions map[string]string) string {
	sortedRepos := append([]string(nil), repos...)
	sort.Strings(sortedRepos)

	permKeys := make([]string, 0, len(permissions))
	for k := range permissions {
		permKeys = append(permKeys, k)
	}
	sort.Strings(permKeys)
	permParts := make([]string, 0, len(permKeys))
	for _, k := range permKeys {
		permParts = append(permParts, k+":"+permissions[k])
	}
	return fmt.Sprintf("%d|%d|%s|%s", appID, installationID, strings.Join(sortedRepos, ","), strings.Join(permParts, ","))
}

func truncate(b []byte, n int) string {
	if len(b) <= n {
		return string(b)
	}
	return string(b[:n]) + "…"
}
