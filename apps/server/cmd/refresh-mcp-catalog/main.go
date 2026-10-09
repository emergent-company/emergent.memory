// Command refresh-mcp-catalog regenerates the embedded curated MCP registry
// catalog (apps/server/domain/mcpregistry/catalog/servers.json) from a curated
// source list that can mix two source kinds.
//
// Each line of the source list (sources.txt) is either:
//
//   - a GitHub MCP-registry slug (`owner/repo`), fetched from
//     https://github.com/mcp/<slug>?format=json with
//     payload.mcpDetailsRoute.server_data.raw_data.server extracted; or
//   - an official MCP registry server name prefixed `official:` (e.g.
//     `official:io.github.homeassistant-ai/ha-mcp`), fetched from
//     {registry-base}/v0.1/servers/<name>/versions/latest with the top-level
//     `server` object extracted.
//
// Both kinds drop the `_meta` key and require a non-empty `name`. The result is
// written as {"servers":[...]} in the official registry `server.json` schema,
// preserving sources.txt order.
//
// It fails loudly (non-zero exit) if any source 404s or yields a server object
// without a `name`, and never overwrites the existing catalog with a partial or
// empty result: the whole catalog is assembled in memory and only written to
// disk once every source has succeeded.
//
// Run from apps/server:
//
//	go run ./cmd/refresh-mcp-catalog
//
// The output is reviewed via PR alongside the sources list (see the header of
// domain/mcpregistry/catalog/sources.txt).
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	defaultSources = "domain/mcpregistry/catalog/sources.txt"
	defaultOut     = "domain/mcpregistry/catalog/servers.json"

	// defaultBaseURL is the base URL of the GitHub MCP registry snapshot.
	defaultBaseURL = "https://github.com/mcp"

	// defaultRegistryBaseURL is the base URL of the official MCP registry.
	defaultRegistryBaseURL = "https://registry.modelcontextprotocol.io"

	// registryAPIVersion is the official MCP registry API version used in
	// server lookup URLs.
	registryAPIVersion = "v0.1"

	// officialPrefix marks a source line as an official MCP registry server
	// name (everything after the prefix) rather than a GitHub slug.
	officialPrefix = "official:"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

// run executes the refresh tool and returns a process exit code. stdout/stderr
// are injected so the tool can be exercised end-to-end in tests.
func run(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("refresh-mcp-catalog", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var (
		sourcesPath     = fs.String("sources", defaultSources, "path to the curated sources list")
		outPath         = fs.String("out", defaultOut, "path to write the generated catalog JSON")
		baseURL         = fs.String("base-url", defaultBaseURL, "base URL of the GitHub MCP registry snapshot")
		registryBaseURL = fs.String("registry-base-url", defaultRegistryBaseURL, "base URL of the official MCP registry")
	)
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if fs.NArg() > 0 {
		fmt.Fprintf(stderr, "unexpected arguments: %v\n", fs.Args())
		return 2
	}

	sources, err := readSources(*sourcesPath)
	if err != nil {
		fmt.Fprintf(stderr, "reading sources: %v\n", err)
		return 1
	}
	if len(sources) == 0 {
		fmt.Fprintf(stderr, "sources list is empty\n")
		return 1
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	client := &http.Client{Timeout: 30 * time.Second}
	servers, err := refresh(ctx, client, *baseURL, *registryBaseURL, sources)
	if err != nil {
		fmt.Fprintf(stderr, "refresh failed: %v\n", err)
		return 1
	}

	if err := writeCatalog(*outPath, servers); err != nil {
		fmt.Fprintf(stderr, "writing catalog: %v\n", err)
		return 1
	}

	fmt.Fprintf(stdout, "wrote %d servers to %s\n", len(servers), *outPath)
	return 0
}

// readSources reads and parses the curated source list from a file path.
func readSources(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return parseSources(f)
}

// parseSources parses a curated source list: one source per line, with blank
// lines and `#` comments (full-line, leading whitespace allowed) ignored.
// Lines are trimmed of surrounding whitespace and returned verbatim, so a line
// prefixed `official:` keeps its prefix for dispatch later.
func parseSources(r io.Reader) ([]string, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}
	var slugs []string
	for _, raw := range strings.Split(string(data), "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		slugs = append(slugs, line)
	}
	return slugs, nil
}

// refresh fetches and extracts the server object for every source, in order.
// A source prefixed `official:` is resolved against the official MCP registry;
// any other source is resolved against the GitHub MCP registry. It returns a
// slice of the same length as sources, or an error on the first failure — the
// caller writes nothing on error, so a bad source can never leave a partial
// catalog on disk.
func refresh(ctx context.Context, client *http.Client, baseURL, registryBaseURL string, sources []string) ([]map[string]any, error) {
	servers := make([]map[string]any, 0, len(sources))
	for _, src := range sources {
		var (
			srv map[string]any
			err error
		)
		if name, ok := strings.CutPrefix(src, officialPrefix); ok {
			srv, err = fetchOfficialServer(ctx, client, registryBaseURL, name)
		} else {
			srv, err = fetchServer(ctx, client, baseURL, src)
		}
		if err != nil {
			return nil, err
		}
		servers = append(servers, srv)
	}
	return servers, nil
}

// fetchServer fetches and extracts the server object for a single slug.
func fetchServer(ctx context.Context, client *http.Client, baseURL, slug string) (map[string]any, error) {
	url := fmt.Sprintf("%s/%s?format=json", strings.TrimRight(baseURL, "/"), slug)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("building request for %s: %w", slug, err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetching %s: %w", slug, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("server %q not found (404) at %s", slug, url)
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("fetching %s: status %d: %s", slug, resp.StatusCode, strings.TrimSpace(string(body)))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", slug, err)
	}

	srv, err := extractServer(body)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", slug, err)
	}
	return srv, nil
}

// fetchOfficialServer fetches and extracts the server object for a single
// official MCP registry server name. The name is path-escaped so slash-bearing
// names (e.g. `io.github.homeassistant-ai/ha-mcp`) become `%2F` in the URL.
func fetchOfficialServer(ctx context.Context, client *http.Client, registryBaseURL, name string) (map[string]any, error) {
	url := fmt.Sprintf("%s/%s/servers/%s/versions/latest",
		strings.TrimRight(registryBaseURL, "/"), registryAPIVersion, url.PathEscape(name))

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("building request for %s: %w", name, err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetching %s: %w", name, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("server %q not found (404) at %s", name, url)
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("fetching %s: status %d: %s", name, resp.StatusCode, strings.TrimSpace(string(body)))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", name, err)
	}

	srv, err := extractOfficialServer(body)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	return srv, nil
}

// githubPayload models the fixed path into the GitHub MCP registry `?format=json`
// response: payload.mcpDetailsRoute.server_data.raw_data.server.
type githubPayload struct {
	Payload struct {
		MCPDetailsRoute struct {
			ServerData struct {
				RawData struct {
					Server json.RawMessage `json:"server"`
				} `json:"raw_data"`
			} `json:"server_data"`
		} `json:"mcpDetailsRoute"`
	} `json:"payload"`
}

// officialPayload models the official MCP registry server response:
// {"server": {...}, "_meta": {...}}. The `_meta` sidecar is ignored.
type officialPayload struct {
	Server json.RawMessage `json:"server"`
}

// extractServer extracts the server object from a GitHub `?format=json`
// payload, drops the `_meta` key, and requires a non-empty `name`. The server
// is returned as a map so the full official server.json object round-trips
// (unknown fields such as `$schema`, icons, and package argument metadata are
// preserved rather than discarded).
func extractServer(body []byte) (map[string]any, error) {
	var p githubPayload
	if err := json.Unmarshal(body, &p); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}

	raw := p.Payload.MCPDetailsRoute.ServerData.RawData.Server
	if len(raw) == 0 {
		return nil, errors.New("payload has no server data")
	}

	return decodeServer(raw)
}

// extractOfficialServer extracts the server object from an official MCP
// registry response, drops the `_meta` key, and requires a non-empty `name`.
func extractOfficialServer(body []byte) (map[string]any, error) {
	var p officialPayload
	if err := json.Unmarshal(body, &p); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}

	if len(p.Server) == 0 {
		return nil, errors.New("payload has no server data")
	}

	return decodeServer(p.Server)
}

// decodeServer decodes a raw server object, drops the `_meta` key, and requires
// a non-empty `name`.
func decodeServer(raw json.RawMessage) (map[string]any, error) {
	var server map[string]any
	if err := json.Unmarshal(raw, &server); err != nil {
		return nil, fmt.Errorf("decoding server object: %w", err)
	}

	delete(server, "_meta")

	name, _ := server["name"].(string)
	if name == "" {
		return nil, errors.New("server is missing a name")
	}

	return server, nil
}

// writeCatalog marshals {"servers":[...]} and writes it atomically to outPath
// (temp file + rename), so a crash mid-write never leaves a truncated catalog.
func writeCatalog(outPath string, servers []map[string]any) error {
	data, err := marshalCatalog(servers)
	if err != nil {
		return err
	}

	dir := filepath.Dir(outPath)
	tmp, err := os.CreateTemp(dir, ".servers-*.json")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) // no-op after a successful rename

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, outPath)
}

// marshalCatalog renders the catalog as the committed {"servers":[...]} shape.
func marshalCatalog(servers []map[string]any) ([]byte, error) {
	out := map[string]any{"servers": servers}
	data, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}
