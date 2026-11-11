package main

import (
	"net/url"
	"strings"
)

// mcpInstallURL builds the https link that redirects to claude://install-mcp
// for a one-click Claude Desktop install. The key rides in the token query
// parameter, so the link is private: anyone holding it can use the key. Never
// log it.
//
// It mirrors the install URL from the memory server's own snippet builder
// (apps/server/domain/mcp/share.go) so the reveal offers the same link the
// invite email already contains.
func mcpInstallURL(baseURL, mcpURL, token string) string {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	mcpURL = strings.TrimSpace(mcpURL)
	token = strings.TrimSpace(token)
	if baseURL == "" || mcpURL == "" || token == "" {
		return ""
	}
	q := url.Values{}
	q.Set("token", token)
	q.Set("url", mcpURL)
	q.Set("name", "memory")
	return baseURL + "/api/mcp/install?" + q.Encode()
}
