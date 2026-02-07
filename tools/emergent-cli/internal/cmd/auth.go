package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/emergent-company/emergent/tools/emergent-cli/internal/auth"
	"github.com/emergent-company/emergent/tools/emergent-cli/internal/config"
	"github.com/spf13/cobra"
)

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
	// First, check for API key authentication (standalone mode)
	var configPath string
	configPath, _ = cmd.Flags().GetString("config")
	if configPath == "" {
		configPath = config.DiscoverPath("")
	}

	cfg, err := config.LoadWithEnv(configPath)
	if err == nil && cfg != nil && cfg.APIKey != "" {
		fmt.Println("Authentication Status:")
		fmt.Println()
		fmt.Println("  Mode:        API Key (Standalone)")
		fmt.Printf("  Server:      %s\n", cfg.ServerURL)
		fmt.Printf("  API Key:     %s...%s\n", cfg.APIKey[:8], cfg.APIKey[len(cfg.APIKey)-4:])
		fmt.Println("  Status:      ✓ Configured")
		fmt.Println()
		fmt.Println("Using API key authentication. No login required.")
		return nil
	}

	// Fall back to OAuth credentials check
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

func init() {
	rootCmd.AddCommand(loginCmd)
	rootCmd.AddCommand(statusCmd)
}
