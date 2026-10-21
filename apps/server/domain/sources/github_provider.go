package sources

import (
	"context"
	"strings"

	"github.com/emergent-company/emergent.memory/internal/github"
	"github.com/emergent-company/emergent.memory/pkg/apperror"
)

// githubCredentialKeyNames are the config keys that carry GitHub credential
// material. They are write-only: stripped from the persisted config and never
// returned. GitHub OAuth App access tokens are long-lived, so unlike Drive
// there is no refresh token.
var githubCredentialKeyNames = []string{"access_token", "client_id", "client_secret"}

// GitHubTransport is the subset of the GitHub client the connector needs. It
// is an interface so tests can substitute a fake without HTTP.
type GitHubTransport interface {
	GetRepo(ctx context.Context, repo string) (*github.Repo, error)
	ListRepos(ctx context.Context) ([]github.Repo, error)
	ListTree(ctx context.Context, repo, ref string) ([]github.Blob, error)
	FetchBlob(ctx context.Context, repo, sha string) ([]byte, error)
	TestConnection(ctx context.Context, repo, ref string) error
}

// githubProvider implements SourceProvider for the github provider: it syncs
// markdown documentation from a (usually private) repository. Credentials are
// a personal access token or OAuth access token; the API origin is the
// deployment's GITHUB_API_BASE_URL, never a per-source value.
type githubProvider struct {
	newClient func(ctx context.Context, creds github.Credentials) (GitHubTransport, error)
}

// newGitHubProvider builds the provider against an explicit API base URL (empty
// means https://api.github.com). It is what the configured registry wires in.
func newGitHubProvider(baseURL string) *githubProvider {
	return &githubProvider{
		newClient: func(ctx context.Context, creds github.Credentials) (GitHubTransport, error) {
			return github.NewWithConfig(creds, github.Config{BaseURL: baseURL})
		},
	}
}

// NewGitHubProvider builds the GitHub provider with an injectable transport
// constructor. It exists so tests can exercise the real connector against a fake
// (or an httptest server) without live GitHub calls.
func NewGitHubProvider(newClient func(ctx context.Context, creds github.Credentials) (GitHubTransport, error)) SourceProvider {
	return &githubProvider{newClient: newClient}
}

func (p *githubProvider) Catalog() Provider {
	return Provider{
		Type:        ProviderTypeGitHub,
		Name:        "GitHub",
		Description: "Sync markdown documentation from a GitHub repository.",
		SourceType:  SourceTypeRepo,
		Vendor:      "github",
		VendorName:  "GitHub",
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
			"ref": map[string]any{
				"type":        "string",
				"description": "The branch, tag, or commit SHA to sync from. Empty syncs the repository's default branch.",
			},
			"paths": map[string]any{
				"type":        "array",
				"items":       map[string]any{"type": "string"},
				"description": "Optional path globs (docs/**, **/*.md). Empty syncs every documentation file in the repository.",
			},
			"extensions": map[string]any{
				"type":        "array",
				"items":       map[string]any{"type": "string"},
				"description": "Optional documentation file extensions. Default: .md, .markdown, .mdx.",
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

// ListResources lists the repositories the token can see so the connect flow can
// offer a repository picker. Private repositories are included. GitHub ignores
// the name filter and never truncates.
func (p *githubProvider) ListResources(ctx context.Context, creds Credentials, cfg map[string]any, _ string) ([]Resource, bool, error) {
	token, _ := creds["access_token"].(string)
	client, err := p.newClient(ctx, github.Credentials{Token: token})
	if err != nil {
		return nil, false, err
	}
	repos, err := client.ListRepos(ctx)
	if err != nil {
		return nil, false, err
	}
	out := make([]Resource, len(repos))
	for i, repo := range repos {
		out[i] = Resource{ID: repo.FullName, Name: repo.FullName, Type: "repo"}
	}
	return out, false, nil
}

func (p *githubProvider) ValidateConfig(cfg map[string]any) error {
	return validateGitHubConfig(cfg)
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

func (p *githubProvider) NewConnector(ctx context.Context, creds Credentials, _ ConnectorDeps) (Connector, error) {
	token, _ := creds["access_token"].(string)
	client, err := p.newClient(ctx, github.Credentials{Token: token})
	if err != nil {
		return nil, err
	}
	return &githubConnector{client: client}, nil
}
