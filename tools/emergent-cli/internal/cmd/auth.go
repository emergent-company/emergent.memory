package cmd

import (
	"encoding/json"
	"fmt"
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

// fetchProjects fetches the list of projects
func fetchProjects(serverURL, apiKey string) ([]projectResponse, error) {
	client := &http.Client{Timeout: 10 * time.Second}
	req, err := http.NewRequest("GET", serverURL+"/api/v2/projects", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-API-Key", apiKey)

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

// maskAPIKey masks an API key for display, showing first 8 and last 4 chars
func maskAPIKey(key string) string {
	if len(key) <= 12 {
		return key
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

	fmt.Println("Authentication:")
	fmt.Println("  Mode:        API Key (Standalone)")
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

	var project *projectResponse
	projects, projErr := fetchProjects(cfg.ServerURL, cfg.APIKey)
	if projErr == nil && len(projects) > 0 {
		project = &projects[0]
		fmt.Println()
		fmt.Println("Project:")
		fmt.Printf("  Name:        %s\n", project.Name)
		fmt.Printf("  ID:          %s\n", project.ID)
	}

	if project != nil {
		fmt.Println()
		fmt.Println(separator)
		fmt.Println("MCP Configuration (copy to your AI agent config)")
		fmt.Println(separator)
		fmt.Println()

		printMCPConfig(cfg, project)
	}
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
