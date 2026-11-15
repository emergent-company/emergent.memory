package market

import (
	"encoding/json"
	"strings"
)

// Catalog is the GET /v1/catalog response envelope.
type Catalog struct {
	Listings   []Listing `json:"listings"`
	NextCursor string    `json:"next_cursor,omitempty"`
}

// Compat is the listing's declared compatibility range.
type Compat struct {
	Memory string `json:"memory,omitempty"`
}

// Publisher identifies who published a listing. Some market responses render it
// as an object, others as a bare slug string; UnmarshalJSON accepts both.
type Publisher struct {
	Slug     string `json:"slug,omitempty"`
	Name     string `json:"name,omitempty"`
	Verified string `json:"verified,omitempty"`
}

func (p *Publisher) UnmarshalJSON(data []byte) error {
	var raw string
	if err := json.Unmarshal(data, &raw); err == nil {
		p.Slug = raw
		return nil
	}
	type alias Publisher
	var a alias
	if err := json.Unmarshal(data, &a); err != nil {
		return err
	}
	*p = Publisher(a)
	return nil
}

// DisplayName returns the publisher's user-facing name (name, else slug).
func (p Publisher) DisplayName() string {
	if p.Name != "" {
		return p.Name
	}
	return p.Slug
}

// ListingDetails is the nested `listing:` metadata block.
type ListingDetails struct {
	Title    string `json:"title,omitempty"`
	Summary  string `json:"summary,omitempty"`
	Category string `json:"category,omitempty"`
}

// CapabilityModel is the optional model disclosure block.
type CapabilityModel struct {
	Provider string `json:"provider,omitempty"`
	Name     string `json:"name,omitempty"`
}

// MCPCapability declares an MCP dependency.
type MCPCapability struct {
	Slug   string   `json:"slug"`
	Scopes []string `json:"scopes,omitempty"`
}

// Capabilities is the listing's declared read/write/egress surface and the tool
// set licensed for delivery. Tools is an extension for install listings: a
// blueprint may only declare agent tools that appear here (or in the top-level
// tools list).
type Capabilities struct {
	Reads     []string         `json:"reads,omitempty"`
	Writes    []string         `json:"writes,omitempty"`
	Egress    []string         `json:"egress,omitempty"`
	MCPs      []MCPCapability  `json:"mcps,omitempty"`
	Schedules bool             `json:"schedules,omitempty"`
	Model     *CapabilityModel `json:"model,omitempty"`
	Tools     []string         `json:"tools,omitempty"`
}

// Tool is one MCP tool declaration.
type Tool struct {
	Name      string          `json:"name"`
	Direction string          `json:"direction,omitempty"`
	Schema    json.RawMessage `json:"schema,omitempty"`
}

// BundleRef describes the install bundle.
type BundleRef struct {
	Blueprint string   `json:"blueprint,omitempty"`
	Files     []string `json:"files,omitempty"`
	Digest    string   `json:"digest,omitempty"`
	URL       string   `json:"url,omitempty"`
}

// Listing is one market listing/version. It accepts both flattened and nested
// metadata shapes so the market can evolve the envelope without breaking the
// instance.
type Listing struct {
	Slug         string          `json:"slug"`
	Version      string          `json:"version,omitempty"`
	Semver       string          `json:"semver,omitempty"`
	Kind         string          `json:"kind,omitempty"`
	Delivery     string          `json:"delivery,omitempty"`
	Compat       Compat          `json:"compat"`
	Publisher    Publisher       `json:"publisher"`
	Title        string          `json:"title,omitempty"`
	Summary      string          `json:"summary,omitempty"`
	Meta         ListingDetails  `json:"listing"`
	Capabilities Capabilities    `json:"capabilities"`
	Tools        []Tool          `json:"tools,omitempty"`
	Bundle       BundleRef       `json:"bundle"`
	Manifest     json.RawMessage `json:"manifest,omitempty"`
}

// DisplayTitle returns the listing title, falling back to the slug.
func (l *Listing) DisplayTitle() string {
	if l.Meta.Title != "" {
		return l.Meta.Title
	}
	if l.Title != "" {
		return l.Title
	}
	return l.Slug
}

// VersionString returns the listing version from either spelling.
func (l *Listing) VersionString() string {
	if l.Version != "" {
		return l.Version
	}
	if l.Semver != "" {
		return l.Semver
	}
	return ""
}

// Digest returns the listing's declared bundle digest (top-level or bundle).
func (l *Listing) Digest() string {
	return l.Bundle.Digest
}

// LicencedTools returns the union of capabilities.tools and top-level tools.
func (l *Listing) LicencedTools() []string {
	seen := map[string]struct{}{}
	var out []string
	for _, name := range l.Capabilities.Tools {
		if name == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		out = append(out, name)
	}
	for _, tool := range l.Tools {
		if tool.Name == "" {
			continue
		}
		if _, ok := seen[tool.Name]; ok {
			continue
		}
		seen[tool.Name] = struct{}{}
		out = append(out, tool.Name)
	}
	return out
}

// Version is GET /v1/listings/{slug}/versions/{semver}: the immutable version
// document the review and the tool-licence check need. The exchange names the
// version, but the listing envelope on the exchange carries only card fields
// (title/publisher/semver/digest); compatibility and capabilities live here.
type Version struct {
	Slug     string   `json:"slug"`
	Version  string   `json:"version"`
	Kind     string   `json:"kind,omitempty"`
	Delivery string   `json:"delivery,omitempty"`
	Digest   string   `json:"digest,omitempty"`
	Compat   string   `json:"compat,omitempty"`
	Manifest Manifest `json:"manifest"`
	Tools    []Tool   `json:"tools,omitempty"`
}

// Manifest is the subset of the published manifest the install review renders:
// the capability surface, the agents (so the review can map their pinned
// models) and the tools licensed for delivery.
type Manifest struct {
	Capabilities Capabilities    `json:"capabilities"`
	Agents       []ManifestAgent `json:"agents,omitempty"`
	Tools        []string        `json:"tools,omitempty"`
}

// ManifestAgent is one agent as published in the version manifest. Only the
// review-relevant fields are carried: the agent name and its pinned model.
type ManifestAgent struct {
	Name  string              `json:"name"`
	Model *ManifestAgentModel `json:"model,omitempty"`
}

// ManifestAgentModel is an agent's pinned model as published. The manifest may
// spell a bare provider/model reference in Name, or publish the provider
// separately; Reference folds both into the one form the model review uses.
type ManifestAgentModel struct {
	Name     string `json:"name,omitempty"`
	Provider string `json:"provider,omitempty"`
}

// UnmarshalJSON accepts both the object form (`model: {name: ..., provider:
// ...}`) and the bare-string form (`model: openai/gpt-5`).
func (m *ManifestAgentModel) UnmarshalJSON(data []byte) error {
	var name string
	if err := json.Unmarshal(data, &name); err == nil {
		m.Name = name
		m.Provider = ""
		return nil
	}
	type alias ManifestAgentModel
	var a alias
	if err := json.Unmarshal(data, &a); err != nil {
		return err
	}
	*m = ManifestAgentModel(a)
	return nil
}

// Reference returns the pinned model as one provider/model string. Name wins
// when it already carries a provider prefix.
func (m *ManifestAgentModel) Reference() string {
	if m == nil {
		return ""
	}
	name := strings.TrimSpace(m.Name)
	provider := strings.TrimSpace(m.Provider)
	if name == "" {
		return ""
	}
	if provider == "" || strings.Contains(name, "/") {
		return name
	}
	return provider + "/" + name
}

// ExchangeRequest is POST /v1/installs/exchange.
type ExchangeRequest struct {
	Code string `json:"code"`
}

// ExchangeResponse is the authoritative install facts returned by the market.
type ExchangeResponse struct {
	InstallID  string          `json:"install_id,omitempty"`
	Listing    Listing         `json:"listing"`
	License    string          `json:"license,omitempty"`
	BundleURL  string          `json:"bundle_url,omitempty"`
	Digest     string          `json:"digest,omitempty"`
	LicenseJTI string          `json:"license_jti,omitempty"`
	Raw        json.RawMessage `json:"-"`
	// Version is the fetched version document, not part of the exchange body.
	Version *Version `json:"-"`
}

// Normalize folds the response's redundant spellings into the canonical fields.
func (e *ExchangeResponse) Normalize() {
	if e.Digest == "" {
		e.Digest = e.Listing.Digest()
	}
	if e.BundleURL == "" {
		e.BundleURL = e.Listing.Bundle.URL
	}
	if e.InstallID == "" {
		e.InstallID = e.Listing.Slug
	}
}

// BundleDigest returns the authoritative digest from the exchange or listing.
func (e *ExchangeResponse) BundleDigest() string {
	if e.Digest != "" {
		return e.Digest
	}
	return e.Listing.Digest()
}

// Install report states. The market's POST /v1/installs decodes with
// DisallowUnknownFields and accepts only these values.
const (
	ReportStateInstalled = "installed"
	ReportStateFailed    = "failed"
)

// ReportRequest is the POST /v1/installs report body. The market identifies
// the install from the licence, so the body carries only the terminal state and
// an optional stable error code. Exactly these two keys are accepted.
type ReportRequest struct {
	State     string `json:"state"`
	ErrorCode string `json:"error_code,omitempty"`
}

// CleanDigest strips an optional "sha256:" prefix and lower-cases hex.
func CleanDigest(digest string) string {
	d := strings.TrimSpace(strings.ToLower(digest))
	d = strings.TrimPrefix(d, "sha256:")
	return d
}
