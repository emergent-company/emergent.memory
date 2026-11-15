package market

import (
	"log/slog"

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
// allowlist. It takes the process logger so the in-product outage and deferral
// diagnostics reach the instance's log instead of a discard handler: "usage
// queued, will replay" is only useful if someone can see it.
func NewClientFromConfig(cfg *config.Config, log *slog.Logger) (*Client, error) {
	return NewClient(ClientConfig{
		Issuers:     cfg.Market.TrustedIssuers,
		OriginID:    cfg.Market.OriginID,
		InstanceURL: cfg.AppURL,
		Logger:      log,
	})
}
