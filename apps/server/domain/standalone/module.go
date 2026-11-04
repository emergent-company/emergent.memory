package standalone

import (
	"context"

	"go.uber.org/fx"
)

var Module = fx.Module("standalone",
	fx.Provide(NewBootstrapService),
	fx.Invoke(func(bs *BootstrapService, lc fx.Lifecycle) {
		lc.Append(fx.Hook{
			OnStart: func(ctx context.Context) error {
				return bs.Initialize(ctx)
			},
		})
	}),
)
