package extraction

import (
	"context"

	"go.uber.org/fx"
)

// Worker is implemented by background workers that can be started and stopped
// with a context. Workers satisfying this interface can be registered with
// RegisterWorkerLifecycle instead of writing an inline lc.Append(fx.Hook{...}).
//
// Note: DocumentParsingWorker and ObjectExtractionWorker have different signatures
// and are NOT covered by this interface — their lifecycle hooks remain explicit.
type Worker interface {
	Start(ctx context.Context) error
	Stop(ctx context.Context) error
}

// RegisterWorkerLifecycle wires a Worker into the fx application lifecycle.
// It calls Start with context.Background() (not the fx lifecycle context, which
// has a 15-second timeout unsuitable for long-running background workers).
func RegisterWorkerLifecycle(lc fx.Lifecycle, w Worker) {
	lc.Append(fx.Hook{
		OnStart: func(ctx context.Context) error {
			// Use context.Background(): the fx lifecycle context has a 15s timeout
			// which would surface as spurious errors in long-running workers.
			return w.Start(context.Background())
		},
		OnStop: func(ctx context.Context) error {
			return w.Stop(ctx)
		},
	})
}
