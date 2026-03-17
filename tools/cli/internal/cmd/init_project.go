package cmd

import (
	"bufio"
	"context"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/apitokens"
	sdkerrors "github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/errors"
	"github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/projects"
	"github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/provider"
	"github.com/emergent-company/emergent.memory/tools/cli/internal/client"
	"github.com/emergent-company/emergent.memory/tools/cli/internal/config"
	"github.com/emergent-company/emergent.memory/tools/cli/internal/skillsfs"
	"github.com/joho/godotenv"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

var initProjectCmd = &cobra.Command{
	Use:   "init",
	Short: "Initialize a Memory project in the current directory",
	Long: `Interactive wizard that sets up a Memory project in the current directory.

Walks through:
  1. Project selection or creation
  2. LLM provider configuration (org-level)
  3. Memory skills installation for AI agents

Writes MEMORY_PROJECT_ID, MEMORY_PROJECT_NAME, and MEMORY_PROJECT_TOKEN
to .env.local and auto-adds .env.local to .gitignore.

Running 'memory init' again detects existing configuration and offers
to verify or reconfigure each step.

Use --skip-provider or --skip-skills to skip individual steps.`,
	RunE: runInitProject,
}

var (
	initSkipProvider bool
	initSkipSkills   bool
)

func init() {
	initProjectCmd.Flags().BoolVar(&initSkipProvider, "skip-provider", false, "skip LLM provider configuration step")
	initProjectCmd.Flags().BoolVar(&initSkipSkills, "skip-skills", false, "skip Memory skills installation step")
	rootCmd.AddCommand(initProjectCmd)
}

// ---------------------------------------------------------------------------
// Main entry point
// ---------------------------------------------------------------------------

func runInitProject(cmd *cobra.Command, args []string) error {
	// 1.2  Non-interactive guard
	if !isInteractiveTerminal() {
		return fmt.Errorf("memory init requires an interactive terminal (stdin must be a TTY)")
	}

	fmt.Println("Welcome to Memory project setup!")
	fmt.Println()

	c, err := getClient(cmd)
	if err != nil {
		return err
	}

	// Track what was done for the summary.
	var (
		projectName    string
		projectID      string
		providerStatus string // "configured", "skipped", "already configured"
		skillsStatus   string // "installed", "skipped"
	)

	// ------------------------------------------------------------------
	// 2. Idempotent re-run detection
	// ------------------------------------------------------------------
	envMap, _ := godotenv.Read(".env.local")
	existingProjectID := envMap["MEMORY_PROJECT_ID"]
	isRerun := existingProjectID != ""

	if isRerun {
		// 2.2  Validate the project still exists on the server.
		projectList, err := c.SDK.Projects.List(context.Background(), &projects.ListOptions{})
		if err != nil {
			return fmt.Errorf("failed to list projects: %w", err)
		}

		var found *projects.Project
		for i := range projectList {
			if projectList[i].ID == existingProjectID {
				found = &projectList[i]
				break
			}
		}

		if found == nil {
			// 2.2  Stale project — warn and fall through to fresh run.
			fmt.Printf("Warning: previously configured project %s was not found on the server.\n", existingProjectID)
			fmt.Println("Starting fresh setup...")
			fmt.Println()
			isRerun = false
		} else {
			// 2.3  Ask to verify.
			existingName := envMap["MEMORY_PROJECT_NAME"]
			if existingName == "" {
				existingName = found.Name
			}
			yes, err := promptYesNoDefault(fmt.Sprintf("Already initialized for project %q. Verify settings? [Y/n] ", existingName), true)
			if err != nil {
				return err
			}
			if !yes {
				fmt.Println("No changes made.")
				return nil
			}

			// Re-run: walk through each step with current state shown.
			projectID = found.ID
			projectName = found.Name

			// 9.1  Project verification — offer to switch.
			projectID, projectName, err = initVerifyProject(c, projectID, projectName)
			if err != nil {
				return err
			}

			// Persist if project changed.
			if projectID != existingProjectID {
				if err := initPersistProject(cmd, c, projectID, projectName); err != nil {
					return err
				}
			}

			// .gitignore
			ensureGitignore()

			// 9.2  Provider verification.
			if !initSkipProvider {
				providerStatus, err = initVerifyProvider(c)
				if err != nil {
					return err
				}
			} else {
				providerStatus = "skipped"
			}

			// 9.3  Skills verification.
			if !initSkipSkills {
				skillsStatus, err = initVerifySkills()
				if err != nil {
					return err
				}
			} else {
				skillsStatus = "skipped"
			}

			initPrintSummary(projectName, projectID, providerStatus, skillsStatus)
			return nil
		}
	}

	// ------------------------------------------------------------------
	// 3. Fresh run — project selection/creation
	// ------------------------------------------------------------------
	projectID, projectName, err = initSelectOrCreateProject(c)
	if err != nil {
		return err
	}

	// ------------------------------------------------------------------
	// 4. Token + .env.local persistence
	// ------------------------------------------------------------------
	if err := initPersistProject(cmd, c, projectID, projectName); err != nil {
		return err
	}

	// ------------------------------------------------------------------
	// 5. .gitignore
	// ------------------------------------------------------------------
	ensureGitignore()

	// ------------------------------------------------------------------
	// 6. Provider configuration
	// ------------------------------------------------------------------
	if !initSkipProvider {
		providerStatus, err = initConfigureProvider(c)
		if err != nil {
			return err
		}
	} else {
		providerStatus = "skipped"
	}

	// ------------------------------------------------------------------
	// 7. Skills installation
	// ------------------------------------------------------------------
	if !initSkipSkills {
		skillsStatus, err = initInstallSkills()
		if err != nil {
			return err
		}
	} else {
		skillsStatus = "skipped"
	}

	// ------------------------------------------------------------------
	// 8. Summary
	// ------------------------------------------------------------------
	initPrintSummary(projectName, projectID, providerStatus, skillsStatus)
	return nil
}

// ---------------------------------------------------------------------------
// 3. Project selection / creation
// ---------------------------------------------------------------------------

func initSelectOrCreateProject(c *client.Client) (id, name string, err error) {
	fmt.Println("Fetching your projects...")
	projectList, err := c.SDK.Projects.List(context.Background(), &projects.ListOptions{})
	if err != nil {
		return "", "", fmt.Errorf("failed to list projects: %w", err)
	}

	// 3.1  Build picker items with synthetic "Create new" prepended.
	items := make([]PickerItem, 0, len(projectList)+1)
	items = append(items, PickerItem{ID: "__create__", Name: "+ Create new project"})
	for _, p := range projectList {
		items = append(items, PickerItem{ID: p.ID, Name: p.Name})
	}

	// 3.2  Display picker.
	selectedID, selectedName, err := pickResourceWithTitle("Select a project", items, 30*time.Second, os.Stderr)
	if err != nil {
		return "", "", err
	}

	// 3.3  Create new project.
	if selectedID == "__create__" {
		return initCreateProject(c)
	}

	// 3.4  Existing project.
	fmt.Printf("Selected project: %s\n", selectedName)
	return selectedID, selectedName, nil
}

func initCreateProject(c *client.Client) (id, name string, err error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", "", fmt.Errorf("failed to get working directory: %w", err)
	}
	folderName := filepath.Base(cwd)

	fmt.Printf("Project name [%s]: ", folderName)
	scanner := bufio.NewScanner(os.Stdin)
	if !scanner.Scan() {
		if err := scanner.Err(); err != nil {
			return "", "", err
		}
		return "", "", fmt.Errorf("no input received")
	}
	input := strings.TrimSpace(scanner.Text())
	if input == "" {
		input = folderName
	}

	fmt.Printf("Creating project %q...\n", input)

	// Resolve org ID for project creation.
	orgID, err := resolveProviderOrgID(c, "")
	if err != nil {
		return "", "", fmt.Errorf("failed to determine organization: %w", err)
	}

	project, err := c.SDK.Projects.Create(context.Background(), &projects.CreateProjectRequest{
		Name:  input,
		OrgID: orgID,
	})
	if err != nil {
		return "", "", fmt.Errorf("failed to create project: %w", err)
	}

	fmt.Printf("Project %q created.\n", project.Name)
	return project.ID, project.Name, nil
}

// ---------------------------------------------------------------------------
// 4. Token + .env.local + global config
// ---------------------------------------------------------------------------

// cliTokenName returns a token name based on the machine hostname (e.g. "cli-mypc").
// Falls back to "cli-auto-token" if the hostname cannot be determined.
func cliTokenName() string {
	host, err := os.Hostname()
	if err != nil || host == "" {
		return "cli-auto-token"
	}
	return "cli-" + host
}

func initPersistProject(cmd *cobra.Command, c *client.Client, projectID, projectName string) error {
	fmt.Println("Configuring project token...")

	// 4.1  Try to reuse an existing token.
	tokenValue := ""
	tokensResp, err := c.SDK.APITokens.List(context.Background(), projectID)
	if err == nil && len(tokensResp.Tokens) > 0 {
		first := tokensResp.Tokens[0]
		full, err := c.SDK.APITokens.Get(context.Background(), projectID, first.ID)
		if err == nil && full.Token != "" {
			tokenValue = full.Token
		}
	}

	// 4.2  Create a new token if none available.
	if tokenValue == "" {
		tokenName := cliTokenName()
		defaultScopes := []string{"data:read", "data:write", "schema:read", "projects:read", "agents:read"}
		createResp, err := c.SDK.APITokens.Create(context.Background(), projectID, &apitokens.CreateTokenRequest{
			Name:   tokenName,
			Scopes: defaultScopes,
		})
		if err != nil && sdkerrors.IsConflict(err) {
			// Token name already exists (e.g. re-running init on same machine).
			// Retry with a timestamp suffix so we never collide.
			tokenName = fmt.Sprintf("%s-%d", tokenName, time.Now().Unix())
			createResp, err = c.SDK.APITokens.Create(context.Background(), projectID, &apitokens.CreateTokenRequest{
				Name:   tokenName,
				Scopes: defaultScopes,
			})
		}
		if err != nil {
			return fmt.Errorf("failed to create API token: %w", err)
		}
		tokenValue = createResp.Token
	}

	// 4.3  Write .env.local (preserve existing keys).
	envMap, _ := godotenv.Read(".env.local")
	if envMap == nil {
		envMap = make(map[string]string)
	}
	envMap["MEMORY_PROJECT_ID"] = projectID
	envMap["MEMORY_PROJECT_NAME"] = projectName
	envMap["MEMORY_PROJECT_TOKEN"] = tokenValue

	if err := godotenv.Write(envMap, ".env.local"); err != nil {
		return fmt.Errorf("failed to write .env.local: %w", err)
	}
	fmt.Println("Wrote project context to .env.local")

	// 4.4  Update global config.
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
		globalCfg.ProjectID = projectID
		if mkErr := os.MkdirAll(filepath.Dir(configPath), 0755); mkErr == nil {
			if saveErr := config.Save(globalCfg, configPath); saveErr != nil {
				fmt.Fprintf(os.Stderr, "Warning: could not update global config: %v\n", saveErr)
			}
		}
	}

	return nil
}

// ---------------------------------------------------------------------------
// 5. .gitignore
// ---------------------------------------------------------------------------

func ensureGitignore() {
	const entry = ".env.local"
	data, err := os.ReadFile(".gitignore")
	if err != nil {
		if os.IsNotExist(err) {
			// Create .gitignore with the entry.
			_ = os.WriteFile(".gitignore", []byte(entry+"\n"), 0644)
		}
		return
	}

	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		if strings.TrimSpace(line) == entry {
			return // already present
		}
	}

	// Append.
	f, err := os.OpenFile(".gitignore", os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	defer f.Close()

	// Ensure we start on a new line.
	if len(data) > 0 && data[len(data)-1] != '\n' {
		_, _ = f.WriteString("\n")
	}
	_, _ = f.WriteString(entry + "\n")
}

// ---------------------------------------------------------------------------
// 6. Provider configuration (fresh run)
// ---------------------------------------------------------------------------

func initConfigureProvider(c *client.Client) (string, error) {
	fmt.Println()
	fmt.Println("Checking LLM provider configuration...")

	// 6.2  Resolve org and check existing config.
	orgID, err := resolveProviderOrgID(c, "")
	if err != nil {
		return "", fmt.Errorf("failed to determine organization: %w", err)
	}

	configs, err := c.SDK.Provider.ListOrgConfigs(context.Background(), orgID)
	if err != nil {
		return "", fmt.Errorf("failed to check provider config: %w", err)
	}

	// 6.3  Already configured.
	if len(configs) > 0 {
		fmt.Printf("LLM provider already configured (%s).\n", configs[0].Provider)
		return "already configured", nil
	}

	// 6.4  Display provider picker.
	return initProviderPicker(c, orgID)
}

func initProviderPicker(c *client.Client, orgID string) (string, error) {
	fmt.Println("No LLM provider configured. Let's set one up.")
	fmt.Println()

	items := []PickerItem{
		{ID: "google", Name: "Google AI (API key)"},
		{ID: "google-vertex", Name: "Vertex AI (GCP)"},
		{ID: "__skip__", Name: "Skip for now"},
	}

	selectedID, _, err := pickResourceWithTitle("Select an LLM provider", items, 30*time.Second, os.Stderr)
	if err != nil {
		return "", err
	}

	switch selectedID {
	case "google":
		return initConfigureGoogleAI(c, orgID)
	case "google-vertex":
		return initConfigureVertexAI(c, orgID)
	default:
		fmt.Println("Skipping provider configuration.")
		return "skipped", nil
	}
}

// 6.5  Google AI path.
func initConfigureGoogleAI(c *client.Client, orgID string) (string, error) {
	fmt.Print("Enter your Google AI API key: ")
	keyBytes, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Println() // newline after masked input
	if err != nil {
		return "", fmt.Errorf("failed to read API key: %w", err)
	}
	apiKey := strings.TrimSpace(string(keyBytes))
	if apiKey == "" {
		fmt.Println("No API key provided. Skipping provider configuration.")
		return "skipped", nil
	}

	fmt.Println("Configuring Google AI provider...")
	_, err = c.SDK.Provider.UpsertOrgConfig(context.Background(), orgID, provider.ProviderGoogleAI, &provider.UpsertProviderConfigRequest{
		APIKey: apiKey,
	})
	if err != nil {
		return "", fmt.Errorf("failed to configure Google AI: %w", err)
	}

	// Test the provider.
	fmt.Println("Testing provider credentials...")
	testResp, testErr := c.SDK.Provider.TestProvider(context.Background(), provider.ProviderGoogleAI, "", orgID)
	if testErr != nil {
		fmt.Printf("Provider test failed: %v\n", testErr)
		fmt.Println("Credentials were saved but could not be verified. Check with 'memory provider test'.")
		return "configured (test failed)", nil
	}
	fmt.Printf("Provider test passed: model=%s, latency=%dms\n", testResp.Model, testResp.LatencyMs)
	return "configured", nil
}

// 6.6  Vertex AI path.
func initConfigureVertexAI(c *client.Client, orgID string) (string, error) {
	// Check if gcloud is available.
	gcloudPath, err := exec.LookPath("gcloud")
	if err != nil {
		printGcloudInstructions("gcloud CLI not found.")
		return "skipped", nil
	}
	_ = gcloudPath

	// Check if authenticated.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	authCmd := exec.CommandContext(ctx, "gcloud", "auth", "application-default", "print-access-token")
	if out, err := authCmd.CombinedOutput(); err != nil {
		_ = out
		printGcloudInstructions("gcloud is installed but application-default credentials are not configured.")
		return "skipped", nil
	}

	// Prompt for GCP project and region.
	scanner := bufio.NewScanner(os.Stdin)

	fmt.Print("GCP Project ID: ")
	if !scanner.Scan() {
		return "skipped", nil
	}
	gcpProject := strings.TrimSpace(scanner.Text())
	if gcpProject == "" {
		fmt.Println("No GCP project provided. Skipping.")
		return "skipped", nil
	}

	fmt.Print("GCP Region [us-central1]: ")
	if !scanner.Scan() {
		return "skipped", nil
	}
	region := strings.TrimSpace(scanner.Text())
	if region == "" {
		region = "us-central1"
	}

	fmt.Println("Configuring Vertex AI provider...")
	_, err = c.SDK.Provider.UpsertOrgConfig(context.Background(), orgID, provider.ProviderVertexAI, &provider.UpsertProviderConfigRequest{
		GCPProject: gcpProject,
		Location:   region,
	})
	if err != nil {
		return "", fmt.Errorf("failed to configure Vertex AI: %w", err)
	}

	// Test the provider.
	fmt.Println("Testing provider credentials...")
	testResp, testErr := c.SDK.Provider.TestProvider(context.Background(), provider.ProviderVertexAI, "", orgID)
	if testErr != nil {
		fmt.Printf("Provider test failed: %v\n", testErr)
		fmt.Println("Credentials were saved but could not be verified. Check with 'memory provider test'.")
		return "configured (test failed)", nil
	}
	fmt.Printf("Provider test passed: model=%s, latency=%dms\n", testResp.Model, testResp.LatencyMs)
	return "configured", nil
}

func printGcloudInstructions(reason string) {
	fmt.Println(reason)
	fmt.Println()
	fmt.Println("To set up Vertex AI, follow these steps:")
	fmt.Println("  1. Install the gcloud CLI: https://cloud.google.com/sdk/docs/install")
	fmt.Println("  2. Run: gcloud auth application-default login")
	fmt.Println("  3. Re-run: memory init")
	fmt.Println()
	fmt.Println("You can also configure Vertex AI later with:")
	fmt.Println("  memory provider configure google-vertex --gcp-project <project> --location <region>")
	fmt.Println()
	fmt.Println("Continuing without provider configuration...")
}

// ---------------------------------------------------------------------------
// 7. Skills installation
// ---------------------------------------------------------------------------

func initInstallSkills() (string, error) {
	fmt.Println()
	yes, err := promptYesNoDefault("Install Memory skills? [Y/n] ", true)
	if err != nil {
		return "", err
	}
	if !yes {
		return "skipped", nil
	}

	catalog := skillsfs.Catalog()

	cwd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("getting working directory: %w", err)
	}
	targetDir := filepath.Join(cwd, ".agents", "skills")

	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		return "", fmt.Errorf("creating skills directory: %w", err)
	}

	entries, err := fs.ReadDir(catalog, ".")
	if err != nil {
		return "", fmt.Errorf("reading embedded skills catalog: %w", err)
	}

	installed := 0
	skipped := 0
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasPrefix(name, "memory-") {
			continue
		}

		destDir := filepath.Join(targetDir, name)
		if _, err := os.Stat(destDir); err == nil {
			skipped++
			continue
		}

		sub, err := fs.Sub(catalog, name)
		if err != nil {
			return "", fmt.Errorf("accessing skill %s: %w", name, err)
		}
		if err := copyFSTree(sub, destDir); err != nil {
			return "", fmt.Errorf("installing skill %s: %w", name, err)
		}

		fmt.Printf("  installed %s\n", name)
		installed++
	}

	fmt.Printf("%d skill(s) installed", installed)
	if skipped > 0 {
		fmt.Printf(", %d already present", skipped)
	}
	fmt.Println()

	return "installed", nil
}

// ---------------------------------------------------------------------------
// 8. Completion summary
// ---------------------------------------------------------------------------

func initPrintSummary(projectName, projectID, providerStatus, skillsStatus string) {
	fmt.Println()
	fmt.Println("Setup complete!")
	fmt.Println()
	fmt.Printf("  Project:  %s (%s)\n", projectName, projectID)
	fmt.Printf("  Provider: %s\n", providerStatus)
	fmt.Printf("  Skills:   %s\n", skillsStatus)
	fmt.Println()
	fmt.Println("Next steps:")
	fmt.Println("  memory query \"what do you know?\"   — query your project")
	fmt.Println("  memory docs upload <file>           — add documents")
	fmt.Println("  memory provider test                — verify LLM credentials")
}

// ---------------------------------------------------------------------------
// 9. Re-run verification helpers
// ---------------------------------------------------------------------------

// 9.1  Show current project and offer to switch.
func initVerifyProject(c *client.Client, currentID, currentName string) (string, string, error) {
	fmt.Printf("  Project: %s (%s)\n", currentName, currentID)
	yes, err := promptYesNoDefault("  Switch project? [y/N] ", false)
	if err != nil {
		return currentID, currentName, err
	}
	if !yes {
		return currentID, currentName, nil
	}
	return initSelectOrCreateProject(c)
}

// 9.2  Show provider status and offer to reconfigure.
func initVerifyProvider(c *client.Client) (string, error) {
	fmt.Println()
	orgID, err := resolveProviderOrgID(c, "")
	if err != nil {
		return "", fmt.Errorf("failed to determine organization: %w", err)
	}

	configs, err := c.SDK.Provider.ListOrgConfigs(context.Background(), orgID)
	if err != nil {
		return "", fmt.Errorf("failed to check provider config: %w", err)
	}

	if len(configs) > 0 {
		fmt.Printf("  Provider: %s (configured)\n", configs[0].Provider)
		yes, err := promptYesNoDefault("  Reconfigure provider? [y/N] ", false)
		if err != nil {
			return "already configured", err
		}
		if !yes {
			return "already configured", nil
		}
		return initProviderPicker(c, orgID)
	}

	fmt.Println("  Provider: not configured")
	yes, err := promptYesNoDefault("  Configure a provider now? [Y/n] ", true)
	if err != nil {
		return "skipped", err
	}
	if !yes {
		return "skipped", nil
	}
	return initProviderPicker(c, orgID)
}

// 9.3  Show skills status and offer to reinstall.
func initVerifySkills() (string, error) {
	fmt.Println()

	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	skillsDir := filepath.Join(cwd, ".agents", "skills")

	// Check if any memory-* skills exist.
	existing := 0
	if entries, err := os.ReadDir(skillsDir); err == nil {
		for _, e := range entries {
			if e.IsDir() && strings.HasPrefix(e.Name(), "memory-") {
				existing++
			}
		}
	}

	if existing > 0 {
		fmt.Printf("  Skills: %d memory skill(s) installed\n", existing)
		yes, err := promptYesNoDefault("  Reinstall skills (overwrites existing)? [y/N] ", false)
		if err != nil {
			return "installed", err
		}
		if !yes {
			return "installed", nil
		}
		// Remove existing and reinstall.
		return initInstallSkillsForce()
	}

	fmt.Println("  Skills: not installed")
	return initInstallSkills()
}

func initInstallSkillsForce() (string, error) {
	catalog := skillsfs.Catalog()

	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	targetDir := filepath.Join(cwd, ".agents", "skills")

	entries, err := fs.ReadDir(catalog, ".")
	if err != nil {
		return "", fmt.Errorf("reading embedded skills catalog: %w", err)
	}

	installed := 0
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasPrefix(name, "memory-") {
			continue
		}

		destDir := filepath.Join(targetDir, name)
		_ = os.RemoveAll(destDir)

		sub, err := fs.Sub(catalog, name)
		if err != nil {
			return "", fmt.Errorf("accessing skill %s: %w", name, err)
		}
		if err := copyFSTree(sub, destDir); err != nil {
			return "", fmt.Errorf("installing skill %s: %w", name, err)
		}

		fmt.Printf("  reinstalled %s\n", name)
		installed++
	}

	fmt.Printf("%d skill(s) reinstalled.\n", installed)
	return "installed", nil
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// promptYesNoDefault prints a prompt and reads a y/n answer from stdin.
// If the user presses Enter without typing anything, defaultYes determines the result.
func promptYesNoDefault(prompt string, defaultYes bool) (bool, error) {
	fmt.Print(prompt)
	scanner := bufio.NewScanner(os.Stdin)
	if !scanner.Scan() {
		if err := scanner.Err(); err != nil {
			return false, err
		}
		return false, nil
	}
	answer := strings.TrimSpace(scanner.Text())
	if answer == "" {
		return defaultYes, nil
	}
	return answer == "y" || answer == "Y", nil
}
