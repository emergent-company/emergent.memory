package mcpregistry

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
)

// catalogJSON is the curated MCP registry, embedded at build time. It uses the
// official registry `server.json` schema ({"servers": [...]}) so the same
// RegistryServer type models it and CI/PRs can keep it in sync with (or replace
// it by) the live registry without a schema change.
//
//go:embed catalog/servers.json
var catalogJSON []byte

// catalog is an immutable, in-memory snapshot of the curated MCP registry.
type catalog struct {
	servers []RegistryServer
}

var (
	catalogOnce sync.Once
	catalogData catalog
	catalogErr  error
)

// loadCatalog parses the embedded catalog exactly once and returns the shared,
// immutable snapshot. It is safe for concurrent use. A parse failure (a broken
// build artifact) is cached and returned on every subsequent call so startup
// fails fast instead of silently serving an empty catalog.
func loadCatalog() (catalog, error) {
	catalogOnce.Do(func() {
		var wrapper struct {
			Servers []RegistryServer `json:"servers"`
		}
		if err := json.Unmarshal(catalogJSON, &wrapper); err != nil {
			catalogErr = fmt.Errorf("parsing embedded MCP registry catalog: %w", err)
			return
		}
		catalogData = catalog{servers: wrapper.Servers}
	})
	return catalogData, catalogErr
}

// search returns the catalog entries matching query via a case-insensitive
// substring match over name, title, and description. Name matches rank before
// title matches, which rank before description matches; within each rank the
// entries keep file order, so the result is deterministic. An empty query
// returns every entry. At most limit entries are returned; limit <= 0 returns
// every entry, because browse is unpaginated over the whole (small) curated
// set — the cursor is ignored and no next page is produced.
func (c catalog) search(query string, limit int) []RegistryServer {
	if limit <= 0 {
		limit = len(c.servers)
	}

	results := make([]RegistryServer, 0, len(c.servers))
	if query == "" {
		for _, s := range c.servers {
			if len(results) >= limit {
				break
			}
			results = append(results, s)
		}
		return results
	}

	q := strings.ToLower(query)
	var nameMatches, titleMatches, descMatches []RegistryServer
	for _, s := range c.servers {
		switch {
		case strings.Contains(strings.ToLower(s.Name), q):
			nameMatches = append(nameMatches, s)
		case strings.Contains(strings.ToLower(s.Title), q):
			titleMatches = append(titleMatches, s)
		case strings.Contains(strings.ToLower(s.Description), q):
			descMatches = append(descMatches, s)
		}
	}

	for _, s := range nameMatches {
		if len(results) >= limit {
			return results
		}
		results = append(results, s)
	}
	for _, s := range titleMatches {
		if len(results) >= limit {
			return results
		}
		results = append(results, s)
	}
	for _, s := range descMatches {
		if len(results) >= limit {
			return results
		}
		results = append(results, s)
	}
	return results
}

// get returns the entry with the exact name, if present.
func (c catalog) get(name string) (RegistryServer, bool) {
	for _, s := range c.servers {
		if s.Name == name {
			return s, true
		}
	}
	return RegistryServer{}, false
}
