package githubapp

import (
	"context"
	"errors"
	"log/slog"

	"go.uber.org/fx"

	"github.com/emergent-company/emergent.memory/domain/mcp"
	"github.com/emergent-company/emergent.memory/domain/mcpregistry"
	"github.com/emergent-company/emergent.memory/internal/config"
)

// Module provides the GitHub App connection model and the review producer:
// credential storage, installation routing, delivery dedupe, per-head review
// claims, the admin surface, and the session-less webhook ingress.
var Module = fx.Module("githubapp",
	fx.Provide(
		NewRepository,
		provideTokenMinter,
		provideCredentialCipher,
		provideBuiltinToolRefresher,
		NewService,
		NewHandler,
	),
	fx.Invoke(
		registerCredentialGuard,
		RegisterRoutes,
		registerMCPToolHandler,
	),
)

// provideBuiltinToolRefresher maps the registry service onto the narrow
// interface the producer uses to re-seed a project's builtin tools.
func provideBuiltinToolRefresher(svc *mcpregistry.Service) BuiltinToolRefresher {
	return svc
}

func provideTokenMinter(cfg *config.Config) *TokenMinter {
	baseURL := ""
	if cfg != nil {
		baseURL = cfg.GitHubSource.APIBaseURL
	}
	return NewTokenMinter(baseURL, nil)
}

// provideCredentialCipher returns nil when the encryption key is unset; the
// cipher's methods fail closed, so a missing key cannot silently store or read
// plaintext credentials.
func provideCredentialCipher(cfg *config.Config, log *slog.Logger) *CredentialCipher {
	if cfg == nil || cfg.Sandbox.GitHubAppEncryptionKey == "" {
		return nil
	}
	cipher, err := NewCredentialCipher(cfg.Sandbox.GitHubAppEncryptionKey)
	if err != nil {
		log.Error("githubapp: invalid GITHUB_APP_ENCRYPTION_KEY; credential storage disabled", "error", err)
		return nil
	}
	return cipher
}

// registerMCPToolHandler exposes the review tools to the in-process agent
// runtime (githubapp -> agents -> mcp, deferred to fx.Invoke to break the cycle).
func registerMCPToolHandler(mcpSvc *mcp.Service, svc *Service) {
	mcpSvc.RegisterGitHubAppToolHandler(svc)
}

// registerCredentialGuard fails startup when credentials already exist but the
// encryption key is missing: the rows would be unreadable and every integration
// would fail closed at runtime instead.
func registerCredentialGuard(lc fx.Lifecycle, repo *Repository, cfg *config.Config) {
	lc.Append(fx.Hook{
		OnStart: func(ctx context.Context) error {
			if cfg == nil || cfg.Sandbox.GitHubAppEncryptionKey != "" {
				return nil
			}
			n, err := repo.CountApps(ctx)
			if err != nil {
				return err
			}
			if n > 0 {
				return errors.New("githubapp: GITHUB_APP_ENCRYPTION_KEY is required when GitHub App credentials exist")
			}
			return nil
		},
	})
}
