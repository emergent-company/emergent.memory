package cmd

import (
	"context"
	"fmt"
	"strings"

	"github.com/emergent-company/emergent/apps/server-go/pkg/sdk/apitokens"
	"github.com/emergent-company/emergent/tools/emergent-cli/internal/config"
	"github.com/spf13/cobra"
)

var tokensCmd = &cobra.Command{
	Use:   "tokens",
	Short: "Manage project API tokens",
	Long:  "Commands for managing API tokens (emt_* keys) for projects in the Emergent platform",
}

var listTokensCmd = &cobra.Command{
	Use:   "list",
	Short: "List all tokens for a project",
	Long:  "List all API tokens for the specified project",
	RunE:  runListTokens,
}

var createTokenCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a new API token",
	Long: `Create a new API token for the specified project.

The full token value is only shown once at creation time.
Make sure to copy and save it securely.

Valid scopes: schema:read, data:read, data:write`,
	RunE: runCreateToken,
}

var getTokenCmd = &cobra.Command{
	Use:   "get [token-id]",
	Short: "Get token details",
	Long:  "Get details for a specific API token by ID",
	Args:  cobra.ExactArgs(1),
	RunE:  runGetToken,
}

var revokeTokenCmd = &cobra.Command{
	Use:   "revoke [token-id]",
	Short: "Revoke an API token",
	Long:  "Permanently revoke an API token, making it unusable",
	Args:  cobra.ExactArgs(1),
	RunE:  runRevokeToken,
}

var (
	tokenProjectID string
	tokenName      string
	tokenScopes    string
)

// resolveProjectID gets the project ID from the --project-id flag or config
func resolveProjectID(cmd *cobra.Command) (string, error) {
	if tokenProjectID != "" {
		return tokenProjectID, nil
	}

	// Fall back to config / env
	configPath, _ := cmd.Flags().GetString("config")
	if configPath == "" {
		configPath = config.DiscoverPath("")
	}

	cfg, err := config.LoadWithEnv(configPath)
	if err != nil {
		return "", fmt.Errorf("failed to load config: %w", err)
	}

	if cfg.ProjectID != "" {
		return cfg.ProjectID, nil
	}

	return "", fmt.Errorf("project ID is required. Use --project-id flag, set EMERGENT_PROJECT_ID, or configure it in your config file")
}

func runListTokens(cmd *cobra.Command, args []string) error {
	projectID, err := resolveProjectID(cmd)
	if err != nil {
		return err
	}

	c, err := getClient(cmd)
	if err != nil {
		return err
	}

	result, err := c.SDK.APITokens.List(context.Background(), projectID)
	if err != nil {
		return fmt.Errorf("failed to list tokens: %w", err)
	}

	if len(result.Tokens) == 0 {
		fmt.Println("No tokens found for this project.")
		return nil
	}

	fmt.Printf("Found %d token(s):\n\n", len(result.Tokens))
	for i, t := range result.Tokens {
		fmt.Printf("%d. %s\n", i+1, t.Name)
		fmt.Printf("   ID:      %s\n", t.ID)
		fmt.Printf("   Prefix:  %s\n", t.Prefix)
		fmt.Printf("   Scopes:  %s\n", strings.Join(t.Scopes, ", "))
		fmt.Printf("   Created: %s\n", t.CreatedAt)
		if t.RevokedAt != nil {
			fmt.Printf("   Revoked: %s\n", *t.RevokedAt)
		}
		fmt.Println()
	}

	return nil
}

func runCreateToken(cmd *cobra.Command, args []string) error {
	if tokenName == "" {
		return fmt.Errorf("token name is required. Use --name flag")
	}

	projectID, err := resolveProjectID(cmd)
	if err != nil {
		return err
	}

	// Parse scopes
	scopes := []string{"data:read"}
	if tokenScopes != "" {
		scopes = strings.Split(tokenScopes, ",")
		for i := range scopes {
			scopes[i] = strings.TrimSpace(scopes[i])
		}
	}

	c, err := getClient(cmd)
	if err != nil {
		return err
	}

	req := &apitokens.CreateTokenRequest{
		Name:   tokenName,
		Scopes: scopes,
	}

	result, err := c.SDK.APITokens.Create(context.Background(), projectID, req)
	if err != nil {
		return fmt.Errorf("failed to create token: %w", err)
	}

	fmt.Println("Token created successfully!")
	fmt.Println()
	fmt.Println("============================================================")
	fmt.Println("  IMPORTANT: Save this token now. It cannot be shown again!")
	fmt.Println("============================================================")
	fmt.Println()
	fmt.Printf("  Token:   %s\n", result.Token)
	fmt.Println()
	fmt.Println("------------------------------------------------------------")
	fmt.Printf("  ID:      %s\n", result.ID)
	fmt.Printf("  Name:    %s\n", result.Name)
	fmt.Printf("  Prefix:  %s\n", result.Prefix)
	fmt.Printf("  Scopes:  %s\n", strings.Join(result.Scopes, ", "))
	fmt.Printf("  Created: %s\n", result.CreatedAt)
	fmt.Println()

	return nil
}

func runGetToken(cmd *cobra.Command, args []string) error {
	tokenID := args[0]

	projectID, err := resolveProjectID(cmd)
	if err != nil {
		return err
	}

	c, err := getClient(cmd)
	if err != nil {
		return err
	}

	token, err := c.SDK.APITokens.Get(context.Background(), projectID, tokenID)
	if err != nil {
		return fmt.Errorf("failed to get token: %w", err)
	}

	fmt.Printf("Token: %s\n", token.Name)
	fmt.Printf("  ID:      %s\n", token.ID)
	fmt.Printf("  Prefix:  %s\n", token.Prefix)
	fmt.Printf("  Scopes:  %s\n", strings.Join(token.Scopes, ", "))
	fmt.Printf("  Created: %s\n", token.CreatedAt)
	if token.RevokedAt != nil {
		fmt.Printf("  Revoked: %s\n", *token.RevokedAt)
	}

	return nil
}

func runRevokeToken(cmd *cobra.Command, args []string) error {
	tokenID := args[0]

	projectID, err := resolveProjectID(cmd)
	if err != nil {
		return err
	}

	c, err := getClient(cmd)
	if err != nil {
		return err
	}

	err = c.SDK.APITokens.Revoke(context.Background(), projectID, tokenID)
	if err != nil {
		return fmt.Errorf("failed to revoke token: %w", err)
	}

	fmt.Printf("Token %s has been revoked successfully.\n", tokenID)

	return nil
}

func init() {
	// Persistent flag for all token subcommands
	tokensCmd.PersistentFlags().StringVar(&tokenProjectID, "project-id", "", "Project ID (auto-detected from config/env if not specified)")

	// Create token flags
	createTokenCmd.Flags().StringVar(&tokenName, "name", "", "Token name (required)")
	createTokenCmd.Flags().StringVar(&tokenScopes, "scopes", "", "Comma-separated scopes (default: data:read). Valid: schema:read, data:read, data:write")
	_ = createTokenCmd.MarkFlagRequired("name")

	// Register subcommands
	tokensCmd.AddCommand(listTokensCmd)
	tokensCmd.AddCommand(createTokenCmd)
	tokensCmd.AddCommand(getTokenCmd)
	tokensCmd.AddCommand(revokeTokenCmd)
	rootCmd.AddCommand(tokensCmd)
}
