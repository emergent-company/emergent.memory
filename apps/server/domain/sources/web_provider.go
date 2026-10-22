package sources

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"github.com/emergent-company/emergent.memory/internal/outbound"
	"github.com/emergent-company/emergent.memory/pkg/apperror"
)

// Web provider limits (v1 ceilings). max_depth 0 means "ingest only the
// configured start URLs and follow no links".
const (
	webMaxStartURLs       = 50
	webDefaultMaxPages    = 200
	webMaxPages           = 200
	webDefaultMaxDepth    = 3
	webMaxDepth           = 3
	webMinIntervalMin     = 15
	webDefaultIntervalMin = 60
)

// webProvider implements SourceProvider for the web provider: it crawls a
// website and syncs its pages into the knowledge base. Connect is config-only —
// no credential fields are collected or stored — and every fetch runs through
// the deployment's outbound SSRF guard (see internal/outbound). A source config
// has no field that widens that guard.
type webProvider struct {
	policy outbound.Policy
}

func newWebProvider(policy outbound.Policy) *webProvider {
	return &webProvider{policy: policy}
}

func (p *webProvider) Catalog() Provider {
	return Provider{
		Type:        ProviderTypeWeb,
		Name:        "Web",
		Description: "Crawl a website and sync its pages into your knowledge base.",
		SourceType:  SourceTypeWeb,
		Vendor:      "web",
		VendorName:  "Web",
		Available:   true,
	}
}

// ConfigSchema is the non-secret crawl contract. No boolean fields are declared:
// the gateway form omits unchecked checkboxes, so a default-true boolean could
// never be turned off. Limits are described inline; defaults are applied in
// parseWebConfig.
func (p *webProvider) ConfigSchema() ProviderSchema {
	return ProviderSchema{
		Type: ProviderTypeWeb,
		Properties: map[string]any{
			"start_urls": map[string]any{
				"type":        "array",
				"items":       map[string]any{"type": "string"},
				"description": "One or more absolute http(s) URLs to start from (max 50).",
			},
			"include_globs": map[string]any{
				"type":        "array",
				"items":       map[string]any{"type": "string"},
				"description": "Optional URL path globs to keep (e.g. /docs/**). Empty keeps every in-scope page.",
			},
			"exclude_globs": map[string]any{
				"type":        "array",
				"items":       map[string]any{"type": "string"},
				"description": "Optional URL path globs to skip (e.g. /admin/**).",
			},
			"max_pages": map[string]any{
				"type":        "integer",
				"description": "Maximum pages to crawl. Default 200 (the v1 ceiling).",
			},
			"max_depth": map[string]any{
				"type":        "integer",
				"description": "Maximum link depth to follow. Default 3; 0 ingests only the configured start URLs (no link following).",
			},
		},
		Required: []string{"start_urls"},
	}
}

// CredentialFields is empty: web connect is config-only, so there is no
// pasted-credential surface.
func (p *webProvider) CredentialFields() []ProviderField { return nil }

// CredentialKeys is empty: the web provider stores no credential material.
func (p *webProvider) CredentialKeys() []string { return nil }

// AuthMethods is config-only: the connect chooser proceeds straight to the
// config form with no credential step.
func (p *webProvider) AuthMethods() []AuthMethod { return []AuthMethod{AuthMethodConfig} }

func (p *webProvider) OAuth() (OAuthStrategy, bool) { return nil, false }

// ExtractCredentials returns an empty credential map: there is nothing to
// collect or store for a config-only provider.
func (p *webProvider) ExtractCredentials(map[string]any) (Credentials, error) {
	return Credentials{}, nil
}

func (p *webProvider) ValidateConfig(cfg map[string]any) error {
	return validateWebConfig(cfg)
}

// ListResources returns no selectable targets: the web provider has no
// second-level selector (the start_urls list is the whole scope).
func (p *webProvider) ListResources(context.Context, Credentials, map[string]any, string) ([]Resource, bool, error) {
	return []Resource{}, false, nil
}

// NewConnector builds a connector that carries only the deployment policy, a
// user-agent string, and the generic deps. The actual crawl scope is read from
// the cfg argument of Enumerate/TestConnection, not from the constructor.
func (p *webProvider) NewConnector(ctx context.Context, creds Credentials, deps ConnectorDeps) (Connector, error) {
	return newWebConnector(p.policy, webUserAgent, deps), nil
}

// SyncIntervalBounds implements SyncIntervalBoundsProvider: a web source syncs
// by default every 60 minutes and may not be scheduled faster than every 15.
func (p *webProvider) SyncIntervalBounds() (int, int) {
	return webMinIntervalMin, webDefaultIntervalMin
}

// validateWebConfig enforces the web config contract: start_urls required (a
// non-empty list of absolute http(s) URLs, at most 50, no userinfo),
// include_globs/exclude_globs optional string arrays, and max_pages/max_depth
// within their v1 ranges. Unknown keys are ignored (matching other providers).
func validateWebConfig(cfg map[string]any) error {
	raw, present := cfg["start_urls"]
	if !present || raw == nil {
		return apperror.NewValidation("config.start_urls is required: provide at least one absolute http(s) URL")
	}
	var startURLs []string
	switch v := raw.(type) {
	case []string:
		startURLs = v
	case []any:
		for _, item := range v {
			s, ok := item.(string)
			if !ok {
				return apperror.NewValidation("config.start_urls must be an array of strings")
			}
			startURLs = append(startURLs, s)
		}
	default:
		return apperror.NewValidation("config.start_urls must be an array of absolute http(s) URLs")
	}
	if len(startURLs) == 0 {
		return apperror.NewValidation("config.start_urls is required: provide at least one absolute http(s) URL")
	}
	if len(startURLs) > webMaxStartURLs {
		return apperror.NewValidation(fmt.Sprintf("config.start_urls may contain at most %d URLs", webMaxStartURLs))
	}
	for _, s := range startURLs {
		if err := validateWebStartURL(s); err != nil {
			return err
		}
	}

	for _, key := range []string{"include_globs", "exclude_globs"} {
		if v, ok := cfg[key]; ok && v != nil && !isStringArray(v) {
			return apperror.NewValidation("config." + key + " must be an array of strings")
		}
	}

	if err := validateWebIntRange(cfg, "max_pages", 1, webMaxPages); err != nil {
		return err
	}
	if err := validateWebIntRange(cfg, "max_depth", 0, webMaxDepth); err != nil {
		return err
	}
	return nil
}

// validateWebStartURL rejects a non-http(s), non-absolute, or userinfo-carrying
// URL. It is the config-time syntactic check; the SSRF guard's DNS/resolution
// check runs at fetch time (see webFetcher).
func validateWebStartURL(raw string) error {
	s := strings.TrimSpace(raw)
	if s == "" {
		return apperror.NewValidation("config.start_urls entries must be non-empty URLs")
	}
	u, err := url.Parse(s)
	if err != nil {
		return apperror.NewValidation("config.start_urls entry is not a valid URL: " + s)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return apperror.NewValidation("config.start_urls entries must be absolute http(s) URLs: " + s)
	}
	if u.User != nil {
		return apperror.NewValidation("config.start_urls entries must not contain userinfo: " + s)
	}
	if u.Hostname() == "" {
		return apperror.NewValidation("config.start_urls entries must include a host: " + s)
	}
	return nil
}

// isStringArray reports whether v is a JSON array whose elements are all strings.
func isStringArray(v any) bool {
	switch vals := v.(type) {
	case []string:
		return true
	case []any:
		for _, item := range vals {
			if _, ok := item.(string); !ok {
				return false
			}
		}
		return true
	default:
		return false
	}
}

// validateWebIntRange rejects an out-of-range (or non-integer) optional integer
// config field. A blank string means "use the default" and is accepted.
func validateWebIntRange(cfg map[string]any, key string, min, max int) error {
	raw, ok := cfg[key]
	if !ok || raw == nil {
		return nil
	}
	v, ok := intValue(raw)
	if !ok {
		if s, isStr := raw.(string); isStr && strings.TrimSpace(s) == "" {
			return nil
		}
		return apperror.NewValidation("config." + key + " must be an integer")
	}
	if v < min || v > max {
		return apperror.NewValidation(fmt.Sprintf("config.%s must be between %d and %d", key, min, max))
	}
	return nil
}
