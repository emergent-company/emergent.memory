// Package config handles .codebase.yml parsing and SDK client construction.
package config

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	sdk "github.com/emergent-company/emergent.memory/apps/server/pkg/sdk"
	sdkgraph "github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/graph"
	"gopkg.in/yaml.v3"
)

// SyncRoutesConfig configures the route extractor for `codebase sync routes`.
type SyncRoutesConfig struct {
	// Framework selects a bundled extractor (e.g. "nestjs", "express", "fastapi").
	// Mutually exclusive with Command.
	Framework string `yaml:"framework"`
	// Command is a custom extractor script path (relative to repo root).
	// The CLI runs it and reads newline-delimited JSON from stdout.
	Command string `yaml:"command"`
	// Runtime is the interpreter for Command (e.g. "npx ts-node", "python3").
	// Defaults to direct execution if empty.
	Runtime string `yaml:"runtime"`
	// Glob is passed to the extractor as --glob (overrides extractor default).
	Glob string `yaml:"glob"`
	// DomainSegment is the 0-based path segment index used to derive domain name.
	// e.g. "apps/api/src/<domain>/foo.controller.ts" → segment 3
	DomainSegment int `yaml:"domain_segment"`
}

// SyncViewsConfig configures `codebase sync views` — React/frontend view extraction.
type SyncViewsConfig struct {
	// Glob pattern for view files relative to repo root. E.g. "apps/web/src/views/**/*.tsx"
	Glob string `yaml:"glob"`
	// RoutesFile is the path to the routes definition file (TypeScript/JS).
	// The CLI parses string literals from it to map view names to routes.
	RoutesFile string `yaml:"routes_file"`
	// Platform overrides the default platform tag. Defaults to ["web"].
	Platform []string `yaml:"platform"`
}

// SyncComponentsConfig configures `codebase sync components` — UI component extraction.
type SyncComponentsConfig struct {
	// Glob pattern for component files relative to repo root. E.g. "libs/shared-web/src/components/**/*.tsx"
	Glob string `yaml:"glob"`
}

// SyncActionsConfig configures `codebase sync actions` — store/action extraction.
type SyncActionsConfig struct {
	// Glob pattern for store files relative to repo root. E.g. "apps/web/src/store/**/*.ts"
	Glob string `yaml:"glob"`
	// Pattern selects the store framework. E.g. "mobx", "redux", "zustand". Defaults to "mobx".
	Pattern string `yaml:"pattern"`
}

// SyncScenariosConfig configures `codebase sync scenarios`.
type SyncScenariosConfig struct {
	// File is the path to the scenarios definition YAML (relative to repo root).
	// E.g. ".codebase/scenarios.yml"
	File string `yaml:"file"`
	// RouterFile is the path to the main React Router file for --discover mode.
	// E.g. "apps/web/src/App.tsx"
	RouterFile string `yaml:"router_file"`
	// StoreGlob is the glob for store files used in --discover mode.
	// E.g. "apps/web/src/store/**/*.ts"
	StoreGlob string `yaml:"store_glob"`
}

// SyncConfig groups all sync sub-command configuration.
type SyncConfig struct {
	Routes     SyncRoutesConfig     `yaml:"routes"`
	Views      SyncViewsConfig      `yaml:"views"`
	Components SyncComponentsConfig `yaml:"components"`
	Actions    SyncActionsConfig    `yaml:"actions"`
	Scenarios  SyncScenariosConfig  `yaml:"scenarios"`
}

// CodebaseYML is the structure of .codebase.yml.
type CodebaseYML struct {
	Project   string     `yaml:"project"`    // project name (resolved to ID via API)
	ProjectID string     `yaml:"project_id"` // explicit project ID (skips name lookup)
	Server    string     `yaml:"server"`     // optional server URL override
	Sync      SyncConfig `yaml:"sync"`       // sync sub-command configuration
}

// Client wraps the SDK client with resolved project context.
type Client struct {
	SDK       *sdk.Client
	Graph     *sdkgraph.Client
	ProjectID string
	Branch    string
}

// New creates a configured Client. flagProjectID overrides all other sources.
func New(flagProjectID, flagBranch string) (*Client, error) {
	sdkClient, err := sdk.NewFromEnv()
	if err != nil {
		yml, _ := findAndParseYML()
		if yml != nil && yml.Server != "" {
			os.Setenv("MEMORY_SERVER_URL", yml.Server)
			sdkClient, err = sdk.NewFromEnv()
		}
		if err != nil {
			return nil, fmt.Errorf("memory auth: %w", err)
		}
	}

	projectID := flagProjectID
	if projectID == "" {
		projectID = os.Getenv("MEMORY_PROJECT_ID")
	}

	if projectID == "" {
		yml, _ := findAndParseYML()
		if yml != nil {
			if yml.ProjectID != "" {
				projectID = yml.ProjectID
			} else if yml.Project != "" {
				id, err := resolveProjectName(sdkClient, yml.Project)
				if err != nil {
					return nil, err
				}
				projectID = id
			}
		}
	}

	if projectID == "" {
		return nil, fmt.Errorf("project not found")
	}

	sdkClient.SetContext("", projectID)

	return &Client{
		SDK:       sdkClient,
		Graph:     sdkClient.Graph,
		ProjectID: projectID,
		Branch:    flagBranch,
	}, nil
}

// LoadYML returns the parsed .codebase.yml walked up from cwd, or nil if not found.
func LoadYML() *CodebaseYML {
	yml, _ := findAndParseYML()
	return yml
}

func findAndParseYML() (*CodebaseYML, error) {
	dir, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	for {
		candidate := filepath.Join(dir, ".codebase.yml")
		if data, err := os.ReadFile(candidate); err == nil {
			var yml CodebaseYML
			if err := yaml.Unmarshal(data, &yml); err != nil {
				return nil, err
			}
			return &yml, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return nil, os.ErrNotExist
}

func resolveProjectName(client *sdk.Client, name string) (string, error) {
	ctx := context.Background()
	projects, err := client.Projects.List(ctx, nil)
	if err != nil {
		return "", err
	}
	nameLower := strings.ToLower(name)
	for _, p := range projects {
		if strings.ToLower(p.Name) == nameLower {
			return p.ID, nil
		}
	}
	return "", fmt.Errorf("no project named %q found", name)
}
