package main

import (
	"net/url"
	"strings"
)

// externalMCPURL rewrites an MCP endpoint URL reported by the backend so it
// points at the memory server's externally-reachable origin (MEMORY_PUBLIC_URL,
// defaulting to MEMORY_URL) instead of whatever host the backend itself knows
// (an internal service name the outside client cannot reach). MCP routes are
// served by the memory server, so the origin is the server's public origin —
// never this gateway's PUBLIC_BASE_URL and never the request Host, which an
// attacker could influence. The path and query are preserved unchanged. Empty or
// unparseable input, or an unconfigured server origin, is returned as-is so a
// malformed backend value never crashes a render and no URL is fabricated.
func (s *Server) externalMCPURL(raw string) string {
	if strings.TrimSpace(raw) == "" {
		return raw
	}
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	base, err := url.Parse(s.serverPublicBaseURL())
	if err != nil || base.Host == "" {
		return raw
	}
	u.Scheme = base.Scheme
	u.Host = base.Host
	return u.String()
}
