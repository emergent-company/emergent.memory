package auth

import (
	"log/slog"

	"go.uber.org/fx"

	"github.com/emergent-company/emergent.memory/internal/config"
)

// managementClientParams holds deps for building the Zitadel management client.
type managementClientParams struct {
	fx.In
	Cfg *config.Config
	Log *slog.Logger
}

// provideZitadelManagementClient builds the management client from config.
// Returns nil (and no error) when ZITADEL_ADMIN_PAT is not set.
func provideZitadelManagementClient(p managementClientParams) *ZitadelManagementClient {
	c := NewZitadelManagementClient(p.Cfg.Zitadel.Domain, p.Cfg.Zitadel.AdminPAT)
	if c == nil {
		p.Log.Info("Zitadel management client disabled (ZITADEL_ADMIN_PAT not set)")
	} else {
		p.Log.Info("Zitadel management client enabled")
	}
	return c
}

// wireManagementClient injects the management client into UserProfileService after construction.
func wireManagementClient(svc *UserProfileService, mgr *ZitadelManagementClient) {
	svc.SetManagementClient(mgr)
}

var Module = fx.Module("auth",
	fx.Provide(
		NewMiddleware,
		NewUserProfileService,
		provideZitadelManagementClient,
	),
	fx.Invoke(wireManagementClient),
)
