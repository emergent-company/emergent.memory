// Package invitations provides the project invitations client for the Emergent API SDK.
package invitations

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/auth"
	sdkerrors "github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/errors"
)

// Client provides access to the project invitations API.
type Client struct {
	http *http.Client
	base string
	auth auth.Provider
}

// NewClient creates a new invitations client.
func NewClient(httpClient *http.Client, baseURL string, authProvider auth.Provider) *Client {
	return &Client{
		http: httpClient,
		base: baseURL,
		auth: authProvider,
	}
}

// Invite represents a project invitation
type Invite struct {
	ID        string     `json:"id"`
	ProjectID *string    `json:"projectId,omitempty"`
	Email     string     `json:"email"`
	Role      string     `json:"role"`
	Status    string     `json:"status"`
	Token     string     `json:"token,omitempty"`
	ExpiresAt *time.Time `json:"expiresAt,omitempty"`
	CreatedAt time.Time  `json:"createdAt"`
}

// SentInvite represents a summary of a sent invitation (for project admin listing)
type SentInvite struct {
	ID               string     `json:"id"`
	Email            string     `json:"email"`
	Role             string     `json:"role"`
	Status           string     `json:"status"`
	CreatedAt        time.Time  `json:"createdAt"`
	ExpiresAt        *time.Time `json:"expiresAt,omitempty"`
	DeliveryStatus   *string    `json:"deliveryStatus,omitempty"`
	DeliveryStatusAt *time.Time `json:"deliveryStatusAt,omitempty"`
	// DeliveryLog is the invitation's email send history (initial send plus
	// resends), newest first.
	DeliveryLog []InviteDeliveryEvent `json:"deliveryLog"`
}

// InviteDeliveryEvent is one email send in an invitation's delivery log: an
// invite-scoped email job with its current state and Mailgun delivery events.
type InviteDeliveryEvent struct {
	JobID            string                   `json:"jobId"`
	CreatedAt        time.Time                `json:"createdAt"`
	Status           string                   `json:"status"`
	DeliveryStatus   *string                  `json:"deliveryStatus,omitempty"`
	DeliveryStatusAt *time.Time               `json:"deliveryStatusAt,omitempty"`
	LastError        *string                  `json:"lastError,omitempty"`
	Events           []InviteDeliveryLogEvent `json:"events"`
}

// InviteDeliveryLogEvent is one Mailgun delivery event recorded for an
// invitation email send (delivered, opened, bounced, complained, failed, …).
type InviteDeliveryLogEvent struct {
	Type      string    `json:"type"`
	Detail    string    `json:"detail,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
}

// PendingInvite represents an invitation received by the current user
type PendingInvite struct {
	ID               string     `json:"id"`
	ProjectID        *string    `json:"projectId,omitempty"`
	ProjectName      *string    `json:"projectName,omitempty"`
	OrganizationID   string     `json:"organizationId"`
	OrganizationName *string    `json:"organizationName,omitempty"`
	Role             string     `json:"role"`
	Token            string     `json:"token"`
	CreatedAt        time.Time  `json:"createdAt"`
	ExpiresAt        *time.Time `json:"expiresAt,omitempty"`
}

// CreateRequest is the request to create a new invitation
type CreateRequest struct {
	OrgID       string `json:"orgId"`
	ProjectID   string `json:"projectId"`
	Email       string `json:"email"`
	Role        string `json:"role"`
	ProjectName string `json:"projectName,omitempty"`
}

// Create sends a project invitation to the given email address.
func (c *Client) Create(ctx context.Context, req *CreateRequest) (*Invite, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", c.base+"/api/invites", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	if err := c.auth.Authenticate(httpReq); err != nil {
		return nil, fmt.Errorf("authentication failed: %w", err)
	}

	resp, err := c.http.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return nil, sdkerrors.ParseErrorResponse(resp)
	}

	var invite Invite
	if err := json.NewDecoder(resp.Body).Decode(&invite); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &invite, nil
}

// ListByProject returns all invitations for a project.
func (c *Client) ListByProject(ctx context.Context, projectID string) ([]SentInvite, error) {
	req, err := http.NewRequestWithContext(ctx, "GET",
		c.base+"/api/projects/"+url.PathEscape(projectID)+"/invites", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	if err := c.auth.Authenticate(req); err != nil {
		return nil, fmt.Errorf("authentication failed: %w", err)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return nil, sdkerrors.ParseErrorResponse(resp)
	}

	var invites []SentInvite
	if err := json.NewDecoder(resp.Body).Decode(&invites); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return invites, nil
}

// Revoke cancels a pending invitation by ID.
func (c *Client) Revoke(ctx context.Context, inviteID string) error {
	req, err := http.NewRequestWithContext(ctx, "DELETE",
		c.base+"/api/invites/"+url.PathEscape(inviteID), nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	if err := c.auth.Authenticate(req); err != nil {
		return fmt.Errorf("authentication failed: %w", err)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return sdkerrors.ParseErrorResponse(resp)
	}

	_, _ = io.Copy(io.Discard, resp.Body)
	return nil
}

// Resend re-sends a pending invitation email by ID and returns the updated
// invitation.
func (c *Client) Resend(ctx context.Context, inviteID string) (*Invite, error) {
	req, err := http.NewRequestWithContext(ctx, "POST",
		c.base+"/api/invites/"+url.PathEscape(inviteID)+"/resend", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	if err := c.auth.Authenticate(req); err != nil {
		return nil, fmt.Errorf("authentication failed: %w", err)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return nil, sdkerrors.ParseErrorResponse(resp)
	}

	var invite Invite
	if err := json.NewDecoder(resp.Body).Decode(&invite); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &invite, nil
}

// ListPending returns all pending invitations for the current user.
func (c *Client) ListPending(ctx context.Context) ([]PendingInvite, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", c.base+"/api/invites/pending", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	if err := c.auth.Authenticate(req); err != nil {
		return nil, fmt.Errorf("authentication failed: %w", err)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return nil, sdkerrors.ParseErrorResponse(resp)
	}

	var invites []PendingInvite
	if err := json.NewDecoder(resp.Body).Decode(&invites); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return invites, nil
}

// Accept accepts a pending invitation by token.
func (c *Client) Accept(ctx context.Context, token string) error {
	reqBody := struct {
		Token string `json:"token"`
	}{Token: token}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", c.base+"/api/invites/accept", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	if err := c.auth.Authenticate(req); err != nil {
		return fmt.Errorf("authentication failed: %w", err)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return sdkerrors.ParseErrorResponse(resp)
	}

	_, _ = io.Copy(io.Discard, resp.Body)
	return nil
}
