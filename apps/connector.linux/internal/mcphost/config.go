// Package mcphost hosts local MCP servers inside the memory-connector and
// shares a user-selected subset of their tools through the same relay/tool
// mechanism used for the built-in Apple tools. Connection details (commands,
// URLs, headers, secrets) stay on the connector host; Memory only ever sees
// registered tools.
package mcphost

import (
	"fmt"
	"strings"
)

// Transport identifies how the connector reaches an MCP server.
type Transport string

const (
	// TransportStdio launches a local subprocess and speaks MCP over its
	// stdin/stdout.
	TransportStdio Transport = "stdio"
	// TransportHTTP connects to a streamable-HTTP MCP endpoint.
	TransportHTTP Transport = "http"
	// TransportSSE connects to a legacy SSE MCP endpoint.
	TransportSSE Transport = "sse"
)

// ServerConfig describes one local MCP server the connector hosts. Only the
// fields for the selected transport are used: stdio uses Command/Args/Env;
// http and sse use URL/Headers. Enabled defaults to true when omitted.
// DisabledTools is a per-server deny list of the server's own (un-namespaced)
// tool names.
type ServerConfig struct {
	Name          string            `yaml:"name"`
	Transport     Transport         `yaml:"transport"`
	Enabled       *bool             `yaml:"enabled,omitempty"`
	Command       string            `yaml:"command,omitempty"`
	Args          []string          `yaml:"args,omitempty"`
	Env           map[string]string `yaml:"env,omitempty"`
	URL           string            `yaml:"url,omitempty"`
	Headers       map[string]string `yaml:"headers,omitempty"`
	DisabledTools []string          `yaml:"disabled_tools,omitempty"`
}

// IsEnabled reports whether the server is enabled. The YAML field is a
// pointer so an omitted value defaults to enabled.
func (s ServerConfig) IsEnabled() bool {
	return s.Enabled == nil || *s.Enabled
}

// Validate reports configuration errors for one server: a non-blank name, a
// known transport, and the transport-specific connection fields.
func (s ServerConfig) Validate() error {
	if strings.TrimSpace(s.Name) == "" {
		return fmt.Errorf("mcp server: name is required")
	}
	switch s.Transport {
	case TransportStdio:
		if strings.TrimSpace(s.Command) == "" {
			return fmt.Errorf("mcp server %q: command is required for stdio transport", s.Name)
		}
	case TransportHTTP, TransportSSE:
		if strings.TrimSpace(s.URL) == "" {
			return fmt.Errorf("mcp server %q: url is required for %s transport", s.Name, s.Transport)
		}
	case "":
		return fmt.Errorf("mcp server %q: transport is required (stdio, http, or sse)", s.Name)
	default:
		return fmt.Errorf("mcp server %q: unknown transport %q (want stdio, http, or sse)", s.Name, s.Transport)
	}
	return nil
}

// ValidateServers validates every enabled server and rejects duplicate
// (non-blank) server names. Disabled servers keep their config but are not
// otherwise validated, so an incomplete disabled entry never blocks startup.
func ValidateServers(servers []ServerConfig) error {
	seen := make(map[string]struct{}, len(servers))
	for _, s := range servers {
		name := strings.TrimSpace(s.Name)
		if name == "" {
			return fmt.Errorf("mcp server: name is required")
		}
		if _, ok := seen[name]; ok {
			return fmt.Errorf("mcp server %q: duplicate server name", name)
		}
		seen[name] = struct{}{}
		if !s.IsEnabled() {
			continue
		}
		if err := s.Validate(); err != nil {
			return err
		}
	}
	return nil
}
