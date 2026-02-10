package cmd

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/emergent-company/emergent/tools/emergent-cli/internal/auth"
	"github.com/emergent-company/emergent/tools/emergent-cli/internal/client"
	"github.com/emergent-company/emergent/tools/emergent-cli/internal/config"
	"github.com/spf13/cobra"
)

var doctorFlags struct {
	fix bool
}

var doctorCmd = &cobra.Command{
	Use:   "doctor",
	Short: "Check system health and configuration",
	Long: `Run diagnostic checks on your Emergent CLI installation.

This command verifies:
- Configuration file exists and is valid
- Server connectivity
- Authentication status
- API functionality
- Docker container health (for standalone installations)

Use --fix to automatically repair common issues.`,
	RunE: runDoctor,
}

func init() {
	doctorCmd.Flags().BoolVar(&doctorFlags.fix, "fix", false, "Attempt to automatically fix detected issues")
	rootCmd.AddCommand(doctorCmd)
}

type checkResult struct {
	name    string
	status  string
	message string
	fixable bool
	fixType string
}

func runDoctor(cmd *cobra.Command, args []string) error {
	var results []checkResult

	fmt.Println("Emergent CLI Diagnostics")
	fmt.Println("========================")
	fmt.Println()

	configPath, _ := cmd.Flags().GetString("config")
	if configPath == "" {
		configPath = config.DiscoverPath("")
	}

	results = append(results, checkConfig(configPath))

	cfg, _ := config.LoadWithEnv(configPath)

	// Check if this is a standalone installation
	homeDir, _ := os.UserHomeDir()
	installDir := filepath.Join(homeDir, ".emergent")
	isStandalone := isStandaloneInstallation(installDir)

	if isStandalone {
		results = append(results, checkDockerContainers(installDir))
	}

	if cfg != nil && cfg.ServerURL != "" {
		results = append(results, checkServerConnectivity(cfg.ServerURL))
		results = append(results, checkAuth(cfg, configPath))
		results = append(results, checkAPI(cfg))
		results = append(results, checkMCP(cfg))
	}

	fmt.Println()
	fmt.Println("Summary")
	fmt.Println("-------")

	passed := 0
	failed := 0
	warned := 0
	var failedResults []checkResult

	for _, r := range results {
		var icon string
		switch r.status {
		case "pass":
			icon = "✓"
			passed++
		case "fail":
			icon = "✗"
			failed++
			failedResults = append(failedResults, r)
		case "warn":
			icon = "⚠"
			warned++
		}
		fmt.Printf("%s %s: %s\n", icon, r.name, r.message)
	}

	fmt.Println()
	fmt.Printf("Checks: %d passed", passed)
	if warned > 0 {
		fmt.Printf(", %d warnings", warned)
	}
	if failed > 0 {
		fmt.Printf(", %d failed", failed)
	}
	fmt.Println()

	// Offer to fix issues if --fix flag is set or prompt user
	if failed > 0 && isStandalone {
		for _, r := range failedResults {
			if r.fixable && doctorFlags.fix {
				fmt.Println()
				if err := attemptFix(r, installDir); err != nil {
					fmt.Printf("Fix failed: %v\n", err)
				}
			} else if r.fixable && !doctorFlags.fix {
				fmt.Println()
				fmt.Printf("Issue '%s' can be fixed automatically.\n", r.name)
				fmt.Println("Run 'emergent doctor --fix' to attempt repair.")
			}
		}
	}

	if failed > 0 {
		return fmt.Errorf("%d check(s) failed", failed)
	}

	return nil
}

func checkConfig(configPath string) checkResult {
	fmt.Print("Checking configuration... ")

	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		fmt.Println("NOT FOUND")
		return checkResult{
			name:    "Configuration",
			status:  "fail",
			message: fmt.Sprintf("Config file not found at %s", configPath),
		}
	}

	cfg, err := config.LoadWithEnv(configPath)
	if err != nil {
		fmt.Println("ERROR")
		return checkResult{
			name:    "Configuration",
			status:  "fail",
			message: fmt.Sprintf("Failed to load config: %v", err),
		}
	}

	if cfg.ServerURL == "" {
		fmt.Println("INCOMPLETE")
		return checkResult{
			name:    "Configuration",
			status:  "fail",
			message: "Server URL not configured",
		}
	}

	fmt.Println("OK")
	return checkResult{
		name:    "Configuration",
		status:  "pass",
		message: fmt.Sprintf("Loaded from %s", configPath),
	}
}

func checkServerConnectivity(serverURL string) checkResult {
	fmt.Printf("Checking server connectivity (%s)... ", serverURL)

	httpClient := &http.Client{Timeout: 10 * time.Second}

	healthURL := strings.TrimSuffix(serverURL, "/") + "/health"
	resp, err := httpClient.Get(healthURL)
	if err != nil {
		fmt.Println("FAILED")
		return checkResult{
			name:    "Server Connectivity",
			status:  "fail",
			message: fmt.Sprintf("Cannot reach server: %v", err),
		}
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		fmt.Println("UNHEALTHY")
		return checkResult{
			name:    "Server Connectivity",
			status:  "warn",
			message: fmt.Sprintf("Health check returned %d: %s", resp.StatusCode, string(body)),
		}
	}

	fmt.Println("OK")
	return checkResult{
		name:    "Server Connectivity",
		status:  "pass",
		message: "Server is reachable and healthy",
	}
}

func checkAuth(cfg *config.Config, configPath string) checkResult {
	fmt.Print("Checking authentication... ")

	if cfg.APIKey != "" {
		fmt.Println("API KEY")
		return checkResult{
			name:    "Authentication",
			status:  "pass",
			message: fmt.Sprintf("Using API key (%s...)", cfg.APIKey[:8]),
		}
	}

	homeDir, err := os.UserHomeDir()
	if err != nil {
		fmt.Println("ERROR")
		return checkResult{
			name:    "Authentication",
			status:  "fail",
			message: "Cannot determine home directory",
		}
	}

	credsPath := filepath.Join(homeDir, ".emergent", "credentials.json")
	creds, err := auth.Load(credsPath)
	if err != nil {
		if os.IsNotExist(err) {
			fmt.Println("NOT CONFIGURED")
			return checkResult{
				name:    "Authentication",
				status:  "warn",
				message: "No credentials found. Run 'emergent login' or set API key",
			}
		}
		fmt.Println("ERROR")
		return checkResult{
			name:    "Authentication",
			status:  "fail",
			message: fmt.Sprintf("Failed to load credentials: %v", err),
		}
	}

	if creds.IsExpired() {
		fmt.Println("EXPIRED")
		return checkResult{
			name:    "Authentication",
			status:  "warn",
			message: "OAuth token expired. Run 'emergent login' to re-authenticate",
		}
	}

	fmt.Println("OAUTH")
	return checkResult{
		name:    "Authentication",
		status:  "pass",
		message: fmt.Sprintf("OAuth token valid until %s", creds.ExpiresAt.Format("2006-01-02 15:04")),
	}
}

func checkAPI(cfg *config.Config) checkResult {
	fmt.Print("Checking API access... ")

	c := client.New(cfg)

	resp, err := c.Get("/api/projects")
	if err != nil {
		fmt.Println("FAILED")
		return checkResult{
			name:    "API Access",
			status:  "fail",
			message: fmt.Sprintf("Request failed: %v", err),
		}
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	bodyStr := strings.TrimSpace(string(body))

	if resp.StatusCode == http.StatusUnauthorized {
		fmt.Println("UNAUTHORIZED")
		return checkResult{
			name:    "API Access",
			status:  "fail",
			message: "Authentication rejected. Check your API key or re-login",
		}
	}

	if resp.StatusCode == http.StatusForbidden {
		fmt.Println("FORBIDDEN")
		return checkResult{
			name:    "API Access",
			status:  "warn",
			message: "Access denied. You may not have permissions for this endpoint",
		}
	}

	if resp.StatusCode == http.StatusNotFound {
		fmt.Println("NOT FOUND")
		// Provide detailed diagnostics for 404 errors
		hint := diagnose404Error(cfg, bodyStr)
		return checkResult{
			name:    "API Access",
			status:  "fail",
			message: hint,
		}
	}

	if resp.StatusCode != http.StatusOK {
		fmt.Println("ERROR")
		return checkResult{
			name:    "API Access",
			status:  "fail",
			message: fmt.Sprintf("API returned %d: %s", resp.StatusCode, bodyStr),
		}
	}

	fmt.Println("OK")
	return checkResult{
		name:    "API Access",
		status:  "pass",
		message: "Successfully accessed API",
	}
}

// diagnose404Error provides detailed diagnostics for 404 errors
func diagnose404Error(cfg *config.Config, responseBody string) string {
	var hints []string

	// Check if the response looks like it's from the wrong server type
	if strings.Contains(responseBody, `"statusCode":404`) {
		hints = append(hints, "Server appears to be running NestJS instead of Go server")
		hints = append(hints, "Solution: Ensure the Docker image is up-to-date (docker compose pull && docker compose up -d)")
	} else if strings.Contains(responseBody, `"status":"error"`) {
		hints = append(hints, "Server returned unexpected error format - may be running outdated version")
		hints = append(hints, "Solution: Update your installation (run the install script again)")
	}

	// Check for common misconfigurations
	if cfg.APIKey != "" {
		hints = append(hints, fmt.Sprintf("API Key configured: %s...", cfg.APIKey[:min(8, len(cfg.APIKey))]))
	}

	// Check if standalone user might not exist
	hints = append(hints, "Possible causes:")
	hints = append(hints, "  1. Server is running an outdated Docker image")
	hints = append(hints, "  2. Database migrations haven't run (standalone user not created)")
	hints = append(hints, "  3. API key doesn't match server's STANDALONE_API_KEY")

	hints = append(hints, "")
	hints = append(hints, "Troubleshooting steps:")
	hints = append(hints, "  1. Check server logs: docker logs emergent-server")
	hints = append(hints, "  2. Verify API key matches: grep STANDALONE_API_KEY ~/.emergent/config/.env.local")
	hints = append(hints, "  3. Update installation: curl -fsSL https://raw.githubusercontent.com/emergent-company/emergent/main/deploy/minimal/install-online.sh | bash")

	return strings.Join(hints, "\n       ")
}

func isStandaloneInstallation(installDir string) bool {
	composePath := filepath.Join(installDir, "docker", "docker-compose.yml")
	_, err := os.Stat(composePath)
	return err == nil
}

func checkDockerContainers(installDir string) checkResult {
	fmt.Print("Checking Docker containers... ")

	serverLogs, err := getDockerLogs(installDir, "server", 50)
	if err != nil {
		fmt.Println("DOCKER ERROR")
		return checkResult{
			name:    "Docker Containers",
			status:  "warn",
			message: fmt.Sprintf("Could not check Docker: %v", err),
		}
	}

	if strings.Contains(serverLogs, "password authentication failed") {
		fmt.Println("DB AUTH FAILED")
		return checkResult{
			name:    "Docker Containers",
			status:  "fail",
			message: "Database password mismatch. The PostgreSQL container was initialized with a different password than configured.",
			fixable: true,
			fixType: "db_password_reset",
		}
	}

	if strings.Contains(serverLogs, "connection refused") {
		fmt.Println("DB CONNECTION FAILED")
		return checkResult{
			name:    "Docker Containers",
			status:  "fail",
			message: "Database connection refused. Containers may not be running.",
			fixable: true,
			fixType: "restart_containers",
		}
	}

	if strings.Contains(serverLogs, "Server is ready") || strings.Contains(serverLogs, "Starting server") {
		fmt.Println("OK")
		return checkResult{
			name:    "Docker Containers",
			status:  "pass",
			message: "Server container is running",
		}
	}

	fmt.Println("UNKNOWN")
	return checkResult{
		name:    "Docker Containers",
		status:  "warn",
		message: "Could not determine server status. Check 'docker logs emergent-server'",
	}
}

func getDockerLogs(installDir, service string, lines int) (string, error) {
	composePath := filepath.Join(installDir, "docker", "docker-compose.yml")
	envPath := filepath.Join(installDir, "config", ".env.local")

	args := []string{
		"compose", "-f", composePath, "--env-file", envPath,
		"logs", "--tail", fmt.Sprintf("%d", lines), service,
	}

	cmd := exec.Command("docker", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", err
	}
	return string(output), nil
}

func attemptFix(r checkResult, installDir string) error {
	switch r.fixType {
	case "db_password_reset":
		return fixDatabasePassword(installDir)
	case "restart_containers":
		return restartContainers(installDir)
	default:
		return fmt.Errorf("unknown fix type: %s", r.fixType)
	}
}

func fixDatabasePassword(installDir string) error {
	fmt.Println("Fixing database password mismatch...")
	fmt.Println()

	dbPassword, err := getPostgresPasswordFromContainer()
	if err != nil {
		fmt.Printf("Could not recover password from container: %v\n", err)
		fmt.Println()
		return offerDestructiveFix(installDir)
	}

	fmt.Printf("Recovered password from PostgreSQL container.\n")
	fmt.Println()
	fmt.Println("This will:")
	fmt.Println("  1. Update .env.local with the correct password")
	fmt.Println("  2. Restart the server container")
	fmt.Println("  3. Preserve all existing data")
	fmt.Println()
	fmt.Print("Continue? [y/N]: ")

	reader := bufio.NewReader(os.Stdin)
	input, _ := reader.ReadString('\n')
	input = strings.TrimSpace(strings.ToLower(input))

	if input != "y" && input != "yes" {
		fmt.Println("Cancelled.")
		return nil
	}

	fmt.Println()
	fmt.Println("Updating configuration...")
	if err := updateEnvPassword(installDir, dbPassword); err != nil {
		return fmt.Errorf("failed to update config: %w", err)
	}

	fmt.Println("Restarting server...")
	if err := restartServerOnly(installDir); err != nil {
		return err
	}

	fmt.Println("Waiting for server...")
	time.Sleep(10 * time.Second)

	fmt.Println()
	fmt.Println("Password synchronized. Run 'emergent doctor' to verify.")
	return nil
}

func getPostgresPasswordFromContainer() (string, error) {
	cmd := exec.Command("docker", "exec", "emergent-db", "printenv", "POSTGRES_PASSWORD")
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(output)), nil
}

func updateEnvPassword(installDir, newPassword string) error {
	envPath := filepath.Join(installDir, "config", ".env.local")

	content, err := os.ReadFile(envPath)
	if err != nil {
		return err
	}

	lines := strings.Split(string(content), "\n")
	var newLines []string

	for _, line := range lines {
		if strings.HasPrefix(line, "POSTGRES_PASSWORD=") {
			newLines = append(newLines, fmt.Sprintf("POSTGRES_PASSWORD=%s", newPassword))
		} else {
			newLines = append(newLines, line)
		}
	}

	return os.WriteFile(envPath, []byte(strings.Join(newLines, "\n")), 0600)
}

func restartServerOnly(installDir string) error {
	composePath := filepath.Join(installDir, "docker", "docker-compose.yml")
	envPath := filepath.Join(installDir, "config", ".env.local")

	cmd := exec.Command("docker", "compose", "-f", composePath, "--env-file", envPath, "restart", "server")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func offerDestructiveFix(installDir string) error {
	fmt.Println("Cannot recover password. The database container may not be running.")
	fmt.Println()
	fmt.Println("Alternative: Reset database (DATA WILL BE LOST)")
	fmt.Println("  1. Stop all containers")
	fmt.Println("  2. Remove PostgreSQL volume")
	fmt.Println("  3. Start fresh")
	fmt.Println()
	fmt.Print("Reset database? [y/N]: ")

	reader := bufio.NewReader(os.Stdin)
	input, _ := reader.ReadString('\n')
	input = strings.TrimSpace(strings.ToLower(input))

	if input != "y" && input != "yes" {
		fmt.Println("Cancelled.")
		return nil
	}

	fmt.Println()
	fmt.Println("Stopping containers...")
	if err := runDockerCompose(installDir, "down"); err != nil {
		return err
	}

	fmt.Println("Removing PostgreSQL volume...")
	volumeCmd := exec.Command("docker", "volume", "rm", "emergent_postgres_data")
	_ = volumeCmd.Run() // Ignore error - volume may not exist

	fmt.Println("Starting containers...")
	if err := runDockerCompose(installDir, "up", "-d"); err != nil {
		return err
	}

	fmt.Println("Waiting for services...")
	time.Sleep(10 * time.Second)

	fmt.Println()
	fmt.Println("Database reset complete. Run 'emergent doctor' to verify.")
	return nil
}

func restartContainers(installDir string) error {
	fmt.Println("Restarting containers...")
	if err := runDockerCompose(installDir, "up", "-d"); err != nil {
		return err
	}
	fmt.Println("Containers restarted.")
	return nil
}

func runDockerCompose(installDir string, args ...string) error {
	composePath := filepath.Join(installDir, "docker", "docker-compose.yml")
	envPath := filepath.Join(installDir, "config", ".env.local")

	baseArgs := []string{"compose", "-f", composePath, "--env-file", envPath}
	baseArgs = append(baseArgs, args...)

	cmd := exec.Command("docker", baseArgs...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func checkMCP(cfg *config.Config) checkResult {
	fmt.Print("Checking MCP server... ")

	httpClient := &http.Client{Timeout: 10 * time.Second}

	initRequest := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "initialize",
		"params": map[string]interface{}{
			"protocolVersion": "2025-11-25",
			"capabilities":    map[string]interface{}{},
			"clientInfo": map[string]interface{}{
				"name":    "emergent-cli-doctor",
				"version": "1.0.0",
			},
		},
	}

	jsonData, err := json.Marshal(initRequest)
	if err != nil {
		fmt.Println("ERROR")
		return checkResult{
			name:    "MCP Server",
			status:  "fail",
			message: fmt.Sprintf("Failed to build request: %v", err),
		}
	}

	mcpURL := strings.TrimSuffix(cfg.ServerURL, "/") + "/api/mcp/rpc"
	req, err := http.NewRequest("POST", mcpURL, strings.NewReader(string(jsonData)))
	if err != nil {
		fmt.Println("FAILED")
		return checkResult{
			name:    "MCP Server",
			status:  "fail",
			message: fmt.Sprintf("Cannot create request: %v", err),
		}
	}

	req.Header.Set("Content-Type", "application/json")
	if cfg.APIKey != "" {
		req.Header.Set("X-API-Key", cfg.APIKey)
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		fmt.Println("UNREACHABLE")
		return checkResult{
			name:    "MCP Server",
			status:  "fail",
			message: fmt.Sprintf("Cannot reach MCP endpoint: %v", err),
		}
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		fmt.Println("ERROR")
		return checkResult{
			name:    "MCP Server",
			status:  "fail",
			message: fmt.Sprintf("MCP returned %d: %s", resp.StatusCode, string(body)),
		}
	}

	// Parse response
	var mcpResp struct {
		JSONRPC string `json:"jsonrpc"`
		ID      int    `json:"id"`
		Result  struct {
			ProtocolVersion string `json:"protocolVersion"`
			ServerInfo      struct {
				Name    string `json:"name"`
				Version string `json:"version"`
			} `json:"serverInfo"`
			Capabilities struct {
				Tools struct{} `json:"tools,omitempty"`
			} `json:"capabilities"`
		} `json:"result"`
		Error *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error,omitempty"`
	}

	if err := json.Unmarshal(body, &mcpResp); err != nil {
		fmt.Println("INVALID RESPONSE")
		return checkResult{
			name:    "MCP Server",
			status:  "fail",
			message: fmt.Sprintf("Invalid JSON-RPC response: %v", err),
		}
	}

	if mcpResp.Error != nil {
		fmt.Println("RPC ERROR")
		return checkResult{
			name:    "MCP Server",
			status:  "fail",
			message: fmt.Sprintf("MCP error (%d): %s", mcpResp.Error.Code, mcpResp.Error.Message),
		}
	}

	toolsRequest := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      2,
		"method":  "tools/list",
		"params":  map[string]interface{}{},
	}

	jsonData, _ = json.Marshal(toolsRequest)
	req, _ = http.NewRequest("POST", mcpURL, strings.NewReader(string(jsonData)))
	req.Header.Set("Content-Type", "application/json")
	if cfg.APIKey != "" {
		req.Header.Set("X-API-Key", cfg.APIKey)
	}

	resp, err = httpClient.Do(req)
	if err != nil {
		fmt.Println("OK (no tools)")
		return checkResult{
			name:    "MCP Server",
			status:  "warn",
			message: fmt.Sprintf("Connected but cannot list tools: %v", err),
		}
	}
	defer resp.Body.Close()

	body, _ = io.ReadAll(resp.Body)

	var toolsResp struct {
		JSONRPC string `json:"jsonrpc"`
		ID      int    `json:"id"`
		Result  struct {
			Tools []struct {
				Name        string `json:"name"`
				Description string `json:"description"`
			} `json:"tools"`
		} `json:"result"`
		Error *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error,omitempty"`
	}

	if err := json.Unmarshal(body, &toolsResp); err != nil {
		fmt.Println("OK (parse error)")
		return checkResult{
			name:    "MCP Server",
			status:  "warn",
			message: "Connected but cannot parse tools list",
		}
	}

	if toolsResp.Error != nil {
		fmt.Println("OK (tools error)")
		return checkResult{
			name:    "MCP Server",
			status:  "warn",
			message: fmt.Sprintf("Connected but tools/list failed: %s", toolsResp.Error.Message),
		}
	}

	expectedTools := map[string]bool{
		"schema_version":             true,
		"list_entity_types":          true,
		"query_entities":             true,
		"search_entities":            true,
		"get_entity_edges":           true,
		"list_template_packs":        true,
		"get_template_pack":          true,
		"get_available_templates":    true,
		"get_installed_templates":    true,
		"assign_template_pack":       true,
		"update_template_assignment": true,
		"uninstall_template_pack":    true,
		"create_template_pack":       true,
		"delete_template_pack":       true,
		"create_entity":              true,
		"create_relationship":        true,
		"update_entity":              true,
		"delete_entity":              true,
	}

	actualTools := make(map[string]bool)
	for _, tool := range toolsResp.Result.Tools {
		actualTools[tool.Name] = true
	}

	missingTools := []string{}
	for toolName := range expectedTools {
		if !actualTools[toolName] {
			missingTools = append(missingTools, toolName)
		}
	}

	extraTools := []string{}
	for toolName := range actualTools {
		if !expectedTools[toolName] {
			extraTools = append(extraTools, toolName)
		}
	}

	toolCount := len(toolsResp.Result.Tools)

	if len(missingTools) > 0 || len(extraTools) > 0 {
		fmt.Println("MISMATCH")
		msg := fmt.Sprintf("Connected with %d tools", toolCount)
		if len(missingTools) > 0 {
			msg += fmt.Sprintf(", missing %d expected tools: %v", len(missingTools), missingTools)
		}
		if len(extraTools) > 0 {
			msg += fmt.Sprintf(", found %d unexpected tools: %v", len(extraTools), extraTools)
		}
		return checkResult{
			name:    "MCP Server",
			status:  "warn",
			message: msg,
		}
	}

	fmt.Println("OK")
	return checkResult{
		name:    "MCP Server",
		status:  "pass",
		message: fmt.Sprintf("Connected (%s v%s) with %d tools", mcpResp.Result.ServerInfo.Name, mcpResp.Result.ServerInfo.Version, toolCount),
	}
}
