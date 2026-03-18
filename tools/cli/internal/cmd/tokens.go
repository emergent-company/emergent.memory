package cmd

import (
	"context"
	"fmt"
	"strings"

	"github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/apitokens"
	"github.com/spf13/cobra"
)

var tokensCmd = &cobra.Command{
	Use:     "tokens",
	Short:   "Manage API tokens",
	Long:    "Commands for managing API tokens (emt_* keys). Tokens can be account-level (cross-project) or project-scoped.",
	GroupID: "account",
}

var listTokensCmd = &cobra.Command{
	Use:   "list",
	Short: "List API tokens",
	Long: `List API tokens and their details.

Without --project, lists account-level tokens. With --project, lists tokens
for the specified project. Each token entry prints: Name, ID, Prefix, Type
(account or project), Scopes, Created timestamp, and Revoked timestamp (if
applicable). For project tokens, the full plaintext token value is also fetched
and displayed — treat this output as sensitive.`,
	RunE: runListTokens,
}

var createTokenCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a new API token",
	Long: `Create a new API token.

Without --project, creates an account-level token usable across all projects.
With --project, creates a project-scoped token.

On success, prints the full plaintext Token value prominently (this is the only
time the full token is shown — save it immediately), followed by ID, Name, Type,
Prefix, Scopes, and Created timestamp.

Valid scopes: schema:read, data:read, data:write, agents:read, agents:write, projects:read, projects:write`,
	RunE: runCreateToken,
}

var getTokenCmd = &cobra.Command{
	Use:   "get [token-id]",
	Short: "Get token details",
	Long: `Get details for a specific API token by its ID.

Use --project to specify a project-scoped token; without it, looks up an
account-level token.`,
	Args: cobra.ExactArgs(1),
	RunE: runGetToken,
}

var revokeTokenCmd = &cobra.Command{
	Use:   "revoke [token-id]",
	Short: "Revoke an API token",
	Long:  "Permanently revoke an API token, making it unusable. Without --project, revokes an account-level token.",
	Args:  cobra.ExactArgs(1),
	RunE:  runRevokeToken,
}

var (
	tokenProjectID string
	tokenName      string
	tokenScopes    string
	tokenListLimit int
	tokenListPage  int
)

func runListTokens(cmd *cobra.Command, args []string) error {
	// If --project not provided, list account-level tokens (requires account credentials)
	if tokenProjectID == "" {
		c, err := getAccountClient(cmd)
		if err != nil {
			return err
		}

		result, err := c.SDK.APITokens.ListAccountTokens(context.Background())
		if err != nil {
			return fmt.Errorf("failed to list account tokens: %w", err)
		}

		if len(result.Tokens) == 0 {
			fmt.Println("No account-level tokens found.")
			return nil
		}

		total := len(result.Tokens)
		tokens := paginate(result.Tokens, tokenListLimit, tokenListPage)

		if compact {
			for _, t := range tokens {
				fmt.Printf("%-40s  %s\n", t.Name, t.ID)
			}
			return nil
		}

		if h := paginationHeader(total, tokenListLimit, tokenListPage); h != "" {
			fmt.Printf("%s:\n\n", h)
		} else {
			fmt.Printf("Found %d account-level token(s):\n\n", total)
		}
		for i, t := range tokens {
			fmt.Printf("%d. %s\n", i+1, t.Name)
			fmt.Printf("   ID:      %s\n", t.ID)
			fmt.Printf("   Prefix:  %s\n", t.Prefix)
			fmt.Printf("   Type:    account\n")
			fmt.Printf("   Scopes:  %s\n", strings.Join(t.Scopes, ", "))
			fmt.Printf("   Created: %s\n", t.CreatedAt)
			if t.RevokedAt != nil {
				fmt.Printf("   Revoked: %s\n", *t.RevokedAt)
			}
			fmt.Println()
		}
		return nil
	}

	// --project provided: list project-scoped tokens
	c, err := getClient(cmd)
	if err != nil {
		return err
	}

	projectID, err := resolveProjectContext(cmd, tokenProjectID)
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

	total := len(result.Tokens)
	tokens := paginate(result.Tokens, tokenListLimit, tokenListPage)

	if compact {
		for _, t := range tokens {
			fmt.Printf("%-40s  %s\n", t.Name, t.ID)
		}
		return nil
	}

	if h := paginationHeader(total, tokenListLimit, tokenListPage); h != "" {
		fmt.Printf("%s:\n\n", h)
	} else {
		fmt.Printf("Found %d token(s):\n\n", total)
	}
	for i, t := range tokens {
		fmt.Printf("%d. %s\n", i+1, t.Name)
		fmt.Printf("   ID:      %s\n", t.ID)
		fmt.Printf("   Prefix:  %s\n", t.Prefix)

		// Fetch full token value via individual GET
		fullToken, getErr := c.SDK.APITokens.Get(context.Background(), projectID, t.ID)
		if getErr == nil && fullToken.Token != "" {
			fmt.Printf("   Token:   %s\n", fullToken.Token)
		}

		fmt.Printf("   Type:    project\n")
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

	// Parse scopes
	scopes := []string{"data:read"}
	if tokenScopes != "" {
		scopes = strings.Split(tokenScopes, ",")
		for i := range scopes {
			scopes[i] = strings.TrimSpace(scopes[i])
		}
	}

	req := &apitokens.CreateTokenRequest{
		Name:   tokenName,
		Scopes: scopes,
	}

	// If --project not provided, create an account-level token (requires account credentials)
	if tokenProjectID == "" {
		c, err := getAccountClient(cmd)
		if err != nil {
			return err
		}

		result, err := c.SDK.APITokens.CreateAccountToken(context.Background(), req)
		if err != nil {
			return fmt.Errorf("failed to create account token: %w", err)
		}

		fmt.Println("Account token created successfully!")
		fmt.Println()
		fmt.Printf("  Token:   %s\n", result.Token)
		fmt.Println()
		fmt.Println("------------------------------------------------------------")
		fmt.Printf("  ID:      %s\n", result.ID)
		fmt.Printf("  Name:    %s\n", result.Name)
		fmt.Printf("  Type:    account\n")
		fmt.Printf("  Prefix:  %s\n", result.Prefix)
		fmt.Printf("  Scopes:  %s\n", strings.Join(result.Scopes, ", "))
		fmt.Printf("  Created: %s\n", result.CreatedAt)
		fmt.Println()

		return nil
	}

	// --project provided: create a project-scoped token
	c, err := getClient(cmd)
	if err != nil {
		return err
	}

	projectID, err := resolveProjectContext(cmd, tokenProjectID)
	if err != nil {
		return err
	}

	result, err := c.SDK.APITokens.Create(context.Background(), projectID, req)
	if err != nil {
		return fmt.Errorf("failed to create token: %w", err)
	}

	fmt.Println("Token created successfully!")
	fmt.Println()
	fmt.Printf("  Token:   %s\n", result.Token)
	fmt.Println()
	fmt.Println("------------------------------------------------------------")
	fmt.Printf("  ID:      %s\n", result.ID)
	fmt.Printf("  Name:    %s\n", result.Name)
	fmt.Printf("  Type:    project\n")
	fmt.Printf("  Prefix:  %s\n", result.Prefix)
	fmt.Printf("  Scopes:  %s\n", strings.Join(result.Scopes, ", "))
	fmt.Printf("  Created: %s\n", result.CreatedAt)
	fmt.Println()
	fmt.Println("  Retrieve this token later: memory tokens get " + result.ID)
	fmt.Println()

	return nil
}

func runGetToken(cmd *cobra.Command, args []string) error {
	tokenID := args[0]

	// If --project not provided, look up an account-level token
	if tokenProjectID == "" {
		c, err := getAccountClient(cmd)
		if err != nil {
			return err
		}

		token, err := c.SDK.APITokens.GetAccountToken(context.Background(), tokenID)
		if err != nil {
			return fmt.Errorf("failed to get account token: %w", err)
		}

		fmt.Printf("Token: %s\n", token.Name)
		fmt.Printf("  ID:      %s\n", token.ID)
		fmt.Printf("  Prefix:  %s\n", token.Prefix)
		fmt.Printf("  Type:    account\n")
		if token.Token != "" {
			fmt.Println()
			fmt.Println("  ------------------------------------------------------------")
			fmt.Printf("  Token:   %s\n", token.Token)
			fmt.Println("  ------------------------------------------------------------")
		}
		fmt.Printf("  Scopes:  %s\n", strings.Join(token.Scopes, ", "))
		fmt.Printf("  Created: %s\n", token.CreatedAt)
		if token.RevokedAt != nil {
			fmt.Printf("  Revoked: %s\n", *token.RevokedAt)
		}
		return nil
	}

	// --project provided: look up a project-scoped token
	c, err := getClient(cmd)
	if err != nil {
		return err
	}

	projectID, err := resolveProjectContext(cmd, tokenProjectID)
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
	fmt.Printf("  Type:    project\n")
	if token.Token != "" {
		fmt.Println()
		fmt.Println("  ------------------------------------------------------------")
		fmt.Printf("  Token:   %s\n", token.Token)
		fmt.Println("  ------------------------------------------------------------")
	}
	fmt.Printf("  Scopes:  %s\n", strings.Join(token.Scopes, ", "))
	fmt.Printf("  Created: %s\n", token.CreatedAt)
	if token.RevokedAt != nil {
		fmt.Printf("  Revoked: %s\n", *token.RevokedAt)
	}

	return nil
}

func runRevokeToken(cmd *cobra.Command, args []string) error {
	tokenID := args[0]

	// If --project not provided, revoke an account-level token
	if tokenProjectID == "" {
		c, err := getAccountClient(cmd)
		if err != nil {
			return err
		}

		err = c.SDK.APITokens.RevokeAccountToken(context.Background(), tokenID)
		if err != nil {
			return fmt.Errorf("failed to revoke account token: %w", err)
		}

		fmt.Printf("Account token %s has been revoked successfully.\n", tokenID)
		return nil
	}

	// --project provided: revoke a project-scoped token
	c, err := getClient(cmd)
	if err != nil {
		return err
	}

	projectID, err := resolveProjectContext(cmd, tokenProjectID)
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
	// Persistent flag for all token subcommands (optional — omit for account-level tokens)
	tokensCmd.PersistentFlags().StringVar(&tokenProjectID, "project", "", "Project name or ID (omit for account-level tokens)")

	// List pagination flags
	listTokensCmd.Flags().IntVar(&tokenListLimit, "limit", 0, "Maximum number of tokens to show (0 = all)")
	listTokensCmd.Flags().IntVar(&tokenListPage, "page", 1, "Page number (1-based, used with --limit)")

	// Create token flags
	createTokenCmd.Flags().StringVar(&tokenName, "name", "", "Token name (required)")
	createTokenCmd.Flags().StringVar(&tokenScopes, "scopes", "", "Comma-separated scopes (default: data:read). Valid: schema:read, data:read, data:write, agents:read, agents:write, projects:read, projects:write")
	_ = createTokenCmd.MarkFlagRequired("name")

	// Register subcommands
	tokensCmd.AddCommand(listTokensCmd)
	tokensCmd.AddCommand(createTokenCmd)
	tokensCmd.AddCommand(getTokenCmd)
	tokensCmd.AddCommand(revokeTokenCmd)
	rootCmd.AddCommand(tokensCmd)
}
