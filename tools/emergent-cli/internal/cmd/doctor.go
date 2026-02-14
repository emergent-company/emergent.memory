package cmd

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/emergent-company/emergent/tools/emergent-cli/internal/auth"
	"github.com/emergent-company/emergent/tools/emergent-cli/internal/client"
	"github.com/emergent-company/emergent/tools/emergent-cli/internal/config"
	"github.com/spf13/cobra"
)

var doctorFlags struct {
	fix   bool
	debug bool
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
	doctorCmd.Flags().BoolVar(&doctorFlags.debug, "debug", false, "Show detailed debug information (copyable for bug reports)")
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

	homeDir, _ := os.UserHomeDir()
	installDir := filepath.Join(homeDir, ".emergent")
	isStandalone := isStandaloneInstallation(installDir)

	if doctorFlags.debug {
		printSystemInfo(installDir, isStandalone)
	}

	// For standalone installs without config.yaml, synthesize config from .env.local
	cfg, _ := config.LoadWithEnv(configPath)
	if isStandalone {
		cfg = enrichConfigFromEnvLocal(cfg, installDir)
	}

	results = append(results, checkConfigStandalone(configPath, isStandalone, cfg))
	results = append(results, checkShellPath(installDir))

	if isStandalone {
		results = append(results, checkGoogleAPIKey(installDir))
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
	if failed > 0 {
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

// enrichConfigFromEnvLocal reads .env.local from a standalone installation and
// fills in missing config values (server URL, API key).
func enrichConfigFromEnvLocal(cfg *config.Config, installDir string) *config.Config {
	if cfg == nil {
		cfg = &config.Config{}
	}

	envPath := filepath.Join(installDir, "config", ".env.local")
	content, err := os.ReadFile(envPath)
	if err != nil {
		return cfg
	}

	envVars := parseEnvFile(string(content))

	if cfg.APIKey == "" {
		if key, ok := envVars["STANDALONE_API_KEY"]; ok && key != "" {
			cfg.APIKey = key
		}
	}

	if cfg.ServerURL == "" || cfg.ServerURL == "http://localhost:3002" {
		port := "3002"
		if p, ok := envVars["SERVER_PORT"]; ok && p != "" {
			port = p
		}
		cfg.ServerURL = fmt.Sprintf("http://localhost:%s", port)
	}

	return cfg
}

// parseEnvFile parses a .env file into a map of key=value pairs.
func parseEnvFile(content string) map[string]string {
	result := make(map[string]string)
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if idx := strings.Index(line, "="); idx > 0 {
			key := strings.TrimSpace(line[:idx])
			value := strings.TrimSpace(line[idx+1:])
			result[key] = value
		}
	}
	return result
}

// checkConfigStandalone checks configuration, understanding that standalone
// installs may not have config.yaml but can derive config from .env.local.
func checkConfigStandalone(configPath string, isStandalone bool, cfg *config.Config) checkResult {
	fmt.Print("Checking configuration... ")

	// If config.yaml exists, validate it normally
	if _, err := os.Stat(configPath); err == nil {
		fileCfg, err := config.LoadWithEnv(configPath)
		if err != nil {
			fmt.Println("ERROR")
			return checkResult{
				name:    "Configuration",
				status:  "fail",
				message: fmt.Sprintf("Failed to load config: %v", err),
			}
		}

		if fileCfg.ServerURL == "" {
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

	// config.yaml doesn't exist — check if standalone can fill in
	if isStandalone && cfg != nil && cfg.ServerURL != "" && cfg.APIKey != "" {
		fmt.Println("OK (standalone)")
		return checkResult{
			name:    "Configuration",
			status:  "pass",
			message: fmt.Sprintf("No config.yaml (using .env.local: server=%s)", cfg.ServerURL),
		}
	}

	// Neither config.yaml nor usable standalone config
	fmt.Println("NOT FOUND")
	msg := fmt.Sprintf("Config file not found at %s", configPath)
	if isStandalone {
		msg += "\n         To fix: run 'emergent install' to generate config.yaml"
		msg += "\n         Or create it manually:"
		msg += fmt.Sprintf("\n           echo 'server_url: http://localhost:3002' > %s", configPath)
		msg += fmt.Sprintf("\n           echo 'api_key: <your-api-key>' >> %s", configPath)
	} else {
		msg += "\n         To fix: create the config file with your server URL:"
		msg += fmt.Sprintf("\n           mkdir -p %s", filepath.Dir(configPath))
		msg += fmt.Sprintf("\n           echo 'server_url: https://your-server.example.com' > %s", configPath)
	}
	return checkResult{
		name:    "Configuration",
		status:  "fail",
		message: msg,
	}
}

// getShellConfigFiles returns the shell config files to check for PATH setup,
// based on the user's current shell. Returns (primaryFile, fallbackFiles).
func getShellConfigFiles() (primary string, fallbacks []string) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", nil
	}

	shell := os.Getenv("SHELL")
	shellBase := filepath.Base(shell)

	switch shellBase {
	case "zsh":
		primary = filepath.Join(homeDir, ".zshrc")
		fallbacks = []string{
			filepath.Join(homeDir, ".zshenv"),
			filepath.Join(homeDir, ".zprofile"),
		}
	case "bash":
		primary = filepath.Join(homeDir, ".bashrc")
		fallbacks = []string{
			filepath.Join(homeDir, ".bash_profile"),
			filepath.Join(homeDir, ".profile"),
		}
	case "fish":
		primary = filepath.Join(homeDir, ".config", "fish", "config.fish")
	default:
		// Unknown shell — check common files
		primary = filepath.Join(homeDir, ".profile")
		fallbacks = []string{
			filepath.Join(homeDir, ".bashrc"),
			filepath.Join(homeDir, ".zshrc"),
		}
	}
	return primary, fallbacks
}

// fileContainsEmergentPath checks if a file contains the emergent bin PATH entry.
func fileContainsEmergentPath(filePath string) bool {
	content, err := os.ReadFile(filePath)
	if err != nil {
		return false
	}
	return strings.Contains(string(content), ".emergent/bin")
}

func checkShellPath(installDir string) checkResult {
	fmt.Print("Checking shell PATH... ")

	binDir := filepath.Join(installDir, "bin")
	shell := os.Getenv("SHELL")
	shellName := filepath.Base(shell)

	// Check 1: is ~/.emergent/bin in the current runtime PATH?
	pathEnv := os.Getenv("PATH")
	inCurrentPath := strings.Contains(pathEnv, binDir)

	// Check 2: is it configured in the user's shell config file?
	primary, fallbacks := getShellConfigFiles()
	configuredIn := ""

	if primary != "" && fileContainsEmergentPath(primary) {
		configuredIn = primary
	}
	if configuredIn == "" {
		for _, fb := range fallbacks {
			if fileContainsEmergentPath(fb) {
				configuredIn = fb
				break
			}
		}
	}

	// Shorten paths for display
	homeDir, _ := os.UserHomeDir()
	shortPath := func(p string) string {
		if homeDir != "" && strings.HasPrefix(p, homeDir) {
			return "~" + p[len(homeDir):]
		}
		return p
	}

	if configuredIn != "" {
		fmt.Println("OK")
		msg := fmt.Sprintf("Configured in %s (shell: %s)", shortPath(configuredIn), shellName)
		if !inCurrentPath {
			msg += "\n         Note: not active in current session — restart your terminal or run: source " + shortPath(configuredIn)
		}
		return checkResult{
			name:    "Shell PATH",
			status:  "pass",
			message: msg,
		}
	}

	// Not configured in any shell config file
	if inCurrentPath {
		// It's in the current PATH but not persisted
		fmt.Println("WARN")
		return checkResult{
			name:    "Shell PATH",
			status:  "warn",
			message: fmt.Sprintf("~/.emergent/bin is in PATH but not persisted in shell config (%s)", shellName),
			fixable: true,
			fixType: "fix_shell_path",
		}
	}

	fmt.Println("NOT CONFIGURED")
	targetFile := shortPath(primary)
	if primary == "" {
		targetFile = "shell config"
	}
	return checkResult{
		name:   "Shell PATH",
		status: "fail",
		message: fmt.Sprintf("~/.emergent/bin is not in PATH (shell: %s)\n"+
			"         Add to %s: export PATH=\"$HOME/.emergent/bin:$PATH\"",
			shellName, targetFile),
		fixable: true,
		fixType: "fix_shell_path",
	}
}

func fixShellPath(installDir string) error {
	fmt.Println("Fixing shell PATH configuration...")

	primary, fallbacks := getShellConfigFiles()
	pathLine := `export PATH="$HOME/.emergent/bin:$PATH"`

	// Try primary first, then fallbacks
	candidates := []string{}
	if primary != "" {
		candidates = append(candidates, primary)
	}
	candidates = append(candidates, fallbacks...)

	homeDir, _ := os.UserHomeDir()
	shortPath := func(p string) string {
		if homeDir != "" && strings.HasPrefix(p, homeDir) {
			return "~" + p[len(homeDir):]
		}
		return p
	}

	for _, file := range candidates {
		// Skip if already configured
		if fileContainsEmergentPath(file) {
			fmt.Printf("Already configured in %s\n", shortPath(file))
			return nil
		}

		// Try to append
		f, err := os.OpenFile(file, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
		if err != nil {
			continue // Try next candidate
		}
		_, err = fmt.Fprintf(f, "\n# Emergent CLI\n%s\n", pathLine)
		f.Close()
		if err != nil {
			continue
		}

		fmt.Printf("Added PATH to %s\n", shortPath(file))
		fmt.Printf("Restart your terminal or run: source %s\n", shortPath(file))
		return nil
	}

	return fmt.Errorf("could not write to any shell config file — add manually: %s", pathLine)
}

func checkGoogleAPIKey(installDir string) checkResult {
	fmt.Print("Checking Google API key... ")

	envPath := filepath.Join(installDir, "config", ".env.local")
	content, err := os.ReadFile(envPath)
	if err != nil {
		fmt.Println("SKIPPED")
		return checkResult{
			name:    "Google API Key",
			status:  "warn",
			message: "Could not read configuration file",
		}
	}

	for _, line := range strings.Split(string(content), "\n") {
		if strings.HasPrefix(line, "GOOGLE_API_KEY=") {
			value := strings.TrimPrefix(line, "GOOGLE_API_KEY=")
			value = strings.TrimSpace(value)
			if value != "" {
				fmt.Println("OK")
				return checkResult{
					name:    "Google API Key",
					status:  "pass",
					message: fmt.Sprintf("Configured (%s...)", value[:min(8, len(value))]),
				}
			}
		}
	}

	fmt.Println("NOT SET")
	return checkResult{
		name:   "Google API Key",
		status: "warn",
		message: "Google API key is not configured. It is needed for AI-powered features " +
			"including semantic search, document analysis, and entity extraction.\n" +
			"         To get a key: visit https://aistudio.google.com/apikey\n" +
			"         To set it:   emergent config set google_api_key YOUR_KEY",
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

	c, err := client.New(cfg)
	if err != nil {
		fmt.Println("FAILED")
		return checkResult{
			name:    "API Access",
			status:  "fail",
			message: fmt.Sprintf("Failed to create client: %v", err),
		}
	}

	_, err = c.SDK.Projects.List(context.Background(), nil)
	if err != nil {
		fmt.Println("FAILED")
		return checkResult{
			name:    "API Access",
			status:  "fail",
			message: fmt.Sprintf("Request failed: %v", err),
		}
	}

	fmt.Println("OK")
	return checkResult{
		name:    "API Access",
		status:  "pass",
		message: "Successfully accessed API",
	}
}

func isStandaloneInstallation(installDir string) bool {
	composePath := filepath.Join(installDir, "docker", "docker-compose.yml")
	_, err := os.Stat(composePath)
	return err == nil
}

func checkDockerContainers(installDir string) checkResult {
	fmt.Print("Checking Docker containers... ")

	// First check if docker is available in PATH
	if _, err := exec.LookPath("docker"); err != nil {
		fmt.Println("NOT FOUND")
		return checkResult{
			name:   "Docker Containers",
			status: "warn",
			message: "Docker not found in PATH.\n" +
				"         If Docker Desktop is installed, ensure it's running and CLI tools are enabled.\n" +
				"         On macOS: open Docker Desktop, go to Settings > Advanced > enable system PATH.\n" +
				"         Or add manually: export PATH=\"/usr/local/bin:$PATH\"",
		}
	}

	// Check if containers are actually running via docker compose ps
	running, stopped := getContainerStatus(installDir)

	installedVersion := getInstalledVersionFromFile(installDir)
	containerVersion := getContainerVersion("emergent-server")

	if installedVersion != "" && containerVersion != "" && installedVersion != containerVersion {
		fmt.Println("VERSION MISMATCH")
		return checkResult{
			name:    "Docker Containers",
			status:  "warn",
			message: fmt.Sprintf("Version mismatch detected. Installed: %s, Container: %s. Run 'emergent upgrade' to update.", installedVersion, containerVersion),
		}
	}

	// If server container is running, that's a pass — check logs only for diagnostics
	if contains(running, "server") {
		fmt.Println("OK")
		msg := fmt.Sprintf("Server container is running (%d/%d containers up)", len(running), len(running)+len(stopped))
		if len(stopped) > 0 {
			msg += fmt.Sprintf("\n         Stopped: %s", strings.Join(stopped, ", "))
		}
		return checkResult{
			name:    "Docker Containers",
			status:  "pass",
			message: msg,
		}
	}

	// Server not running — inspect logs for diagnostics
	serverLogs, err := getDockerLogs(installDir, "server", 50)
	if err != nil {
		fmt.Println("NOT RUNNING")
		return checkResult{
			name:    "Docker Containers",
			status:  "fail",
			message: "Server container is not running.\n         To start: emergent ctl restart",
			fixable: true,
			fixType: "restart_containers",
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
			message: "Database connection refused. Containers may not be running.\n         To start: emergent ctl restart",
			fixable: true,
			fixType: "restart_containers",
		}
	}

	fmt.Println("NOT RUNNING")
	return checkResult{
		name:    "Docker Containers",
		status:  "fail",
		message: "Server container is not running.\n         To start: emergent ctl restart\n         Check logs: docker logs emergent-server",
		fixable: true,
		fixType: "restart_containers",
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

// getContainerStatus uses docker compose ps to get running and stopped service names.
func getContainerStatus(installDir string) (running []string, stopped []string) {
	composePath := filepath.Join(installDir, "docker", "docker-compose.yml")
	envPath := filepath.Join(installDir, "config", ".env.local")

	cmd := exec.Command("docker", "compose", "-f", composePath, "--env-file", envPath,
		"ps", "--format", "{{.Service}}\t{{.State}}")
	output, err := cmd.Output()
	if err != nil {
		return nil, nil
	}

	for _, line := range strings.Split(strings.TrimSpace(string(output)), "\n") {
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "\t", 2)
		if len(parts) != 2 {
			continue
		}
		service := parts[0]
		state := strings.ToLower(parts[1])
		if state == "running" {
			running = append(running, service)
		} else {
			stopped = append(stopped, fmt.Sprintf("%s (%s)", service, state))
		}
	}
	return running, stopped
}

func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}

func attemptFix(r checkResult, installDir string) error {
	switch r.fixType {
	case "db_password_reset":
		return fixDatabasePassword(installDir)
	case "restart_containers":
		return restartContainers(installDir)
	case "fix_shell_path":
		return fixShellPath(installDir)
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
		"hybrid_search":              true,
		"semantic_search":            true,
		"find_similar":               true,
		"traverse_graph":             true,
		"list_relationships":         true,
		"update_relationship":        true,
		"delete_relationship":        true,
		"restore_entity":             true,
		"batch_create_entities":      true,
		"batch_create_relationships": true,
		"list_tags":                  true,
		"preview_schema_migration":   true,
		"list_migration_archives":    true,
		"get_migration_archive":      true,
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

func getInstalledVersionFromFile(installDir string) string {
	versionPath := filepath.Join(installDir, "version")
	content, err := os.ReadFile(versionPath)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(content))
}

func getContainerVersion(containerName string) string {
	cmd := exec.Command("docker", "inspect", "--format", "{{.Config.Image}}", containerName)
	output, err := cmd.Output()
	if err != nil {
		return ""
	}

	imageFull := strings.TrimSpace(string(output))
	parts := strings.Split(imageFull, ":")
	if len(parts) < 2 {
		return ""
	}

	version := parts[len(parts)-1]
	if !strings.HasPrefix(version, "v") {
		version = "v" + version
	}
	return version
}

func printSystemInfo(installDir string, isStandalone bool) {
	fmt.Println("System Information:")
	fmt.Printf("  CLI Version:        %s\n", getInstalledVersionFromFile(installDir))
	fmt.Printf("  OS/Arch:            %s/%s\n", runtime.GOOS, runtime.GOARCH)
	fmt.Printf("  Installation:       ")
	if isStandalone {
		fmt.Printf("standalone (%s)\n", installDir)
	} else {
		fmt.Println("remote")
	}

	if isStandalone {
		fmt.Println("\nContainer Versions:")
		containers := []string{"emergent-server", "emergent-db", "emergent-minio", "emergent-kreuzberg"}
		for _, name := range containers {
			version := getContainerVersion(name)
			if version == "" {
				version = "not running"
			}
			fmt.Printf("  %-20s %s\n", name+":", version)
		}
	}

	fmt.Println("\n" + strings.Repeat("=", 60))
	fmt.Println()
}
