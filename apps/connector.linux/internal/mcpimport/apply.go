// Package mcpimport merges parsed Claude MCP config into a connector's local
// hosted-server set. It is shared by the loopback management API and the
// connector CLI so both apply the same skip/overwrite/validation rules.
package mcpimport

import (
	"github.com/emergent-company/emergent.memory/apps/connector.linux/internal/mcphost"
	"github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/mcpconfig"
)

// Skip names an entry left untouched and why.
type Skip struct {
	Name   string `json:"name"`
	Reason string `json:"reason"`
}

// Failure names an entry that could not be imported.
type Failure struct {
	Name  string `json:"name"`
	Error string `json:"error"`
}

// Warning is a non-fatal parser observation.
type Warning struct {
	Name    string `json:"name"`
	Message string `json:"message"`
}

// Result summarises an import. Valid entries are applied even when others fail.
type Result struct {
	Created  []string  `json:"created"`
	Updated  []string  `json:"updated"`
	Skipped  []Skip    `json:"skipped"`
	Failed   []Failure `json:"failed"`
	Warnings []Warning `json:"warnings,omitempty"`
	DryRun   bool      `json:"dryRun"`
}

// Changed reports whether any server was created or updated.
func (r Result) Changed() bool { return len(r.Created) > 0 || len(r.Updated) > 0 }

// Apply merges parsed entries into existing, returning the resulting server
// set and a per-entry report. isManaged may be nil; a managed name is skipped
// because Memory owns that server's config. An overwrite preserves the
// existing entry's enabled state and disabled_tools, which a Claude document
// does not carry.
func Apply(existing []mcphost.ServerConfig, parsed mcpconfig.Result, overwrite bool, isManaged func(string) bool) ([]mcphost.ServerConfig, Result) {
	next := make([]mcphost.ServerConfig, len(existing))
	copy(next, existing)

	result := Result{
		Created:  []string{},
		Updated:  []string{},
		Skipped:  []Skip{},
		Failed:   []Failure{},
		Warnings: []Warning{},
	}
	for _, entryErr := range parsed.Errors {
		result.Failed = append(result.Failed, Failure{Name: entryErr.Name, Error: entryErr.Err.Error()})
	}
	for _, warning := range parsed.Warnings {
		result.Warnings = append(result.Warnings, Warning{Name: warning.Name, Message: warning.Message})
	}

	for _, entry := range parsed.Servers {
		cfg := toConfig(entry)

		if isManaged != nil && isManaged(cfg.Name) {
			result.Skipped = append(result.Skipped, Skip{Name: cfg.Name, Reason: "managed by Memory"})
			continue
		}
		if err := cfg.Validate(); err != nil {
			result.Failed = append(result.Failed, Failure{Name: cfg.Name, Error: err.Error()})
			continue
		}

		idx := -1
		for i := range next {
			if next[i].Name == cfg.Name {
				idx = i
				break
			}
		}
		if idx >= 0 {
			if !overwrite {
				result.Skipped = append(result.Skipped, Skip{Name: cfg.Name, Reason: "already exists"})
				continue
			}
			cfg.Enabled = next[idx].Enabled
			cfg.DisabledTools = next[idx].DisabledTools
			next[idx] = cfg
			result.Updated = append(result.Updated, cfg.Name)
			continue
		}
		next = append(next, cfg)
		result.Created = append(result.Created, cfg.Name)
	}
	if len(result.Warnings) == 0 {
		result.Warnings = nil
	}
	return next, result
}

// toConfig maps a parsed Claude entry onto the connector's hosted-server config.
func toConfig(entry mcpconfig.Server) mcphost.ServerConfig {
	return mcphost.ServerConfig{
		Name:      entry.Name,
		Transport: mcphost.Transport(entry.Transport),
		Command:   entry.Command,
		Args:      entry.Args,
		Env:       entry.Env,
		URL:       entry.URL,
		Headers:   entry.Headers,
	}
}
