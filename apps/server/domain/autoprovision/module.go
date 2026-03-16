package autoprovision

import (
	"go.uber.org/fx"

	"github.com/emergent-company/emergent.memory/pkg/auth"
)

// Module provides the auto-provisioning service for new user signup.
var Module = fx.Module("autoprovision",
	fx.Provide(
		fx.Annotate(
			NewService,
			fx.As(new(auth.AutoProvisionService)),
		),
	),
)
