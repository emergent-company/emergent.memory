// Package mcpconfig parses MCP client configuration files. It currently
// supports the Claude MCP format shared by Claude Desktop
// (claude_desktop_config.json), Claude Code (.mcp.json / ~/.claude.json), and
// the object accepted by `claude mcp add-json`.
//
// The package is deliberately dependency-free and client-agnostic: it turns a
// config document into neutral Server values, and reports per-entry problems
// without aborting the whole document. Callers (the Memory server, the CLI, and
// the connector) map the neutral values onto their own storage model.
package mcpconfig

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
)

// Transport is the connection transport of one MCP server.
type Transport string

const (
	TransportStdio Transport = "stdio"
	TransportHTTP  Transport = "http"
	TransportSSE   Transport = "sse"
)

// Server is one parsed MCP server. Only the fields for Transport are set:
// stdio uses Command/Args/Env; http and sse use URL/Headers.
type Server struct {
	Name      string
	Transport Transport
	Command   string
	Args      []string
	Env       map[string]string
	URL       string
	Headers   map[string]string
}

// Warning is a non-fatal observation about one entry (ignored field,
// preserved placeholder, ...).
type Warning struct {
	Name    string
	Message string
}

// EntryError is a per-entry parse/validation failure. The rest of the document
// is still parsed and returned.
type EntryError struct {
	Name string
	Err  error
}

func (e EntryError) Error() string {
	if e.Name == "" {
		return e.Err.Error()
	}
	return e.Name + ": " + e.Err.Error()
}

// Unwrap returns the underlying parse error.
func (e EntryError) Unwrap() error { return e.Err }

// Result is a parsed config document. Servers contains only valid entries;
// Errors names the entries that were rejected. Servers are sorted by name.
type Result struct {
	Servers  []Server
	Warnings []Warning
	Errors   []EntryError
}

// ParseOptions controls single-server documents (a bare server object with no
// mcpServers wrapper, as accepted by `claude mcp add-json`). Name is required
// for a single-server document unless the object itself carries a "name".
type ParseOptions struct {
	Name string
}

// ParseClaudeConfig parses a Claude MCP config document. It accepts either a
// document with an "mcpServers" object or a bare server object. A malformed
// JSON document returns an error; per-entry problems are reported in
// Result.Errors and do not fail the parse.
func ParseClaudeConfig(data []byte) (Result, error) {
	return ParseClaudeConfigWithOptions(data, ParseOptions{})
}

// ParseClaudeConfigWithOptions is ParseClaudeConfig with single-server options.
func ParseClaudeConfigWithOptions(data []byte, opts ParseOptions) (Result, error) {
	data = bytes.TrimSpace(data)
	if len(data) == 0 {
		return Result{}, fmt.Errorf("mcp config: empty document")
	}

	var root map[string]json.RawMessage
	if err := json.Unmarshal(data, &root); err != nil {
		return Result{}, fmt.Errorf("mcp config: parse JSON: %w", err)
	}
	if root == nil {
		return Result{}, fmt.Errorf("mcp config: top-level value must be an object")
	}

	raw, hasServers := root["mcpServers"]
	if hasServers {
		var servers map[string]json.RawMessage
		if err := json.Unmarshal(raw, &servers); err != nil {
			return Result{}, fmt.Errorf("mcp config: parse mcpServers: %w", err)
		}
		if len(servers) == 0 {
			return Result{}, fmt.Errorf("mcp config: mcpServers is empty")
		}
		names := make([]string, 0, len(servers))
		for name := range servers {
			names = append(names, name)
		}
		sort.Strings(names)

		result := Result{Servers: make([]Server, 0, len(servers))}
		for _, name := range names {
			srv, warnings, err := parseEntry(name, servers[name])
			if err != nil {
				result.Errors = append(result.Errors, EntryError{Name: name, Err: err})
				continue
			}
			result.Servers = append(result.Servers, srv)
			result.Warnings = append(result.Warnings, warnings...)
		}
		return result, nil
	}

	// No mcpServers wrapper: accept a bare single-server object.
	if !looksLikeServer(root) {
		return Result{}, fmt.Errorf(`mcp config: no "mcpServers" object found`)
	}
	name := strings.TrimSpace(opts.Name)
	if name == "" {
		var named struct {
			Name string `json:"name"`
		}
		_ = json.Unmarshal(data, &named)
		name = strings.TrimSpace(named.Name)
	}
	if name == "" {
		return Result{}, fmt.Errorf("mcp config: single-server document needs a name")
	}
	srv, warnings, err := parseEntry(name, data)
	if err != nil {
		return Result{Errors: []EntryError{{Name: name, Err: err}}}, nil
	}
	return Result{Servers: []Server{srv}, Warnings: warnings}, nil
}

// ParseClaudeConfigFile reads and parses a Claude MCP config file.
func ParseClaudeConfigFile(path string) (Result, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Result{}, fmt.Errorf("mcp config: read %s: %w", path, err)
	}
	return ParseClaudeConfig(data)
}

// looksLikeServer reports whether a bare object carries any server field.
func looksLikeServer(root map[string]json.RawMessage) bool {
	for _, key := range []string{"command", "args", "env", "url", "headers", "type"} {
		if _, ok := root[key]; ok {
			return true
		}
	}
	return false
}

// rawEntry is the subset of a Claude server entry this package understands.
// Env and Headers are raw so scalar coercion and non-scalar rejection are
// explicit rather than silently dropped by encoding/json.
type rawEntry struct {
	Type    string                     `json:"type"`
	Command string                     `json:"command"`
	Args    []string                   `json:"args"`
	Env     map[string]json.RawMessage `json:"env"`
	URL     string                     `json:"url"`
	Headers map[string]json.RawMessage `json:"headers"`
}

func parseEntry(name string, data json.RawMessage) (Server, []Warning, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return Server{}, nil, fmt.Errorf("name is required")
	}

	var entry rawEntry
	if err := json.Unmarshal(data, &entry); err != nil {
		return Server{}, nil, fmt.Errorf("parse entry: %w", err)
	}

	env, err := stringMap(entry.Env)
	if err != nil {
		return Server{}, nil, fmt.Errorf("env: %w", err)
	}
	headers, err := stringMap(entry.Headers)
	if err != nil {
		return Server{}, nil, fmt.Errorf("headers: %w", err)
	}

	command := strings.TrimSpace(entry.Command)
	rawURL := strings.TrimSpace(entry.URL)

	transport, err := resolveTransport(entry.Type, command, rawURL)
	if err != nil {
		return Server{}, nil, err
	}

	srv := Server{Name: name, Transport: transport}
	switch transport {
	case TransportStdio:
		srv.Command = command
		srv.Args = entry.Args
		srv.Env = env
	case TransportHTTP, TransportSSE:
		srv.URL = rawURL
		srv.Headers = headers
	}

	var warnings []Warning
	if transport == TransportStdio && len(headers) > 0 {
		warnings = append(warnings, Warning{Name: name, Message: "headers are ignored for a stdio server"})
	}
	if transport != TransportStdio && len(env) > 0 {
		warnings = append(warnings, Warning{Name: name, Message: "env is ignored for a remote server"})
	}
	if hasPlaceholder(env) || hasPlaceholder(headers) {
		warnings = append(warnings, Warning{Name: name, Message: "${VAR} placeholders are stored literally, not expanded"})
	}
	return srv, warnings, nil
}

// resolveTransport normalises the declared type and infers one when omitted.
// A server with both command and url is ambiguous and rejected.
func resolveTransport(declared, command, rawURL string) (Transport, error) {
	t := strings.ToLower(strings.TrimSpace(declared))
	switch t {
	case "":
		switch {
		case command != "" && rawURL != "":
			return "", fmt.Errorf("both command and url are set; specify a transport type")
		case command != "":
			return TransportStdio, nil
		case rawURL != "":
			return TransportHTTP, nil
		default:
			return "", fmt.Errorf("entry has neither command nor url")
		}
	case "stdio":
		if command == "" {
			return "", fmt.Errorf("stdio server requires command")
		}
		return TransportStdio, nil
	case "http", "streamable-http", "streamablehttp", "streamable_http":
		if rawURL == "" {
			return "", fmt.Errorf("%s server requires url", t)
		}
		return TransportHTTP, nil
	case "sse":
		if rawURL == "" {
			return "", fmt.Errorf("sse server requires url")
		}
		return TransportSSE, nil
	default:
		return "", fmt.Errorf("unknown transport type %q (want stdio, http, or sse)", declared)
	}
}

// stringMap converts a raw JSON object of scalars into a string map. Null
// values are dropped; numbers and booleans are stringified; objects and arrays
// are rejected.
func stringMap(raw map[string]json.RawMessage) (map[string]string, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	out := make(map[string]string, len(raw))
	for key, value := range raw {
		s, ok, err := scalarString(value)
		if err != nil {
			return nil, fmt.Errorf("key %q: %w", key, err)
		}
		if !ok {
			continue
		}
		out[key] = s
	}
	if len(out) == 0 {
		return nil, nil
	}
	return out, nil
}

// scalarString decodes a JSON scalar. ok is false for null.
func scalarString(raw json.RawMessage) (value string, ok bool, err error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return "", false, nil
	}
	switch trimmed[0] {
	case '"':
		var s string
		if err := json.Unmarshal(trimmed, &s); err != nil {
			return "", false, err
		}
		return s, true, nil
	case '{', '[':
		return "", false, fmt.Errorf("value must be a string, number, or boolean")
	default:
		var v any
		dec := json.NewDecoder(bytes.NewReader(trimmed))
		dec.UseNumber()
		if err := dec.Decode(&v); err != nil {
			return "", false, err
		}
		return fmt.Sprint(v), true, nil
	}
}

func hasPlaceholder(m map[string]string) bool {
	for _, v := range m {
		if strings.Contains(v, "${") {
			return true
		}
	}
	return false
}
