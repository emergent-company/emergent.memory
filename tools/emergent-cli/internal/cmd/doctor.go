package cmd

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/emergent-company/emergent/tools/emergent-cli/internal/auth"
	"github.com/emergent-company/emergent/tools/emergent-cli/internal/client"
	"github.com/emergent-company/emergent/tools/emergent-cli/internal/config"
	"github.com/spf13/cobra"
)

var doctorCmd = &cobra.Command{
	Use:   "doctor",
	Short: "Check system health and configuration",
	Long: `Run diagnostic checks on your Emergent CLI installation.

This command verifies:
- Configuration file exists and is valid
- Server connectivity
- Authentication status
- API functionality`,
	RunE: runDoctor,
}

type checkResult struct {
	name    string
	status  string
	message string
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

	if cfg != nil && cfg.ServerURL != "" {
		results = append(results, checkServerConnectivity(cfg.ServerURL))
		results = append(results, checkAuth(cfg, configPath))
		results = append(results, checkAPI(cfg))
	}

	fmt.Println()
	fmt.Println("Summary")
	fmt.Println("-------")

	passed := 0
	failed := 0
	warned := 0

	for _, r := range results {
		var icon string
		switch r.status {
		case "pass":
			icon = "✓"
			passed++
		case "fail":
			icon = "✗"
			failed++
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

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		fmt.Println("ERROR")
		return checkResult{
			name:    "API Access",
			status:  "fail",
			message: fmt.Sprintf("API returned %d: %s", resp.StatusCode, string(body)),
		}
	}

	fmt.Println("OK")
	return checkResult{
		name:    "API Access",
		status:  "pass",
		message: "Successfully accessed API",
	}
}

func init() {
	rootCmd.AddCommand(doctorCmd)
}
