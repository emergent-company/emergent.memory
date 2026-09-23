// Command mcp-registry-stub is a hermetic stand-in for the official MCP
// registry API (https://registry.modelcontextprotocol.io) used by the e2e
// harness. It serves canned fixtures over the same /v0.1 contract that the
// server's registry client consumes, so API e2e tests never hit the live
// upstream and never flake on upstream latency.
//
// Configuration (env):
//
//	PORT                listen port (default 8080)
//	STUB_FORCE_STATUS   when > 0, every /v0.1/ request returns this status
//	                    with a plain-text body (default 0 = normal operation)
package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
)

// ─────────────────────────────────────────────────────────────────────────────
// Registry contract types (field names must match apps/server/domain/mcpregistry/registry_client.go)
// ─────────────────────────────────────────────────────────────────────────────

// RegistryHeader describes an HTTP header needed for remote connections.
type RegistryHeader struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	IsRequired  bool   `json:"isRequired"`
	IsSecret    bool   `json:"isSecret"`
	Value       string `json:"value,omitempty"`
}

// RegistryEnvVar describes an environment variable needed by the server.
type RegistryEnvVar struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	IsRequired  bool   `json:"isRequired"`
	IsSecret    bool   `json:"isSecret"`
}

// RegistryTransport describes the transport type for a package.
type RegistryTransport struct {
	Type string `json:"type"` // stdio, sse, streamable-http
}

// RegistryPackage describes how to run the server locally.
type RegistryPackage struct {
	RegistryType string            `json:"registryType"`
	Name         string            `json:"name"`
	Identifier   string            `json:"identifier,omitempty"`
	Version      string            `json:"version,omitempty"`
	Transport    RegistryTransport `json:"transport"`
}

// RegistryRemote describes a remote server endpoint.
type RegistryRemote struct {
	Type    string           `json:"type"` // streamable-http, sse
	URL     string           `json:"url"`
	Headers []RegistryHeader `json:"headers,omitempty"`
}

// RegistryRepo contains repository metadata.
type RegistryRepo struct {
	URL    string `json:"url"`
	Source string `json:"source"`
}

// RegistryServer is the core server object from the registry.
type RegistryServer struct {
	Name        string            `json:"name"`
	Description string            `json:"description"`
	Title       string            `json:"title,omitempty"`
	Version     string            `json:"version"`
	Repository  *RegistryRepo     `json:"repository,omitempty"`
	Packages    []RegistryPackage `json:"packages,omitempty"`
	Remotes     []RegistryRemote  `json:"remotes,omitempty"`
}

// RegistryOfficialMeta contains official registry metadata.
type RegistryOfficialMeta struct {
	Status   string `json:"status,omitempty"`
	IsLatest bool   `json:"isLatest,omitempty"`
}

// RegistryMeta contains metadata about a server entry.
type RegistryMeta struct {
	Official *RegistryOfficialMeta `json:"io.modelcontextprotocol.registry/official,omitempty"`
}

// RegistryServerEntry is a single server entry in search results.
type RegistryServerEntry struct {
	Server RegistryServer `json:"server"`
	Meta   RegistryMeta   `json:"_meta,omitempty"`
}

// RegistryServerResponse is the response for a single server version lookup.
type RegistryServerResponse struct {
	Server RegistryServer `json:"server"`
	Meta   RegistryMeta   `json:"_meta,omitempty"`
}

// RegistryMetadata contains pagination info for list responses.
type RegistryMetadata struct {
	NextCursor string `json:"nextCursor"`
	Count      int    `json:"count,omitempty"`
}

// RegistrySearchResponse is the top-level response for server search/list.
type RegistrySearchResponse struct {
	Servers  []RegistryServerEntry `json:"servers"`
	Metadata RegistryMetadata      `json:"metadata"`
}

// ─────────────────────────────────────────────────────────────────────────────
// Fixtures
// ─────────────────────────────────────────────────────────────────────────────

// fixtures returns the canned registry servers. Field names and shapes are the
// exact contract the server's registry client and install path rely on.
func fixtures() []RegistryServer {
	return []RegistryServer{
		{
			Name:        "io.github.github/github-mcp-server",
			Title:       "GitHub MCP Server",
			Description: "Official GitHub MCP server providing repository, issue, and pull request tools via the GitHub API.",
			Version:     "1.0.0",
			Repository:  &RegistryRepo{URL: "https://github.com/github/github-mcp-server", Source: "github"},
			Remotes: []RegistryRemote{
				{
					Type: "streamable-http",
					URL:  "http://test-mcp-registry:8080/mcp",
					Headers: []RegistryHeader{
						{
							Name:        "Authorization",
							Description: "GitHub personal access token",
							IsRequired:  true,
							IsSecret:    true,
							Value:       "Bearer {github_token}",
						},
					},
				},
			},
		},
		{
			Name:        "io.github.upstash/context7",
			Title:       "Context7",
			Description: "Context7 provides up-to-date documentation for libraries and frameworks at build time.",
			Version:     "1.0.0",
			// No remotes — stdio-only package, so the server's install path
			// rejects it with a message containing "stdio".
			Packages: []RegistryPackage{
				{
					RegistryType: "npm",
					Name:         "@upstash/context7-mcp",
					Identifier:   "@upstash/context7-mcp",
					Version:      "1.0.0",
					Transport:    RegistryTransport{Type: "stdio"},
				},
			},
		},
		{
			Name:        "io.modelcontextprotocol/server-everything",
			Description: "Reference / echo MCP server used for conformance testing",
			Version:     "1.0.0",
			Remotes: []RegistryRemote{
				{
					Type: "streamable-http",
					URL:  "http://test-mcp-registry:8080/mcp",
				},
			},
		},
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Handler
// ─────────────────────────────────────────────────────────────────────────────

// stubHandler serves the registry stub contract. Routing is performed against
// r.URL.EscapedPath() (not the decoded Path) so a percent-encoded "%2F" inside a
// server name is preserved as a single path segment.
type stubHandler struct {
	forceStatus int
	fixtures    []RegistryServer
}

// newHandler builds the stub http.Handler. forceStatus > 0 forces every
// /v0.1/ request to return that status with a plain-text body (used to prove
// the API tests still fail when the upstream errors).
func newHandler(forceStatus int) http.Handler {
	return &stubHandler{forceStatus: forceStatus, fixtures: fixtures()}
}

func (h *stubHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Log every request so docker logs show exactly what was requested.
	log.Printf("%s %s %s", r.Method, r.URL.EscapedPath(), r.URL.RawQuery)

	if h.forceStatus > 0 && strings.HasPrefix(r.URL.EscapedPath(), "/v0.1/") {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(h.forceStatus)
		fmt.Fprintf(w, "stub forced status %d", h.forceStatus)
		return
	}

	path := r.URL.EscapedPath()
	switch {
	case path == "/healthz":
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "ok")
	case path == "/v0.1/servers":
		h.handleSearch(w, r)
	case strings.HasPrefix(path, "/v0.1/servers/") && strings.Contains(path, "/versions/"):
		h.handleDetail(w, r, path)
	default:
		// /mcp and anything else → 405, so post-install tool discovery against
		// the fixture remote URL fails fast and stays hermetic (no internet).
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusMethodNotAllowed)
		fmt.Fprint(w, "method not allowed")
	}
}

// handleSearch serves GET /v0.1/servers (search/list).
func (h *stubHandler) handleSearch(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	search := strings.ToLower(q.Get("search"))

	limit := 20
	if ls := q.Get("limit"); ls != "" {
		if n, err := strconv.Atoi(ls); err == nil && n > 0 {
			limit = n
		}
	}

	entries := make([]RegistryServerEntry, 0, len(h.fixtures))
	for _, s := range h.fixtures {
		if search != "" {
			hay := strings.ToLower(s.Name + "\n" + s.Title + "\n" + s.Description)
			if !strings.Contains(hay, search) {
				continue
			}
		}
		if len(entries) >= limit {
			break
		}
		entries = append(entries, RegistryServerEntry{
			Server: s,
			Meta: RegistryMeta{
				Official: &RegistryOfficialMeta{Status: "active", IsLatest: true},
			},
		})
	}

	resp := RegistrySearchResponse{
		Servers: entries,
		Metadata: RegistryMetadata{
			Count:      len(entries),
			NextCursor: "",
		},
	}
	writeJSON(w, http.StatusOK, resp)
}

// handleDetail serves GET /v0.1/servers/{name}/versions/{version}.
func (h *stubHandler) handleDetail(w http.ResponseWriter, r *http.Request, path string) {
	rest := strings.TrimPrefix(path, "/v0.1/servers/")
	idx := strings.LastIndex(rest, "/versions/")
	if idx < 0 {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusNotFound)
		fmt.Fprint(w, "not found")
		return
	}

	nameEnc := rest[:idx]
	name, err := url.PathUnescape(nameEnc)
	if err != nil {
		name = nameEnc
	}
	// Tolerate a double-encoded name: if the unescaped value still contains a
	// "%2F" escape, unescape once more. This makes the stub robust to how Echo
	// passes the path param through to the server's registry client.
	if strings.Contains(name, "%2F") || strings.Contains(name, "%2f") {
		if u2, err2 := url.PathUnescape(name); err2 == nil {
			name = u2
		}
	}

	for _, s := range h.fixtures {
		if s.Name == name {
			resp := RegistryServerResponse{
				Server: s,
				Meta: RegistryMeta{
					Official: &RegistryOfficialMeta{Status: "active", IsLatest: true},
				},
			}
			writeJSON(w, http.StatusOK, resp)
			return
		}
	}

	// Unknown name → 404 (server maps this to "server %q not found in registry").
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusNotFound)
	fmt.Fprint(w, "not found")
}

// writeJSON writes v as JSON with the given status code.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("encode error: %v", err)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Entrypoint
// ─────────────────────────────────────────────────────────────────────────────

func main() {
	log.SetOutput(os.Stdout)

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	forceStatus := 0
	if fs := os.Getenv("STUB_FORCE_STATUS"); fs != "" {
		if n, err := strconv.Atoi(fs); err == nil {
			forceStatus = n
		}
	}

	addr := ":" + port
	srv := &http.Server{
		Addr:    addr,
		Handler: newHandler(forceStatus),
	}

	log.Printf("mcp-registry stub listening on %s (forceStatus=%d)", addr, forceStatus)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("server error: %v", err)
	}
}
