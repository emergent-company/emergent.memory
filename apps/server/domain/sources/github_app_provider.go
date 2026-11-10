package sources

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/google/uuid"
)

// githubAppRepoPattern validates an owner/name allowlist entry.
var githubAppRepoPattern = regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`)

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
				"description": "The org-level source App to use (registered under Organization → Apps).",
				"x-ui-widget": "source-app",
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

func (p *githubAppProvider) NormalizeConfig(cfg map[string]any) {
	if v, ok := cfg["installation_id"]; ok {
		cfg["installation_id"] = strings.TrimSpace(fmt.Sprint(v))
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
	inst := strings.TrimSpace(fmt.Sprint(cfg["installation_id"]))
	if inst == "" || inst == "<nil>" {
		return fmt.Errorf("github_app: installation_id is required")
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

func (triggerOnlyConnector) Enumerate(context.Context, map[string]any) ([]SourceItem, error) {
	return nil, errTriggerOnlySource
}

func (triggerOnlyConnector) Fetch(context.Context, SourceItem) (string, string, error) {
	return "", "", errTriggerOnlySource
}

func (triggerOnlyConnector) TestConnection(context.Context, map[string]any) error {
	return nil
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
