package cmd

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"time"

	"github.com/atotto/clipboard"
	"github.com/emergent-company/emergent.memory/tools/cli/internal/auth"
	"github.com/emergent-company/emergent.memory/tools/cli/internal/config"
	"github.com/joho/godotenv"
	"github.com/spf13/cobra"
	"golang.org/x/term"
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
		return "memory"
	}

	// Check common installation paths
	paths := []string{
		filepath.Join(homeDir, ".memory", "bin", "memory"),
		"/usr/local/bin/memory",
		filepath.Join(homeDir, "bin", "memory"),
	}

	for _, p := range paths {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}

	// Return platform-appropriate default
	if runtime.GOOS == "windows" {
		return filepath.Join(homeDir, ".memory", "bin", "memory.exe")
	}
	return filepath.Join(homeDir, ".memory", "bin", "memory")
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

// tokenInfoResponse mirrors the server-side TokenInfoResponse for /api/auth/me
type tokenInfoResponse struct {
	UserID    string `json:"user_id"`
	Email     string `json:"email,omitempty"`
	Type      string `json:"type"`
	ProjectID string `json:"project_id,omitempty"`
	OrgID     string `json:"org_id,omitempty"`
}

// registerCmd is a hidden alias for loginCmd — kept for backwards compatibility.
var registerCmd = &cobra.Command{
	Use:    "register",
	Short:  "Create a new Memory account (alias for login)",
	Hidden: true,
	RunE:   runLogin,
}

// fetchAuthMe calls GET /api/auth/me with the provided Bearer token and returns
// the parsed response. Returns an error if the request fails or returns non-200.
func fetchAuthMe(serverURL, accessToken string) (*tokenInfoResponse, error) {
	client := &http.Client{Timeout: 10 * time.Second}
	req, err := http.NewRequest("GET", serverURL+"/api/auth/me", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("server returned status %d", resp.StatusCode)
	}

	var info tokenInfoResponse
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return nil, err
	}
	return &info, nil
}

// issuerResponse mirrors the server-side IssuerResponse for /api/auth/issuer
type issuerResponse struct {
	Issuer     string `json:"issuer"`
	Standalone bool   `json:"standalone"`
}

// fetchIssuer calls GET /api/auth/issuer on the API server and returns the
// OIDC issuer URL to use for DiscoverOIDC. Returns an error if the server is
// in standalone mode (no Zitadel) or if the endpoint is unreachable.
func fetchIssuer(serverURL string) (string, error) {
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(serverURL + "/api/auth/issuer")
	if err != nil {
		return "", fmt.Errorf("could not reach server at %s: %w", serverURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("server returned status %d for /api/auth/issuer", resp.StatusCode)
	}

	var info issuerResponse
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return "", fmt.Errorf("failed to parse issuer response: %w", err)
	}

	if info.Standalone {
		return "", fmt.Errorf(
			"this server is running in standalone mode and does not support OAuth authentication.\n" +
				"Use an API key instead: memory config set-api-key <key>",
		)
	}

	if info.Issuer == "" {
		return "", fmt.Errorf("server did not return an OIDC issuer URL")
	}

	return info.Issuer, nil
}

var loginCmd = &cobra.Command{
	Use:     "login",
	Short:   "Sign in or create a Memory account",
	GroupID: "account",
	Long: `Authenticate using the OAuth Device Authorization flow.

Opens your browser so you can sign in or create a new account.
Your credentials are saved locally for future CLI use.

If this server is running in standalone mode, use an API key instead:
  memory config set-api-key <key>`,
	RunE: runLogin,
}

// runLogin runs the OAuth device flow: fetch code → prompt → open browser → poll → save.
// It is also invoked by the hidden "register" alias.
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

	// Apply the --server flag override. The flag is registered on the root
	// command so we traverse up to find it.
	if flagServer, _ := cmd.Flags().GetString("server"); flagServer != "" {
		cfg.ServerURL = flagServer
	} else if serverURL != "" {
		// serverURL is the package-level var bound to --server in root.go.
		cfg.ServerURL = serverURL
	}

	if cfg.ServerURL == "" {
		return fmt.Errorf("no server URL configured. Run: memory config set-server <url>")
	}

	clientID := "362800068257972227"

	issuerURL, err := fetchIssuer(cfg.ServerURL)
	if err != nil {
		return fmt.Errorf("login is not available: %w", err)
	}

	oidcConfig, err := auth.DiscoverOIDC(issuerURL)
	if err != nil {
		return fmt.Errorf(
			"could not discover OAuth endpoints from %s\n\n"+
				"This server may be running in standalone mode. Use an API key instead:\n"+
				"  memory config set-api-key <key>",
			issuerURL,
		)
	}

	deviceResp, err := auth.RequestDeviceCode(oidcConfig, clientID, []string{"openid", "profile", "email", "offline_access"})
	if err != nil {
		return fmt.Errorf("failed to request device code: %w", err)
	}

	// Display the user code prominently.
	fmt.Println()
	fmt.Printf("  Your code:  %s\n", deviceResp.UserCode)
	fmt.Println()

	// Try to copy the code to the clipboard silently.
	if err := clipboard.WriteAll(deviceResp.UserCode); err == nil {
		fmt.Println("  (Copied to clipboard)")
	}

	// Pick the best URL to open: use the complete URI if available.
	browserURL := deviceResp.VerificationURIComplete
	if browserURL == "" {
		browserURL = deviceResp.VerificationURI
	}

	// Gate on Enter so the user can read the code before the browser opens.
	fmt.Printf("  Press Enter to open your browser to sign in or create an account.\n")
	fmt.Printf("  Or visit manually: %s\n\n", browserURL)

	// Start polling immediately — the user may navigate to the URL manually
	// without ever pressing Enter, so we must not block polling on that gate.
	fmt.Print("  Waiting for authorization")
	type pollResult struct {
		resp *auth.TokenResponse
		err  error
	}
	pollCh := make(chan pollResult, 1)
	go func() {
		resp, err := pollWithSpinner(oidcConfig, deviceResp, clientID)
		pollCh <- pollResult{resp, err}
	}()

	// Open the browser when Enter is pressed, but do not wait for it before polling.
	go func() {
		waitForEnter()
		if openErr := auth.OpenBrowser(browserURL); openErr != nil {
			// Non-fatal — the user can use the manual URL above.
			fmt.Fprintf(os.Stderr, "  Note: could not open browser automatically.\n\n")
		}
	}()

	result := <-pollCh
	fmt.Println() // end the spinner line
	tokenResp, pollErr := result.resp, result.err
	if pollErr != nil {
		return fmt.Errorf("authorization failed: %w", pollErr)
	}

	// Confirm by calling /api/auth/me — also ensures the server-side profile exists.
	userInfo, meErr := fetchAuthMe(cfg.ServerURL, tokenResp.AccessToken)
	if meErr != nil {
		// Non-fatal.
		fmt.Fprintf(os.Stderr, "  Warning: could not confirm account details: %v\n", meErr)
	}

	expiresAt := time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second)
	creds := &auth.Credentials{
		AccessToken:  tokenResp.AccessToken,
		RefreshToken: tokenResp.RefreshToken,
		ExpiresAt:    expiresAt,
		IssuerURL:    issuerURL,
	}
	if userInfo != nil && userInfo.Email != "" {
		creds.UserEmail = userInfo.Email
	}

	homeDir, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("failed to get home directory: %w", err)
	}
	credsPath := filepath.Join(homeDir, ".memory", "credentials.json")
	if err := auth.Save(creds, credsPath); err != nil {
		return fmt.Errorf("failed to save credentials: %w", err)
	}

	fmt.Println()
	if userInfo != nil && userInfo.Email != "" {
		fmt.Printf("  Logged in as %s\n", userInfo.Email)
	} else {
		fmt.Println("  Logged in successfully.")
	}
	fmt.Println()

	// Context-aware post-login hint: check if current folder has an initialized project.
	envMap, _ := godotenv.Read(".env.local")
	projectID := envMap["MEMORY_PROJECT_ID"]

	if projectID != "" {
		// Folder is initialized — show inline auth status + project info.
		fmt.Println("Authentication Status:")
		fmt.Println()
		fmt.Println("  Mode:        OAuth")
		if userInfo != nil && userInfo.Email != "" {
			fmt.Printf("  User:        %s\n", userInfo.Email)
		}
		fmt.Println("  Status:      ✓ Authenticated")
		fmt.Println()

		projectName := envMap["MEMORY_PROJECT_NAME"]
		if projectName != "" {
			fmt.Printf("  Current project:  %s (%s)\n", projectName, projectID)
		} else {
			fmt.Printf("  Current project:  %s\n", projectID)
		}
		fmt.Println()
	} else {
		// Folder not initialized — suggest memory init.
		fmt.Println("  Run 'memory init' to set up a project in this folder.")
		fmt.Println()
	}

	return nil
}

// waitForEnter reads (and discards) a single line from stdin, unblocking when
// the user presses Enter. It is a no-op when stdin is not a terminal.
func waitForEnter() {
	// Only gate on Enter when running interactively.
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return
	}
	reader := bufio.NewReader(os.Stdin)
	_, _ = reader.ReadString('\n')
}

// pollWithSpinner wraps auth.PollForToken and prints a dot every 2 s so the
// user sees progress. The spinner goroutine is stopped when polling returns.
func pollWithSpinner(oidcCfg *auth.OIDCConfig, deviceResp *auth.DeviceCodeResponse, clientID string) (*auth.TokenResponse, error) {
	var done atomic.Bool

	go func() {
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()
		for {
			if done.Load() {
				return
			}
			<-ticker.C
			if !done.Load() {
				fmt.Print(".")
			}
		}
	}()

	tokenResp, err := auth.PollForToken(oidcCfg, deviceResp.DeviceCode, clientID, deviceResp.Interval, deviceResp.ExpiresIn)
	done.Store(true)
	return tokenResp, err
}

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show current authentication status",
	Long: `Display detailed information about the current authentication session and server health.

Shows authentication Mode (project token, account API key, or OAuth), Server URL,
masked credential key, and connection Status (Connected or unreachable). Also
displays server Health and Version. If authenticated as a user, prints the active
project Name and ID, or a numbered list of all accessible projects.

Additionally prints full Usage Statistics for the active project including:
Documents, Graph Objects, Relationships, Type Registry (Types, Enabled,
TypesWithObjects), Template Packs, and Processing Pipeline job queue depths.`,
	GroupID: "account",
	RunE:    runStatus,
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

	credsPath := filepath.Join(homeDir, ".memory", "credentials.json")

	creds, err := auth.Load(credsPath)
	if err != nil {
		if os.IsNotExist(err) {
			fmt.Println("Not authenticated.")
			fmt.Println("\nSign in or create a new account:")
			fmt.Println("  memory login")
			fmt.Println("\nOr configure a static API key:")
			fmt.Println("  export MEMORY_API_KEY=your-api-key")
			fmt.Println("  # or add 'api_key: your-api-key' to ~/.memory/config.yaml")
			return nil
		}
		return fmt.Errorf("failed to load credentials: %w", err)
	}

	separator := strings.Repeat("━", 50)
	fmt.Println()
	fmt.Println("Memory Status")
	fmt.Println(separator)
	fmt.Println()
	fmt.Printf("CLI Version:   %s\n", Version)
	fmt.Println()

	fmt.Println("Authentication:")
	fmt.Println("  Mode:        OAuth")
	if creds.UserEmail != "" {
		fmt.Printf("  User:        %s\n", creds.UserEmail)
	}
	if creds.IssuerURL != "" {
		fmt.Printf("  Issuer:      %s\n", creds.IssuerURL)
	}
	if creds.IsExpired() {
		fmt.Println("  Status:      ⚠️  EXPIRED")
		fmt.Println("\nYour session has expired. Run 'memory login' to re-authenticate.")
		return nil
	}
	fmt.Println("  Status:      ✓ Authenticated")
	if !creds.ExpiresAt.IsZero() {
		remaining := time.Until(creds.ExpiresAt)
		h := int(remaining.Hours())
		m := int(remaining.Minutes()) % 60
		fmt.Printf("  Expires in:  %dh %dm\n", h, m)
	}

	// Resolve server URL from config (may be empty if not configured).
	serverURL := ""
	if cfg != nil && cfg.ServerURL != "" {
		serverURL = cfg.ServerURL
	}

	if serverURL != "" {
		fmt.Println()
		fmt.Println("Server:")
		fmt.Printf("  URL:         %s\n", serverURL)
		health, healthErr := fetchHealth(serverURL)
		if healthErr == nil {
			fmt.Println("  Status:      ✓ Connected")
			if health.Version != "" {
				fmt.Printf("  Version:     %s\n", health.Version)
			}
		} else {
			fmt.Printf("  Status:      ⚠ Cannot reach server (%v)\n", healthErr)
		}

		// Show active project and project list using the OAuth access token.
		projects, projErr := fetchProjects(serverURL, creds.AccessToken)
		if projErr == nil && len(projects) > 0 {
			// Determine active project from config.
			activeProjectID := ""
			activeProjectName := ""
			if cfg != nil {
				activeProjectID = cfg.ProjectID
				activeProjectName = cfg.ProjectName
			}
			if ep := os.Getenv("MEMORY_PROJECT"); ep != "" && activeProjectID == "" {
				activeProjectName = ep
			}

			var activeProject *projectResponse
			if activeProjectID != "" || activeProjectName != "" {
				for i := range projects {
					if projects[i].ID == activeProjectID ||
						strings.EqualFold(projects[i].Name, activeProjectName) {
						activeProject = &projects[i]
						break
					}
				}
			}

			fmt.Println()
			if activeProject != nil {
				fmt.Println("Active Project:")
				fmt.Printf("  Name:        %s\n", activeProject.Name)
				fmt.Printf("  ID:          %s\n", activeProject.ID)
			} else {
				fmt.Printf("Projects:      %d\n", len(projects))
				for _, p := range projects {
					fmt.Printf("  • %s (%s)\n", p.Name, p.ID)
				}
			}
		}
	}

	return nil
}

func printAPIKeyStatus(cfg *config.Config) {
	separator := strings.Repeat("━", 50)

	fmt.Println()
	fmt.Println("Memory Status")
	fmt.Println(separator)
	fmt.Println()

	// Determine the active key and whether it's a project-scoped token.
	// When only a project token is configured, cfg.APIKey may be the global
	// account key from ~/.memory/config.yaml; prefer cfg.ProjectToken when set.
	activeKey := cfg.APIKey
	isProjectToken := cfg.ProjectToken != "" || strings.HasPrefix(cfg.APIKey, "emt_")
	if cfg.ProjectToken != "" {
		activeKey = cfg.ProjectToken
	}

	// MEMORY_PROJECT name-scope: account API key scoped to a specific project
	// by name/slug (no project token, but a project name or pre-resolved ID).
	hasNameScope := !isProjectToken && (cfg.ProjectName != "" || cfg.ProjectID != "")

	fmt.Printf("CLI Version:   %s\n", Version)
	fmt.Println()

	fmt.Println("Authentication:")
	if isProjectToken {
		fmt.Println("  Mode:        Project token (scoped to one project)")
	} else if hasNameScope {
		scopeLabel := cfg.ProjectName
		if scopeLabel == "" {
			// Prefer the MEMORY_PROJECT slug over a raw UUID
			if ep := os.Getenv("MEMORY_PROJECT"); ep != "" {
				scopeLabel = ep
			} else {
				scopeLabel = cfg.ProjectID
			}
		}
		fmt.Printf("  Mode:        Account API key (project scope: %s)\n", scopeLabel)
	} else {
		fmt.Println("  Mode:        Account API key (access to all projects)")
	}
	fmt.Printf("  Server:      %s\n", cfg.ServerURL)
	fmt.Printf("  Key:         %s\n", maskAPIKey(activeKey))

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

	projects, projErr := fetchProjects(cfg.ServerURL, activeKey)
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
		fmt.Println("  Access:      project token (scoped)")

		if healthErr == nil {
			printUsageStats(cfg, activeKey, project.ID)
		}
	} else if hasNameScope {
		// Account API key scoped to one project by name — find the matching project.
		var matchedProject *projectResponse
		for i := range projects {
			if projects[i].ID == cfg.ProjectID ||
				strings.EqualFold(projects[i].Name, cfg.ProjectName) {
				matchedProject = &projects[i]
				break
			}
		}
		if matchedProject != nil {
			fmt.Println()
			fmt.Println("Project:")
			fmt.Printf("  Name:        %s\n", matchedProject.Name)
			fmt.Printf("  ID:          %s\n", matchedProject.ID)
			fmt.Println("  Access:      account key (MEMORY_PROJECT scoped)")

			if healthErr == nil {
				printUsageStats(cfg, activeKey, matchedProject.ID)
			}
		} else {
			// Fallback — show all projects
			fmt.Println()
			fmt.Printf("Projects:      %d\n", len(projects))
			for _, p := range projects {
				fmt.Printf("  • %s (%s)\n", p.Name, p.ID)
			}
		}
	} else {
		// Account API key: show all projects with aggregated stats
		fmt.Println()
		fmt.Printf("Projects:      %d\n", len(projects))
		for _, p := range projects {
			fmt.Printf("  • %s (%s)\n", p.Name, p.ID)
		}

		if healthErr == nil {
			printAggregatedUsageStats(cfg, activeKey, projects)
		}
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
func printUsageStats(cfg *config.Config, apiKey string, projectID string) {
	fmt.Println()
	fmt.Println("Usage Statistics:")

	// Documents
	docSources := fetchDocumentSourceTypes(cfg.ServerURL, apiKey, projectID)
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
	objTotal := fetchGraphObjectsTotal(cfg.ServerURL, apiKey, projectID)
	if objTotal >= 0 {
		fmt.Printf("  Graph Objects: %d\n", objTotal)
	}

	// Graph relationships
	relTotal := fetchGraphRelationshipsTotal(cfg.ServerURL, apiKey, projectID)
	if relTotal >= 0 {
		fmt.Printf("  Relationships: %d\n", relTotal)
	}

	// Type Registry
	typeStats := fetchTypeRegistryStats(cfg.ServerURL, apiKey, projectID)
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
	packs := fetchInstalledPacks(cfg.ServerURL, apiKey, projectID)
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
	tasks := fetchTaskCounts(cfg.ServerURL, apiKey, projectID)
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
func printAggregatedUsageStats(cfg *config.Config, apiKey string, projects []projectResponse) {
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
		docSources := fetchDocumentSourceTypes(cfg.ServerURL, apiKey, p.ID)
		for _, s := range docSources {
			docs += s.Count
		}
		objs := fetchGraphObjectsTotal(cfg.ServerURL, apiKey, p.ID)
		if objs < 0 {
			objs = 0
		}
		rels := fetchGraphRelationshipsTotal(cfg.ServerURL, apiKey, p.ID)
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
			"memory": map[string]interface{}{
				"command": cliPath,
				"args":    []string{"mcp"},
				"env": map[string]string{
					"MEMORY_SERVER_URL": cfg.ServerURL,
					"MEMORY_API_KEY":    cfg.APIKey,
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
			"memory": map[string]interface{}{
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

// logoutAll is bound to the --all flag on the logout command.
var logoutAll bool

func newLogoutCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "logout",
		Short: "Clear stored credentials",
		Long: `Remove locally stored OAuth credentials and log out from the Memory platform.

Before deleting local credentials, attempts to revoke tokens server-side via
the OIDC revocation endpoint. Revocation is best-effort — if it fails, local
credentials are still removed.

Use --all to also clear api_key and project_token from your config file,
removing all locally stored authentication state.`,
		GroupID: "account",
		RunE:    runLogout,
	}
	cmd.Flags().BoolVar(&logoutAll, "all", false, "Also clear api_key and project_token from config")
	return cmd
}

var logoutCmd = newLogoutCmd()

// oidcClientID is the OAuth client ID used for device flow authentication.
const oidcClientID = "362800068257972227"

func runLogout(cmd *cobra.Command, args []string) error {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("failed to get home directory: %w", err)
	}

	credsPath := filepath.Join(homeDir, ".memory", "credentials.json")
	clearedAnything := false

	// --- OAuth credentials ---
	if _, err := os.Stat(credsPath); os.IsNotExist(err) {
		fmt.Fprintln(os.Stderr, "No OAuth credentials found")
	} else {
		// Load credentials to attempt revocation before deleting
		creds, loadErr := auth.Load(credsPath)
		if loadErr != nil {
			fmt.Fprintf(os.Stderr, "Warning: could not load credentials for revocation: %v\n", loadErr)
		} else {
			// Attempt server-side token revocation (best-effort)
			warnings := auth.RevokeCredentials(creds, oidcClientID)
			for _, w := range warnings {
				fmt.Fprintf(os.Stderr, "Warning: %s\n", w)
			}
			if len(warnings) == 0 {
				fmt.Println("Tokens revoked server-side")
			}
		}

		// Always delete local credentials regardless of revocation outcome
		if err := os.Remove(credsPath); err != nil {
			return fmt.Errorf("failed to remove credentials: %w", err)
		}
		fmt.Printf("OAuth credentials removed: %s\n", credsPath)
		clearedAnything = true
	}

	// --- Config auth fields (--all) ---
	if logoutAll {
		var configPath string
		configPath, _ = cmd.Flags().GetString("config")
		if configPath == "" {
			configPath = config.DiscoverPath("")
		}

		cfg, loadErr := config.Load(configPath)
		if loadErr != nil {
			fmt.Fprintf(os.Stderr, "Warning: could not load config: %v\n", loadErr)
		} else {
			changed := false

			if cfg.APIKey != "" {
				cfg.APIKey = ""
				fmt.Println("Cleared api_key from config")
				changed = true
			}
			if cfg.ProjectToken != "" {
				cfg.ProjectToken = ""
				fmt.Println("Cleared project_token from config")
				changed = true
			}

			if changed {
				if saveErr := config.Save(cfg, configPath); saveErr != nil {
					return fmt.Errorf("failed to save config: %w", saveErr)
				}
				clearedAnything = true
			} else {
				fmt.Println("No api_key or project_token in config to clear")
			}
		}
	}

	if clearedAnything {
		fmt.Println("Logged out successfully")
	} else {
		fmt.Println("No credentials found to clear")
	}
	return nil
}

// setTokenCmd stores a static Bearer token as CLI credentials, bypassing
// the OAuth device flow. Useful for CI, test harnesses, and development
// environments that provide a pre-issued token (e.g. an e2e test token).
//
// The token is saved to ~/.memory/credentials.json with a 24-hour expiry so
// the normal OAuth provider picks it up on the next CLI invocation.
var setTokenCmd = &cobra.Command{
	Use:     "set-token <bearer-token>",
	Short:   "Save a static Bearer token as CLI credentials",
	GroupID: "account",
	Long: `Save a static Bearer token to ~/.memory/credentials.json.

Useful in CI, test harnesses, and dev environments where a token is
pre-issued rather than obtained via the OAuth device flow.

Example:
  memory auth set-token e2e-test-user`,
	Args: cobra.ExactArgs(1),
	RunE: runSetToken,
}

var setTokenDuration string

func runSetToken(cmd *cobra.Command, args []string) error {
	token := args[0]

	var configPath string
	configPath, _ = cmd.Flags().GetString("config")
	if configPath == "" {
		configPath = config.DiscoverPath("")
	}

	cfg, err := config.Load(configPath)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to load config: %w", err)
	}

	dur := 24 * time.Hour
	if setTokenDuration != "" {
		dur, err = time.ParseDuration(setTokenDuration)
		if err != nil {
			return fmt.Errorf("invalid --duration %q: %w", setTokenDuration, err)
		}
	}

	issuerURL := ""
	if cfg != nil {
		issuerURL = cfg.ServerURL
	}

	creds := &auth.Credentials{
		AccessToken: token,
		ExpiresAt:   time.Now().Add(dur),
		IssuerURL:   issuerURL,
	}

	homeDir, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("failed to get home directory: %w", err)
	}

	credsPath := filepath.Join(homeDir, ".memory", "credentials.json")
	if err := auth.Save(creds, credsPath); err != nil {
		return fmt.Errorf("failed to save credentials: %w", err)
	}

	// Clear the api_key in config so the credentials.json path is preferred
	// over any stale standalone key on subsequent CLI invocations.
	if cfg != nil && cfg.APIKey != "" {
		cfg.APIKey = ""
		if saveErr := config.Save(cfg, configPath); saveErr != nil {
			fmt.Fprintf(os.Stderr, "Warning: could not clear api_key from config: %v\n", saveErr)
		}
	}

	fmt.Printf("Token saved to %s (expires in %s).\n", credsPath, dur)
	return nil
}

func init() {
	rootCmd.AddCommand(loginCmd)
	rootCmd.AddCommand(registerCmd)
	rootCmd.AddCommand(statusCmd)
	rootCmd.AddCommand(logoutCmd)
	rootCmd.AddCommand(mcpGuideCmd)

	setTokenCmd.Flags().StringVar(&setTokenDuration, "duration", "", "Token validity duration (default 24h, e.g. 48h, 168h)")
	rootCmd.AddCommand(setTokenCmd)
}

// mcpGuideCmd prints MCP configuration snippets for connecting AI agents to Memory.
var mcpGuideCmd = &cobra.Command{
	Use:   "mcp-guide",
	Short: "Show MCP configuration for AI agents",
	Long: `Print ready-to-use MCP server configuration snippets for connecting AI agents to Memory.

Outputs JSON configuration blocks for Claude Desktop, Cursor, and other MCP-
compatible clients. Snippets use the active server URL and API key (project
token takes precedence over account key). Copy the relevant block into your
AI client's MCP configuration to enable Memory tools.`,
	GroupID: "ai",
	RunE:    runMCPGuide,
}

func runMCPGuide(cmd *cobra.Command, args []string) error {
	var configPath string
	configPath, _ = cmd.Flags().GetString("config")
	if configPath == "" {
		configPath = config.DiscoverPath("")
	}

	cfg, err := config.LoadWithEnv(configPath)
	if err != nil || cfg == nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	// Determine active key (prefer project token)
	activeKey := cfg.APIKey
	if cfg.ProjectToken != "" {
		activeKey = cfg.ProjectToken
	}

	if activeKey == "" {
		return fmt.Errorf("no API key configured. Set MEMORY_API_KEY or MEMORY_PROJECT_TOKEN")
	}

	projects, err := fetchProjects(cfg.ServerURL, activeKey)
	if err != nil || len(projects) == 0 {
		return fmt.Errorf("could not fetch projects to generate MCP config: %w", err)
	}

	separator := strings.Repeat("━", 50)
	fmt.Println()
	fmt.Println(separator)
	fmt.Println("MCP Configuration (copy to your AI agent config)")
	fmt.Println(separator)
	fmt.Println()
	printMCPConfig(cfg, &projects[0])
	return nil
}
