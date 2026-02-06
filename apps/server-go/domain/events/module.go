package events

import (
	"context"
	"log/slog"

	"github.com/labstack/echo/v4"
	"go.uber.org/fx"

	"github.com/emergent/emergent-core/pkg/auth"
)

// Module provides the events domain
var Module = fx.Module("events",
	fx.Provide(NewService),
	fx.Provide(NewHandler),
	fx.Invoke(RegisterRoutes),
	fx.Invoke(RegisterLifecycle),
)

// RouteParams are the dependencies for registering routes
type RouteParams struct {
	fx.In

	Echo           *echo.Echo
	Handler        *Handler
	AuthMiddleware *auth.Middleware
}

// RegisterRoutes registers the events routes
func RegisterRoutes(p RouteParams) {
	RegisterRoutesManual(p.Echo, p.Handler, p.AuthMiddleware)
}

// RegisterRoutesManual registers the events routes without fx
func RegisterRoutesManual(e *echo.Echo, h *Handler, authMiddleware *auth.Middleware) {
	events := e.Group("/api/events")
	events.Use(authMiddleware.RequireAuth())

	events.GET("/stream", h.HandleStream)
	events.GET("/connections/count", h.HandleConnectionsCount)
}

// LifecycleParams are the dependencies for lifecycle hooks
type LifecycleParams struct {
	fx.In

	LC      fx.Lifecycle
	Handler *Handler
	Log     *slog.Logger
}

// RegisterLifecycle registers lifecycle hooks for cleanup
func RegisterLifecycle(p LifecycleParams) {
	p.LC.Append(fx.Hook{
		OnStop: func(ctx context.Context) error {
			p.Log.Info("stopping events handler")
			p.Handler.Stop()
			return nil
		},
	})
}
