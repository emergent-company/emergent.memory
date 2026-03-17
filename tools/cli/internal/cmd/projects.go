package cmd

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/apitokens"
	sdkerrors "github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/errors"
	"github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/projects"
	"github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/provider"
	"github.com/emergent-company/emergent.memory/tools/cli/internal/client"
	"github.com/emergent-company/emergent.memory/tools/cli/internal/completion"
	"github.com/emergent-company/emergent.memory/tools/cli/internal/config"
	"github.com/joho/godotenv"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var projectsCmd = &cobra.Command{
	Use:     "projects",
	Short:   "Manage projects",
	Long:    "Commands for managing projects in the Memory platform",
	GroupID: "account",
}

var listProjectsCmd = &cobra.Command{
	Use:   "list",
	Short: "List all projects",
	Long: `List all projects you have access to.

Output prints a numbered list with each project's Name and ID. If the project
has a project info document set, it is shown beneath the name. Use the --stats
flag to also display per-project counts for Documents, Graph Objects,
Relationships, Extraction jobs (total/running/queued), and installed Schemas
(with their object and relationship type names).`,
	RunE: runListProjects,
}

var getProjectCmd = &cobra.Command{
	Use:   "get [name-or-id]",
	Short: "Get project details",
	Long: `Get details for a specific project by name or ID.

Prints the project's Name, ID, and Org ID. If a project info document is set
it is shown as well. Use the --stats flag to additionally display counts for
Documents, Graph Objects, Relationships, Extraction jobs, and installed Schemas
with their object and relationship type names.`,
	Args:              cobra.MaximumNArgs(1),
	ValidArgsFunction: completion.ProjectNamesCompletionFunc(),
	RunE:              runGetProject,
}

var createProjectCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a new project",
	Long: `Create a new project in the Memory platform.

Prints the new project's Name and ID on success. If no LLM provider credentials
are configured for the organization, a warning is shown explaining that AI
features (embeddings, search, extraction) will not work until a provider is
added via 'memory provider configure'.`,
	RunE: runCreateProject,
}

var deleteProjectCmd = &cobra.Command{
	Use:               "delete [project-id]",
	Short:             "Delete a project",
	Long:              "Permanently delete a project and all its data",
	Args:              cobra.MaximumNArgs(1),
	ValidArgsFunction: completion.ProjectNamesCompletionFunc(),
	RunE:              runDeleteProject,
}

var setProjectCmd = &cobra.Command{
	Use:   "set [name-or-id]",
	Short: "Set active project",
	Long: `Set the active project context.

Updates project_id in ~/.memory/config.yaml and writes MEMORY_PROJECT_ID,
MEMORY_PROJECT_NAME, and MEMORY_PROJECT_TOKEN into .env.local in the current
directory so that subsequent CLI commands and application code automatically use
the selected project. If no existing token is found for the project, a new one
is created automatically. Run without arguments to select interactively from a
numbered list of available projects.

Use --clear to remove the active project from the global config.`,
	Args:              cobra.MaximumNArgs(1),
	ValidArgsFunction: completion.ProjectNamesCompletionFunc(),
	RunE:              runSetProject,
}

var projectsCreateTokenCmd = &cobra.Command{
	Use:   "create-token [project-name-or-id]",
	Short: "Create a new API token for a project",
	Long: `Create a new project-scoped API token (emt_...) and print it.

The token is also written to .env.local in the current directory as
MEMORY_PROJECT_TOKEN so subsequent CLI commands pick it up automatically.

Scopes default to: data:read data:write schema:read agents:read agents:write

Example:
  memory projects create-token my-project --name onboard-token`,
	Args:              cobra.MaximumNArgs(1),
	ValidArgsFunction: completion.ProjectNamesCompletionFunc(),
	RunE:              runProjectsCreateToken,
}

var (
	createTokenName   string
	createTokenScopes []string
	createTokenNoEnv  bool
)

func runProjectsCreateToken(cmd *cobra.Command, args []string) error {
	c, err := getClient(cmd)
	if err != nil {
		return err
	}

	projectID, err := resolveProjectArgOrPick(cmd, c, args)
	if err != nil {
		return err
	}

	scopes := createTokenScopes
	if len(scopes) == 0 {
		scopes = []string{"data:read", "data:write", "schema:read", "agents:read", "agents:write"}
	}

	name := createTokenName
	if name == "" {
		name = "cli-token"
	}

	resp, err := c.SDK.APITokens.Create(context.Background(), projectID, &apitokens.CreateTokenRequest{
		Name:   name,
		Scopes: scopes,
	})
	if err != nil {
		return fmt.Errorf("failed to create token: %w", err)
	}

	fmt.Printf("Token created: %s\n", resp.Token)

	if !createTokenNoEnv {
		envMap, _ := godotenv.Read(".env.local")
		if envMap == nil {
			envMap = make(map[string]string)
		}
		envMap["MEMORY_PROJECT_TOKEN"] = resp.Token
		if err := godotenv.Write(envMap, ".env.local"); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: could not write .env.local: %v\n", err)
		} else {
			fmt.Println("MEMORY_PROJECT_TOKEN written to .env.local")
		}
	}

	return nil
}

var setProjectProviderCmd = &cobra.Command{
	Use:   "set-provider [project-name-or-id] <provider>",
	Short: "Configure the LLM provider for a project",
	Long: `Configure the LLM provider credentials for a specific project.

Supported providers: google, google-vertex. Prints the provider name, the
configured generative model, and the embedding model on success. Use flags
such as --api-key, --embedding-model, and --generative-model to specify
credentials and model overrides.`,
	Args: cobra.RangeArgs(1, 2),
	RunE: runSetProjectProvider,
}

var setProjectInfoCmd = &cobra.Command{
	Use:   "set-info [project-name-or-id]",
	Short: "Set the project info document",
	Long: `Set the project info document — a Markdown description of this project's
purpose, goals, audience, and context. Agents and MCP clients read this via the
get_project_info tool to orient themselves before working with the project's data.

Provide content via --file (read a .md file) or --text (inline string).
If no project is specified, the active project from config/env is used.

Examples:
  memory projects set-info --file README.md
  memory projects set-info my-project --file docs/project-info.md
  memory projects set-info --text "This project tracks internal HR documents."`,
	Args:              cobra.MaximumNArgs(1),
	ValidArgsFunction: completion.ProjectNamesCompletionFunc(),
	RunE:              runSetProjectInfo,
}

var setBudgetCmd = &cobra.Command{
	Use:   "set-budget [project-name-or-id]",
	Short: "Set a monthly spend budget for a project",
	Long: `Set or clear the monthly spend budget for a project.

When the project's estimated spend for the current month exceeds
budget_usd * budget_alert_threshold (default 0.8), an in-app notification
is sent to all org members. Set --budget 0 to clear an existing budget.

Examples:
  memory projects set-budget my-project --budget 50
  memory projects set-budget my-project --budget 100 --threshold 0.9
  memory projects set-budget --budget 25`,
	Args:              cobra.MaximumNArgs(1),
	ValidArgsFunction: completion.ProjectNamesCompletionFunc(),
	RunE:              runSetBudget,
}

var (
	setBudgetAmount    float64
	setBudgetThreshold float64
)

func runSetBudget(cmd *cobra.Command, args []string) error {
	if !cmd.Flags().Changed("budget") {
		return fmt.Errorf("--budget is required")
	}

	c, err := getClient(cmd)
	if err != nil {
		return err
	}

	projectID, err := resolveProjectArgOrPick(cmd, c, args)
	if err != nil {
		return err
	}

	req := &projects.UpdateProjectRequest{
		BudgetUSD: &setBudgetAmount,
	}
	if cmd.Flags().Changed("threshold") {
		req.BudgetAlertThreshold = &setBudgetThreshold
	}

	project, err := c.SDK.Projects.Update(context.Background(), projectID, req)
	if err != nil {
		return fmt.Errorf("failed to update project budget: %w", err)
	}

	if setBudgetAmount == 0 {
		fmt.Printf("Budget cleared for project %q (%s).\n", project.Name, project.ID)
	} else {
		threshold := 0.8
		if project.BudgetAlertThreshold != nil && *project.BudgetAlertThreshold != 0 {
			threshold = *project.BudgetAlertThreshold
		}
		fmt.Printf("Budget set for project %q (%s):\n", project.Name, project.ID)
		fmt.Printf("  Monthly budget:    $%.2f\n", setBudgetAmount)
		fmt.Printf("  Alert threshold:   %.0f%% ($%.2f)\n", threshold*100, setBudgetAmount*threshold)
	}
	return nil
}

var (
	setInfoFile string
	setInfoText string
)

var (
	setProviderAPIKey     string
	setProviderSAFile     string
	setProviderGCPProject string
	setProviderLocation   string
	setProviderEmbedding  string
	setProviderGenerative string
	setProjectClearFlag   bool
)

var (
	projectName        string
	projectDescription string
	projectOrgID       string
	projectStatsFlag   bool
	// Query flags
	filterFlag string
	sortFlag   string
	limitFlag  int
	offsetFlag int
	searchFlag string
)

func getClient(cmd *cobra.Command) (*client.Client, error) {
	configPath, _ := cmd.Flags().GetString("config")
	if configPath == "" {
		configPath = config.DiscoverPath("")
	}

	cfg, err := config.LoadWithEnv(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load config: %w", err)
	}

	// Apply persistent flag values from global viper. LoadWithEnv uses a
	// separate viper instance (env vars + config file only), so CLI flags
	// bound in root.go init() are not visible to it. Read them here and
	// let them override whatever was loaded from the config file.
	if v := viper.GetString("server"); v != "" {
		cfg.ServerURL = v
	}
	if v := viper.GetString("project_token"); v != "" {
		cfg.ProjectToken = v
	}

	if cfg.ServerURL == "" {
		return nil, fmt.Errorf("no server URL configured. Set MEMORY_SERVER_URL or run: memory config set-server <url>")
	}

	return client.New(cfg)
}

func printProjectStats(stats *projects.ProjectStats) {
	if stats == nil {
		return
	}
	fmt.Println("   Stats:")
	fmt.Printf("     • Documents: %d\n", stats.DocumentCount)
	fmt.Printf("     • Objects: %d\n", stats.ObjectCount)
	fmt.Printf("     • Relationships: %d\n", stats.RelationshipCount)

	jobsStr := fmt.Sprintf("%d total", stats.TotalJobs)
	if stats.RunningJobs > 0 {
		jobsStr += fmt.Sprintf(", %d running", stats.RunningJobs)
	}
	if stats.QueuedJobs > 0 {
		jobsStr += fmt.Sprintf(", %d queued", stats.QueuedJobs)
	}
	fmt.Printf("     • Extraction jobs: %s\n", jobsStr)

	if len(stats.Schemas) == 0 {
		fmt.Println("     • Schemas: none")
	} else {
		fmt.Println("     • Schemas:")
		for _, pack := range stats.Schemas {
			fmt.Printf("       - %s@%s\n", pack.Name, pack.Version)

			if len(pack.ObjectTypes) > 0 {
				fmt.Printf("         Objects: %s\n", strings.Join(pack.ObjectTypes, ", "))
			} else {
				fmt.Println("         Objects: none")
			}

			if len(pack.RelationshipTypes) > 0 {
				fmt.Printf("         Relationships: %s\n", strings.Join(pack.RelationshipTypes, ", "))
			} else {
				fmt.Println("         Relationships: none")
			}
		}
	}
}

func runListProjects(cmd *cobra.Command, args []string) error {
	c, err := getAccountClient(cmd)
	if err != nil {
		return err
	}

	opts := &projects.ListOptions{}
	if projectStatsFlag {
		opts.IncludeStats = true
	}

	projectList, err := c.SDK.Projects.List(context.Background(), opts)
	if err != nil {
		return fmt.Errorf("failed to list projects: %w", err)
	}

	if len(projectList) == 0 {
		if output == "json" {
			fmt.Println("[]")
			return nil
		}
		fmt.Println("No projects found.")
		return nil
	}

	if output == "json" {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(projectList)
	}

	fmt.Printf("Found %d project(s):\n\n", len(projectList))
	for i, p := range projectList {
		fmt.Printf("%d. %s (%s)\n", i+1, p.Name, p.ID)
		if p.ProjectInfo != nil && *p.ProjectInfo != "" {
			fmt.Printf("   Project Info: %s\n", *p.ProjectInfo)
		}

		// Print stats if requested
		if projectStatsFlag && p.Stats != nil {
			printProjectStats(p.Stats)
		}

		fmt.Println()
	}

	return nil
}

func runGetProject(cmd *cobra.Command, args []string) error {
	c, err := getClient(cmd)
	if err != nil {
		return err
	}

	projectID, err := resolveProjectArgOrPick(cmd, c, args)
	if err != nil {
		return err
	}

	opts := &projects.GetOptions{}
	if projectStatsFlag {
		opts.IncludeStats = true
	}

	project, err := c.SDK.Projects.Get(context.Background(), projectID, opts)
	if err != nil {
		return fmt.Errorf("failed to get project: %w", err)
	}

	if output == "json" {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(project)
	}

	fmt.Printf("Project: %s (%s)\n", project.Name, project.ID)
	fmt.Printf("  Org ID:     %s\n", project.OrgID)
	if project.ProjectInfo != nil && *project.ProjectInfo != "" {
		fmt.Printf("  Project Info: %s\n", *project.ProjectInfo)
	}

	// Print stats if requested
	if projectStatsFlag && project.Stats != nil {
		fmt.Println()
		printProjectStats(project.Stats)
	}

	return nil
}

// isUUID checks if a string looks like a UUID
func isUUID(s string) bool {
	// Simple check: UUIDs are 36 chars with hyphens in specific positions
	if len(s) != 36 {
		return false
	}
	for i, c := range s {
		if i == 8 || i == 13 || i == 18 || i == 23 {
			if c != '-' {
				return false
			}
		} else if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
			return false
		}
	}
	return true
}

// resolveProjectArgOrPick resolves a project name/ID from args[0], or, when
// args is empty and stdin is a terminal, launches the interactive project
// picker. Returns the resolved project ID.
func resolveProjectArgOrPick(cmd *cobra.Command, c *client.Client, args []string) (string, error) {
	if len(args) > 0 && args[0] != "" {
		return resolveProjectNameOrID(c, args[0])
	}

	// No arg — try the interactive picker.
	cfg, _ := config.LoadWithEnv(config.DiscoverPath(""))
	if cfg == nil {
		cfg = &config.Config{}
	}
	pickedID, pickErr := promptProjectPicker(cmd, cfg)
	if pickErr != nil {
		return "", pickErr
	}
	if pickedID != "" {
		return pickedID, nil
	}
	return "", fmt.Errorf("project is required — pass a project name or ID, or select one interactively")
}

// resolveProjectNameOrID resolves a project name or ID to a project ID.
// If the input looks like a UUID, it's returned as-is.
// Otherwise, it fetches all projects and finds a match by name (case-insensitive).
func resolveProjectNameOrID(c *client.Client, nameOrID string) (string, error) {
	if isUUID(nameOrID) {
		return nameOrID, nil
	}

	// Treat as a name — look up all projects and match
	projectList, err := c.SDK.Projects.List(context.Background(), nil)
	if err != nil {
		return "", fmt.Errorf("failed to list projects for name resolution: %w", err)
	}

	var matches []projects.Project
	for _, p := range projectList {
		if strings.EqualFold(p.Name, nameOrID) {
			matches = append(matches, p)
		}
	}

	switch len(matches) {
	case 0:
		return "", fmt.Errorf("no project found with name %q", nameOrID)
	case 1:
		return matches[0].ID, nil
	default:
		fmt.Printf("Multiple projects match %q:\n", nameOrID)
		for _, p := range matches {
			fmt.Printf("  - %s (%s)\n", p.Name, p.ID)
		}
		return "", fmt.Errorf("ambiguous project name %q — use the project ID instead", nameOrID)
	}
}

func runCreateProject(cmd *cobra.Command, args []string) error {
	if projectName == "" {
		return fmt.Errorf("project name is required. Use --name flag")
	}

	c, err := getAccountClient(cmd)
	if err != nil {
		return err
	}

	orgID := projectOrgID
	if orgID == "" {
		orgs, err := c.SDK.Orgs.List(context.Background())
		if err != nil {
			return fmt.Errorf("failed to fetch organizations: %w", err)
		}
		if len(orgs) == 0 {
			// Org list may be empty for OAuth users due to membership resolution.
			// Fall back to deriving the org ID from an existing project.
			existingProjects, listErr := c.SDK.Projects.List(context.Background(), nil)
			if listErr == nil && len(existingProjects) > 0 && existingProjects[0].OrgID != "" {
				orgID = existingProjects[0].OrgID
			}
		} else {
			orgID = orgs[0].ID
		}
	}
	if orgID == "" {
		return fmt.Errorf("could not determine organization ID.\nSpecify it with: memory projects create --name %q --org-id <org-id>", projectName)
	}

	req := &projects.CreateProjectRequest{
		Name:  projectName,
		OrgID: orgID,
	}

	project, err := c.SDK.Projects.Create(context.Background(), req)
	if err != nil {
		return fmt.Errorf("failed to create project: %w", err)
	}

	fmt.Println("Project created successfully!")
	fmt.Printf("  Name: %s (%s)\n", project.Name, project.ID)

	// Warn if no LLM provider is configured for the org
	configs, credErr := c.SDK.Provider.ListOrgConfigs(context.Background(), orgID)
	if credErr == nil && len(configs) == 0 {
		fmt.Println()
		fmt.Println("Warning: No LLM provider credentials are configured for your organization.")
		fmt.Println("AI features (embeddings, search, extraction) will not work until you add one.")
		fmt.Println("  Run: memory provider configure google --api-key <api-key>")
	}

	return nil
}

func runDeleteProject(cmd *cobra.Command, args []string) error {
	c, err := getAccountClient(cmd)
	if err != nil {
		return err
	}

	projectID, err := resolveProjectArgOrPick(cmd, c, args)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := c.SDK.Projects.Delete(ctx, projectID); err != nil {
		return fmt.Errorf("failed to delete project: %w", err)
	}

	fmt.Printf("Project %s deletion initiated. It will be removed in the background.\n", projectID)
	return nil
}

func runSetProject(cmd *cobra.Command, args []string) error {
	// Handle --clear: remove project_id from global config and .env.local
	if setProjectClearFlag {
		configPath, _ := cmd.Flags().GetString("config")
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
		cfg.ProjectID = ""
		if err := os.MkdirAll(filepath.Dir(configPath), 0755); err != nil {
			return fmt.Errorf("failed to create config directory: %w", err)
		}
		if err := config.Save(cfg, configPath); err != nil {
			return fmt.Errorf("failed to save config: %w", err)
		}
		fmt.Println("Active project cleared from config.")
		return nil
	}

	c, err := getAccountClient(cmd)
	if err != nil {
		return err
	}

	var selectedProjectID string
	var selectedProjectName string

	if len(args) == 0 {
		// Interactive mode
		fmt.Println("Fetching projects...")
		projectList, err := c.SDK.Projects.List(context.Background(), &projects.ListOptions{})
		if err != nil {
			return fmt.Errorf("failed to list projects: %w", err)
		}
		if len(projectList) == 0 {
			return fmt.Errorf("no projects found. Create one first.")
		}

		fmt.Println("\nAvailable Projects:")
		for i, p := range projectList {
			fmt.Printf("%d. %s (%s)\n", i+1, p.Name, p.ID)
		}

		reader := bufio.NewReader(os.Stdin)
		for {
			fmt.Print("\nSelect a project (enter number): ")
			input, _ := reader.ReadString('\n')
			input = strings.TrimSpace(input)
			if input == "" {
				return fmt.Errorf("selection cancelled")
			}

			num, err := strconv.Atoi(input)
			if err == nil && num > 0 && num <= len(projectList) {
				selectedProjectID = projectList[num-1].ID
				selectedProjectName = projectList[num-1].Name
				break
			}
			fmt.Println("Invalid selection. Please enter a valid number.")
		}
	} else {
		// Resolve name or ID
		id, err := resolveProjectNameOrID(c, args[0])
		if err != nil {
			return err
		}
		selectedProjectID = id

		// Fetch the project to get its name
		projectList, err := c.SDK.Projects.List(context.Background(), &projects.ListOptions{})
		if err != nil {
			return err
		}
		for _, p := range projectList {
			if p.ID == selectedProjectID {
				selectedProjectName = p.Name
				break
			}
		}
		if selectedProjectName == "" {
			selectedProjectName = "Unknown Project"
		}
	}

	fmt.Printf("\nSelected project: %s\n", selectedProjectName)
	fmt.Println("Fetching/generating project token...")

	// Find or create a token
	tokensResp, err := c.SDK.APITokens.List(context.Background(), selectedProjectID)
	if err != nil {
		return fmt.Errorf("failed to list tokens: %w", err)
	}

	var tokenValue string

	if len(tokensResp.Tokens) > 0 {
		// Try to fetch full token for the first one
		firstToken := tokensResp.Tokens[0]
		fullTokenResp, err := c.SDK.APITokens.Get(context.Background(), selectedProjectID, firstToken.ID)
		if err == nil && fullTokenResp.Token != "" {
			tokenValue = fullTokenResp.Token
		}
	}

	if tokenValue == "" {
		// Generate a new one
		fmt.Println("No existing viewable token found. Generating a new one...")
		tokenName := cliTokenName()
		req := &apitokens.CreateTokenRequest{
			Name:   tokenName,
			Scopes: []string{"data:read", "data:write", "schema:read"},
		}
		createResp, err := c.SDK.APITokens.Create(context.Background(), selectedProjectID, req)
		if err != nil && sdkerrors.IsConflict(err) {
			// Token name already exists (e.g. re-running setup on same machine).
			// Retry with a timestamp suffix so we never collide.
			tokenName = fmt.Sprintf("%s-%d", tokenName, time.Now().Unix())
			req.Name = tokenName
			createResp, err = c.SDK.APITokens.Create(context.Background(), selectedProjectID, req)
		}
		if err != nil {
			return fmt.Errorf("failed to create token: %w", err)
		}
		tokenValue = createResp.Token
	}

	// Write to .env.local
	envMap, _ := godotenv.Read(".env.local")
	if envMap == nil {
		envMap = make(map[string]string)
	}
	envMap["MEMORY_PROJECT_ID"] = selectedProjectID
	envMap["MEMORY_PROJECT_NAME"] = selectedProjectName
	envMap["MEMORY_PROJECT_TOKEN"] = tokenValue

	err = godotenv.Write(envMap, ".env.local")
	if err != nil {
		return fmt.Errorf("failed to write to .env.local: %w", err)
	}

	// Also update project_id in the global config (~/.memory/config.yaml)
	configPath, _ := cmd.Flags().GetString("config")
	if configPath == "" {
		configPath = config.DiscoverPath("")
	}
	globalCfg, loadErr := config.Load(configPath)
	if loadErr != nil && !os.IsNotExist(loadErr) {
		fmt.Fprintf(os.Stderr, "Warning: could not load global config: %v\n", loadErr)
	} else {
		if globalCfg == nil {
			globalCfg = &config.Config{}
		}
		globalCfg.ProjectID = selectedProjectID
		if mkErr := os.MkdirAll(filepath.Dir(configPath), 0755); mkErr == nil {
			if saveErr := config.Save(globalCfg, configPath); saveErr != nil {
				fmt.Fprintf(os.Stderr, "Warning: could not update global config: %v\n", saveErr)
			}
		}
	}

	fmt.Println("\nSuccessfully updated .env.local with project context!")
	fmt.Printf("Active project set to: %s (%s)\n", selectedProjectName, selectedProjectID)
	return nil
}

func runSetProjectProvider(cmd *cobra.Command, args []string) error {
	var nameOrID, providerName string
	if len(args) == 1 {
		// Only provider given — pick a project interactively.
		providerName = args[0]
	} else {
		// Both project and provider given.
		nameOrID = args[0]
		providerName = args[1]
	}

	if providerName != "google" && providerName != "google-vertex" {
		return fmt.Errorf("invalid provider %q: must be 'google' or 'google-vertex'", providerName)
	}

	c, err := getClient(cmd)
	if err != nil {
		return err
	}

	var pickArgs []string
	if nameOrID != "" {
		pickArgs = []string{nameOrID}
	}
	projectID, err := resolveProjectArgOrPick(cmd, c, pickArgs)
	if err != nil {
		return err
	}

	req := &provider.UpsertProviderConfigRequest{
		APIKey:          setProviderAPIKey,
		GCPProject:      setProviderGCPProject,
		Location:        setProviderLocation,
		EmbeddingModel:  setProviderEmbedding,
		GenerativeModel: setProviderGenerative,
	}

	if setProviderSAFile != "" {
		data, err := os.ReadFile(setProviderSAFile)
		if err != nil {
			return fmt.Errorf("failed to read service account file: %w", err)
		}
		req.ServiceAccountJSON = string(data)
	}

	cfg, err := c.SDK.Provider.UpsertProjectConfig(context.Background(), projectID, providerName, req)
	if err != nil {
		return fmt.Errorf("failed to set project provider config: %w", err)
	}

	fmt.Printf("Provider config for project %q set (provider: %s).\n", projectID, providerName)
	if cfg.GenerativeModel != "" {
		fmt.Printf("  Generative model: %s\n", cfg.GenerativeModel)
	}
	if cfg.EmbeddingModel != "" {
		fmt.Printf("  Embedding model:  %s\n", cfg.EmbeddingModel)
	}
	return nil
}

func runSetProjectInfo(cmd *cobra.Command, args []string) error {
	if setInfoFile == "" && setInfoText == "" {
		return fmt.Errorf("provide content via --file <path> or --text <string>")
	}
	if setInfoFile != "" && setInfoText != "" {
		return fmt.Errorf("use either --file or --text, not both")
	}

	var content string
	if setInfoFile != "" {
		data, err := os.ReadFile(setInfoFile)
		if err != nil {
			return fmt.Errorf("failed to read file %q: %w", setInfoFile, err)
		}
		content = string(data)
	} else {
		content = setInfoText
	}

	if strings.TrimSpace(content) == "" {
		return fmt.Errorf("project info content is empty")
	}

	c, err := getClient(cmd)
	if err != nil {
		return err
	}

	var projectID string
	if len(args) > 0 {
		projectID, err = resolveProjectNameOrID(c, args[0])
		if err != nil {
			return err
		}
	} else {
		projectID, err = resolveProjectContext(cmd, "")
		if err != nil {
			return err
		}
	}

	_, err = c.SDK.Projects.Update(context.Background(), projectID, &projects.UpdateProjectRequest{
		ProjectInfo: &content,
	})
	if err != nil {
		return fmt.Errorf("failed to update project info: %w", err)
	}

	if setInfoFile != "" {
		fmt.Printf("Project info updated from %q.\n", setInfoFile)
	} else {
		fmt.Println("Project info updated.")
	}
	return nil
}

func init() {
	createProjectCmd.Flags().StringVar(&projectName, "name", "", "Project name (required)")
	createProjectCmd.Flags().StringVar(&projectDescription, "description", "", "Project description")
	createProjectCmd.Flags().StringVar(&projectOrgID, "org-id", "", "Organization ID (auto-detected if not specified)")
	_ = createProjectCmd.MarkFlagRequired("name")

	listProjectsCmd.Flags().BoolVar(&projectStatsFlag, "stats", false, "Include project statistics (documents, objects, jobs, schemas)")
	listProjectsCmd.Flags().StringVar(&filterFlag, "filter", "", "Filter results (e.g., 'name=MyProject,status=active')")
	listProjectsCmd.Flags().StringVar(&sortFlag, "sort", "", "Sort results (e.g., 'name:asc' or 'updated_at:desc')")
	listProjectsCmd.Flags().IntVar(&limitFlag, "limit", 0, "Maximum number of results (default from config)")
	listProjectsCmd.Flags().IntVar(&offsetFlag, "offset", 0, "Number of results to skip")
	listProjectsCmd.Flags().StringVar(&searchFlag, "search", "", "Search projects by name or description")

	getProjectCmd.Flags().BoolVar(&projectStatsFlag, "stats", false, "Include project statistics (documents, objects, jobs, schemas)")

	setProjectCmd.Flags().BoolVar(&setProjectClearFlag, "clear", false, "Clear the active project from config")

	setProjectProviderCmd.Flags().StringVar(&setProviderAPIKey, "api-key", "", "Google AI API key (for google)")
	setProjectProviderCmd.Flags().StringVar(&setProviderSAFile, "sa-file", "", "Path to Vertex AI service account JSON (for google-vertex)")
	setProjectProviderCmd.Flags().StringVar(&setProviderGCPProject, "gcp-project", "", "GCP project ID (for google-vertex)")
	setProjectProviderCmd.Flags().StringVar(&setProviderLocation, "location", "", "GCP region (for google-vertex)")
	setProjectProviderCmd.Flags().StringVar(&setProviderEmbedding, "embedding-model", "", "Override embedding model for this project")
	setProjectProviderCmd.Flags().StringVar(&setProviderGenerative, "generative-model", "", "Override generative model for this project")

	projectsCreateTokenCmd.Flags().StringVar(&createTokenName, "name", "cli-token", "Token name")
	projectsCreateTokenCmd.Flags().StringSliceVar(&createTokenScopes, "scopes", nil, "Token scopes (default: data:read,data:write,schema:read,agents:read,agents:write)")
	projectsCreateTokenCmd.Flags().BoolVar(&createTokenNoEnv, "no-env", false, "Do not write token to .env.local")

	setProjectInfoCmd.Flags().StringVar(&setInfoFile, "file", "", "Path to a Markdown file to use as project info")
	setProjectInfoCmd.Flags().StringVar(&setInfoText, "text", "", "Inline project info text")

	setBudgetCmd.Flags().Float64Var(&setBudgetAmount, "budget", 0, "Monthly budget in USD (set to 0 to clear)")
	setBudgetCmd.Flags().Float64Var(&setBudgetThreshold, "threshold", 0.8, "Alert threshold as a fraction of budget (e.g. 0.8 = 80%)")

	projectsCmd.AddCommand(listProjectsCmd)
	projectsCmd.AddCommand(getProjectCmd)
	projectsCmd.AddCommand(createProjectCmd)
	projectsCmd.AddCommand(deleteProjectCmd)
	projectsCmd.AddCommand(setProjectCmd)
	projectsCmd.AddCommand(setProjectInfoCmd)
	projectsCmd.AddCommand(setProjectProviderCmd)
	projectsCmd.AddCommand(projectsCreateTokenCmd)
	projectsCmd.AddCommand(setBudgetCmd)
	rootCmd.AddCommand(projectsCmd)
}
