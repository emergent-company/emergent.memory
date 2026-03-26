package mcp

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/labstack/echo/v4"

	"github.com/emergent-company/emergent.memory/domain/email"
	"github.com/emergent-company/emergent.memory/pkg/apperror"
	"github.com/emergent-company/emergent.memory/pkg/auth"
)

// ShareMCPAccessRequest is the request body for POST /api/projects/:projectId/mcp/share.
type ShareMCPAccessRequest struct {
	// Name is an optional display name for the generated token.
	// Defaults to "MCP Read-Only Share — <YYYY-MM-DD>".
	Name string `json:"name"`

	// Emails is an optional list of email addresses to send the MCP invite to.
	Emails []string `json:"emails"`
}

// MCPSnippets contains pre-formatted agent config blocks.
type MCPSnippets struct {
	ClaudeDesktop string `json:"claudeDesktop"`
	ClaudeCode    string `json:"claudeCode"`
	Cursor        string `json:"cursor"`
	CloudCode     string `json:"cloudCode"`
	InstallURL    string `json:"installUrl"`
}

// ShareMCPAccessResponse is the response for POST /api/projects/:projectId/mcp/share.
type ShareMCPAccessResponse struct {
	// Token is the raw API token value — returned only once.
	Token string `json:"token"`

	// MCPURL is the fully-qualified MCP server endpoint URL.
	MCPURL string `json:"mcpUrl"`

	// ProjectID is the project this token is scoped to.
	ProjectID string `json:"projectId"`

	// Snippets contains ready-to-paste agent config blocks.
	Snippets MCPSnippets `json:"snippets"`
}

// readOnlyMCPScopes are the scopes granted to a read-only MCP share token.
var readOnlyMCPScopes = []string{"data:read", "schema:read", "agents:read", "projects:read", "chat:use"}

// emailRegexp is a simple RFC 5322-ish email validator.
var emailRegexp = regexp.MustCompile(`^[a-zA-Z0-9._%+\-]+@[a-zA-Z0-9.\-]+\.[a-zA-Z]{2,}$`)

// ShareMCPAccess creates a read-only MCP API token for a project and optionally
// sends invite emails to the provided addresses.
func (s *Service) ShareMCPAccess(ctx context.Context, projectID, userID, senderName, mcpBaseURL string, req ShareMCPAccessRequest) (*ShareMCPAccessResponse, error) {
	// Enforce project admin role.
	role, err := s.apitokenSvc.GetUserProjectRole(ctx, projectID, userID)
	if err != nil {
		return nil, fmt.Errorf("check project role: %w", err)
	}
	if role != "project_admin" {
		return nil, apperror.ErrForbidden.WithMessage("project admin role required to share MCP access")
	}

	mcpURL := mcpBaseURL + "/api/mcp"
	timestamp := time.Now().UTC().Format("2006-01-02 15:04:05")

	// When emails are provided, create one token per recipient so each token name
	// clearly identifies its purpose and recipient (e.g. "MCP Share → alice@example.com — 2026-01-02 15:04:05").
	// This makes it trivial to find and revoke a specific recipient's access later.
	if len(req.Emails) > 0 {
		// Fetch project name once for all invite emails.
		var projectName string
		_ = s.db.NewSelect().
			TableExpr("kb.projects").
			ColumnExpr("name").
			Where("id = ?", projectID).
			Scan(ctx, &projectName)

		// Issue one token per recipient.
		var firstToken string
		var firstSnippets MCPSnippets
		for i, addr := range req.Emails {
			tokenName := req.Name
			if tokenName == "" {
				tokenName = fmt.Sprintf("MCP Share → %s — %s", addr, timestamp)
			}

			tokenResp, err := s.apitokenSvc.Create(ctx, projectID, userID, tokenName, readOnlyMCPScopes)
			if err != nil {
				return nil, fmt.Errorf("create mcp share token for %s: %w", addr, err)
			}

			snippets := buildSnippets(mcpBaseURL, mcpURL, tokenResp.Token)
			if i == 0 {
				firstToken = tokenResp.Token
				firstSnippets = snippets
			}

			if s.emailSvc != nil {
				bundleURL := fmt.Sprintf("%s/api/mcp/bundle?token=%s", mcpBaseURL, tokenResp.Token)
				enqErr := s.enqueueMCPInviteEmail(ctx, addr, senderName, projectID, projectName, mcpURL, tokenResp.Token, bundleURL, snippets)
				if enqErr != nil {
					s.log.Warn("failed to enqueue mcp invite email",
						"email", addr,
						"error", enqErr)
				}
			}
		}

		return &ShareMCPAccessResponse{
			Token:     firstToken,
			MCPURL:    mcpURL,
			ProjectID: projectID,
			Snippets:  firstSnippets,
		}, nil
	}

	// No emails — create a single token with the provided name or a generic default.
	name := req.Name
	if name == "" {
		name = fmt.Sprintf("MCP Read-Only Share — %s", timestamp)
	}

	tokenResp, err := s.apitokenSvc.Create(ctx, projectID, userID, name, readOnlyMCPScopes)
	if err != nil {
		return nil, fmt.Errorf("create mcp share token: %w", err)
	}

	snippets := buildSnippets(mcpBaseURL, mcpURL, tokenResp.Token)

	return &ShareMCPAccessResponse{
		Token:     tokenResp.Token,
		MCPURL:    mcpURL,
		ProjectID: projectID,
		Snippets:  snippets,
	}, nil
}

// buildSnippets constructs ready-to-paste config blocks for supported MCP clients.
func buildSnippets(mcpBaseURL, mcpURL, apiKey string) MCPSnippets {
	claudeDesktop := fmt.Sprintf(`{
  "mcpServers": {
    "memory": {
      "url": %q,
      "headers": {
        "X-API-Key": %q
      }
    }
  }
}`, mcpURL, apiKey)

	cursor := fmt.Sprintf(`{
  "mcpServers": {
    "memory": {
      "url": %q,
      "headers": {
        "X-API-Key": %q
      }
    }
  }
}`, mcpURL, apiKey)

	// Claude Code (CLI) — .mcp.json in project root (--scope project)
	// Uses "type": "http" with headers for remote servers.
	claudeCode := fmt.Sprintf(`{
  "mcpServers": {
    "memory": {
      "type": "http",
      "url": %q,
      "headers": {
        "X-API-Key": %q
      }
    }
  }
}`, mcpURL, apiKey)

	// Cloud Code / Gemini Code Assist — .gemini/settings.json
	// Uses "httpUrl" key (not "url") and supports headers for auth.
	cloudCode := fmt.Sprintf(`{
  "mcpServers": {
    "memory": {
      "httpUrl": %q,
      "headers": {
        "X-API-Key": %q
      }
    }
  }
}`, mcpURL, apiKey)

	// Use an HTTPS redirect through our server so email clients don't block the link.
	// /api/mcp/install?token=<token>&url=<mcpURL>&name=memory → 302 → claude://install-mcp?…
	installURL := fmt.Sprintf("%s/api/mcp/install?token=%s&url=%s&name=memory", mcpBaseURL, url.QueryEscape(apiKey), url.QueryEscape(mcpURL))

	return MCPSnippets{
		ClaudeDesktop: claudeDesktop,
		ClaudeCode:    claudeCode,
		Cursor:        cursor,
		CloudCode:     cloudCode,
		InstallURL:    installURL,
	}
}

// HandleInstallRedirect handles GET /api/mcp/install
// Redirects to the claude://install-mcp deep link so email clients (which block
// custom protocol hrefs) can follow an https:// link that bounces to Claude Desktop.
//
// When a token is provided, it builds a full npx mcp-remote invocation with the
// API key in --header args so Claude Desktop installs an authenticated stdio server
// (preventing the OAuth discovery crash that occurs when no auth is present).
//
// @Summary      Claude Desktop install redirect
// @Description  Redirects to the claude://install-mcp deep link for one-click Claude Desktop MCP server installation. No authentication required.
// @Tags         mcp
// @Param        url    query string true  "MCP server URL"
// @Param        name   query string false "Server name (default: memory)"
// @Param        token  query string false "API key — when present, installs via npx mcp-remote with auth header"
// @Success      302   "Redirect to claude://install-mcp deep link"
// @Failure      400   {object} apperror.Error "Missing url parameter"
// @Router       /api/mcp/install [get]
func (h *Handler) HandleInstallRedirect(c echo.Context) error {
	mcpURL := c.QueryParam("url")
	if mcpURL == "" {
		return apperror.New(http.StatusBadRequest, "missing_param", "url parameter is required")
	}
	name := c.QueryParam("name")
	if name == "" {
		name = "memory"
	}
	token := c.QueryParam("token")

	var claudeURL string
	if token != "" {
		// Build a full npx mcp-remote deep link with the API key in --header args.
		// This installs an authenticated stdio server so the proxy never hits the
		// OAuth discovery path (which crashes when our server has no /register route).
		params := url.Values{}
		params.Set("name", name)
		params.Set("command", "npx")
		params.Add("args", "-y")
		params.Add("args", "mcp-remote")
		params.Add("args", mcpURL)
		params.Add("args", "--header")
		params.Add("args", fmt.Sprintf("Authorization: Bearer %s", token))
		params.Add("args", "--transport")
		params.Add("args", "http-first")
		claudeURL = "claude://install-mcp?" + params.Encode()
	} else {
		// Fallback: no token — old behaviour (url+name only).
		claudeURL = fmt.Sprintf("claude://install-mcp?url=%s&name=%s", mcpURL, name)
	}
	return c.Redirect(http.StatusFound, claudeURL)
}

// enqueueMCPInviteEmail enqueues a single MCP invite email.
func (s *Service) enqueueMCPInviteEmail(ctx context.Context, toEmail, senderName, projectID, projectName, mcpURL, apiKey, bundleURL string, snippets MCPSnippets) error {
	displayName := projectName
	if displayName == "" {
		displayName = projectID
	}
	subject := fmt.Sprintf("%s has shared Memory project access with you", senderName)

	_, err := s.emailSvc.Enqueue(ctx, email.EnqueueOptions{
		TemplateName: "mcp-invite",
		ToEmail:      toEmail,
		Subject:      subject,
		TemplateData: map[string]interface{}{
			"senderName":  senderName,
			"projectId":   projectID,
			"projectName": displayName,
			"mcpUrl":      mcpURL,
			"apiKey":      apiKey,
			"installUrl":  snippets.InstallURL,
			"bundleUrl":   bundleURL,
			"snippets": map[string]interface{}{
				"claudeDesktop": snippets.ClaudeDesktop,
				"claudeCode":    snippets.ClaudeCode,
				"cursor":        snippets.Cursor,
				"cloudCode":     snippets.CloudCode,
			},
		},
	})
	return err
}

// HandleShareMCPAccess handles POST /api/projects/:projectId/mcp/share.
//
// @Summary      Share read-only MCP access
// @Description  Generates a read-only MCP API token for a project and optionally sends invite emails with agent setup instructions.
// @Tags         mcp
// @Accept       json
// @Produce      json
// @Param        projectId path string true "Project ID (UUID)"
// @Param        request body ShareMCPAccessRequest false "Share request (name and optional email list)"
// @Success      201 {object} ShareMCPAccessResponse "Token and agent config snippets"
// @Failure      400 {object} apperror.Error "Invalid request"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Failure      403 {object} apperror.Error "Forbidden — project admin required"
// @Failure      422 {object} apperror.Error "Invalid email address"
// @Router       /api/projects/{projectId}/mcp/share [post]
// @Security     bearerAuth
func (h *Handler) HandleShareMCPAccess(c echo.Context) error {
	user := auth.GetUser(c)
	if user == nil {
		return apperror.ErrUnauthorized
	}

	projectID := c.Param("projectId")
	if projectID == "" {
		return apperror.ErrBadRequest.WithMessage("projectId is required")
	}

	var req ShareMCPAccessRequest
	if err := c.Bind(&req); err != nil {
		return apperror.ErrBadRequest.WithMessage("invalid request body")
	}

	// Validate email addresses before creating the token.
	for _, email := range req.Emails {
		if !emailRegexp.MatchString(email) {
			return apperror.New(http.StatusUnprocessableEntity, "invalid_email",
				fmt.Sprintf("invalid email address: %s", email))
		}
	}

	// Derive the public base URL from the incoming request.
	scheme := "https"
	if c.Request().TLS == nil && c.Request().Header.Get("X-Forwarded-Proto") == "" {
		scheme = "http"
	}
	if proto := c.Request().Header.Get("X-Forwarded-Proto"); proto != "" {
		scheme = proto
	}
	mcpBaseURL := fmt.Sprintf("%s://%s", scheme, c.Request().Host)

	senderName := user.Email
	if profile, err := h.userProfileSvc.GetByID(c.Request().Context(), user.ID); err == nil {
		if profile.DisplayName != nil && *profile.DisplayName != "" {
			senderName = *profile.DisplayName
		} else if profile.FirstName != nil && *profile.FirstName != "" {
			senderName = strings.TrimSpace(*profile.FirstName + " " + func() string {
				if profile.LastName != nil {
					return *profile.LastName
				}
				return ""
			}())
		}
	}
	if senderName == "" {
		senderName = "A team member"
	}

	resp, err := h.svc.ShareMCPAccess(c.Request().Context(), projectID, user.ID, senderName, mcpBaseURL, req)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusCreated, resp)
}
