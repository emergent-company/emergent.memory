package cmd

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/emergent-company/emergent.memory/apps/cli/internal/blueprints"
	"github.com/emergent-company/emergent.memory/apps/cli/internal/client"
	"github.com/spf13/cobra"
)

// installModelMapping captures the blueprint identity and the resolved
// agent→model choices for persistence after a successful install.
type installModelMapping struct {
	name      string
	overrides map[string]string
}

// runModelMapping resolves install-time model choices for a blueprint's agents
// and rewrites their model names in place (agents is the slice already
// normalized by Preflight). It returns the mapping to persist (nil when mapping
// was skipped via --no-map or because nothing needed mapping).
//
// The mapping step is advisory: a failure to list providers or fetch stored
// overrides is a warning, never an abort. The Preflight gate remains the only
// hard failure.
func runModelMapping(
	cmd *cobra.Command,
	c *client.Client,
	projectID string,
	projectFile *blueprints.ProjectFile,
	agents []blueprints.AgentFile,
	dir, source string,
	out io.Writer,
) (*installModelMapping, error) {
	if blueprintsNoMapFlag {
		return nil, nil
	}

	// Collect agents with a pinned model (normalized by Preflight to
	// "provider/model"). Agents without a model block have nothing to map.
	var pinned []*blueprints.AgentFile
	for i := range agents {
		if agents[i].Model != nil && agents[i].Model.Name != "" {
			pinned = append(pinned, &agents[i])
		}
	}
	if len(pinned) == 0 {
		return nil, nil
	}

	// Resolve the blueprint name that keys the per-project override store:
	// ProjectFile.Name first, else the source dir/repo base name.
	name := ""
	if projectFile != nil {
		name = strings.TrimSpace(projectFile.Name)
	}
	if name == "" {
		name = filepath.Base(strings.TrimSuffix(dir, "/"))
		_, _ = fmt.Fprintf(out, "  note     blueprint has no project name; using %q for model mapping\n", name)
	}
	if name == "" || name == "." || name == string(filepath.Separator) {
		// No stable identity to key on — skip mapping rather than persist under
		// a bogus name.
		_, _ = fmt.Fprintf(out, "  warning  could not determine a blueprint name for model mapping; skipping\n")
		return nil, nil
	}

	// Fetch the project's configured providers (advisory — a failure is a
	// warning, not an abort).
	ctx := context.Background()
	var providers []blueprints.ProjectProvider
	configs, providersErr := c.SDK.Provider.ListProjectConfigs(ctx, projectID)
	if providersErr != nil {
		_, _ = fmt.Fprintf(out, "  warning  could not list project providers: %v\n", providersErr)
	} else {
		for _, cfg := range configs {
			providers = append(providers, blueprints.ProjectProvider{
				Provider:        cfg.Provider,
				GenerativeModel: cfg.GenerativeModel,
				EmbeddingModel:  cfg.EmbeddingModel,
			})
		}
	}

	// Fetch stored overrides for this blueprint (advisory).
	stored := map[string]string{}
	data, err := blueprintRequest(c, http.MethodGet,
		c.BaseURL()+"/api/blueprints/model-overrides?name="+url.QueryEscape(name), nil)
	if err != nil {
		_, _ = fmt.Fprintf(out, "  warning  could not fetch stored model overrides: %v\n", err)
	} else {
		var resp struct {
			ModelOverrides map[string]string `json:"modelOverrides"`
		}
		if jsonErr := json.Unmarshal(data, &resp); jsonErr == nil && resp.ModelOverrides != nil {
			stored = resp.ModelOverrides
		}
	}

	mm := blueprints.NewModelMap(providers, stored)
	if providersErr != nil {
		// Provider list unavailable: availability is unknown, so Resolve keeps
		// pinned models instead of downgrading them.
		mm.MarkProvidersUnknown()
	}

	// Warn about pinned models the project cannot serve (advisory, only when we
	// actually know the project's providers).
	if mm.ProvidersKnown() {
		for _, a := range pinned {
			ref := blueprints.PinnedModel(*a)
			if ref == "" || mm.IsServable(ref) {
				continue
			}
			_, _ = fmt.Fprintf(out, "  warning  agent %q pins %s, which this project cannot serve (%s)\n",
				a.Name, ref, mm.ConfiguredModelsSummary())
			_, _ = fmt.Fprintf(out, "           map it with --map-model %s=<provider/model> or run 'memory provider configure-project <provider>'\n",
				a.Name)
		}
	}

	// Parse explicit --map-model flags. A malformed flag is a hard error.
	explicit := make(map[string]string, len(blueprintsMapModelFlag))
	for _, raw := range blueprintsMapModelFlag {
		agent, model, perr := blueprints.ParseMapModel(raw)
		if perr != nil {
			return nil, perr
		}
		explicit[agent] = model
	}

	// Resolve and apply. Any --map-model flag switches to non-interactive (the
	// remaining agents use stored override > pinned > project default).
	interactive := len(explicit) == 0 && !isNonInteractive()
	final := make(map[string]string, len(pinned))
	for _, a := range pinned {
		choice := mm.Resolve(*a)
		if v, ok := explicit[a.Name]; ok {
			choice = v
		} else if interactive {
			choice = promptModelChoice(cmd, a.Name, blueprints.PinnedModel(*a), mm, choice)
		}
		blueprints.ApplyModelChoice(a, choice)
		final[a.Name] = choice
	}

	printModelMappingSummary(out, final)

	// Persist only when the user made a decision (explicit --map-model flags or
	// an interactive prompt). A purely auto-resolved mapping (non-interactive,
	// no flags) reflects the blueprint's own pins — persisting it would freeze
	// them against future blueprint changes.
	if len(explicit) == 0 && !interactive {
		return nil, nil
	}
	return &installModelMapping{name: name, overrides: final}, nil
}

// promptModelChoice presents a numbered stdin selection for one agent's model,
// defaulting to the already-resolved choice. The first option is always
// "Project default" (empty model); the rest are the project's configured
// generative models.
func promptModelChoice(cmd *cobra.Command, agentName, pinned string, mm *blueprints.ModelMap, defaultChoice string) string {
	options := mm.ServableModels()
	out := cmd.OutOrStdout()

	avail := "servable"
	if !mm.IsServable(pinned) {
		avail = "not servable"
	}

	_, _ = fmt.Fprintf(out, "\nModel mapping for agent %q:\n", agentName)
	_, _ = fmt.Fprintf(out, "  blueprint pins: %s  [%s]\n", pinned, avail)
	_, _ = fmt.Fprintf(out, "  configured models:\n")
	_, _ = fmt.Fprintf(out, "    1. Project default\n")
	for i, m := range options {
		_, _ = fmt.Fprintf(out, "    %d. %s\n", i+2, m)
	}

	// Map the default choice to a selection index (defaults to "Project default").
	defaultIdx := 1
	if defaultChoice != "" {
		for i, m := range options {
			if m == defaultChoice {
				defaultIdx = i + 2
				break
			}
		}
	}
	defaultLabel := "Project default"
	if defaultIdx > 1 {
		defaultLabel = options[defaultIdx-2]
	}

	reader := bufio.NewReader(cmd.InOrStdin())
	_, _ = fmt.Fprintf(out, "  choose (enter = %s): ", defaultLabel)
	line, err := reader.ReadString('\n')
	if err != nil {
		return defaultChoice
	}
	line = strings.TrimSpace(line)
	if line == "" {
		return defaultChoice
	}
	n, err := strconv.Atoi(line)
	if err != nil || n < 1 || n > len(options)+1 {
		_, _ = fmt.Fprintf(out, "  invalid choice; keeping %s\n", defaultLabel)
		return defaultChoice
	}
	if n == 1 {
		return ""
	}
	return options[n-2]
}

// printModelMappingSummary prints a single-line summary of the mapping applied.
func printModelMappingSummary(out io.Writer, final map[string]string) {
	names := make([]string, 0, len(final))
	for n := range final {
		names = append(names, n)
	}
	sort.Strings(names)

	parts := make([]string, 0, len(names))
	for _, n := range names {
		v := final[n]
		if v == "" {
			parts = append(parts, fmt.Sprintf("%s=%s", n, "project default"))
		} else {
			parts = append(parts, fmt.Sprintf("%s=%s", n, v))
		}
	}
	_, _ = fmt.Fprintf(out, "Model mapping (%d agents): %s\n", len(names), strings.Join(parts, ", "))
}

// persistModelOverrides stores the resolved agent→model choices for this
// blueprint via PUT /api/blueprints/model-overrides so a re-install/upgrade
// pre-fills them.
func persistModelOverrides(c *client.Client, m *installModelMapping) error {
	body, err := json.Marshal(map[string]any{
		"name":           m.name,
		"modelOverrides": m.overrides,
	})
	if err != nil {
		return fmt.Errorf("marshal overrides: %w", err)
	}
	_, err = blueprintRequest(c, http.MethodPut, c.BaseURL()+"/api/blueprints/model-overrides", body)
	return err
}
