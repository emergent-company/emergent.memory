package sources

import (
	"context"
	"strings"

	"github.com/emergent-company/emergent.memory/pkg/apperror"
)

// githubCredentialKeyNames are the config keys that carry GitHub credential
// material. They are write-only: stripped from the persisted config and never
// returned. GitHub OAuth App access tokens are long-lived, so unlike Drive
// there is no refresh token.
var githubCredentialKeyNames = []string{"access_token", "client_id", "client_secret"}

// githubProvider implements SourceProvider for the github provider. It is a
// readiness stub in this iteration: the catalog, config schema, credential
// handling, and OAuth flow are wired, while resource listing and the connector
// (repo → markdown/docs enumeration + sync) ship in a follow-up change (§7.3).
type githubProvider struct{}

func newGitHubProvider() *githubProvider {
	return &githubProvider{}
}

func (p *githubProvider) Catalog() Provider {
	return Provider{
		Type:        ProviderTypeGitHub,
		Name:        "GitHub",
		Description: "Sync documentation from a GitHub repository.",
		SourceType:  SourceTypeRepo,
		Available:   true,
	}
}

func (p *githubProvider) ConfigSchema() ProviderSchema {
	return ProviderSchema{
		Type: ProviderTypeGitHub,
		Properties: map[string]any{
			"repo": map[string]any{
				"type":        "string",
				"description": "The GitHub repository to sync from (owner/name).",
			},
			"paths": map[string]any{
				"type":  "array",
				"items": map[string]any{"type": "string"},
			},
			"ref": map[string]any{
				"type":        "string",
				"description": "The branch, tag, or commit SHA to sync from.",
			},
		},
		Required: []string{"repo"},
	}
}

func (p *githubProvider) CredentialFields() []ProviderField {
	return []ProviderField{
		{Key: "access_token", Label: "Personal access token", Type: "string", WriteOnly: true, Required: true},
	}
}

func (p *githubProvider) AuthMethods() []AuthMethod {
	return []AuthMethod{AuthMethodOAuth, AuthMethodCredentials}
}

func (p *githubProvider) OAuth() (OAuthStrategy, bool) {
	return newGitHubOAuthStrategy(), true
}

// CredentialKeys returns the write-only GitHub credential keys.
func (p *githubProvider) CredentialKeys() []string { return githubCredentialKeyNames }

// ListResources is a documented readiness stub: GitHub resource listing (repo
// selection) is not yet implemented.
func (p *githubProvider) ListResources(ctx context.Context, creds Credentials, cfg map[string]any) ([]Resource, error) {
	return nil, apperror.NewValidation("github: resource listing is not yet implemented")
}

func (p *githubProvider) ValidateConfig(cfg map[string]any) error {
	if repo, _ := cfg["repo"].(string); strings.TrimSpace(repo) == "" {
		return apperror.NewValidation("config.repo is required")
	}
	return nil
}

func (p *githubProvider) ExtractCredentials(cfg map[string]any) (Credentials, error) {
	creds := Credentials{}
	for _, k := range githubCredentialKeyNames {
		if v, ok := cfg[k]; ok {
			creds[k] = v
		}
	}
	if token, _ := creds["access_token"].(string); strings.TrimSpace(token) == "" {
		return nil, apperror.NewValidation("config.access_token is required")
	}
	return creds, nil
}

func (p *githubProvider) NewConnector(ctx context.Context, creds Credentials, deps ConnectorDeps) (Connector, error) {
	return &githubConnector{}, nil
}
