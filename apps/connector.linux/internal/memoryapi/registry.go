package memoryapi

import (
	"context"
	"fmt"

	"github.com/emergent-company/emergent.memory/apps/connector.linux/internal/mcpregistry"
	sdkerrors "github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/errors"
	sdkmcpregistry "github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/mcpregistry"
)

// SearchRegistry searches the official MCP registry through Memory and returns
// connector-owned structs (not SDK types).
func (c *Client) SearchRegistry(ctx context.Context, query string, limit int, cursor string) (*mcpregistry.RegistrySearchResult, error) {
	res, err := c.sdk.MCPRegistry.SearchRegistry(ctx, query, limit, cursor)
	if err != nil {
		return nil, fmt.Errorf("memoryapi: search registry: %w", err)
	}
	out := &mcpregistry.RegistrySearchResult{
		Servers:    make([]mcpregistry.RegistryServer, 0, len(res.Servers)),
		NextCursor: res.NextCursor,
		Count:      res.Count,
	}
	for _, s := range res.Servers {
		out.Servers = append(out.Servers, toRegistryServer(s))
	}
	return out, nil
}

// GetRegistryServer fetches one registry entry by name. A not-found upstream
// failure is mapped to mcpregistry.ErrNotFound so the management API can answer
// 404 rather than a generic gateway error.
func (c *Client) GetRegistryServer(ctx context.Context, name string) (*mcpregistry.RegistryServer, error) {
	res, err := c.sdk.MCPRegistry.GetRegistryServer(ctx, name)
	if err != nil {
		if sdkerrors.IsNotFound(err) {
			return nil, mcpregistry.ErrNotFound
		}
		return nil, fmt.Errorf("memoryapi: get registry server: %w", err)
	}
	s := toRegistryServer(*res)
	return &s, nil
}

// toRegistryServer converts an SDK registry server DTO to the connector-owned
// model.
func toRegistryServer(s sdkmcpregistry.RegistryServer) mcpregistry.RegistryServer {
	out := mcpregistry.RegistryServer{
		Name:        s.Name,
		Title:       s.Title,
		Description: s.Description,
		Version:     s.Version,
		HasRemotes:  s.HasRemotes,
		HasPackages: s.HasPackages,
		Packages:    make([]mcpregistry.RegistryPackage, 0, len(s.Packages)),
		Remotes:     make([]mcpregistry.RegistryRemote, 0, len(s.Remotes)),
		EnvVars:     make([]mcpregistry.RegistryEnvVar, 0, len(s.EnvVars)),
		Headers:     make([]mcpregistry.RegistryHeader, 0, len(s.Headers)),
	}
	if s.Repository != nil {
		out.Repository = &mcpregistry.RegistryRepo{URL: s.Repository.URL, Source: s.Repository.Source}
	}
	for _, p := range s.Packages {
		out.Packages = append(out.Packages, mcpregistry.RegistryPackage{
			RegistryType:     p.RegistryType,
			Name:             p.Name,
			Identifier:       p.Identifier,
			Transport:        p.Transport,
			RuntimeArguments: append([]string(nil), p.RuntimeArguments...),
			PackageArguments: append([]string(nil), p.PackageArguments...),
		})
	}
	for _, r := range s.Remotes {
		out.Remotes = append(out.Remotes, mcpregistry.RegistryRemote{Type: r.Type, URL: r.URL})
	}
	for _, e := range s.EnvVars {
		out.EnvVars = append(out.EnvVars, mcpregistry.RegistryEnvVar{
			Name:        e.Name,
			Description: e.Description,
			IsRequired:  e.IsRequired,
			IsSecret:    e.IsSecret,
		})
	}
	for _, h := range s.Headers {
		// The connector does not need the registry's value template (it emits
		// empty placeholders for the user to fill), so Value is intentionally
		// dropped.
		out.Headers = append(out.Headers, mcpregistry.RegistryHeader{
			Name:        h.Name,
			Description: h.Description,
			IsRequired:  h.IsRequired,
			IsSecret:    h.IsSecret,
		})
	}
	return out
}
