package server

import (
	"context"
	"reflect"
	"testing"

	"github.com/uptrace/bun"
	"go.uber.org/fx"
)

// dbMarker stands in for *bun.DB in the ordering test below.
type dbMarker struct{}

// TestStartServerDependsOnBunDB pins the actual wiring the shutdown-ordering
// guarantee rests on: StartServer must declare a *bun.DB parameter so fx
// constructs the database before the server's invoke (and thus registers the
// DB's OnStop before the server's). Removing the parameter would silently
// reintroduce issue #1481 (the pool closing under live handlers) while the
// framework-semantics test above still passed.
func TestStartServerDependsOnBunDB(t *testing.T) {
	ft := reflect.TypeOf(StartServer)
	bunDB := reflect.TypeOf((*bun.DB)(nil))
	for i := 0; i < ft.NumIn(); i++ {
		if ft.In(i) == bunDB {
			return
		}
	}
	t.Fatalf("StartServer must depend on *bun.DB for safe shutdown ordering; params: %v", ft)
}

// TestFxStopOrderIsReverseRegistration pins the fx lifecycle guarantee that
// StartServer relies on for safe shutdown (issue #1481): OnStop hooks run in
// reverse registration order, and registration order follows dependency
// construction order. Because StartServer depends on *bun.DB, its hook is
// registered after the database hooks and therefore runs FIRST — the HTTP
// server drains before the pool closes. If this test fails, the shutdown
// ordering fix in StartServer must be revisited.
func TestFxStopOrderIsReverseRegistration(t *testing.T) {
	var order []string

	app := fx.New(
		fx.NopLogger,
		fx.Provide(func(lc fx.Lifecycle) *dbMarker {
			lc.Append(fx.Hook{
				OnStop: func(context.Context) error {
					order = append(order, "db")
					return nil
				},
			})
			return &dbMarker{}
		}),
		fx.Invoke(func(lc fx.Lifecycle, _ *dbMarker) {
			// _ *dbMarker forces this invoke to run after the provider above,
			// mirroring StartServer's `db *bun.DB` dependency.
			lc.Append(fx.Hook{
				OnStop: func(context.Context) error {
					order = append(order, "server")
					return nil
				},
			})
		}),
	)

	ctx := context.Background()
	if err := app.Start(ctx); err != nil {
		t.Fatalf("app.Start: %v", err)
	}
	if err := app.Stop(ctx); err != nil {
		t.Fatalf("app.Stop: %v", err)
	}

	if want := []string{"server", "db"}; !reflect.DeepEqual(order, want) {
		t.Fatalf("OnStop order = %v, want %v (server must drain before DB closes)", order, want)
	}
}
