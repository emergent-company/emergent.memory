package oauthas

import (
	"go.uber.org/fx"
)

// Module provides the OAuth 2.1 authorization-server domain: dynamic client
// registration, the consent/authorize surface, PKCE code exchange, and grant
// revocation.
var Module = fx.Module("oauthas",
	fx.Provide(NewRepository),
	fx.Provide(NewService),
	fx.Provide(NewHandler),
	fx.Invoke(RegisterRoutes),
)
