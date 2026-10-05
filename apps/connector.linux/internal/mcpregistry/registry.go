// Package mcpregistry owns the connector's registry domain model and the
// translation of a Memory MCP-registry entry into a suggested local
// mcphost.ServerConfig. Translation is stdio-first (npm → npx, pypi → uvx) with
// a remote fallback (http preferred over sse); an entry with neither is
// reported unsupported. Runner availability (npx/uvx) is a warning, never an
// error, so a missing runner never blocks browsing.
package mcpregistry

import (
	"errors"
	"fmt"
	"os/exec"
	"strings"

	"github.com/emergent-company/emergent.memory/apps/connector.linux/internal/mcphost"
)

// Sentinel errors surfaced by SuggestConfig and mapped by the management API.
var (
	// ErrUnsupported is returned when an entry has neither a supported local
	// package (npm/pypi) nor a remote transport (http/sse).
	ErrUnsupported = errors.New("registry entry unsupported: no supported local package or remote transport")
	// ErrNotFound marks an upstream "entry does not exist" lookup failure.
	ErrNotFound = errors.New("registry entry not found")
)

// RegistryRepo is the connector-owned view of an entry's source repository.
type RegistryRepo struct {
	URL    string `json:"url"`
	Source string `json:"source"`
}

// RegistryEnvVar is one declared environment variable (name + required/secret).
type RegistryEnvVar struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	IsRequired  bool   `json:"isRequired"`
	IsSecret    bool   `json:"isSecret"`
}

// RegistryHeader is one declared HTTP header (name + required/secret).
type RegistryHeader struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	IsRequired  bool   `json:"isRequired"`
	IsSecret    bool   `json:"isSecret"`
}

// RegistryPackage is how to run the entry locally. RuntimeArguments and
// PackageArguments are the already-rendered command tokens, appended after the
// runner and package in registry order.
type RegistryPackage struct {
	RegistryType     string   `json:"registryType"` // npm, pypi, oci, nuget, mcpb
	Name             string   `json:"name,omitempty"`
	Identifier       string   `json:"identifier,omitempty"`
	Transport        string   `json:"transport"` // stdio, sse, streamable-http
	RuntimeArguments []string `json:"runtimeArguments,omitempty"`
	PackageArguments []string `json:"packageArguments,omitempty"`
}

// RegistryRemote is a remote endpoint.
type RegistryRemote struct {
	Type string `json:"type"` // streamable-http, sse
	URL  string `json:"url"`
}

// RegistryServer is the connector-owned view of one registry entry. EnvVars and
// Headers are the aggregated declarations across packages and remotes (env is
// the same aggregation the server's RegistryServerDTO exposes; headers are the
// connector-side counterpart that the current server DTO does not yet surface).
type RegistryServer struct {
	Name        string            `json:"name"`
	Title       string            `json:"title,omitempty"`
	Description string            `json:"description"`
	Version     string            `json:"version"`
	Repository  *RegistryRepo     `json:"repository,omitempty"`
	HasRemotes  bool              `json:"hasRemotes"`
	HasPackages bool              `json:"hasPackages"`
	Packages    []RegistryPackage `json:"packages,omitempty"`
	Remotes     []RegistryRemote  `json:"remotes,omitempty"`
	EnvVars     []RegistryEnvVar  `json:"envVars,omitempty"`
	Headers     []RegistryHeader  `json:"headers,omitempty"`
}

// RegistrySearchResult is the connector-owned search result.
type RegistrySearchResult struct {
	Servers    []RegistryServer `json:"servers"`
	NextCursor string           `json:"nextCursor,omitempty"`
	Count      int              `json:"count"`
}

// Suggestion carries the metadata of a suggested local config: the chosen
// transport, the required env/header keys the user must fill, and any non-fatal
// warnings (e.g. a missing runner).
type Suggestion struct {
	Transport          mcphost.Transport
	RequiredEnvKeys    []string
	RequiredHeaderKeys []string
	Warnings           []string
}

// lookPath is exec.LookPath, injectable for tests.
var lookPath = exec.LookPath

// SuggestConfig derives a suggested local mcphost.ServerConfig from a registry
// entry, preferring a locally-runnable package (npm → npx, pypi → uvx) over a
// remote transport (http preferred over sse). localNameOverride wins over the
// derived local name. A missing runner yields a warning, never an error.
func SuggestConfig(entry RegistryServer, localNameOverride string) (mcphost.ServerConfig, Suggestion, error) {
	name := localNameOverride
	if name == "" {
		name = deriveLocalName(entry.Name)
	}
	env := envPlaceholders(entry.EnvVars)
	requiredEnv := requiredEnvKeys(entry.EnvVars)

	// stdio-first: first supported local package in registry order.
	for _, pkg := range entry.Packages {
		runner, ok := runnerFor(pkg.RegistryType)
		if !ok {
			continue
		}
		cfg := mcphost.ServerConfig{
			Name:      name,
			Transport: mcphost.TransportStdio,
			Command:   runner,
			Args:      stdioArgs(runner, pkg.Identifier, entry.Version, pkg),
			Env:       env,
		}
		sug := Suggestion{
			Transport:       mcphost.TransportStdio,
			RequiredEnvKeys: requiredEnv,
		}
		if _, err := lookPath(runner); err != nil {
			sug.Warnings = append(sug.Warnings, fmt.Sprintf("runner %q not found on PATH", runner))
		}
		return cfg, sug, nil
	}

	// remote fallback: http (streamable-http) preferred over sse.
	if remote, ok := pickRemote(entry.Remotes); ok {
		cfg := mcphost.ServerConfig{
			Name:      name,
			Transport: remoteTransport(remote.Type),
			URL:       remote.URL,
			Env:       env,
			Headers:   headerPlaceholders(entry.Headers),
		}
		sug := Suggestion{
			Transport:          cfg.Transport,
			RequiredEnvKeys:    requiredEnv,
			RequiredHeaderKeys: requiredHeaderKeys(entry.Headers),
		}
		return cfg, sug, nil
	}

	return mcphost.ServerConfig{}, Suggestion{}, ErrUnsupported
}

// runnerFor maps a package registry type to its runner ("" + false when the
// type has no supported local runner).
func runnerFor(registryType string) (string, bool) {
	switch registryType {
	case "npm":
		return "npx", true
	case "pypi":
		return "uvx", true
	default:
		return "", false
	}
}

// stdioArgs builds the command tokens: the runner's own args (npx gets -y),
// then <identifier>[@<version>], then runtime then package arguments in order.
func stdioArgs(runner, identifier, version string, pkg RegistryPackage) []string {
	args := make([]string, 0, 2+len(pkg.RuntimeArguments)+len(pkg.PackageArguments))
	if runner == "npx" {
		args = append(args, "-y")
	}
	args = append(args, packageSpec(identifier, version))
	args = append(args, pkg.RuntimeArguments...)
	args = append(args, pkg.PackageArguments...)
	return args
}

// packageSpec renders "<identifier>@<version>" (pinning to the entry version),
// or the bare identifier when no version is present.
func packageSpec(identifier, version string) string {
	if version == "" {
		return identifier
	}
	return identifier + "@" + version
}

// pickRemote selects the remote to fall back to: streamable-http preferred over
// sse.
func pickRemote(remotes []RegistryRemote) (RegistryRemote, bool) {
	for _, r := range remotes {
		if r.Type == "streamable-http" {
			return r, true
		}
	}
	for _, r := range remotes {
		if r.Type == "sse" {
			return r, true
		}
	}
	return RegistryRemote{}, false
}

// remoteTransport maps a registry remote type to an mcphost transport.
func remoteTransport(t string) mcphost.Transport {
	switch t {
	case "sse":
		return mcphost.TransportSSE
	default:
		return mcphost.TransportHTTP
	}
}

// envPlaceholders builds the empty env map (one "" entry per declared var).
func envPlaceholders(envVars []RegistryEnvVar) map[string]string {
	out := make(map[string]string, len(envVars))
	for _, ev := range envVars {
		out[ev.Name] = ""
	}
	return out
}

// headerPlaceholders builds the empty headers map (one "" entry per declared
// header).
func headerPlaceholders(headers []RegistryHeader) map[string]string {
	out := make(map[string]string, len(headers))
	for _, h := range headers {
		out[h.Name] = ""
	}
	return out
}

// requiredEnvKeys returns the names of required env vars, in declaration order.
func requiredEnvKeys(envVars []RegistryEnvVar) []string {
	out := make([]string, 0, len(envVars))
	for _, ev := range envVars {
		if ev.IsRequired {
			out = append(out, ev.Name)
		}
	}
	return out
}

// requiredHeaderKeys returns the names of required headers, in declaration order.
func requiredHeaderKeys(headers []RegistryHeader) []string {
	out := make([]string, 0, len(headers))
	for _, h := range headers {
		if h.IsRequired {
			out = append(out, h.Name)
		}
	}
	return out
}

// deriveLocalName extracts a human-friendly name from a registry name:
// "io.github.x/y" → "y".
func deriveLocalName(registryName string) string {
	if i := strings.LastIndex(registryName, "/"); i >= 0 {
		return registryName[i+1:]
	}
	return registryName
}
