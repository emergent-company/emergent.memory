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
	Name          string            `yaml:"name" json:"name"`
	Transport     Transport         `yaml:"transport" json:"transport"`
	Enabled       *bool             `yaml:"enabled,omitempty" json:"enabled,omitempty"`
	Command       string            `yaml:"command,omitempty" json:"command,omitempty"`
	Args          []string          `yaml:"args,omitempty" json:"args,omitempty"`
	Env           map[string]string `yaml:"env,omitempty" json:"env,omitempty"`
	URL           string            `yaml:"url,omitempty" json:"url,omitempty"`
	Headers       map[string]string `yaml:"headers,omitempty" json:"headers,omitempty"`
	DisabledTools []string          `yaml:"disabled_tools,omitempty" json:"disabled_tools,omitempty"`
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
	if err := validateHeaders(s.Headers); err != nil {
		return fmt.Errorf("mcp server %q: %w", s.Name, err)
	}
	if err := validateEnv(s.Env); err != nil {
		return fmt.Errorf("mcp server %q: %w", s.Name, err)
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

// validateHeaders rejects header names/values that could smuggle additional
// HTTP headers or corrupt a request: a header name must be a legal HTTP token
// and a header value must not contain CR, LF, or NUL.
func validateHeaders(headers map[string]string) error {
	for name, value := range headers {
		if !validHeaderName(name) {
			return fmt.Errorf("illegal header name %q", name)
		}
		if hasCRLFOrNUL(value) {
			return fmt.Errorf("illegal header value for %q (must not contain CR/LF/NUL)", name)
		}
	}
	return nil
}

// validateEnv rejects env keys/values that cannot be represented in a
// subprocess environment: a key must be a non-empty name without CR/LF/NUL,
// '=', or spaces, and a value must not contain CR, LF, or NUL.
func validateEnv(env map[string]string) error {
	for key, value := range env {
		if !validEnvKey(key) {
			return fmt.Errorf("illegal env key %q", key)
		}
		if hasCRLFOrNUL(value) {
			return fmt.Errorf("illegal env value for %q (must not contain CR/LF/NUL)", key)
		}
	}
	return nil
}

// validHeaderName reports whether name is a legal HTTP header field name (an
// RFC 7230 token: no control chars, no space, no colon, non-empty).
func validHeaderName(name string) bool {
	if name == "" {
		return false
	}
	for i := 0; i < len(name); i++ {
		c := name[i]
		if c <= ' ' || c >= 127 || c == ':' {
			return false
		}
	}
	return true
}

// validEnvKey reports whether key is a legal environment variable name: no
// control chars, no spaces, no '=', non-empty.
func validEnvKey(key string) bool {
	if key == "" {
		return false
	}
	for i := 0; i < len(key); i++ {
		c := key[i]
		if c <= ' ' || c == '=' || c >= 127 {
			return false
		}
	}
	return true
}

// hasCRLFOrNUL reports whether s contains a carriage return, line feed, or NUL
// byte.
func hasCRLFOrNUL(s string) bool {
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '\r', '\n', 0:
			return true
		}
	}
	return false
}
