package sources

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"

	"github.com/google/uuid"
)

// githubAppRepoPattern validates an owner/name allowlist entry.
var githubAppRepoPattern = regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`)

// githubInstallationIDPattern matches GitHub's numeric installation id.
var githubInstallationIDPattern = regexp.MustCompile(`^[0-9]+$`)

// normalizeInstallationID canonicalises an installation id as decimal digits.
//
// Form clients submit it as a string, but GitHub's install redirect and
// JSON.stringify hand back a number, which decodes to float64; fmt.Sprint of
// that is "1.2345678e+07" instead of the "12345678" the dispatcher compares
// against (strconv.FormatInt of the webhook's installation id), so the source
// would store cleanly and silently never dispatch. Normalise in one place and
// reject a non-integral value instead of stringifying it.
func normalizeInstallationID(v any) (string, error) {
	var digits string
	switch t := v.(type) {
	case string:
		digits = strings.TrimSpace(t)
	case json.Number:
		digits = t.String()
	case int:
		digits = strconv.Itoa(t)
	case int64:
		digits = strconv.FormatInt(t, 10)
	case uint64:
		digits = strconv.FormatUint(t, 10)
	case float64:
		if t != math.Trunc(t) || t < 0 || t > math.MaxInt64 {
			return "", fmt.Errorf("installation_id must be a whole number, got %v", t)
		}
		digits = strconv.FormatInt(int64(t), 10)
	case float32:
		f := float64(t)
		if f != math.Trunc(f) || f < 0 || f > math.MaxInt64 {
			return "", fmt.Errorf("installation_id must be a whole number, got %v", t)
		}
		digits = strconv.FormatInt(int64(f), 10)
	case nil:
		return "", fmt.Errorf("installation_id is required")
	default:
		return "", fmt.Errorf("installation_id must be a string or a number, got %T", v)
	}
	if digits == "" || digits == "<nil>" {
		return "", fmt.Errorf("installation_id is required")
	}
	if !githubInstallationIDPattern.MatchString(digits) {
		return "", fmt.Errorf("installation_id must be numeric, got %q", digits)
	}
	return digits, nil
}

// githubAppProvider is the trigger-backed GitHub App bot. It has no doc sync:
// webhooks dispatch agent runs through source_triggers.
type githubAppProvider struct{}

func newGitHubAppProvider() SourceProvider {
	return &githubAppProvider{}
}

func (p *githubAppProvider) Catalog() Provider {
	return Provider{
		Type:        ProviderTypeGitHubApp,
		Name:        "GitHub App / Bot",
		Description: "Connect a GitHub App installation and trigger an agent on pull-request events.",
		SourceType:  SourceTypeApp,
		Vendor:      "github",
		VendorName:  "GitHub",
		Available:   true,
	}
}

func (p *githubAppProvider) ConfigSchema() ProviderSchema {
	return ProviderSchema{
		Type: ProviderTypeGitHubApp,
		Properties: map[string]any{
			"app_id": map[string]any{
				"type":        "string",
				"description": "Registered org source App id (POST /api/orgs/:orgId/source-apps).",
			},
			"installation_id": map[string]any{
				"type":        "string",
				"description": "The GitHub App installation id for the selected repositories.",
			},
			"repos": map[string]any{
				"type":        "array",
				"items":       map[string]any{"type": "string"},
				"description": "Repository allowlist (owner/name). Webhooks outside it are ignored.",
			},
		},
		Required: []string{"app_id", "installation_id", "repos"},
	}
}

func (p *githubAppProvider) CredentialFields() []ProviderField { return nil }
func (p *githubAppProvider) AuthMethods() []AuthMethod         { return []AuthMethod{AuthMethodConfig} }
func (p *githubAppProvider) OAuth() (OAuthStrategy, bool)      { return nil, false }
func (p *githubAppProvider) CredentialKeys() []string          { return nil }

// TriggerOnly marks the provider as event-driven: it never syncs documents, so
// the Service refuses a scheduled sync mode for its sources.
func (p *githubAppProvider) TriggerOnly() bool { return true }

func (p *githubAppProvider) NormalizeConfig(cfg map[string]any) {
	if v, ok := cfg["installation_id"]; ok {
		// Canonical digits: a JSON number must not be stored as 1.2e+07, which
		// the dispatcher (strconv.FormatInt of the webhook's installation id)
		// would never match. Keep an unparseable value visible for ValidateConfig.
		digits, err := normalizeInstallationID(v)
		if err != nil {
			digits = strings.TrimSpace(fmt.Sprint(v))
			if digits == "<nil>" {
				digits = ""
			}
		}
		cfg["installation_id"] = digits
	}
	if v, ok := cfg["repos"]; ok {
		cfg["repos"] = appRepoList(v)
	}
}

func (p *githubAppProvider) ValidateConfig(cfg map[string]any) error {
	appID := strings.TrimSpace(fmt.Sprint(cfg["app_id"]))
	if appID == "" || appID == "<nil>" {
		return fmt.Errorf("github_app: app_id is required")
	}
	if _, err := uuid.Parse(appID); err != nil {
		return fmt.Errorf("github_app: app_id must be a UUID")
	}
	if _, err := normalizeInstallationID(cfg["installation_id"]); err != nil {
		return fmt.Errorf("github_app: %w", err)
	}
	repos := appRepoList(cfg["repos"])
	if len(repos) == 0 {
		return fmt.Errorf("github_app: repos must not be empty")
	}
	for _, repo := range repos {
		if !githubAppRepoPattern.MatchString(repo) {
			return fmt.Errorf("github_app: %q is not owner/name", repo)
		}
	}
	return nil
}

func (p *githubAppProvider) ExtractCredentials(map[string]any) (Credentials, error) {
	return Credentials{}, nil
}

func (p *githubAppProvider) ListResources(context.Context, Credentials, map[string]any, string, ConnectorDeps) ([]Resource, bool, error) {
	return nil, false, nil
}

func (p *githubAppProvider) NewConnector(context.Context, Credentials, ConnectorDeps) (Connector, error) {
	return triggerOnlyConnector{}, nil
}

// triggerOnlyConnector refuses sync: the provider is event-driven, not polled.
type triggerOnlyConnector struct{}

var errTriggerOnlySource = fmt.Errorf("github_app is a trigger source; it does not sync documents")

// errTriggerOnlyTest is the connection-test answer. A github_app source holds no
// callable credential of its own: the App key and secret live in kb.source_apps
// and are exercised per delivery, so there is nothing to poll here. Reporting
// success would show a green check for a deleted or revoked installation, which
// is exactly what the test endpoint exists to catch.
var errTriggerOnlyTest = fmt.Errorf("connection testing is not supported for trigger sources; the App credentials are verified on every webhook delivery")

func (triggerOnlyConnector) Enumerate(context.Context, map[string]any) ([]SourceItem, error) {
	return nil, errTriggerOnlySource
}

func (triggerOnlyConnector) Fetch(context.Context, SourceItem) (string, string, error) {
	return "", "", errTriggerOnlySource
}

func (triggerOnlyConnector) TestConnection(context.Context, map[string]any) error {
	return errTriggerOnlyTest
}

func appRepoList(v any) []string {
	var raw []string
	switch t := v.(type) {
	case []string:
		raw = t
	case []any:
		raw = make([]string, 0, len(t))
		for _, item := range t {
			raw = append(raw, fmt.Sprint(item))
		}
	default:
		return nil
	}
	out := make([]string, 0, len(raw))
	seen := map[string]struct{}{}
	for _, repo := range raw {
		repo = strings.TrimSpace(repo)
		if repo == "" {
			continue
		}
		key := strings.ToLower(repo)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, repo)
	}
	return out
}
