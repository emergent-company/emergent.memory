package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// ZitadelManagementClient calls the Zitadel Management API using a PAT.
// It is nil-safe: all methods are no-ops when the client is nil.
type ZitadelManagementClient struct {
	baseURL    string
	pat        string
	httpClient *http.Client
}

// ZitadelUserInfo holds the profile data returned from the Management API.
type ZitadelUserInfo struct {
	Email       string
	FirstName   string
	LastName    string
	DisplayName string
}

// NewZitadelManagementClient creates a client for the Zitadel Management API.
// Returns nil if pat is empty (management API disabled).
func NewZitadelManagementClient(domain, pat string) *ZitadelManagementClient {
	if pat == "" {
		return nil
	}
	scheme := "https"
	return &ZitadelManagementClient{
		baseURL: fmt.Sprintf("%s://%s", scheme, domain),
		pat:     pat,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// GetUser fetches a user's profile from Zitadel by their Zitadel user ID.
// Returns nil, nil if the client is not configured.
func (c *ZitadelManagementClient) GetUser(ctx context.Context, zitadelUserID string) (*ZitadelUserInfo, error) {
	if c == nil {
		return nil, nil
	}

	url := fmt.Sprintf("%s/management/v1/users/%s", c.baseURL, zitadelUserID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.pat)
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("management API request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("management API returned %d for user %s", resp.StatusCode, zitadelUserID)
	}

	var body struct {
		User struct {
			Human *struct {
				Profile struct {
					FirstName   string `json:"firstName"`
					LastName    string `json:"lastName"`
					DisplayName string `json:"displayName"`
				} `json:"profile"`
				Email struct {
					Email string `json:"email"`
				} `json:"email"`
			} `json:"human"`
		} `json:"user"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, fmt.Errorf("decode management API response: %w", err)
	}

	if body.User.Human == nil {
		// Machine user — no email/profile
		return nil, nil
	}

	return &ZitadelUserInfo{
		Email:       body.User.Human.Email.Email,
		FirstName:   body.User.Human.Profile.FirstName,
		LastName:    body.User.Human.Profile.LastName,
		DisplayName: body.User.Human.Profile.DisplayName,
	}, nil
}
