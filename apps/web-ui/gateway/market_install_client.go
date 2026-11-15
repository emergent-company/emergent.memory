package main

import (
	"context"
	"net/http"
)

// --- marketplace install client types (mirror Memory's /api/market/installs) ---

// MarketListingPreview is the review card's listing block: the fields the
// market exchange/licence made authoritative. It mirrors the server's
// market.ListingPreview.
type MarketListingPreview struct {
	Slug      string `json:"slug"`
	Title     string `json:"title"`
	Publisher string `json:"publisher"`
	Semver    string `json:"semver"`
	Digest    string `json:"digest"`
}

// MarketMCPCapability is one declared MCP dependency. It mirrors the server's
// market.MCPCapability.
type MarketMCPCapability struct {
	Slug   string   `json:"slug"`
	Scopes []string `json:"scopes,omitempty"`
}

// MarketCapabilityModel is the optional model disclosure block. It mirrors the
// server's market.CapabilityModel.
type MarketCapabilityModel struct {
	Provider string `json:"provider,omitempty"`
	Name     string `json:"name,omitempty"`
}

// MarketCapabilities is the listing's declared read/write/egress surface and
// the capabilities the review renders. It mirrors the server's
// market.Capabilities (Tools is carried for completeness; the review page does
// not render it).
type MarketCapabilities struct {
	Reads     []string               `json:"reads,omitempty"`
	Writes    []string               `json:"writes,omitempty"`
	Egress    []string               `json:"egress,omitempty"`
	MCPs      []MarketMCPCapability  `json:"mcps,omitempty"`
	Schedules bool                   `json:"schedules,omitempty"`
	Model     *MarketCapabilityModel `json:"model,omitempty"`
	Tools     []string               `json:"tools,omitempty"`
}

// MarketCompat is the listing's declared compatibility range. It mirrors the
// server's market.Compat.
type MarketCompat struct {
	Memory string `json:"memory,omitempty"`
}

// MarketResolveResult is the review payload returned by POST
// /api/market/installs/resolve. It mirrors the server's market.ResolveResult.
type MarketResolveResult struct {
	Listing          MarketListingPreview `json:"listing"`
	Capabilities     MarketCapabilities   `json:"capabilities"`
	Compat           MarketCompat         `json:"compat"`
	AlreadyInstalled bool                 `json:"already_installed"`
}

// MarketApplyResult is the confirmed-install response returned by POST
// /api/market/installs/apply. It mirrors the server's market.ApplyResult.
type MarketApplyResult struct {
	BlueprintID string `json:"blueprint_id"`
	InstallID   string `json:"install_id"`
}

// marketResolveRequest is the body for POST /api/market/installs/resolve.
type marketResolveRequest struct {
	Market string `json:"market"`
	Code   string `json:"code"`
}

// marketApplyRequest is the body for POST /api/market/installs/apply. The
// project is carried explicitly so the server can authorize it; the request
// headers also pin the active project for the resolve half.
type marketApplyRequest struct {
	Market         string            `json:"market"`
	Code           string            `json:"code"`
	ProjectID      string            `json:"project_id"`
	ModelOverrides map[string]string `json:"model_overrides,omitempty"`
}

// ResolveMarketInstall redeems an install code for review without downloading
// or applying anything (POST /api/market/installs/resolve). Project-scoped via
// the request's active project (X-Project-ID, set from the session).
func (m *MemoryClient) ResolveMarketInstall(ctx context.Context, market, code string) (*MarketResolveResult, error) {
	req := marketResolveRequest{Market: market, Code: code}
	var out MarketResolveResult
	if err := m.doH(ctx, http.MethodPost, "/api/market/installs/resolve", req, m.documentHeaders(ctx), &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ApplyMarketInstall performs the install into projectID (POST
// /api/market/installs/apply). The server authorizes projectID explicitly;
// overrides may be nil for a plain install.
func (m *MemoryClient) ApplyMarketInstall(ctx context.Context, market, code, projectID string, overrides map[string]string) (*MarketApplyResult, error) {
	req := marketApplyRequest{Market: market, Code: code, ProjectID: projectID, ModelOverrides: overrides}
	var out MarketApplyResult
	if err := m.doH(ctx, http.MethodPost, "/api/market/installs/apply", req, m.documentHeaders(ctx), &out); err != nil {
		return nil, err
	}
	return &out, nil
}
