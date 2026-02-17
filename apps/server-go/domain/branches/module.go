package branches

import (
	"github.com/labstack/echo/v4"
	"github.com/uptrace/bun"
	"go.uber.org/fx"

	"github.com/emergent-company/emergent/pkg/auth"
)

// Module provides branches dependencies
var Module = fx.Options(
	fx.Provide(newStoreFromDB),
	fx.Provide(NewService),
	fx.Provide(NewHandler),
	fx.Invoke(RegisterBranchesRoutes),
)

// RegisterBranchesRoutes registers branch routes
func RegisterBranchesRoutes(e *echo.Echo, h *Handler, authMiddleware *auth.Middleware) {
	RegisterRoutes(e, h, authMiddleware)
}

// newStoreFromDB creates a branches store with the bun DB (fx constructor)
func newStoreFromDB(db *bun.DB) *Store {
	return NewStore(db)
}
