package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"text/tabwriter"
	"time"

	"github.com/emergent-company/emergent/apps/server-go/pkg/sdk/provider"
	"github.com/emergent-company/emergent/tools/emergent-cli/internal/client"
	"github.com/emergent-company/emergent/tools/emergent-cli/internal/config"
	"github.com/spf13/cobra"
)

// providerCmd is the root for the `emergent provider` command group.
var providerCmd = &cobra.Command{
	Use:   "provider",
	Short: "Manage LLM provider credentials and models",
	Long:  "Commands for managing LLM provider credentials, model selections, and usage reporting.",
}

// ── set-key ──────────────────────────────────────────────────────────────────

var setKeyCmd = &cobra.Command{
	Use:   "set-key <api-key>",
	Short: "Save a Google AI API key for the organization",
	Long: `Save a Google AI (Gemini) API key for the current organization.

The key is encrypted at rest and used for all projects that do not have a
project-level override.

Example:
  emergent provider set-key AIzaSy...`,
	Args: cobra.ExactArgs(1),
	RunE: runProviderSetKey,
}

var setKeyOrgID string

func runProviderSetKey(cmd *cobra.Command, args []string) error {
	apiKey := args[0]

	c, err := getClient(cmd)
	if err != nil {
		return err
	}

	orgID, err := resolveProviderOrgID(c, setKeyOrgID)
	if err != nil {
		return err
	}

	req := &provider.SaveGoogleAICredentialRequest{APIKey: apiKey}
	if err := c.SDK.Provider.SaveGoogleAICredential(context.Background(), orgID, req); err != nil {
		return fmt.Errorf("failed to save Google AI credential: %w", err)
	}

	fmt.Println("Google AI API key saved successfully.")
	fmt.Println("Run 'emergent provider models google-ai' to list available models.")
	return nil
}

// ── set-vertex ────────────────────────────────────────────────────────────────

var setVertexCmd = &cobra.Command{
	Use:   "set-vertex",
	Short: "Save Vertex AI credentials for the organization",
	Long: `Save Google Cloud Vertex AI credentials for the current organization.

Provide a service account JSON file, GCP project ID, and region.

Example:
  emergent provider set-vertex --project my-project --location us-central1 --sa-file sa.json`,
	RunE: runProviderSetVertex,
}

var (
	setVertexSAFile   string
	setVertexProject  string
	setVertexLocation string
	setVertexOrgID    string
)

func runProviderSetVertex(cmd *cobra.Command, args []string) error {
	var saJSON string
	if setVertexSAFile != "" {
		data, err := os.ReadFile(setVertexSAFile)
		if err != nil {
			return fmt.Errorf("failed to read service account file: %w", err)
		}
		saJSON = string(data)
	}

	c, err := getClient(cmd)
	if err != nil {
		return err
	}

	orgID, err := resolveProviderOrgID(c, setVertexOrgID)
	if err != nil {
		return err
	}

	req := &provider.SaveVertexAICredentialRequest{
		ServiceAccountJSON: saJSON,
		GCPProject:         setVertexProject,
		Location:           setVertexLocation,
	}
	if err := c.SDK.Provider.SaveVertexAICredential(context.Background(), orgID, req); err != nil {
		return fmt.Errorf("failed to save Vertex AI credential: %w", err)
	}

	fmt.Println("Vertex AI credentials saved successfully.")
	fmt.Println("Run 'emergent provider models vertex-ai' to list available models.")
	return nil
}

// ── list-credentials ──────────────────────────────────────────────────────────

var listCredentialsCmd = &cobra.Command{
	Use:   "list-credentials",
	Short: "List configured LLM provider credentials",
	RunE:  runProviderListCredentials,
}

var listCredentialsOrgID string

func runProviderListCredentials(cmd *cobra.Command, args []string) error {
	c, err := getClient(cmd)
	if err != nil {
		return err
	}

	orgID, err := resolveProviderOrgID(c, listCredentialsOrgID)
	if err != nil {
		return err
	}

	creds, err := c.SDK.Provider.ListOrgCredentials(context.Background(), orgID)
	if err != nil {
		return fmt.Errorf("failed to list credentials: %w", err)
	}

	if len(creds) == 0 {
		fmt.Println("No credentials configured.")
		fmt.Println("Run 'emergent provider set-key <api-key>' to configure Google AI.")
		return nil
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "PROVIDER\tGCP PROJECT\tLOCATION\tUPDATED")
	for _, cred := range creds {
		gcpProject := cred.GCPProject
		if gcpProject == "" {
			gcpProject = "-"
		}
		location := cred.Location
		if location == "" {
			location = "-"
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\n",
			cred.Provider,
			gcpProject,
			location,
			cred.UpdatedAt.Format(time.DateTime),
		)
	}
	return w.Flush()
}

// ── models ────────────────────────────────────────────────────────────────────

var providerModelsCmd = &cobra.Command{
	Use:   "models [provider]",
	Short: "List available models from the provider catalog",
	Long: `List models available in the cached model catalog.

Without a provider argument, lists models for all configured providers.
Pass a provider name to filter to a single provider.

Use --type to filter by model type (embedding or generative).

Examples:
  emergent provider models
  emergent provider models vertex-ai
  emergent provider models google-ai --type generative`,
	Args:      cobra.MaximumNArgs(1),
	ValidArgs: []string{"google-ai", "vertex-ai"},
	RunE:      runProviderModels,
}

var modelsTypeFlag string

func runProviderModels(cmd *cobra.Command, args []string) error {
	c, err := getClient(cmd)
	if err != nil {
		return err
	}

	ctx := context.Background()

	// Explicit provider: single-provider behaviour
	if len(args) > 0 {
		providerArg := args[0]
		models, err := c.SDK.Provider.ListModels(ctx, providerArg, modelsTypeFlag)
		if err != nil {
			return fmt.Errorf("failed to list models: %w", err)
		}
		if len(models) == 0 {
			fmt.Printf("No models cached for provider %q.\n", providerArg)
			fmt.Println("Check that credentials are configured with 'emergent provider list-credentials'.")
			return nil
		}
		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "MODEL\tTYPE\tDISPLAY NAME")
		for _, m := range models {
			display := m.DisplayName
			if display == "" {
				display = "-"
			}
			fmt.Fprintf(w, "%s\t%s\t%s\n", m.ModelName, m.ModelType, display)
		}
		return w.Flush()
	}

	// No provider argument: discover configured providers and list models for all
	configPath, _ := cmd.Flags().GetString("config")
	if configPath == "" {
		configPath = config.DiscoverPath("")
	}
	cfg, err := config.LoadWithEnv(configPath)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	orgID := cfg.OrgID
	if orgID == "" {
		// Fall back to API discovery if not in config
		orgID, err = resolveProviderOrgID(c, "")
		if err != nil {
			return fmt.Errorf("failed to resolve organization: %w", err)
		}
	}

	creds, err := c.SDK.Provider.ListOrgCredentials(ctx, orgID)
	if err != nil {
		return fmt.Errorf("failed to list credentials: %w", err)
	}
	if len(creds) == 0 {
		fmt.Println("No providers configured.")
		fmt.Println("Run 'emergent provider set-key <api-key>' or 'emergent provider set-vertex' to configure a provider.")
		return nil
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "PROVIDER\tMODEL\tTYPE\tDISPLAY NAME")

	anyModels := false
	for _, cred := range creds {
		models, err := c.SDK.Provider.ListModels(ctx, cred.Provider, modelsTypeFlag)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: could not fetch models for %s: %v\n", cred.Provider, err)
			continue
		}
		for _, m := range models {
			display := m.DisplayName
			if display == "" {
				display = "-"
			}
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", cred.Provider, m.ModelName, m.ModelType, display)
			anyModels = true
		}
	}

	if !anyModels {
		w.Flush()
		fmt.Println("No models cached for any configured provider.")
		fmt.Println("Tip: use --type embedding or --type generative to filter by model type.")
		return nil
	}

	return w.Flush()
}

// ── usage ─────────────────────────────────────────────────────────────────────

var providerUsageCmd = &cobra.Command{
	Use:   "usage",
	Short: "Show LLM usage and estimated cost",
	Long: `Show aggregated LLM token usage and estimated cost.

Without --project, reports org-wide usage across all projects.
With --project, reports usage for that specific project.

Examples:
  emergent provider usage
  emergent provider usage --project <id>
  emergent provider usage --since 2024-01-01`,
	RunE: runProviderUsage,
}

var (
	usageProjectID string
	usageSince     string
	usageUntil     string
	usageOrgID     string
	usageJSONFlag  bool
)

func runProviderUsage(cmd *cobra.Command, args []string) error {
	c, err := getClient(cmd)
	if err != nil {
		return err
	}

	var since, until time.Time
	if usageSince != "" {
		t, err := time.Parse(time.DateOnly, usageSince)
		if err != nil {
			return fmt.Errorf("invalid --since value %q: expected YYYY-MM-DD", usageSince)
		}
		since = t
	}
	if usageUntil != "" {
		t, err := time.Parse(time.DateOnly, usageUntil)
		if err != nil {
			return fmt.Errorf("invalid --until value %q: expected YYYY-MM-DD", usageUntil)
		}
		until = t
	}

	var summary *provider.UsageSummary

	if usageProjectID != "" {
		summary, err = c.SDK.Provider.GetProjectUsage(context.Background(), usageProjectID, since, until)
		if err != nil {
			return fmt.Errorf("failed to get project usage: %w", err)
		}
	} else {
		orgID, err := resolveProviderOrgID(c, usageOrgID)
		if err != nil {
			return err
		}
		summary, err = c.SDK.Provider.GetOrgUsage(context.Background(), orgID, since, until)
		if err != nil {
			return fmt.Errorf("failed to get org usage: %w", err)
		}
	}

	if usageJSONFlag {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(summary)
	}

	if summary.Note != "" {
		fmt.Println("Note:", summary.Note)
		fmt.Println()
	}

	if len(summary.Data) == 0 {
		fmt.Println("No usage data found for the specified period.")
		return nil
	}

	var totalCost float64
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "PROVIDER\tMODEL\tTEXT IN\tIMAGE\tVIDEO\tAUDIO\tOUTPUT\tEST. COST (USD)")
	for _, row := range summary.Data {
		fmt.Fprintf(w, "%s\t%s\t%d\t%d\t%d\t%d\t%d\t$%.4f\n",
			row.Provider,
			row.Model,
			row.TotalText,
			row.TotalImage,
			row.TotalVideo,
			row.TotalAudio,
			row.TotalOutput,
			row.EstimatedCostUSD,
		)
		totalCost += row.EstimatedCostUSD
	}
	_ = w.Flush()
	fmt.Printf("\nTotal estimated cost: $%.4f\n", totalCost)
	return nil
}

// ── resolveProviderOrgID helper ───────────────────────────────────────────────

// resolveProviderOrgID returns the explicit orgID if provided; otherwise it
// auto-detects by listing the caller's organizations and returning the first.
func resolveProviderOrgID(c *client.Client, explicit string) (string, error) {
	if explicit != "" {
		return explicit, nil
	}

	orgs, err := c.SDK.Orgs.List(context.Background())
	if err != nil {
		return "", fmt.Errorf("failed to list organizations: %w", err)
	}
	if len(orgs) == 0 {
		return "", fmt.Errorf("no organizations found. Create one first or specify --org-id")
	}
	if len(orgs) > 1 {
		fmt.Fprintf(os.Stderr, "Multiple organizations found; using %q (%s). Use --org-id to specify another.\n",
			orgs[0].Name, orgs[0].ID)
	}
	return orgs[0].ID, nil
}

func init() {
	// set-key flags
	setKeyCmd.Flags().StringVar(&setKeyOrgID, "org-id", "", "Organization ID (auto-detected from config)")

	// set-vertex flags
	setVertexCmd.Flags().StringVar(&setVertexSAFile, "sa-file", "", "Path to service account JSON key file")
	setVertexCmd.Flags().StringVar(&setVertexProject, "project", "", "GCP project ID (required)")
	setVertexCmd.Flags().StringVar(&setVertexLocation, "location", "", "GCP region, e.g. us-central1 (required)")
	setVertexCmd.Flags().StringVar(&setVertexOrgID, "org-id", "", "Organization ID (auto-detected from config)")
	_ = setVertexCmd.MarkFlagRequired("project")
	_ = setVertexCmd.MarkFlagRequired("location")

	// list-credentials flags
	listCredentialsCmd.Flags().StringVar(&listCredentialsOrgID, "org-id", "", "Organization ID (auto-detected from config)")

	// models flags
	providerModelsCmd.Flags().StringVar(&modelsTypeFlag, "type", "", "Filter by model type: embedding or generative")

	// usage flags
	providerUsageCmd.Flags().StringVar(&usageProjectID, "project", "", "Filter usage to a specific project ID")
	providerUsageCmd.Flags().StringVar(&usageSince, "since", "", "Start date for usage window (YYYY-MM-DD)")
	providerUsageCmd.Flags().StringVar(&usageUntil, "until", "", "End date for usage window (YYYY-MM-DD)")
	providerUsageCmd.Flags().StringVar(&usageOrgID, "org-id", "", "Organization ID (auto-detected from config)")
	providerUsageCmd.Flags().BoolVar(&usageJSONFlag, "json", false, "Output raw JSON")

	// Wire sub-commands
	providerCmd.AddCommand(setKeyCmd)
	providerCmd.AddCommand(setVertexCmd)
	providerCmd.AddCommand(listCredentialsCmd)
	providerCmd.AddCommand(providerModelsCmd)
	providerCmd.AddCommand(providerUsageCmd)

	rootCmd.AddCommand(providerCmd)
}
