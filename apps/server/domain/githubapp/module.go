package githubapp

import (
	"log/slog"

	"go.uber.org/fx"

	"github.com/emergent-company/emergent.memory/internal/config"
)

// Module provides the GitHub App connection model: credential storage,
// installation routing, delivery dedupe, and per-head review claims.
var Module = fx.Module("githubapp",
	fx.Provide(
		NewRepository,
		provideTokenMinter,
		provideCredentialCipher,
	),
)

func provideTokenMinter() *TokenMinter {
	return NewTokenMinter("", nil)
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
