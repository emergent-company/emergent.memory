// Package completion provides shell completion functionality for the CLI.
package completion

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/emergent-company/emergent/tools/emergent-cli/internal/cache"
	"github.com/emergent-company/emergent/tools/emergent-cli/internal/client"
	"github.com/emergent-company/emergent/tools/emergent-cli/internal/config"
	"github.com/spf13/cobra"
)

// ValidOutputFormats returns valid values for --output flag completion.
func ValidOutputFormats() []string {
	return []string{"table", "json", "yaml", "csv"}
}

// OutputFormatCompletionFunc returns a ValidArgsFunction for output format completion.
func OutputFormatCompletionFunc() func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	return func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		return ValidOutputFormats(), cobra.ShellCompDirectiveDefault
	}
}

// NoCompletion returns an empty completion function.
func NoCompletion() func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	return func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
}

// Error returns a completion function that shows an error message.
func Error(err error) func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	return func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		return []string{fmt.Sprintf("Error: %v", err)}, cobra.ShellCompDirectiveError
	}
}

// ProjectNamesCompletionFunc returns a ValidArgsFunction that provides project name completion
// with caching support.
func ProjectNamesCompletionFunc() func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	return func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		// Try to get cache manager
		cacheManager, err := getCacheManager(cmd)
		if err == nil && cacheManager != nil {
			// Check cache first
			if cached, ok := cacheManager.Get("project-names"); ok {
				return filterCompletions(cached, toComplete), cobra.ShellCompDirectiveNoFileComp
			}
		}

		// Get client
		c, err := getClient(cmd)
		if err != nil {
			// Silently fail - completions shouldn't be intrusive
			return nil, cobra.ShellCompDirectiveNoFileComp
		}

		// Set timeout context
		ctx, cancel := getCompletionContext(cmd)
		defer cancel()

		// Fetch projects
		projects, err := c.SDK.Projects.List(ctx, nil)
		if err != nil {
			// Silently fail
			return nil, cobra.ShellCompDirectiveNoFileComp
		}

		// Extract names
		names := make([]string, 0, len(projects))
		for _, p := range projects {
			names = append(names, p.Name)
		}

		// Cache the result
		if cacheManager != nil {
			_ = cacheManager.Set("project-names", names)
		}

		return filterCompletions(names, toComplete), cobra.ShellCompDirectiveNoFileComp
	}
}

// DocumentIDsCompletionFunc returns a ValidArgsFunction that provides document ID completion
// with caching support. It requires a project ID to be specified.
func DocumentIDsCompletionFunc() func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	return func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		// Get project ID from flag
		projectID, err := cmd.Flags().GetString("project")
		if err != nil || projectID == "" {
			// No project specified, can't complete documents
			return nil, cobra.ShellCompDirectiveNoFileComp
		}

		// Try to get cache manager
		cacheKey := fmt.Sprintf("document-ids-%s", projectID)
		cacheManager, err := getCacheManager(cmd)
		if err == nil && cacheManager != nil {
			// Check cache first
			if cached, ok := cacheManager.Get(cacheKey); ok {
				return filterCompletions(cached, toComplete), cobra.ShellCompDirectiveNoFileComp
			}
		}

		// Get client
		c, err := getClient(cmd)
		if err != nil {
			// Silently fail
			return nil, cobra.ShellCompDirectiveNoFileComp
		}

		// Get org ID - might need it for SetContext
		// For now, just set the project (org is already set on client initialization)
		c.SDK.Documents.SetContext("", projectID)

		// Set timeout context
		ctx, cancel := getCompletionContext(cmd)
		defer cancel()

		// Fetch documents for project
		result, err := c.SDK.Documents.List(ctx, nil)
		if err != nil {
			// Silently fail
			return nil, cobra.ShellCompDirectiveNoFileComp
		}

		// Extract IDs with names as descriptions
		// Format: "id\tdescription" for shell completion with descriptions
		completions := make([]string, 0, len(result.Documents))
		for _, d := range result.Documents {
			completion := d.ID
			if d.Filename != nil && *d.Filename != "" {
				completion += "\t" + *d.Filename
			}
			completions = append(completions, completion)
		}

		// Cache just the IDs
		ids := make([]string, 0, len(result.Documents))
		for _, d := range result.Documents {
			ids = append(ids, d.ID)
		}
		if cacheManager != nil {
			_ = cacheManager.Set(cacheKey, ids)
		}

		return filterCompletions(completions, toComplete), cobra.ShellCompDirectiveNoFileComp
	}
}

// Helper functions

// getClient creates a client from command flags and config.
func getClient(cmd *cobra.Command) (*client.Client, error) {
	configPath, _ := cmd.Flags().GetString("config")
	if configPath == "" {
		configPath = config.DiscoverPath("")
	}

	cfg, err := config.LoadWithEnv(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load config: %w", err)
	}

	if cfg.ServerURL == "" {
		return nil, fmt.Errorf("no server URL configured")
	}

	return client.New(cfg)
}

// getCacheManager creates a cache manager from config.
func getCacheManager(cmd *cobra.Command) (*cache.Manager, error) {
	configPath, _ := cmd.Flags().GetString("config")
	if configPath == "" {
		configPath = config.DiscoverPath("")
	}

	cfg, err := config.LoadWithEnv(configPath)
	if err != nil {
		return nil, err
	}

	if !cfg.Cache.Enabled {
		return nil, nil
	}

	ttl, err := time.ParseDuration(cfg.Cache.TTL)
	if err != nil {
		ttl = 5 * time.Minute // Default
	}

	return cache.NewManager("", ttl)
}

// getCompletionContext creates a context with timeout for completion operations.
func getCompletionContext(cmd *cobra.Command) (context.Context, context.CancelFunc) {
	configPath, _ := cmd.Flags().GetString("config")
	if configPath == "" {
		configPath = config.DiscoverPath("")
	}

	cfg, _ := config.LoadWithEnv(configPath)
	timeout := 2 * time.Second // Default

	if cfg != nil {
		if t, err := time.ParseDuration(cfg.Completion.Timeout); err == nil {
			timeout = t
		}
	}

	return context.WithTimeout(context.Background(), timeout)
}

// filterCompletions filters completions based on the toComplete prefix.
func filterCompletions(completions []string, toComplete string) []string {
	if toComplete == "" {
		return completions
	}

	filtered := make([]string, 0)
	for _, c := range completions {
		// Handle tab-separated descriptions (id\tdescription)
		parts := strings.Split(c, "\t")
		value := parts[0]

		if strings.HasPrefix(value, toComplete) {
			filtered = append(filtered, c)
		}
	}

	return filtered
}
