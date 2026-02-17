package githubapp

import (
	"log/slog"

	"github.com/labstack/echo/v4"
	"github.com/uptrace/bun"
	"go.uber.org/fx"

	"github.com/emergent/emergent-core/internal/config"
	"github.com/emergent/emergent-core/pkg/auth"
)

// Module provides GitHub App integration dependencies.
var Module = fx.Module("githubapp",
	fx.Provide(newStore),
	fx.Provide(newCrypto),
	fx.Provide(newTokenService),
	fx.Provide(newService),
	fx.Provide(NewHandler),
	fx.Invoke(registerRoutes),
)

// newStore creates a GitHub App store from the bun DB.
func newStore(db *bun.DB) *Store {
	return NewStore(db)
}

// newCrypto creates the encryption service from centralized config.
func newCrypto(cfg *config.Config, log *slog.Logger) *Crypto {
	key := cfg.Workspace.GitHubAppEncryptionKey
	crypto, err := NewCrypto(key)
	if err != nil {
		log.Warn("GitHub App encryption key not configured or invalid",
			"error", err,
			"hint", "Set GITHUB_APP_ENCRYPTION_KEY to a 64-character hex string (32 bytes) to enable GitHub App integration",
		)
		// Return unconfigured crypto — will error on encrypt/decrypt operations
		crypto, _ = NewCrypto("")
	}
	if !crypto.IsConfigured() {
		log.Info("GitHub App encryption not configured — GitHub integration disabled until GITHUB_APP_ENCRYPTION_KEY is set")
	}
	return crypto
}

// newTokenService creates the token generation service.
func newTokenService(store *Store, crypto *Crypto, log *slog.Logger) *TokenService {
	return NewTokenService(store, crypto, log)
}

// newService creates the GitHub App service.
func newService(store *Store, crypto *Crypto, tokenService *TokenService, log *slog.Logger) *Service {
	return NewService(store, crypto, tokenService, log)
}

// registerRoutes registers GitHub App HTTP routes.
func registerRoutes(e *echo.Echo, h *Handler, authMiddleware *auth.Middleware) {
	RegisterRoutes(e, h, authMiddleware)
}
