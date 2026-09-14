package cmd

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/apitokens"
	"github.com/emergent-company/emergent.memory/tools/cli/internal/client"
	"github.com/spf13/cobra"
)

var uuidRegexp = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

// tokenScope is a single API token scope with a human-readable description.
type tokenScope struct {
	Name        string
	Description string
}

// tokenScopeGroups lists every valid API token scope, grouped for display.
// Keep in sync with ValidApiTokenScopes in apps/server/domain/apitoken/entity.go.
var tokenScopeGroups = []struct {
	Title  string
	Scopes []tokenScope
}{
	{
		Title: "Coarse-grained (legacy)",
		Scopes: []tokenScope{
			{"schema:read", "Read schema definitions"},
			{"schema:write", "Install, uninstall and modify schemas (also grants schema:migrate)"},
			{"data:read", "Read documents, chunks, graph objects, search and journal"},
			{"data:write", "Write documents, chunks, graph objects, ingest and extraction"},
			{"agents:read", "Read agents and use chat"},
			{"agents:write", "Manage agents (also grants chat:admin)"},
			{"projects:read", "Read projects"},
			{"projects:write", "Create and manage projects"},
			{"chat:use", "Use chat"},
		},
	},
	{
		Title: "Fine-grained (MCP)",
		Scopes: []tokenScope{
			{"graph:read", "Read graph objects and relationships"},
			{"graph:write", "Create, update and delete graph objects and relationships"},
			{"schema:migrate", "Run schema migrations"},
			{"branches:read", "Read graph branches"},
			{"branches:write", "Create, merge and delete branches"},
			{"search", "Semantic and hybrid search"},
			{"journal:read", "Read the project journal"},
			{"journal:write", "Write to the project journal"},
			{"skills:read", "Read skills"},
			{"skills:write", "Manage skills"},
			{"documents:read", "Read documents"},
			{"documents:write", "Write documents"},
		},
	},
	{
		Title: "Admin",
		Scopes: []tokenScope{
			{"admin", "MCP admin tools (project create, tokens, providers, traces, embeddings)"},
			{"admin:all", "Full account admin - every scope (requires org admin or superadmin)"},
		},
	},
}

// resolveTokenIDForProject resolves a token name or ID to an ID for project-scoped tokens.
// If nameOrID looks like a UUID, it is returned as-is. Otherwise, tokens are listed and
// the first active token matching the name is returned.
func resolveTokenIDForProject(ctx context.Context, c *apitokens.Client, projectID, nameOrID string) (string, error) {
	if uuidRegexp.MatchString(nameOrID) {
		return nameOrID, nil
	}
	result, err := c.List(ctx, projectID)
	if err != nil {
		return "", fmt.Errorf("failed to list tokens: %w", err)
	}
	for _, t := range result.Tokens {
		if t.Name == nameOrID && t.RevokedAt == nil {
			return t.ID, nil
		}
	}
	return "", fmt.Errorf("no active project token named %q found", nameOrID)
}

// resolveTokenIDForAccount resolves a token name or ID to an ID for account-level tokens.
func resolveTokenIDForAccount(ctx context.Context, c *apitokens.Client, nameOrID string) (string, error) {
	if uuidRegexp.MatchString(nameOrID) {
		return nameOrID, nil
	}
	result, err := c.ListAccountTokens(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to list account tokens: %w", err)
	}
	for _, t := range result.Tokens {
		if t.Name == nameOrID && t.RevokedAt == nil {
			return t.ID, nil
		}
	}
	return "", fmt.Errorf("no active account token named %q found", nameOrID)
}

// resolveTokenArgOrPick resolves a token ID from args[0], or, when args is
// empty and stdin is a terminal, lists active tokens and shows an interactive
// picker. account selects account-level vs project-scoped tokens. Returns the
// resolved token ID and a display name.
func resolveTokenArgOrPick(cmd *cobra.Command, c *client.Client, args []string, account bool, projectID string) (id, name string, err error) {
	if len(args) > 0 && args[0] != "" {
		var tokenID string
		if account {
			tokenID, err = resolveTokenIDForAccount(context.Background(), c.SDK.APITokens, args[0])
		} else {
			tokenID, err = resolveTokenIDForProject(context.Background(), c.SDK.APITokens, projectID, args[0])
		}
		if err != nil {
			return "", "", err
		}
		return tokenID, args[0], nil
	}

	if isNonInteractive() {
		return "", "", fmt.Errorf("token ID or name is required — pass one or run interactively to pick from a list")
	}

	var tokens []apitokens.APIToken
	if account {
		result, err := c.SDK.APITokens.ListAccountTokens(context.Background())
		if err != nil {
			return "", "", fmt.Errorf("failed to list account tokens: %w", err)
		}
		tokens = result.Tokens
	} else {
		result, err := c.SDK.APITokens.List(context.Background(), projectID)
		if err != nil {
			return "", "", fmt.Errorf("failed to list tokens: %w", err)
		}
		tokens = result.Tokens
	}

	items := make([]PickerItem, 0, len(tokens))
	for _, t := range tokens {
		if t.RevokedAt != nil {
			continue
		}
		label := t.Name
		if t.Prefix != "" {
			label = t.Name + "  " + t.Prefix
		}
		items = append(items, PickerItem{ID: t.ID, Name: label})
	}
	if len(items) == 0 {
		return "", "", fmt.Errorf("no active tokens found")
	}

	pickedID, pickedName, err := promptResourcePicker("Select a token", items)
	if err != nil {
		return "", "", err
	}
	if pickedID == "" {
		return "", "", fmt.Errorf("token ID is required")
	}
	return pickedID, pickedName, nil
}

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

var scopesCmd = &cobra.Command{
	Use:   "scopes",
	Short: "List available API token scopes",
	Long: `List all valid API token scopes with descriptions.

For full account admin access, create a token with --scopes all (grants admin:all).`,
	RunE: runListScopes,
}

func runListScopes(cmd *cobra.Command, args []string) error {
	for _, group := range tokenScopeGroups {
		fmt.Printf("\n%s\n", group.Title)
		for _, s := range group.Scopes {
			fmt.Printf("  %-16s  %s\n", s.Name, s.Description)
		}
	}
	fmt.Println()
	fmt.Println("Full account access: --scopes all  (grants admin:all)")
	return nil
}

// validScopesSummary returns a comma-separated list of every valid scope name,
// derived from tokenScopeGroups so the create --help text can never drift from
// the authoritative scope list.
func validScopesSummary() string {
	var names []string
	for _, group := range tokenScopeGroups {
		for _, s := range group.Scopes {
			names = append(names, s.Name)
		}
	}
	return strings.Join(names, ", ")
}

var createTokenCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a new API token",
	Long: "Create a new API token.\n\n" +
		"Without --project, creates an account-level token usable across all projects.\n" +
		"With --project, creates a project-scoped token.\n\n" +
		"On success, prints the full plaintext Token value prominently (this is the only\n" +
		"time the full token is shown — save it immediately), followed by ID, Name, Type,\n" +
		"Prefix, Scopes, and Created timestamp.\n\n" +
		"Valid scopes: " + validScopesSummary() + ".\n" +
		"Scopes are comma-separated. Use --scopes all to grant full admin access (admin:all).\n" +
		"Run 'memory tokens scopes' for a description of each scope.",
	RunE: runCreateToken,
}

var getTokenCmd = &cobra.Command{
	Use:   "get [token-id]",
	Short: "Get token details",
	Long: `Get details for a specific API token by its ID.

Use --project to specify a project-scoped token; without it, looks up an
account-level token.`,
	Args: cobra.MaximumNArgs(1),
	RunE: runGetToken,
}

var revokeTokenCmd = &cobra.Command{
	Use:     "revoke [token-id]",
	Aliases: []string{"delete"},
	Short:   "Revoke (delete) an API token",
	Long:    "Permanently revoke (delete) an API token, making it unusable. Without --project, revokes an account-level token.",
	Args:    cobra.MaximumNArgs(1),
	RunE:    runRevokeToken,
}

var regenerateTokenCmd = &cobra.Command{
	Use:   "regenerate [token-id]",
	Short: "Regenerate an API token",
	Long: `Atomically revoke an existing API token and issue a new one with the same name and scopes.

Without --project, regenerates an account-level token. With --project, regenerates a project-scoped token.

The new plaintext token value is printed once — save it immediately.`,
	Args: cobra.MaximumNArgs(1),
	RunE: runRegenerateToken,
}

var updateScopesCmd = &cobra.Command{
	Use:   "update-scopes [token-id]",
	Short: "Update a token's scopes",
	Long: `Update the scopes of an existing API token.

Without --project, updates an account-level token. With --project, updates a
project-scoped token.

Use --scopes all for full admin access (admin:all).`,
	Args: cobra.MaximumNArgs(1),
	RunE: runUpdateScopes,
}

var cleanupTokensCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Bulk-revoke account tokens by name prefix",
	Long: `Revoke all account-level tokens whose names start with a given prefix.

Useful for cleaning up stale tokens left by e2e tests or automated tooling.
Prompts for confirmation before revoking unless --force is passed.

Example:
  memory tokens cleanup --name-prefix "e2e-"
  memory tokens cleanup --name-prefix "cli-test-" --force`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if tokenNamePrefix == "" {
			return fmt.Errorf("--name-prefix is required")
		}

		c, err := getAccountClient(cmd)
		if err != nil {
			return err
		}

		result, err := c.SDK.APITokens.ListAccountTokens(context.Background())
		if err != nil {
			return fmt.Errorf("failed to list account tokens: %w", err)
		}

		var matches []struct{ id, name string }
		for _, t := range result.Tokens {
			if strings.HasPrefix(t.Name, tokenNamePrefix) && t.RevokedAt == nil {
				matches = append(matches, struct{ id, name string }{t.ID, t.Name})
			}
		}

		if len(matches) == 0 {
			fmt.Printf("No active account tokens found with prefix %q.\n", tokenNamePrefix)
			return nil
		}

		fmt.Printf("Found %d token(s) matching prefix %q:\n", len(matches), tokenNamePrefix)
		for _, m := range matches {
			fmt.Printf("  • %s (%s)\n", m.name, m.id)
		}

		if !tokenCleanupForce {
			fmt.Printf("\nRevoke all %d token(s)? [y/N] ", len(matches))
			var answer string
			_, _ = fmt.Scanln(&answer)
			if strings.ToLower(strings.TrimSpace(answer)) != "y" {
				fmt.Println("Aborted.")
				return nil
			}
		}

		revoked, failed := 0, 0
		for _, m := range matches {
			if err := c.SDK.APITokens.RevokeAccountToken(context.Background(), m.id); err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "  ✗ failed to revoke %s (%s): %v\n", m.name, m.id, err)
				failed++
			} else {
				fmt.Printf("  ✓ revoked %s\n", m.name)
				revoked++
			}
		}

		fmt.Printf("\nDone: %d revoked, %d failed.\n", revoked, failed)
		return nil
	},
}

var (
	tokenProjectID    string
	tokenName         string
	tokenScopes       []string
	updateTokenScopes []string
	tokenListLimit    int
	tokenListPage     int
	tokenNamePrefix   string
	tokenCleanupForce bool
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

		filtered := result.Tokens
		if tokenNamePrefix != "" {
			filtered = filtered[:0:0]
			for _, t := range result.Tokens {
				if strings.HasPrefix(t.Name, tokenNamePrefix) {
					filtered = append(filtered, t)
				}
			}
		}

		if len(filtered) == 0 {
			if tokenNamePrefix != "" {
				fmt.Printf("No account-level tokens found with prefix %q.\n", tokenNamePrefix)
			} else {
				fmt.Println("No account-level tokens found.")
			}
			return nil
		}

		total := len(filtered)
		tokens := paginate(filtered, tokenListLimit, tokenListPage)

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
			fmt.Printf("   Created: %s\n", fmtTimeStr(t.CreatedAt))
			if t.RevokedAt != nil {
				fmt.Printf("   Revoked: %s\n", fmtTimePStr(t.RevokedAt))
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
		fmt.Printf("   Created: %s\n", fmtTimeStr(t.CreatedAt))
		if t.RevokedAt != nil {
			fmt.Printf("   Revoked: %s\n", fmtTimePStr(t.RevokedAt))
		}
		fmt.Println()
	}

	return nil
}

// normalizeTokenScopes resolves the raw --scopes flag values into the scopes
// sent to the API. Empty input defaults to data:read; the sentinel "all"
// expands to full admin access (admin:all).
func normalizeTokenScopes(scopes []string) []string {
	if len(scopes) == 0 {
		return []string{"data:read"}
	}
	if len(scopes) == 1 && scopes[0] == "all" {
		return []string{"admin:all"}
	}
	return scopes
}

func runCreateToken(cmd *cobra.Command, args []string) error {
	if tokenName == "" {
		return fmt.Errorf("token name is required. Use --name flag")
	}

	// Parse scopes
	scopes := normalizeTokenScopes(tokenScopes)

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
		fmt.Printf("  Created: %s\n", fmtTimeStr(result.CreatedAt))
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
	fmt.Printf("  Created: %s\n", fmtTimeStr(result.CreatedAt))
	fmt.Println()
	fmt.Println("  Retrieve this token later: memory tokens get " + result.ID)
	fmt.Println()

	return nil
}

func runUpdateScopes(cmd *cobra.Command, args []string) error {
	if len(updateTokenScopes) == 0 {
		return fmt.Errorf("--scopes is required (comma-separated, or 'all' for full admin)")
	}
	scopes := updateTokenScopes
	if len(scopes) == 1 && scopes[0] == "all" {
		scopes = []string{"admin:all"}
	}

	account := tokenProjectID == ""

	var c *client.Client
	var err error
	if account {
		c, err = getAccountClient(cmd)
	} else {
		c, err = getClient(cmd)
	}
	if err != nil {
		return err
	}

	projectID := ""
	if !account {
		projectID, err = resolveProjectContext(cmd, tokenProjectID)
		if err != nil {
			return err
		}
	}

	tokenID, tokenName, err := resolveTokenArgOrPick(cmd, c, args, account, projectID)
	if err != nil {
		return err
	}

	if account {
		updated, err := c.SDK.APITokens.UpdateAccountTokenScopes(context.Background(), tokenID, scopes)
		if err != nil {
			return fmt.Errorf("failed to update account token scopes: %w", err)
		}
		fmt.Printf("Updated scopes for account token %q:\n", tokenName)
		fmt.Printf("  Scopes: %s\n", strings.Join(updated.Scopes, ", "))
		return nil
	}

	updated, err := c.SDK.APITokens.UpdateScopes(context.Background(), projectID, tokenID, scopes)
	if err != nil {
		return fmt.Errorf("failed to update token scopes: %w", err)
	}
	fmt.Printf("Updated scopes for token %q:\n", tokenName)
	fmt.Printf("  Scopes: %s\n", strings.Join(updated.Scopes, ", "))
	return nil
}

func runGetToken(cmd *cobra.Command, args []string) error {
	account := tokenProjectID == ""

	var c *client.Client
	var err error
	if account {
		c, err = getAccountClient(cmd)
	} else {
		c, err = getClient(cmd)
	}
	if err != nil {
		return err
	}

	projectID := ""
	if !account {
		projectID, err = resolveProjectContext(cmd, tokenProjectID)
		if err != nil {
			return err
		}
	}

	tokenID, _, err := resolveTokenArgOrPick(cmd, c, args, account, projectID)
	if err != nil {
		return err
	}

	// If --project not provided, look up an account-level token
	if account {
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
		fmt.Printf("  Created: %s\n", fmtTimeStr(token.CreatedAt))
		if token.RevokedAt != nil {
			fmt.Printf("  Revoked: %s\n", fmtTimePStr(token.RevokedAt))
		}
		return nil
	}

	// --project provided: look up a project-scoped token
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
	fmt.Printf("  Created: %s\n", fmtTimeStr(token.CreatedAt))
	if token.RevokedAt != nil {
		fmt.Printf("  Revoked: %s\n", fmtTimePStr(token.RevokedAt))
	}

	return nil
}

func runRegenerateToken(cmd *cobra.Command, args []string) error {
	account := tokenProjectID == ""

	var c *client.Client
	var err error
	if account {
		c, err = getAccountClient(cmd)
	} else {
		c, err = getClient(cmd)
	}
	if err != nil {
		return err
	}

	projectID := ""
	if !account {
		projectID, err = resolveProjectContext(cmd, tokenProjectID)
		if err != nil {
			return err
		}
	}

	tokenID, _, err := resolveTokenArgOrPick(cmd, c, args, account, projectID)
	if err != nil {
		return err
	}

	if account {
		// Account-level token
		result, err := c.SDK.APITokens.RegenerateAccountToken(context.Background(), tokenID)
		if err != nil {
			return fmt.Errorf("failed to regenerate account token: %w", err)
		}

		fmt.Println("Account token regenerated successfully!")
		fmt.Println()
		fmt.Printf("  Token:   %s\n", result.Token)
		fmt.Println()
		fmt.Println("------------------------------------------------------------")
		fmt.Printf("  ID:      %s\n", result.ID)
		fmt.Printf("  Name:    %s\n", result.Name)
		fmt.Printf("  Type:    account\n")
		fmt.Printf("  Prefix:  %s\n", result.Prefix)
		fmt.Printf("  Scopes:  %s\n", strings.Join(result.Scopes, ", "))
		fmt.Printf("  Created: %s\n", fmtTimeStr(result.CreatedAt))
		fmt.Println()
		return nil
	}

	// Project-scoped token
	result, err := c.SDK.APITokens.Regenerate(context.Background(), projectID, tokenID)
	if err != nil {
		return fmt.Errorf("failed to regenerate token: %w", err)
	}

	fmt.Println("Token regenerated successfully!")
	fmt.Println()
	fmt.Printf("  Token:   %s\n", result.Token)
	fmt.Println()
	fmt.Println("------------------------------------------------------------")
	fmt.Printf("  ID:      %s\n", result.ID)
	fmt.Printf("  Name:    %s\n", result.Name)
	fmt.Printf("  Type:    project\n")
	fmt.Printf("  Prefix:  %s\n", result.Prefix)
	fmt.Printf("  Scopes:  %s\n", strings.Join(result.Scopes, ", "))
	fmt.Printf("  Created: %s\n", fmtTimeStr(result.CreatedAt))
	fmt.Println()

	return nil
}

func runRevokeToken(cmd *cobra.Command, args []string) error {
	account := tokenProjectID == ""

	var c *client.Client
	var err error
	if account {
		c, err = getAccountClient(cmd)
	} else {
		c, err = getClient(cmd)
	}
	if err != nil {
		return err
	}

	projectID := ""
	if !account {
		projectID, err = resolveProjectContext(cmd, tokenProjectID)
		if err != nil {
			return err
		}
	}

	tokenID, tokenName, err := resolveTokenArgOrPick(cmd, c, args, account, projectID)
	if err != nil {
		return err
	}

	// If --project not provided, revoke an account-level token
	if account {
		err = c.SDK.APITokens.RevokeAccountToken(context.Background(), tokenID)
		if err != nil {
			return fmt.Errorf("failed to revoke account token: %w", err)
		}

		fmt.Printf("Account token %s has been revoked successfully.\n", tokenName)
		return nil
	}

	// --project provided: revoke a project-scoped token
	err = c.SDK.APITokens.Revoke(context.Background(), projectID, tokenID)
	if err != nil {
		return fmt.Errorf("failed to revoke token: %w", err)
	}

	fmt.Printf("Token %s has been revoked successfully.\n", tokenName)

	return nil
}

func init() {
	// Persistent flag for all token subcommands (optional — omit for account-level tokens)
	tokensCmd.PersistentFlags().StringVar(&tokenProjectID, "project", "", "Project name or ID (omit for account-level tokens)")

	// List pagination flags
	listTokensCmd.Flags().IntVar(&tokenListLimit, "limit", 0, "Maximum number of tokens to show (0 = all)")
	listTokensCmd.Flags().IntVar(&tokenListPage, "page", 1, "Page number (1-based, used with --limit)")
	listTokensCmd.Flags().StringVar(&tokenNamePrefix, "name-prefix", "", "Filter tokens by name prefix (account-level only)")

	// Create token flags
	createTokenCmd.Flags().StringVar(&tokenName, "name", "", "Token name (required)")
	createTokenCmd.Flags().StringSliceVar(&tokenScopes, "scopes", nil, "Comma-separated scopes (e.g. --scopes data:read,data:write). Use --scopes all for full admin access.")
	_ = createTokenCmd.MarkFlagRequired("name")

	updateScopesCmd.Flags().StringSliceVar(&updateTokenScopes, "scopes", nil, "Comma-separated scopes (e.g. --scopes data:read,data:write). Use --scopes all for full admin access.")

	// Cleanup flags
	cleanupTokensCmd.Flags().StringVar(&tokenNamePrefix, "name-prefix", "", "Revoke tokens whose names start with this prefix (required)")
	cleanupTokensCmd.Flags().BoolVar(&tokenCleanupForce, "force", false, "Skip confirmation prompt")

	// Register subcommands
	tokensCmd.AddCommand(listTokensCmd)
	tokensCmd.AddCommand(scopesCmd)
	tokensCmd.AddCommand(createTokenCmd)
	tokensCmd.AddCommand(getTokenCmd)
	tokensCmd.AddCommand(revokeTokenCmd)
	tokensCmd.AddCommand(regenerateTokenCmd)
	tokensCmd.AddCommand(updateScopesCmd)
	tokensCmd.AddCommand(cleanupTokensCmd)
	rootCmd.AddCommand(tokensCmd)
}
