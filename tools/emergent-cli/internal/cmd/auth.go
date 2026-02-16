package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/emergent-company/emergent/tools/emergent-cli/internal/auth"
	"github.com/emergent-company/emergent/tools/emergent-cli/internal/config"
	"github.com/spf13/cobra"
)

// healthResponse represents the server health endpoint response
type healthResponse struct {
	Status  string `json:"status"`
	Version string `json:"version,omitempty"`
}

// projectResponse represents a project from the API
type projectResponse struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

// fetchHealth fetches server health status
func fetchHealth(serverURL string) (*healthResponse, error) {
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(serverURL + "/health")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("health check failed with status %d", resp.StatusCode)
	}

	var health healthResponse
	if err := json.NewDecoder(resp.Body).Decode(&health); err != nil {
		return nil, err
	}
	return &health, nil
}

// setAuthHeader sets the appropriate authentication header based on the API key type.
// Project API tokens (emt_ prefix) use Bearer auth; standalone keys use X-API-Key.
func setAuthHeader(req *http.Request, apiKey string) {
	if strings.HasPrefix(apiKey, "emt_") {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	} else {
		req.Header.Set("X-API-Key", apiKey)
	}
}

// fetchProjects fetches the list of projects
func fetchProjects(serverURL, apiKey string) ([]projectResponse, error) {
	client := &http.Client{Timeout: 10 * time.Second}
	req, err := http.NewRequest("GET", serverURL+"/api/projects", nil)
	if err != nil {
		return nil, err
	}
	setAuthHeader(req, apiKey)

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("projects fetch failed with status %d", resp.StatusCode)
	}

	var projects []projectResponse
	if err := json.NewDecoder(resp.Body).Decode(&projects); err != nil {
		return nil, err
	}
	return projects, nil
}

// getCLIPath returns the path to the CLI binary
func getCLIPath() string {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "emergent"
	}

	// Check common installation paths
	paths := []string{
		filepath.Join(homeDir, ".emergent", "bin", "emergent"),
		"/usr/local/bin/emergent",
		filepath.Join(homeDir, "bin", "emergent"),
	}

	for _, p := range paths {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}

	// Return platform-appropriate default
	if runtime.GOOS == "windows" {
		return filepath.Join(homeDir, ".emergent", "bin", "emergent.exe")
	}
	return filepath.Join(homeDir, ".emergent", "bin", "emergent")
}

// maskAPIKey masks an API key for display, showing prefix and last 4 chars
func maskAPIKey(key string) string {
	if len(key) <= 12 {
		return key
	}
	// For emt_ tokens, show the prefix
	if strings.HasPrefix(key, "emt_") {
		return "emt_" + "..." + key[len(key)-4:]
	}
	return key[:8] + "..." + key[len(key)-4:]
}

var loginCmd = &cobra.Command{
	Use:   "login",
	Short: "Authenticate with the Emergent platform",
	Long: `Authenticate using OAuth Device Flow.

This command will:
1. Discover OAuth endpoints from your server
2. Request a device code
3. Open your browser for authorization
4. Wait for you to complete the flow
5. Save your credentials locally`,
	RunE: runLogin,
}

func runLogin(cmd *cobra.Command, args []string) error {
	var configPath string
	configPath, _ = cmd.Flags().GetString("config")
	if configPath == "" {
		configPath = config.DiscoverPath("")
	}

	cfg, err := config.Load(configPath)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to load config: %w", err)
	}
	if cfg == nil {
		cfg = &config.Config{}
	}

	if cfg.ServerURL == "" {
		return fmt.Errorf("no server URL configured. Run: emergent-cli config set-server <url>")
	}

	clientID := "emergent-cli"

	fmt.Printf("Authenticating with %s...\n\n", cfg.ServerURL)

	oidcConfig, err := auth.DiscoverOIDC(cfg.ServerURL)
	if err != nil {
		return fmt.Errorf("failed to discover OIDC configuration: %w", err)
	}

	deviceResp, err := auth.RequestDeviceCode(oidcConfig, clientID, []string{"openid", "profile", "email"})
	if err != nil {
		return fmt.Errorf("failed to request device code: %w", err)
	}

	fmt.Println("Please visit the following URL and enter the code:")
	fmt.Printf("\n  URL:  %s\n", deviceResp.VerificationURI)
	fmt.Printf("  Code: %s\n\n", deviceResp.UserCode)

	if deviceResp.VerificationURIComplete != "" {
		fmt.Println("Or visit this URL with the code pre-filled:")
		fmt.Printf("  %s\n\n", deviceResp.VerificationURIComplete)

		if err := auth.OpenBrowser(deviceResp.VerificationURIComplete); err != nil {
			fmt.Fprintf(os.Stderr, "Note: %v\n\n", err)
		}
	}

	fmt.Println("Waiting for authorization...")

	tokenResp, err := auth.PollForToken(oidcConfig, deviceResp.DeviceCode, clientID, deviceResp.Interval, deviceResp.ExpiresIn)
	if err != nil {
		return fmt.Errorf("failed to obtain token: %w", err)
	}

	userInfo, err := auth.GetUserInfo(oidcConfig, tokenResp.AccessToken)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to fetch user info: %v\n", err)
	}

	expiresAt := time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second)
	creds := &auth.Credentials{
		AccessToken:  tokenResp.AccessToken,
		RefreshToken: tokenResp.RefreshToken,
		ExpiresAt:    expiresAt,
		IssuerURL:    cfg.ServerURL,
	}

	if userInfo != nil {
		creds.UserEmail = userInfo.Email
	}

	homeDir, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("failed to get home directory: %w", err)
	}

	credsPath := filepath.Join(homeDir, ".emergent", "credentials.json")
	if err := auth.Save(creds, credsPath); err != nil {
		return fmt.Errorf("failed to save credentials: %w", err)
	}

	fmt.Println("\n✓ Successfully authenticated!")
	fmt.Printf("Credentials saved to: %s\n", credsPath)

	return nil
}

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show current authentication status",
	Long:  "Display information about the current authentication session including token expiry and user details.",
	RunE:  runStatus,
}

func runStatus(cmd *cobra.Command, args []string) error {
	var configPath string
	configPath, _ = cmd.Flags().GetString("config")
	if configPath == "" {
		configPath = config.DiscoverPath("")
	}

	cfg, err := config.LoadWithEnv(configPath)
	if err == nil && cfg != nil && cfg.APIKey != "" {
		printAPIKeyStatus(cfg)
		return nil
	}

	homeDir, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("failed to get home directory: %w", err)
	}

	credsPath := filepath.Join(homeDir, ".emergent", "credentials.json")

	creds, err := auth.Load(credsPath)
	if err != nil {
		if os.IsNotExist(err) {
			fmt.Println("Not authenticated.")
			fmt.Println("\nRun 'emergent login' to authenticate, or configure an API key:")
			fmt.Println("  export EMERGENT_API_KEY=your-api-key")
			fmt.Println("  # or add 'api_key: your-api-key' to ~/.emergent/config.yaml")
			return nil
		}
		return fmt.Errorf("failed to load credentials: %w", err)
	}

	fmt.Println("Authentication Status:")
	fmt.Println()
	fmt.Println("  Mode:        OAuth (Zitadel)")

	if creds.UserEmail != "" {
		fmt.Printf("  User:        %s\n", creds.UserEmail)
	}

	if creds.IssuerURL != "" {
		fmt.Printf("  Issuer:      %s\n", creds.IssuerURL)
	}

	fmt.Printf("  Expires At:  %s\n", creds.ExpiresAt.Format(time.RFC1123))

	if creds.IsExpired() {
		fmt.Println("  Status:      ⚠️  EXPIRED")
		fmt.Println("\nYour session has expired. Run 'emergent login' to re-authenticate.")
	} else {
		timeUntilExpiry := time.Until(creds.ExpiresAt)
		fmt.Printf("  Status:      ✓ Valid (expires in %s)\n", timeUntilExpiry.Round(time.Minute))
	}

	return nil
}

func printAPIKeyStatus(cfg *config.Config) {
	separator := strings.Repeat("━", 50)

	fmt.Println()
	fmt.Println("Emergent Status")
	fmt.Println(separator)
	fmt.Println()

	isProjectToken := strings.HasPrefix(cfg.APIKey, "emt_")

	fmt.Println("Authentication:")
	if isProjectToken {
		fmt.Println("  Mode:        Project API Token")
	} else {
		fmt.Println("  Mode:        API Key (Standalone)")
	}
	fmt.Printf("  Server:      %s\n", cfg.ServerURL)
	fmt.Printf("  API Key:     %s\n", maskAPIKey(cfg.APIKey))

	health, healthErr := fetchHealth(cfg.ServerURL)
	if healthErr == nil {
		fmt.Println("  Status:      ✓ Connected")
	} else {
		fmt.Println("  Status:      ⚠ Cannot reach server")
	}

	fmt.Println()
	fmt.Println("Server:")
	if healthErr == nil {
		fmt.Println("  Health:      ✓ Healthy")
		if health.Version != "" {
			fmt.Printf("  Version:     %s\n", health.Version)
		}
	} else {
		fmt.Printf("  Health:      ✗ Unreachable (%v)\n", healthErr)
	}

	projects, projErr := fetchProjects(cfg.ServerURL, cfg.APIKey)
	if projErr != nil || len(projects) == 0 {
		return
	}

	if isProjectToken {
		// Project-scoped token: show single project
		project := &projects[0]
		fmt.Println()
		fmt.Println("Project:")
		fmt.Printf("  Name:        %s\n", project.Name)
		fmt.Printf("  ID:          %s\n", project.ID)
		fmt.Println("  Source:      embedded in API token")

		if healthErr == nil {
			printUsageStats(cfg, project.ID)
		}

		fmt.Println()
		fmt.Println(separator)
		fmt.Println("MCP Configuration (copy to your AI agent config)")
		fmt.Println(separator)
		fmt.Println()
		printMCPConfig(cfg, project)
	} else {
		// Standalone API key: show all projects with aggregated stats
		fmt.Println()
		fmt.Printf("Projects:      %d\n", len(projects))
		for _, p := range projects {
			fmt.Printf("  • %s (%s)\n", p.Name, p.ID)
		}

		if healthErr == nil {
			printAggregatedUsageStats(cfg, projects)
		}

		// Use first project for MCP config
		fmt.Println()
		fmt.Println(separator)
		fmt.Println("MCP Configuration (copy to your AI agent config)")
		fmt.Println(separator)
		fmt.Println()
		printMCPConfig(cfg, &projects[0])
	}
}

// --- Usage statistics types ---

type documentSourceTypesResponse struct {
	SourceTypes []sourceTypeCount `json:"sourceTypes"`
}

type sourceTypeCount struct {
	SourceType string `json:"sourceType"`
	Count      int    `json:"count"`
}

type graphCountResponse struct {
	Count int `json:"count"`
}

type typeRegistryStatsResponse struct {
	TotalTypes       int `json:"total_types"`
	EnabledTypes     int `json:"enabled_types"`
	TemplateTypes    int `json:"template_types"`
	CustomTypes      int `json:"custom_types"`
	DiscoveredTypes  int `json:"discovered_types"`
	TotalObjects     int `json:"total_objects"`
	TypesWithObjects int `json:"types_with_objects"`
}

type installedPackResponse struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Version string `json:"version"`
	Active  bool   `json:"active"`
}

type jobMetricsResponse struct {
	Queues []jobQueueMetrics `json:"queues"`
}

type jobQueueMetrics struct {
	Queue      string `json:"queue"`
	Pending    int64  `json:"pending"`
	Processing int64  `json:"processing"`
	Completed  int64  `json:"completed"`
	Failed     int64  `json:"failed"`
	Total      int64  `json:"total"`
}

type taskCountsResponse struct {
	Pending   int64 `json:"pending"`
	Accepted  int64 `json:"accepted"`
	Rejected  int64 `json:"rejected"`
	Cancelled int64 `json:"cancelled"`
}

// printUsageStats fetches and displays usage statistics for the project.
func printUsageStats(cfg *config.Config, projectID string) {
	fmt.Println()
	fmt.Println("Usage Statistics:")

	// Documents
	docSources := fetchDocumentSourceTypes(cfg.ServerURL, cfg.APIKey, projectID)
	if docSources != nil {
		totalDocs := 0
		for _, s := range docSources {
			totalDocs += s.Count
		}
		if totalDocs == 0 {
			fmt.Println("  Documents:     0")
		} else {
			parts := []string{}
			for _, s := range docSources {
				if s.Count > 0 {
					parts = append(parts, fmt.Sprintf("%d %s", s.Count, s.SourceType))
				}
			}
			fmt.Printf("  Documents:     %d", totalDocs)
			if len(parts) > 0 {
				fmt.Printf(" (%s)", strings.Join(parts, ", "))
			}
			fmt.Println()
		}
	}

	// Graph objects
	objTotal := fetchGraphObjectsTotal(cfg.ServerURL, cfg.APIKey, projectID)
	if objTotal >= 0 {
		fmt.Printf("  Graph Objects: %d\n", objTotal)
	}

	// Graph relationships
	relTotal := fetchGraphRelationshipsTotal(cfg.ServerURL, cfg.APIKey, projectID)
	if relTotal >= 0 {
		fmt.Printf("  Relationships: %d\n", relTotal)
	}

	// Type Registry
	typeStats := fetchTypeRegistryStats(cfg.ServerURL, cfg.APIKey, projectID)
	if typeStats != nil && typeStats.TotalTypes > 0 {
		fmt.Println()
		fmt.Println("Type Registry:")
		parts := []string{}
		if typeStats.TemplateTypes > 0 {
			parts = append(parts, fmt.Sprintf("%d template", typeStats.TemplateTypes))
		}
		if typeStats.CustomTypes > 0 {
			parts = append(parts, fmt.Sprintf("%d custom", typeStats.CustomTypes))
		}
		if typeStats.DiscoveredTypes > 0 {
			parts = append(parts, fmt.Sprintf("%d discovered", typeStats.DiscoveredTypes))
		}
		fmt.Printf("  Types:         %d", typeStats.TotalTypes)
		if len(parts) > 0 {
			fmt.Printf(" (%s)", strings.Join(parts, ", "))
		}
		fmt.Println()
		fmt.Printf("  Enabled:       %d\n", typeStats.EnabledTypes)
		fmt.Printf("  Types in Use:  %d\n", typeStats.TypesWithObjects)
	}

	// Template Packs
	packs := fetchInstalledPacks(cfg.ServerURL, cfg.APIKey, projectID)
	if packs != nil {
		fmt.Println()
		if len(packs) == 0 {
			fmt.Println("Template Packs:  none installed")
		} else {
			fmt.Printf("Template Packs:  %d installed\n", len(packs))
			for _, p := range packs {
				status := ""
				if !p.Active {
					status = " (inactive)"
				}
				fmt.Printf("  • %s v%s%s\n", p.Name, p.Version, status)
			}
		}
	}

	// Job queue metrics (no auth required)
	jobMetrics := fetchJobMetrics(cfg.ServerURL)
	if len(jobMetrics) > 0 {
		hasAnyJobs := false
		for _, q := range jobMetrics {
			if q.Total > 0 {
				hasAnyJobs = true
				break
			}
		}
		if hasAnyJobs {
			fmt.Println()
			fmt.Println("Processing Pipeline:")
			for _, q := range jobMetrics {
				if q.Total == 0 {
					continue
				}
				parts := []string{}
				if q.Completed > 0 {
					parts = append(parts, fmt.Sprintf("%d completed", q.Completed))
				}
				if q.Processing > 0 {
					parts = append(parts, fmt.Sprintf("%d processing", q.Processing))
				}
				if q.Pending > 0 {
					parts = append(parts, fmt.Sprintf("%d pending", q.Pending))
				}
				if q.Failed > 0 {
					parts = append(parts, fmt.Sprintf("%d failed", q.Failed))
				}
				label := formatQueueName(q.Queue)
				fmt.Printf("  %-16s %s\n", label+":", strings.Join(parts, ", "))
			}
		}
	}

	// Tasks
	tasks := fetchTaskCounts(cfg.ServerURL, cfg.APIKey, projectID)
	if tasks != nil {
		total := tasks.Pending + tasks.Accepted + tasks.Rejected + tasks.Cancelled
		if total > 0 {
			fmt.Println()
			parts := []string{}
			if tasks.Pending > 0 {
				parts = append(parts, fmt.Sprintf("%d pending", tasks.Pending))
			}
			if tasks.Accepted > 0 {
				parts = append(parts, fmt.Sprintf("%d accepted", tasks.Accepted))
			}
			if tasks.Rejected > 0 {
				parts = append(parts, fmt.Sprintf("%d rejected", tasks.Rejected))
			}
			if tasks.Cancelled > 0 {
				parts = append(parts, fmt.Sprintf("%d cancelled", tasks.Cancelled))
			}
			fmt.Printf("Tasks:           %s\n", strings.Join(parts, ", "))
		}
	}
}

// printAggregatedUsageStats fetches and displays usage statistics summed across all projects.
func printAggregatedUsageStats(cfg *config.Config, projects []projectResponse) {
	fmt.Println()
	fmt.Println("Usage Statistics (all projects):")

	type projectStats struct {
		name string
		docs int
		objs int
		rels int
	}

	stats := make([]projectStats, len(projects))
	totalDocs := 0
	totalObjects := 0
	totalRelationships := 0

	for i, p := range projects {
		docs := 0
		docSources := fetchDocumentSourceTypes(cfg.ServerURL, cfg.APIKey, p.ID)
		for _, s := range docSources {
			docs += s.Count
		}
		objs := fetchGraphObjectsTotal(cfg.ServerURL, cfg.APIKey, p.ID)
		if objs < 0 {
			objs = 0
		}
		rels := fetchGraphRelationshipsTotal(cfg.ServerURL, cfg.APIKey, p.ID)
		if rels < 0 {
			rels = 0
		}

		stats[i] = projectStats{name: p.Name, docs: docs, objs: objs, rels: rels}
		totalDocs += docs
		totalObjects += objs
		totalRelationships += rels
	}

	fmt.Printf("  Documents:     %d\n", totalDocs)
	fmt.Printf("  Graph Objects: %d\n", totalObjects)
	fmt.Printf("  Relationships: %d\n", totalRelationships)

	// Show per-project breakdown if more than one project has data
	var withData []projectStats
	for _, ps := range stats {
		if ps.docs > 0 || ps.objs > 0 || ps.rels > 0 {
			withData = append(withData, ps)
		}
	}

	if len(withData) > 1 {
		fmt.Println()
		fmt.Println("  Per Project:")
		for _, ps := range withData {
			fmt.Printf("    %s: %d docs, %d objects, %d relationships\n", ps.name, ps.docs, ps.objs, ps.rels)
		}
	}

	// Job queue metrics (not project-scoped)
	jobMetrics := fetchJobMetrics(cfg.ServerURL)
	if len(jobMetrics) > 0 {
		hasActive := false
		for _, jm := range jobMetrics {
			if jm.Pending > 0 || jm.Processing > 0 {
				hasActive = true
				break
			}
		}
		if hasActive {
			fmt.Println()
			fmt.Println("Active Jobs:")
			for _, jm := range jobMetrics {
				if jm.Pending > 0 || jm.Processing > 0 {
					label := formatQueueName(jm.Queue)
					parts := []string{}
					if jm.Processing > 0 {
						parts = append(parts, fmt.Sprintf("%d processing", jm.Processing))
					}
					if jm.Pending > 0 {
						parts = append(parts, fmt.Sprintf("%d pending", jm.Pending))
					}
					fmt.Printf("  %-16s %s\n", label+":", strings.Join(parts, ", "))
				}
			}
		}
	}
}

// formatQueueName converts queue names like "object_extraction" to "Extraction"
func formatQueueName(queue string) string {
	names := map[string]string{
		"object_extraction": "Extraction",
		"chunk_embedding":   "Chunk Embed",
		"graph_embedding":   "Graph Embed",
		"document_parsing":  "Doc Parsing",
		"data_source_sync":  "Data Sync",
		"document_chunking": "Chunking",
		"email":             "Email",
	}
	if name, ok := names[queue]; ok {
		return name
	}
	return queue
}

// --- Fetch helpers for usage stats ---

func fetchDocumentSourceTypes(serverURL, apiKey, projectID string) []sourceTypeCount {
	body, err := fetchAPI(serverURL, "/api/documents/source-types", apiKey, projectID)
	if err != nil {
		return nil
	}
	var resp documentSourceTypesResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil
	}
	return resp.SourceTypes
}

func fetchGraphObjectsTotal(serverURL, apiKey, projectID string) int {
	body, err := fetchAPI(serverURL, "/api/graph/objects/count", apiKey, projectID)
	if err != nil {
		return -1
	}
	var resp graphCountResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return -1
	}
	return resp.Count
}

func fetchGraphRelationshipsTotal(serverURL, apiKey, projectID string) int {
	body, err := fetchAPI(serverURL, "/api/graph/relationships/count", apiKey, projectID)
	if err != nil {
		return -1
	}
	var resp graphCountResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return -1
	}
	return resp.Count
}

func fetchTypeRegistryStats(serverURL, apiKey, projectID string) *typeRegistryStatsResponse {
	body, err := fetchAPI(serverURL, fmt.Sprintf("/api/type-registry/projects/%s/stats", projectID), apiKey, projectID)
	if err != nil {
		return nil
	}
	var resp typeRegistryStatsResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil
	}
	return &resp
}

func fetchInstalledPacks(serverURL, apiKey, projectID string) []installedPackResponse {
	body, err := fetchAPI(serverURL, fmt.Sprintf("/api/template-packs/projects/%s/installed", projectID), apiKey, projectID)
	if err != nil {
		return nil
	}
	var resp []installedPackResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil
	}
	return resp
}

func fetchJobMetrics(serverURL string) []jobQueueMetrics {
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(serverURL + "/api/metrics/jobs")
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil
	}
	var metrics jobMetricsResponse
	if err := json.Unmarshal(body, &metrics); err != nil {
		return nil
	}
	return metrics.Queues
}

func fetchTaskCounts(serverURL, apiKey, projectID string) *taskCountsResponse {
	body, err := fetchAPI(serverURL, "/api/tasks/counts?project_id="+projectID, apiKey, projectID)
	if err != nil {
		return nil
	}
	var resp taskCountsResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil
	}
	return &resp
}

// fetchAPI is a helper that makes authenticated GET requests with project context.
func fetchAPI(serverURL, path, apiKey, projectID string) ([]byte, error) {
	client := &http.Client{Timeout: 10 * time.Second}
	req, err := http.NewRequest("GET", serverURL+path, nil)
	if err != nil {
		return nil, err
	}
	setAuthHeader(req, apiKey)
	if projectID != "" {
		req.Header.Set("X-Project-ID", projectID)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API returned status %d", resp.StatusCode)
	}

	return io.ReadAll(resp.Body)
}

func printMCPConfig(cfg *config.Config, project *projectResponse) {
	cliPath := getCLIPath()

	fmt.Println("For Claude Desktop (~/.config/claude/claude_desktop_config.json):")
	fmt.Println()

	stdioConfig := map[string]interface{}{
		"mcpServers": map[string]interface{}{
			"emergent": map[string]interface{}{
				"command": cliPath,
				"args":    []string{"mcp"},
				"env": map[string]string{
					"EMERGENT_SERVER_URL": cfg.ServerURL,
					"EMERGENT_API_KEY":    cfg.APIKey,
				},
			},
		},
	}

	stdioJSON, _ := json.MarshalIndent(stdioConfig, "", "  ")
	fmt.Println(string(stdioJSON))

	fmt.Println()
	fmt.Println("Alternative (SSE transport - for Cursor, Continue, etc.):")
	fmt.Println()

	sseConfig := map[string]interface{}{
		"servers": map[string]interface{}{
			"emergent": map[string]interface{}{
				"type": "sse",
				"url":  fmt.Sprintf("%s/api/mcp/sse/%s", cfg.ServerURL, project.ID),
				"headers": map[string]string{
					"X-API-Key": cfg.APIKey,
				},
			},
		},
	}

	sseJSON, _ := json.MarshalIndent(sseConfig, "", "  ")
	fmt.Println(string(sseJSON))
	fmt.Println()
}

func newLogoutCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "logout",
		Short: "Clear stored credentials",
		Long:  "Remove locally stored OAuth credentials and log out from the Emergent platform.",
		RunE:  runLogout,
	}
}

var logoutCmd = newLogoutCmd()

func runLogout(cmd *cobra.Command, args []string) error {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("failed to get home directory: %w", err)
	}

	credsPath := filepath.Join(homeDir, ".emergent", "credentials.json")

	if _, err := os.Stat(credsPath); os.IsNotExist(err) {
		fmt.Println("No credentials found")
		return nil
	}

	if err := os.Remove(credsPath); err != nil {
		return fmt.Errorf("failed to remove credentials: %w", err)
	}

	fmt.Println("Logged out successfully")
	fmt.Printf("Credentials removed from: %s\n", credsPath)
	return nil
}

func init() {
	rootCmd.AddCommand(loginCmd)
	rootCmd.AddCommand(statusCmd)
	rootCmd.AddCommand(logoutCmd)
}
