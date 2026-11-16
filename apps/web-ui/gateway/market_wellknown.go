package main

import (
	"net/http"

	"github.com/labstack/echo/v4"
)

// memoryInstanceWellKnown serves GET /.well-known/memory-instance: a public,
// non-secret discovery document the marketplace reads from the browser to
// confirm the instance is reachable and which market issuers it trusts.
//
// It deliberately carries no configuration beyond the public issuer origins:
// no secrets, no internal hostnames. CORS is open because the market fetches it
// cross-origin from the user's browser, and the document is cacheable for five
// minutes.
func (s *Server) memoryInstanceWellKnown(c echo.Context) error {
	markets := s.cfg.MarketTrustedIssuers
	if markets == nil {
		markets = []string{}
	}
	c.Response().Header().Set("Access-Control-Allow-Origin", "*")
	c.Response().Header().Set("Cache-Control", "public, max-age=300")
	return c.JSON(http.StatusOK, map[string]any{
		"name":    "Memory",
		"version": version,
		"install": true,
		"markets": markets,
	})
}
