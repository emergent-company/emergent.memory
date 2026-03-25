package mcp

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/labstack/echo/v4"

	"github.com/emergent-company/emergent.memory/pkg/apperror"
	"github.com/emergent-company/emergent.memory/pkg/auth"
)

//go:embed mcpremote/proxy-bundle.js
var mcpRemoteFS embed.FS

// mcpbManifest is the JSON structure for a .mcpb bundle manifest.
// This format is read by Claude Desktop's MCP installer.
type mcpbManifest struct {
	ManifestVersion string     `json:"manifest_version"`
	Name            string     `json:"name"`
	DisplayName     string     `json:"display_name"`
	Version         string     `json:"version"`
	Description     string     `json:"description"`
	Author          mcpbAuthor `json:"author"`
	Homepage        string     `json:"homepage"`
	Server          mcpbServer `json:"server"`
	Tools           []any      `json:"tools"`
	ToolsGenerated  bool       `json:"tools_generated"`
	Keywords        []string   `json:"keywords"`
	Compatibility   mcpbCompat `json:"compatibility"`
}

type mcpbAuthor struct {
	Name string `json:"name"`
	URL  string `json:"url"`
}

type mcpbServer struct {
	Type       string        `json:"type"`
	EntryPoint string        `json:"entry_point"`
	MCPConfig  mcpbMCPConfig `json:"mcp_config"`
}

type mcpbMCPConfig struct {
	Command string   `json:"command"`
	Args    []string `json:"args"`
}

type mcpbCompat struct {
	Platforms []string `json:"platforms"`
}

// slugRegexp removes characters that are not alphanumeric, hyphens, or underscores.
var slugRegexp = regexp.MustCompile(`[^a-z0-9\-_]+`)

// projectSlug converts a project name to a safe filename slug.
func projectSlug(name string) string {
	s := strings.ToLower(name)
	s = slugRegexp.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	if s == "" {
		s = "memory-project"
	}
	return s
}

// GenerateMCPBundle generates a .mcpb ZIP archive for a project.
// The bundle contains a manifest.json with the project name and API key baked in,
// plus the mcp-remote proxy script for Claude Desktop.
// The caller must have project_admin role.
func (s *Service) GenerateMCPBundle(ctx context.Context, projectID, userID, mcpBaseURL string) ([]byte, string, error) {
	// Enforce project admin role.
	role, err := s.apitokenSvc.GetUserProjectRole(ctx, projectID, userID)
	if err != nil {
		return nil, "", fmt.Errorf("check project role: %w", err)
	}
	if role != "project_admin" {
		return nil, "", apperror.ErrForbidden.WithMessage("project admin role required to generate MCP bundle")
	}

	// Look up project name.
	var projectName string
	_ = s.db.NewSelect().
		TableExpr("kb.projects").
		ColumnExpr("name").
		Where("id = ?", projectID).
		Scan(ctx, &projectName)
	if projectName == "" {
		projectName = "Memory Project"
	}

	// Create a read-only MCP share token for this bundle.
	tokenName := fmt.Sprintf("Claude Desktop Bundle — %s", time.Now().UTC().Format("2006-01-02 15:04:05"))
	tokenResp, err := s.apitokenSvc.Create(ctx, projectID, userID, tokenName, readOnlyMCPScopes)
	if err != nil {
		return nil, "", fmt.Errorf("create bundle token: %w", err)
	}

	mcpURL := mcpBaseURL + "/api/mcp"
	slug := projectSlug(projectName)

	// Build the manifest with all config baked in (no user_config fields).
	manifest := mcpbManifest{
		ManifestVersion: "0.3",
		Name:            slug,
		DisplayName:     projectName,
		Version:         "1.0.0",
		Description:     fmt.Sprintf("Access the %s knowledge graph from Claude Desktop. Search entities, explore relationships, and query your knowledge base.", projectName),
		Author: mcpbAuthor{
			Name: "Emergent Company",
			URL:  "https://emergent.memory",
		},
		Homepage: "https://emergent.memory",
		Server: mcpbServer{
			Type:       "node",
			EntryPoint: "mcp-remote/proxy-bundle.js",
			MCPConfig: mcpbMCPConfig{
				Command: "node",
				Args: []string{
					"${__dirname}/mcp-remote/proxy-bundle.js",
					mcpURL,
					"--header",
					fmt.Sprintf("Authorization: Bearer %s", tokenResp.Token),
					"--transport",
					"http-first",
				},
			},
		},
		Tools:          []any{},
		ToolsGenerated: true,
		Keywords:       []string{"memory", "knowledge-graph", "mcp", projectSlug(projectName)},
		Compatibility: mcpbCompat{
			Platforms: []string{"darwin", "win32", "linux"},
		},
	}

	manifestBytes, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return nil, "", fmt.Errorf("marshal manifest: %w", err)
	}

	// Build the ZIP archive in memory.
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)

	// Add manifest.json
	if err := addZipFile(zw, "manifest.json", manifestBytes); err != nil {
		return nil, "", fmt.Errorf("add manifest: %w", err)
	}

	// Add the self-contained mcp-remote bundle (all deps inlined, CJS-compatible).
	proxyBundle, err := mcpRemoteFS.ReadFile("mcpremote/proxy-bundle.js")
	if err != nil {
		return nil, "", fmt.Errorf("read proxy-bundle.js: %w", err)
	}
	if err := addZipFile(zw, "mcp-remote/proxy-bundle.js", proxyBundle); err != nil {
		return nil, "", fmt.Errorf("add proxy-bundle.js: %w", err)
	}

	if err := zw.Close(); err != nil {
		return nil, "", fmt.Errorf("close zip: %w", err)
	}

	filename := fmt.Sprintf("%s.mcpb", slug)
	return buf.Bytes(), filename, nil
}

// GenerateMCPBundleFromToken generates a .mcpb bundle using an existing API token.
// The token is validated and used directly — no new token is created.
// This is used by the public email-link download endpoint.
func (s *Service) GenerateMCPBundleFromToken(ctx context.Context, apiToken, mcpBaseURL string) ([]byte, string, error) {
	// Validate the token via SHA-256 hash lookup directly in the DB.
	hash := sha256.Sum256([]byte(apiToken))
	tokenHash := hex.EncodeToString(hash[:])

	var result struct {
		ProjectID string `bun:"project_id"`
	}
	err := s.db.NewSelect().
		TableExpr("core.api_tokens").
		ColumnExpr("project_id").
		Where("token_hash = ?", tokenHash).
		Where("revoked_at IS NULL").
		Where("(expires_at IS NULL OR expires_at > NOW())").
		Scan(ctx, &result)
	if err != nil || result.ProjectID == "" {
		return nil, "", apperror.ErrUnauthorized.WithMessage("invalid or expired token")
	}

	// Look up project name.
	var projectName string
	_ = s.db.NewSelect().
		TableExpr("kb.projects").
		ColumnExpr("name").
		Where("id = ?", result.ProjectID).
		Scan(ctx, &projectName)
	if projectName == "" {
		projectName = "Memory Project"
	}

	mcpURL := mcpBaseURL + "/api/mcp"
	slug := projectSlug(projectName)

	return buildMCPBundleZIP(projectName, slug, mcpURL, apiToken)
}

// buildMCPBundleZIP constructs the ZIP archive bytes for a .mcpb bundle.
func buildMCPBundleZIP(projectName, slug, mcpURL, apiToken string) ([]byte, string, error) {
	manifest := mcpbManifest{
		ManifestVersion: "0.3",
		Name:            slug,
		DisplayName:     projectName,
		Version:         "1.0.0",
		Description:     fmt.Sprintf("Access the %s knowledge graph from Claude Desktop. Search entities, explore relationships, and query your knowledge base.", projectName),
		Author: mcpbAuthor{
			Name: "Emergent Company",
			URL:  "https://emergent.memory",
		},
		Homepage: "https://emergent.memory",
		Server: mcpbServer{
			Type:       "node",
			EntryPoint: "mcp-remote/proxy-bundle.js",
			MCPConfig: mcpbMCPConfig{
				Command: "node",
				Args: []string{
					"${__dirname}/mcp-remote/proxy-bundle.js",
					mcpURL,
					"--header",
					fmt.Sprintf("Authorization: Bearer %s", apiToken),
					"--transport",
					"http-first",
				},
			},
		},
		Tools:          []any{},
		ToolsGenerated: true,
		Keywords:       []string{"memory", "knowledge-graph", "mcp", slug},
		Compatibility: mcpbCompat{
			Platforms: []string{"darwin", "win32", "linux"},
		},
	}

	manifestBytes, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return nil, "", fmt.Errorf("marshal manifest: %w", err)
	}

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)

	if err := addZipFile(zw, "manifest.json", manifestBytes); err != nil {
		return nil, "", fmt.Errorf("add manifest: %w", err)
	}

	proxyBundle, err := mcpRemoteFS.ReadFile("mcpremote/proxy-bundle.js")
	if err != nil {
		return nil, "", fmt.Errorf("read proxy-bundle.js: %w", err)
	}
	if err := addZipFile(zw, "mcp-remote/proxy-bundle.js", proxyBundle); err != nil {
		return nil, "", fmt.Errorf("add proxy-bundle.js: %w", err)
	}

	if err := zw.Close(); err != nil {
		return nil, "", fmt.Errorf("close zip: %w", err)
	}

	filename := fmt.Sprintf("%s.mcpb", slug)
	return buf.Bytes(), filename, nil
}

// addZipFile adds a file to the zip archive.
func addZipFile(zw *zip.Writer, name string, data []byte) error {
	f, err := zw.Create(name)
	if err != nil {
		return err
	}
	_, err = f.Write(data)
	return err
}

// HandleDownloadMCPBundle handles GET /api/mcp/bundle?token=<emt_token>
// Public endpoint — downloads a pre-configured .mcpb bundle for the given API token.
// Linked from MCP invite emails for zero-login one-click Claude Desktop install.
//
// @Summary      Download project MCP bundle (public)
// @Description  Downloads a .mcpb Claude Desktop bundle for the given read-only API token. No login required — the token is the credential. Double-clicking the downloaded file installs the MCP server in Claude Desktop with the project already configured.
// @Tags         mcp
// @Produce      application/zip
// @Param        token query string true "Read-only MCP API token (emt_*)"
// @Success      200 "Binary .mcpb file download"
// @Failure      400 {object} apperror.Error "Missing token parameter"
// @Failure      401 {object} apperror.Error "Invalid or expired token"
// @Router       /api/mcp/bundle [get]
func (h *Handler) HandleDownloadMCPBundle(c echo.Context) error {
	token := c.QueryParam("token")
	if token == "" {
		return apperror.New(http.StatusBadRequest, "missing_param", "token parameter is required")
	}

	scheme := "https"
	if c.Request().TLS == nil && c.Request().Header.Get("X-Forwarded-Proto") == "" {
		scheme = "http"
	}
	if proto := c.Request().Header.Get("X-Forwarded-Proto"); proto != "" {
		scheme = proto
	}
	mcpBaseURL := fmt.Sprintf("%s://%s", scheme, c.Request().Host)

	data, filename, err := h.svc.GenerateMCPBundleFromToken(c.Request().Context(), token, mcpBaseURL)
	if err != nil {
		return err
	}

	c.Response().Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))
	c.Response().Header().Set("Content-Type", "application/zip")
	c.Response().Header().Set("Content-Length", fmt.Sprintf("%d", len(data)))
	return c.Blob(http.StatusOK, "application/zip", data)
}

// HandleGenerateMCPBundle handles GET /api/projects/:projectId/mcp/bundle
// Generates and serves a per-project .mcpb bundle for Claude Desktop.
//
// @Summary      Generate Claude Desktop MCP bundle
// @Description  Generates a .mcpb file for one-click Claude Desktop installation. The bundle includes a project-specific MCP server configuration with a pre-issued read-only API key. Double-clicking the file installs the MCP server in Claude Desktop.
// @Tags         mcp
// @Produce      application/zip
// @Param        projectId path string true "Project ID (UUID)"
// @Success      200 "Binary .mcpb file download"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Failure      403 {object} apperror.Error "Forbidden — project admin required"
// @Router       /api/projects/{projectId}/mcp/bundle [get]
// @Security     bearerAuth
func (h *Handler) HandleGenerateMCPBundle(c echo.Context) error {
	user := auth.GetUser(c)
	if user == nil {
		return apperror.ErrUnauthorized
	}

	projectID := c.Param("projectId")
	if projectID == "" {
		return apperror.ErrBadRequest.WithMessage("projectId is required")
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

	data, filename, err := h.svc.GenerateMCPBundle(c.Request().Context(), projectID, user.ID, mcpBaseURL)
	if err != nil {
		return err
	}

	c.Response().Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))
	c.Response().Header().Set("Content-Type", "application/zip")
	c.Response().Header().Set("Content-Length", fmt.Sprintf("%d", len(data)))
	return c.Blob(http.StatusOK, "application/zip", data)
}
