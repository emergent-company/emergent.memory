package projects

import (
	"os"
	"time"

	"go.uber.org/fx"
)

// Module provides the projects domain
var Module = fx.Module("projects",
	fx.Provide(NewRepository),
	fx.Provide(NewService),
	fx.Provide(NewHandler),
	fx.Invoke(RegisterRoutes),
	fx.Invoke(configureDeletionGracePeriod),
)

// configureDeletionGracePeriod wires PROJECT_DELETION_GRACE_PERIOD (a Go
// duration string, e.g. "1h") into the projects service. Invalid/absent values
// keep the service default.
func configureDeletionGracePeriod(svc *Service) {
	if svc == nil {
		return
	}
	if raw := os.Getenv("PROJECT_DELETION_GRACE_PERIOD"); raw != "" {
		if d, err := time.ParseDuration(raw); err == nil {
			svc.ConfigureDeletionGracePeriod(d)
		}
	}
}
