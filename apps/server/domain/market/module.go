package market

import (
	"go.uber.org/fx"

	"github.com/emergent-company/emergent.memory/internal/config"
)

// Module provides the marketplace install client, provenance store and routes.
var Module = fx.Module("market",
	fx.Provide(NewClientFromConfig),
	fx.Provide(NewRepository),
	fx.Provide(NewService),
	// Expose the concrete service as the handler's InstallService interface.
	fx.Provide(func(svc *Service) InstallService { return svc }),
	fx.Provide(NewHandler),
	fx.Invoke(RegisterRoutes),
)

// NewClientFromConfig builds the client from the operator-configured issuer
// allowlist.
func NewClientFromConfig(cfg *config.Config) (*Client, error) {
	return NewClient(ClientConfig{Issuers: cfg.Market.TrustedIssuers})
}
