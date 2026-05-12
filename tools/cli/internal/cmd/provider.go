package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"text/tabwriter"
	"time"

	"github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/provider"
	"github.com/emergent-company/emergent.memory/tools/cli/internal/client"
	"github.com/spf13/cobra"
)

// providerCmd is the root for the `memory provider` command group.
var providerCmd = &cobra.Command{
	Use:     "provider",
	Short:   "Manage LLM provider credentials and models",
	Long:    "Commands for managing LLM provider credentials, model selections, and usage reporting.",
	GroupID: "ai",
}

// ── configure (org-level) ─────────────────────────────────────────────────────

var configureCmd = &cobra.Command{
	Use:   "configure <provider>",
	Short: "Save LLM provider credentials and model selections for the organization",
	Long: `Save LLM provider credentials (and optionally model selections) for the
current organization. Runs a live credential test and syncs the model catalog
on success. Models are auto-selected from the catalog if not specified.

Supported providers:
  google            — Google AI (Gemini API); requires --api-key
  google-vertex     — Google Cloud Vertex AI; requires --gcp-project, --location
  openai-compatible — OpenAI-compatible API (Ollama, vLLM, etc.); requires --api-key, --base-url, --generative-model
  deepseek          — DeepSeek AI models; requires --api-key

Examples:
  memory provider configure google --api-key AIzaSy...
  memory provider configure google-vertex --gcp-project my-project --location us-central1 --key-file sa.json
  memory provider configure openai-compatible --api-key sk-... --base-url http://localhost:11434/v1 --generative-model llama3
  memory provider configure google --api-key AIzaSy... --generative-model gemini-2.5-flash --embedding-model text-embedding-004
  memory provider configure deepseek --api-key sk-...
  memory provider configure deepseek --api-key sk-... --generative-model deepseek-v4-flash`,
	Args:      cobra.ExactArgs(1),
	ValidArgs: []string{"google", "google-vertex", "openai-compatible", "deepseek"},
	RunE:      runProviderConfigure,
}

var (
	configureAPIKey          string
	configureKeyFile         string
	configureGCPProject      string
	configureLocation        string
	configureBaseURL         string
	configureGenerativeModel string
	configureEmbeddingModel  string
	configureOrgID           string
)

func runProviderConfigure(cmd *cobra.Command, args []string) error {
	providerArg := args[0]

	c, err := getClient(cmd)
	if err != nil {
		return err
	}

	orgID, err := resolveProviderOrgID(c, configureOrgID)
	if err != nil {
		return err
	}

	req := &provider.UpsertProviderConfigRequest{
		GenerativeModel: configureGenerativeModel,
		EmbeddingModel:  configureEmbeddingModel,
	}

	switch providerArg {
	case provider.ProviderGoogleAI:
		if configureAPIKey == "" {
			return fmt.Errorf("--api-key is required for google")
		}
		req.APIKey = configureAPIKey

	case provider.ProviderVertexAI:
		if configureGCPProject == "" {
			return fmt.Errorf("--gcp-project is required for google-vertex")
		}
		if configureLocation == "" {
			return fmt.Errorf("--location is required for google-vertex")
		}
		if configureKeyFile != "" {
			data, err := os.ReadFile(configureKeyFile)
			if err != nil {
				return fmt.Errorf("failed to read key file: %w", err)
			}
			req.ServiceAccountJSON = string(data)
		}
		req.GCPProject = configureGCPProject
		req.Location = configureLocation

	case provider.ProviderOpenAICompatible:
		if configureAPIKey == "" {
			return fmt.Errorf("--api-key is required for openai-compatible")
		}
		if configureBaseURL == "" {
			return fmt.Errorf("--base-url is required for openai-compatible")
		}
		if configureGenerativeModel == "" {
			return fmt.Errorf("--generative-model is required for openai-compatible")
		}
		req.APIKey = configureAPIKey
		req.BaseURL = configureBaseURL

	case provider.ProviderDeepSeek:
		if configureAPIKey == "" {
			return fmt.Errorf("--api-key is required for deepseek")
		}
		req.APIKey = configureAPIKey

	default:
		return fmt.Errorf("unsupported provider %q; must be google, google-vertex, openai-compatible, or deepseek", providerArg)
	}

	fmt.Printf("Configuring %s for org %s...\n", providerArg, orgID)
	cfg, err := c.SDK.Provider.UpsertOrgConfig(context.Background(), orgID, providerArg, req)
	if err != nil {
		return fmt.Errorf("failed to configure %s: %w", providerArg, err)
	}

	fmt.Printf("%s configured successfully.\n", providerArg)
	if cfg.GenerativeModel != "" {
		fmt.Printf("  Generative model: %s\n", cfg.GenerativeModel)
	}
	if cfg.EmbeddingModel != "" {
		fmt.Printf("  Embedding model:  %s\n", cfg.EmbeddingModel)
	}
	fmt.Printf("Run 'memory provider test' to verify the configuration.\n")
	return nil
}

// ── configure-project (project-level) ────────────────────────────────────────

var configureProjectCmd = &cobra.Command{
	Use:   "configure-project <provider>",
	Short: "Save project-level LLM provider credentials (overrides org config)",
	Long: `Save project-specific credentials and model selections for the given provider.
This overrides the organization's provider config for this project.

Use --remove to remove the project-level override and fall back to the org config.

Supported providers:
  google            — Google AI (Gemini API); requires --api-key
  google-vertex     — Google Cloud Vertex AI; requires --gcp-project, --location
  openai-compatible — OpenAI-compatible API (Ollama, vLLM, etc.); requires --api-key, --base-url, --generative-model
  deepseek          — DeepSeek AI models; requires --api-key

The project is read from --project or the MEMORY_PROJECT_ID environment variable.

Examples:
  memory provider configure-project google --api-key AIzaSy...
  memory provider configure-project google-vertex --gcp-project my-proj --location us-central1 --key-file sa.json
  memory provider configure-project openai-compatible --api-key sk-... --base-url http://localhost:11434/v1 --generative-model llama3
  memory provider configure-project deepseek --api-key sk-... --generative-model deepseek-v4-flash
  memory provider configure-project google --remove`,
	Args:      cobra.ExactArgs(1),
	ValidArgs: []string{"google", "google-vertex", "openai-compatible", "deepseek"},
	RunE:      runProviderConfigureProject,
}

var (
	configureProjectAPIKey          string
	configureProjectKeyFile         string
	configureProjectGCPProject      string
	configureProjectLocation        string
	configureProjectBaseURL         string
	configureProjectGenerativeModel string
	configureProjectEmbeddingModel  string
	configureProjectID              string
	configureProjectRemove          bool
)

func runProviderConfigureProject(cmd *cobra.Command, args []string) error {
	providerArg := args[0]

	c, err := getClient(cmd)
	if err != nil {
		return err
	}

	// Resolve project ID: flag → env var → error
	projectID := configureProjectID
	if projectID == "" {
		projectID = os.Getenv("MEMORY_PROJECT_ID")
	}
	if projectID == "" {
		return fmt.Errorf("--project is required (or set MEMORY_PROJECT_ID / MEMORY_PROJECT in .env.local)")
	}

	if configureProjectRemove {
		if err := c.SDK.Provider.DeleteProjectConfig(context.Background(), projectID, providerArg); err != nil {
			return fmt.Errorf("failed to remove project config for %s: %w", providerArg, err)
		}
		fmt.Printf("Project-level %s config removed. The project will now inherit the org config.\n", providerArg)
		return nil
	}

	req := &provider.UpsertProviderConfigRequest{
		GenerativeModel: configureProjectGenerativeModel,
		EmbeddingModel:  configureProjectEmbeddingModel,
	}

	switch providerArg {
	case provider.ProviderGoogleAI:
		if configureProjectAPIKey == "" {
			return fmt.Errorf("--api-key is required for google")
		}
		req.APIKey = configureProjectAPIKey

	case provider.ProviderVertexAI:
		if configureProjectGCPProject == "" {
			return fmt.Errorf("--gcp-project is required for google-vertex")
		}
		if configureProjectLocation == "" {
			return fmt.Errorf("--location is required for google-vertex")
		}
		if configureProjectKeyFile != "" {
			data, err := os.ReadFile(configureProjectKeyFile)
			if err != nil {
				return fmt.Errorf("failed to read key file: %w", err)
			}
			req.ServiceAccountJSON = string(data)
		}
		req.GCPProject = configureProjectGCPProject
		req.Location = configureProjectLocation

	case provider.ProviderOpenAICompatible:
		if configureProjectAPIKey == "" {
			return fmt.Errorf("--api-key is required for openai-compatible")
		}
		if configureProjectBaseURL == "" {
			return fmt.Errorf("--base-url is required for openai-compatible")
		}
		if configureProjectGenerativeModel == "" {
			return fmt.Errorf("--generative-model is required for openai-compatible")
		}
		req.APIKey = configureProjectAPIKey
		req.BaseURL = configureProjectBaseURL

	case provider.ProviderDeepSeek:
		if configureProjectAPIKey == "" {
			return fmt.Errorf("--api-key is required for deepseek")
		}
		req.APIKey = configureProjectAPIKey

	default:
		return fmt.Errorf("unsupported provider %q; must be google, google-vertex, openai-compatible, or deepseek", providerArg)
	}

	fmt.Printf("Configuring %s for project %s...\n", providerArg, projectID)
	cfg, err := c.SDK.Provider.UpsertProjectConfig(context.Background(), projectID, providerArg, req)
	if err != nil {
		return fmt.Errorf("failed to configure project %s: %w", providerArg, err)
	}

	fmt.Printf("%s configured successfully for project %s.\n", providerArg, projectID)
	if cfg.GenerativeModel != "" {
		fmt.Printf("  Generative model: %s\n", cfg.GenerativeModel)
	}
	if cfg.EmbeddingModel != "" {
		fmt.Printf("  Embedding model:  %s\n", cfg.EmbeddingModel)
	}
	fmt.Printf("Run 'memory provider test --project %s' to verify the configuration.\n", projectID)
	return nil
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
  memory provider models
  memory provider models openai-compatible
  memory provider models google-vertex
  memory provider models google --type generative
  memory provider models deepseek`,
	Args:      cobra.MaximumNArgs(1),
	ValidArgs: []string{"google", "google-vertex", "openai-compatible", "deepseek"},
	RunE:      runProviderModels,
}

var modelsTypeFlag string
var modelsOrgID string

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
			fmt.Println("Check that credentials are configured with 'memory provider configure'.")
			return nil
		}
		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "MODEL\tTYPE\tSLUG")
		for _, m := range sortModelsByType(models) {
			fmt.Fprintf(w, "%s\t%s\t%s/%s\n", m.ModelName, m.ModelType, providerArg, m.ModelName)
		}
		return w.Flush()
	}

	// No provider argument: iterate all orgs and collect models from every
	// configured provider. Orgs that 403 (JWT not scoped to them) are skipped
	// silently. Providers seen in multiple orgs are deduplicated.
	orgs, err := c.SDK.Orgs.List(ctx)
	if err != nil {
		return fmt.Errorf("failed to list organizations: %w", err)
	}
	if len(orgs) == 0 {
		fmt.Println("No organizations found.")
		return nil
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "PROVIDER\tMODEL\tTYPE\tSLUG")

	seenProviders := map[string]bool{}
	anyModels := false

	for _, org := range orgs {
		configs, listErr := c.SDK.Provider.ListOrgConfigs(ctx, org.ID)
		if listErr != nil {
			// 403 expected for orgs not matching the JWT scope — skip silently.
			continue
		}
		for _, pc := range configs {
			if seenProviders[pc.Provider] {
				continue
			}
			seenProviders[pc.Provider] = true
			models, modelsErr := c.SDK.Provider.ListModels(ctx, pc.Provider, modelsTypeFlag)
			if modelsErr != nil {
				fmt.Fprintf(os.Stderr, "Warning: could not fetch models for %s: %v\n", pc.Provider, modelsErr)
				continue
			}
			for _, m := range sortModelsByType(models) {
				fmt.Fprintf(w, "%s\t%s\t%s\t%s/%s\n", pc.Provider, m.ModelName, m.ModelType, pc.Provider, m.ModelName)
				anyModels = true
			}
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

Use --by-project to break org-wide totals down per project instead of per model.

Output is a table with columns: PROVIDER, MODEL, TEXT IN (tokens), IMAGE
(tokens), VIDEO (tokens), AUDIO (tokens), OUTPUT (tokens), and EST. COST (USD).
A total estimated cost line is printed below the table.

Examples:
  memory provider usage
  memory provider usage --project <id>
  memory provider usage --since 2024-01-01
  memory provider usage --by-project`,
	RunE: runProviderUsage,
}

var (
	usageProjectID string
	usageSince     string
	usageUntil     string
	usageOrgID     string
	usageJSONFlag  bool
	usageByProject bool
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

	// --by-project: org-wide breakdown grouped by project
	if usageByProject {
		orgID, err := resolveProviderOrgID(c, usageOrgID)
		if err != nil {
			return err
		}
		result, err := c.SDK.Provider.GetOrgUsageByProject(context.Background(), orgID, since, until)
		if err != nil {
			return fmt.Errorf("failed to get org usage by project: %w", err)
		}

		if usageJSONFlag || output == "json" {
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			return enc.Encode(result)
		}

		if result.Note != "" {
			fmt.Println("Note:", result.Note)
			fmt.Println()
		}

		if len(result.Data) == 0 {
			fmt.Println("No usage data found for the specified period.")
			return nil
		}

		var totalCost float64
		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "PROJECT\tTEXT IN\tIMAGE\tVIDEO\tAUDIO\tOUTPUT\tEST. COST (USD)")
		for _, row := range result.Data {
			name := row.ProjectName
			if name == "" {
				name = row.ProjectID
			}
			fmt.Fprintf(w, "%s\t%d\t%d\t%d\t%d\t%d\t$%.4f\n",
				name,
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

	if usageJSONFlag || output == "json" {
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

// ── usage timeseries ──────────────────────────────────────────────────────────

var providerUsageTimeseriesCmd = &cobra.Command{
	Use:   "timeseries",
	Short: "Show LLM usage over time",
	Long: `Show LLM token usage and estimated cost broken down by time period.

Without --project, reports org-wide usage. With --project, reports usage for
that specific project. Use --granularity to control bucket size (default: day).

Output is a table with columns: PERIOD, PROVIDER, MODEL, TEXT IN, IMAGE, VIDEO,
AUDIO, OUTPUT, and EST. COST (USD). A running subtotal is shown per period.

Examples:
  memory provider timeseries
  memory provider timeseries --project <id> --granularity week
  memory provider timeseries --since 2024-01-01 --until 2024-03-31 --granularity month`,
	RunE: runProviderUsageTimeseries,
}

var (
	timeseriesProjectID   string
	timeseriesSince       string
	timeseriesUntil       string
	timeseriesOrgID       string
	timeseriesGranularity string
	timeseriesJSONFlag    bool
)

func runProviderUsageTimeseries(cmd *cobra.Command, args []string) error {
	c, err := getClient(cmd)
	if err != nil {
		return err
	}

	var since, until time.Time
	if timeseriesSince != "" {
		t, err := time.Parse(time.DateOnly, timeseriesSince)
		if err != nil {
			return fmt.Errorf("invalid --since value %q: expected YYYY-MM-DD", timeseriesSince)
		}
		since = t
	}
	if timeseriesUntil != "" {
		t, err := time.Parse(time.DateOnly, timeseriesUntil)
		if err != nil {
			return fmt.Errorf("invalid --until value %q: expected YYYY-MM-DD", timeseriesUntil)
		}
		until = t
	}

	gran := timeseriesGranularity
	if gran == "" {
		gran = "day"
	}

	var result *provider.UsageTimeSeries

	if timeseriesProjectID != "" {
		result, err = c.SDK.Provider.GetProjectUsageTimeSeries(context.Background(), timeseriesProjectID, gran, since, until)
		if err != nil {
			return fmt.Errorf("failed to get project usage timeseries: %w", err)
		}
	} else {
		orgID, err := resolveProviderOrgID(c, timeseriesOrgID)
		if err != nil {
			return err
		}
		result, err = c.SDK.Provider.GetOrgUsageTimeSeries(context.Background(), orgID, gran, since, until)
		if err != nil {
			return fmt.Errorf("failed to get org usage timeseries: %w", err)
		}
	}

	if timeseriesJSONFlag || output == "json" {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(result)
	}

	if result.Note != "" {
		fmt.Println("Note:", result.Note)
		fmt.Println()
	}

	if len(result.Data) == 0 {
		fmt.Println("No usage data found for the specified period.")
		return nil
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "PERIOD\tPROVIDER\tMODEL\tTEXT IN\tIMAGE\tVIDEO\tAUDIO\tOUTPUT\tEST. COST (USD)")

	var periodTotal float64
	var currentPeriod string
	for i, row := range result.Data {
		period := row.Period.Format("2006-01-02")
		if period != currentPeriod {
			// Print subtotal for the previous period before moving to the next
			if currentPeriod != "" {
				fmt.Fprintf(w, "\t\t  subtotal\t\t\t\t\t\t$%.4f\n", periodTotal)
			}
			currentPeriod = period
			periodTotal = 0
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%d\t%d\t%d\t%d\t%d\t$%.4f\n",
			period,
			row.Provider,
			row.Model,
			row.TotalText,
			row.TotalImage,
			row.TotalVideo,
			row.TotalAudio,
			row.TotalOutput,
			row.EstimatedCostUSD,
		)
		periodTotal += row.EstimatedCostUSD

		// Print subtotal at the end of the last period
		if i == len(result.Data)-1 {
			fmt.Fprintf(w, "\t\t  subtotal\t\t\t\t\t\t$%.4f\n", periodTotal)
		}
	}
	return w.Flush()
}

// ── test ──────────────────────────────────────────────────────────────────────

var providerTestCmd = &cobra.Command{
	Use:   "test [provider]",
	Short: "Test LLM provider credentials with a live generate call",
	Long: `Send a live "say hello" generate call to verify that provider credentials
work end-to-end.

Without a provider argument, tests all configured providers.
Pass a provider name (google, google-vertex, openai-compatible, or deepseek) to test a specific one.

Use --project to test using the project-level credential hierarchy
(project override → org) instead of org credentials only.

Examples:
  memory provider test
  memory provider test openai-compatible
  memory provider test deepseek
  memory provider test google-vertex
  memory provider test google --project <id>`,
	Args:      cobra.MaximumNArgs(1),
	ValidArgs: []string{"google", "google-vertex", "openai-compatible", "deepseek"},
	RunE:      runProviderTest,
}

var (
	testProviderOrgID     string
	testProviderProjectID string
)

func runProviderTest(cmd *cobra.Command, args []string) error {
	c, err := getClient(cmd)
	if err != nil {
		return err
	}

	ctx := context.Background()

	// If --project was not explicitly passed, fall back to MEMORY_PROJECT_ID
	// from the environment (auto-loaded from .env.local by the CLI).
	if testProviderProjectID == "" {
		testProviderProjectID = os.Getenv("MEMORY_PROJECT_ID")
	}

	// Always resolve the org ID so we can pass it to TestProvider for credential resolution.
	resolvedOrgID, err := resolveProviderOrgID(c, testProviderOrgID)
	if err != nil {
		return err
	}

	// Build list of providers to test
	var providers []string
	if len(args) > 0 {
		providers = []string{args[0]}
	} else {
		// Auto-discover from configured org configs
		configs, err := c.SDK.Provider.ListOrgConfigs(ctx, resolvedOrgID)
		if err != nil {
			return fmt.Errorf("failed to list provider configs: %w", err)
		}
		if len(configs) == 0 {
			fmt.Println("No providers configured.")
			fmt.Println("Run 'memory provider configure google --api-key <key>' to configure a provider.")
			return nil
		}
		for _, pc := range configs {
			providers = append(providers, pc.Provider)
		}
	}

	anyFailed := false
	for _, p := range providers {
		fmt.Printf("Testing %s... ", p)
		result, err := c.SDK.Provider.TestProvider(ctx, p, testProviderProjectID, resolvedOrgID)
		if err != nil {
			fmt.Printf("FAILED\n  Error: %v\n", err)
			anyFailed = true
			continue
		}
		fmt.Printf("OK (%dms)\n", result.LatencyMs)
		fmt.Printf("  Model:  %s\n", result.Model)
		fmt.Printf("  Reply:  %s\n", result.Reply)
	}

	if anyFailed {
		return fmt.Errorf("one or more provider tests failed")
	}
	return nil
}

// ── list ──────────────────────────────────────────────────────────────────────

var providerListCmd = &cobra.Command{
	Use:   "list",
	Short: "Show current provider configurations",
	Long: `List all configured LLM providers at the organization level, plus any
project-level overrides across all projects in the organization.

The output is a table with columns: SCOPE, PROVIDER, GENERATIVE MODEL,
EMBEDDING MODEL, GCP PROJECT, LOCATION, and UPDATED.

Use --project to filter results to a single project (name or ID).
If multiple projects share the same name, an error is returned with all matching IDs.

Examples:
  memory provider list
  memory provider list --org-id <id>
  memory provider list --project my-project
  memory provider list --json`,
	RunE: runProviderList,
}

var (
	listOrgID     string
	listJSONFlag  bool
	listProjectID string
)

func runProviderList(cmd *cobra.Command, _ []string) error {
	c, err := getClient(cmd)
	if err != nil {
		return err
	}

	orgID, err := resolveProviderOrgID(c, listOrgID)
	if err != nil {
		return err
	}

	ctx := context.Background()

	// Resolve --project flag to a project ID (supports name or ID).
	var filterProjectID string
	if listProjectID != "" {
		filterProjectID, err = resolveProjectNameOrID(c, listProjectID)
		if err != nil {
			return err
		}
	}

	// Fetch org-level configs and project-level overrides in parallel.
	orgConfigs, orgErr := c.SDK.Provider.ListOrgConfigs(ctx, orgID)
	if orgErr != nil {
		return fmt.Errorf("failed to list org provider configs: %w", orgErr)
	}

	projectConfigs, projErr := c.SDK.Provider.ListProjectConfigsByOrg(ctx, orgID)
	if projErr != nil {
		return fmt.Errorf("failed to list project provider configs: %w", projErr)
	}

	// Filter to a specific project if requested.
	if filterProjectID != "" {
		var filtered []provider.ProjectProviderConfig
		for _, cfg := range projectConfigs {
			if cfg.ProjectID == filterProjectID {
				filtered = append(filtered, cfg)
			}
		}
		projectConfigs = filtered
		// Keep orgConfigs — they represent the inherited base for the project.
	}

	if len(orgConfigs) == 0 && len(projectConfigs) == 0 {
		if filterProjectID != "" {
			fmt.Printf("No providers configured for project %q.\n", listProjectID)
		} else {
			fmt.Println("No providers configured.")
			fmt.Println("Run 'memory provider configure google --api-key <key>' to get started.")
		}
		return nil
	}

	// JSON output
	if listJSONFlag || output == "json" {
		out := struct {
			Org      []provider.ProviderConfig        `json:"org"`
			Projects []provider.ProjectProviderConfig `json:"projects"`
		}{
			Org:      orgConfigs,
			Projects: projectConfigs,
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(out)
	}

	// Table output
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "SCOPE\tPROVIDER\tGENERATIVE MODEL\tEMBEDDING MODEL\tBASE URL\tGCP PROJECT\tLOCATION\tUPDATED")

	for _, cfg := range orgConfigs {
		fmt.Fprintf(w, "org\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			cfg.Provider,
			valueOrDash(cfg.GenerativeModel),
			valueOrDash(cfg.EmbeddingModel),
			valueOrDash(cfg.BaseURL),
			valueOrDash(cfg.GCPProject),
			valueOrDash(cfg.Location),
			cfg.UpdatedAt.Format(time.DateOnly),
		)
	}
	for _, cfg := range projectConfigs {
		scope := "project:" + shortID(cfg.ProjectID)
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			scope,
			cfg.Provider,
			valueOrDash(cfg.GenerativeModel),
			valueOrDash(cfg.EmbeddingModel),
			valueOrDash(cfg.BaseURL),
			valueOrDash(cfg.GCPProject),
			valueOrDash(cfg.Location),
			cfg.UpdatedAt.Format(time.DateOnly),
		)
	}
	return w.Flush()
}

// valueOrDash returns the value or "-" if empty.
func valueOrDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

// shortID returns the first 8 characters of a UUID for compact display.
func shortID(id string) string {
	if len(id) > 8 {
		return id[:8]
	}
	return id
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
	// configure flags
	configureCmd.Flags().StringVar(&configureAPIKey, "api-key", "", "API key (required for google)")
	configureCmd.Flags().StringVar(&configureKeyFile, "key-file", "", "Path to service account JSON key file (google-vertex)")
	configureCmd.Flags().StringVar(&configureGCPProject, "gcp-project", "", "GCP project ID (required for google-vertex)")
	configureCmd.Flags().StringVar(&configureLocation, "location", "", "GCP region, e.g. us-central1 (required for google-vertex)")
	configureCmd.Flags().StringVar(&configureBaseURL, "base-url", "", "OpenAI-compatible base URL (required for openai-compatible)")
	configureCmd.Flags().StringVar(&configureGenerativeModel, "generative-model", "", "Generative model to use (auto-selected from catalog if omitted)")
	configureCmd.Flags().StringVar(&configureEmbeddingModel, "embedding-model", "", "Embedding model to use (auto-selected from catalog if omitted)")
	configureCmd.Flags().StringVar(&configureOrgID, "org-id", "", "Organization ID (auto-detected from config)")

	// configure-project flags
	configureProjectCmd.Flags().StringVar(&configureProjectAPIKey, "api-key", "", "API key (required for google)")
	configureProjectCmd.Flags().StringVar(&configureProjectKeyFile, "key-file", "", "Path to service account JSON key file (google-vertex)")
	configureProjectCmd.Flags().StringVar(&configureProjectGCPProject, "gcp-project", "", "GCP project ID (required for google-vertex)")
	configureProjectCmd.Flags().StringVar(&configureProjectLocation, "location", "", "GCP region, e.g. us-central1 (required for google-vertex)")
	configureProjectCmd.Flags().StringVar(&configureProjectBaseURL, "base-url", "", "OpenAI-compatible base URL (required for openai-compatible)")
	configureProjectCmd.Flags().StringVar(&configureProjectGenerativeModel, "generative-model", "", "Generative model to use (auto-selected from catalog if omitted)")
	configureProjectCmd.Flags().StringVar(&configureProjectEmbeddingModel, "embedding-model", "", "Embedding model to use (auto-selected from catalog if omitted)")
	configureProjectCmd.Flags().StringVar(&configureProjectID, "project", "", "Project ID (auto-detected from MEMORY_PROJECT_ID)")
	configureProjectCmd.Flags().BoolVar(&configureProjectRemove, "remove", false, "Remove the project-level override and inherit org config")

	// models flags
	providerModelsCmd.Flags().StringVar(&modelsTypeFlag, "type", "", "Filter by model type: embedding or generative")
	providerModelsCmd.Flags().StringVar(&modelsOrgID, "org-id", "", "Organization ID (auto-detected from config)")

	// usage flags
	providerUsageCmd.Flags().StringVar(&usageProjectID, "project", "", "Filter usage to a specific project ID")
	providerUsageCmd.Flags().StringVar(&usageSince, "since", "", "Start date for usage window (YYYY-MM-DD)")
	providerUsageCmd.Flags().StringVar(&usageUntil, "until", "", "End date for usage window (YYYY-MM-DD)")
	providerUsageCmd.Flags().StringVar(&usageOrgID, "org-id", "", "Organization ID (auto-detected from config)")
	providerUsageCmd.Flags().BoolVar(&usageJSONFlag, "json", false, "Output raw JSON")
	providerUsageCmd.Flags().BoolVar(&usageByProject, "by-project", false, "Break down org usage by project instead of by model")

	// timeseries flags
	providerUsageTimeseriesCmd.Flags().StringVar(&timeseriesProjectID, "project", "", "Filter to a specific project ID")
	providerUsageTimeseriesCmd.Flags().StringVar(&timeseriesSince, "since", "", "Start date (YYYY-MM-DD)")
	providerUsageTimeseriesCmd.Flags().StringVar(&timeseriesUntil, "until", "", "End date (YYYY-MM-DD)")
	providerUsageTimeseriesCmd.Flags().StringVar(&timeseriesOrgID, "org-id", "", "Organization ID (auto-detected from config)")
	providerUsageTimeseriesCmd.Flags().StringVar(&timeseriesGranularity, "granularity", "day", "Time bucket size: day, week, or month")
	providerUsageTimeseriesCmd.Flags().BoolVar(&timeseriesJSONFlag, "json", false, "Output raw JSON")

	// test flags
	providerTestCmd.Flags().StringVar(&testProviderOrgID, "org-id", "", "Organization ID (auto-detected from config)")
	providerTestCmd.Flags().StringVar(&testProviderProjectID, "project", "", "Project ID for project-level credential resolution")

	// list flags
	providerListCmd.Flags().StringVar(&listOrgID, "org-id", "", "Organization ID (auto-detected from config)")
	providerListCmd.Flags().BoolVar(&listJSONFlag, "json", false, "Output raw JSON")
	providerListCmd.Flags().StringVar(&listProjectID, "project", "", "Filter to a specific project (name or ID)")

	// Wire sub-commands
	providerCmd.AddCommand(configureCmd)
	providerCmd.AddCommand(configureProjectCmd)
	providerCmd.AddCommand(providerListCmd)
	providerCmd.AddCommand(providerModelsCmd)
	providerCmd.AddCommand(providerUsageCmd)
	providerCmd.AddCommand(providerUsageTimeseriesCmd)
	providerCmd.AddCommand(providerTestCmd)

	rootCmd.AddCommand(providerCmd)
}

// sortModelsByType returns models sorted: generative first, then embedding,
// alphabetically within each group.
func sortModelsByType(models []provider.SupportedModel) []provider.SupportedModel {
	out := make([]provider.SupportedModel, len(models))
	copy(out, models)
	sort.SliceStable(out, func(i, j int) bool {
		ti, tj := out[i].ModelType, out[j].ModelType
		if ti != tj {
			// generative < embedding for ordering purposes
			return ti == "generative"
		}
		return out[i].ModelName < out[j].ModelName
	})
	return out
}
