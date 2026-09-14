// Package config loads and stores the memory-connector's single-profile
// configuration.
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/emergent-company/memory.web-ui/connector/internal/mcphost"
)

// Config holds the per-machine connector profile. project_id is optional when
// the token is project-scoped; instance_id defaults to <hostname>-connector;
// disabled_tools is optional — names here are filtered out of registration,
// status, and dispatch. tools configures the platform tool providers.
// mcp_servers is optional — each entry hosts a local MCP server and shares its
// tools through the relay.
//
// Global disabled_tools entries are matched against the connector registry key.
// Hosted tools are namespaced <serverName>_<toolName>, so a global entry that
// disables one must use that full namespaced name; per-server exclusions belong
// in the server's own disabled_tools list.
type Config struct {
	ServerURL     string                 `yaml:"server_url"`
	Token         string                 `yaml:"token"`
	ProjectID     string                 `yaml:"project_id,omitempty"`
	InstanceID    string                 `yaml:"instance_id,omitempty"`
	DisabledTools []string               `yaml:"disabled_tools,omitempty"`
	Tools         ToolsConfig            `yaml:"tools,omitempty"`
	MCPServers    []mcphost.ServerConfig `yaml:"mcp_servers,omitempty"`
}

// ToolsConfig is the platform tool configuration. The section is optional:
// existing configs without it load unchanged (host info defaults on, the
// filesystem is absent).
type ToolsConfig struct {
	Linux LinuxToolsConfig `yaml:"linux,omitempty"`
}

// LinuxToolsConfig configures the Linux tool providers. A nil HostInfo means
// host info is enabled; a nil Filesystem means there are no filesystem roots
// (and therefore no filesystem tools).
type LinuxToolsConfig struct {
	HostInfo   *HostInfoConfig   `yaml:"host_info,omitempty"`
	Filesystem *FilesystemConfig `yaml:"filesystem,omitempty"`
}

// HostInfoConfig toggles the host-info tool. A nil Enabled defaults to true.
type HostInfoConfig struct {
	Enabled *bool `yaml:"enabled,omitempty"`
}

// FilesystemConfig configures the root-scoped filesystem tools.
type FilesystemConfig struct {
	// Enabled is tri-state: nil (or absent) enables the filesystem when roots
	// exist; false disables the filesystem tools regardless of roots.
	Enabled              *bool        `yaml:"enabled,omitempty"`
	Roots                []RootConfig `yaml:"roots,omitempty"`
	MaxReadBytes         int64        `yaml:"max_read_bytes,omitempty"`
	MaxWriteBytes        int64        `yaml:"max_write_bytes,omitempty"`
	AllowPermanentDelete *bool        `yaml:"allow_permanent_delete,omitempty"`
}

// RootConfig is one allowed filesystem subtree and its capabilities.
type RootConfig struct {
	Path   string `yaml:"path"`
	Read   bool   `yaml:"read,omitempty"`
	Write  bool   `yaml:"write,omitempty"`
	Delete bool   `yaml:"delete,omitempty"`
}

// DefaultConfigPath returns the default config location. When XDG_CONFIG_HOME
// is set (the norm on Linux) it is $XDG_CONFIG_HOME/memory-connector.yml;
// otherwise it falls back to ~/.config/memory-connector.yml.
func DefaultConfigPath() string {
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "memory-connector.yml")
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return "memory-connector.yml"
	}
	return filepath.Join(home, ".config", "memory-connector.yml")
}

// DefaultInstanceID returns the default instance id, <hostname>-connector.
func DefaultInstanceID() string {
	host, err := os.Hostname()
	if err != nil || host == "" {
		host = "unknown"
	}
	return host + "-connector"
}

// Validate reports configuration errors. server_url and token are required.
func (c *Config) Validate() error {
	if strings.TrimSpace(c.ServerURL) == "" {
		return errors.New("config: server_url is required")
	}
	if strings.TrimSpace(c.Token) == "" {
		return errors.New("config: token is required")
	}
	if err := mcphost.ValidateServers(c.MCPServers); err != nil {
		return fmt.Errorf("config: %w", err)
	}
	return nil
}

// Load reads the YAML config at path, applies defaults (instance_id), and
// validates it. Missing files and validation failures return wrapped errors.
func Load(path string) (*Config, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config %s: %w", path, err)
	}
	cfg := &Config{}
	if err := yaml.Unmarshal(b, cfg); err != nil {
		return nil, fmt.Errorf("parse config %s: %w", path, err)
	}
	if cfg.InstanceID == "" {
		cfg.InstanceID = DefaultInstanceID()
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

// Save writes c to path as YAML with mode 0600, creating the parent directory
// if missing.
func (c *Config) Save(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create config dir for %s: %w", path, err)
	}
	b, err := yaml.Marshal(c)
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}
	if err := os.WriteFile(path, b, 0o600); err != nil {
		return fmt.Errorf("write config %s: %w", path, err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		return fmt.Errorf("chmod config %s: %w", path, err)
	}
	return nil
}

// MaterializeOptions optionally overrides values when materializing an engine
// config. Nil fields preserve the existing config's values (or defaults).
type MaterializeOptions struct {
	// InstanceID, when non-nil, overrides the existing/default instance id.
	InstanceID *string
	// DisabledTools, when non-nil, replaces the existing disabled-tools list
	// (a non-nil empty slice clears it).
	DisabledTools *[]string
}

// Materialize writes an engine config for serverURL/token/projectID as YAML
// with mode 0600, exactly as Config.Save does. When a config already exists at
// path, its instance_id and disabled_tools are preserved; a missing
// instance_id defaults to DefaultInstanceID(). An absent file is not an error:
// the config is created with the default instance id.
func Materialize(path, serverURL, token, projectID string) error {
	return MaterializeWithOptions(path, serverURL, token, projectID, MaterializeOptions{})
}

// MaterializeWithOptions is Materialize with explicit overrides for
// instance_id and disabled_tools. Nil option fields preserve existing values.
func MaterializeWithOptions(path, serverURL, token, projectID string, opts MaterializeOptions) error {
	cfg := &Config{}
	b, err := os.ReadFile(path)
	switch {
	case err == nil:
		if err := yaml.Unmarshal(b, cfg); err != nil {
			return fmt.Errorf("parse existing config %s: %w", path, err)
		}
	case errors.Is(err, os.ErrNotExist):
		// no existing config: start fresh
	default:
		return fmt.Errorf("read config %s: %w", path, err)
	}

	cfg.ServerURL = serverURL
	cfg.Token = token
	cfg.ProjectID = projectID
	if opts.InstanceID != nil {
		cfg.InstanceID = *opts.InstanceID
	}
	if cfg.InstanceID == "" {
		cfg.InstanceID = DefaultInstanceID()
	}
	if opts.DisabledTools != nil {
		cfg.DisabledTools = append([]string(nil), (*opts.DisabledTools)...)
	}
	return cfg.Save(path)
}
