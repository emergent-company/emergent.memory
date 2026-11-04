package apitoken

import (
	"github.com/labstack/echo/v4"

	"github.com/emergent-company/emergent.memory/pkg/auth"
)

// RegisterRoutes registers API token routes
func RegisterRoutes(e *echo.Echo, h *Handler, authMiddleware *auth.Middleware) {
	// Project-scoped token routes. The CRUD surface (List/Get/UpdateScopes/
	// Revoke/Regenerate) runs the same authorization pair as the mint routes
	// fixed in #873: RequireProjectTokenScope binds an emt_* token to its
	// project, then RequireProjectMember asserts session/account-token
	// membership in the project's owning org (issue #878). Like the mint routes,
	// the project:read scope guard is omitted — membership is the real gate, and
	// a member (or a token bound to the addressed project) should be able to
	// manage their own project's tokens without a bespoke scope.
	g := e.Group("/api/projects/:projectId/tokens")
	g.Use(authMiddleware.RequireAuth())
	g.Use(authMiddleware.RequireProjectTokenScope())
	g.Use(authMiddleware.RequireProjectMember())

	// Creating a token requires auth, token↔project binding, and project
	// membership (issue #870). The project:read scope guard is intentionally
	// omitted here — a project_admin authenticated via a token without
	// project:read should still be able to bootstrap new tokens for their
	// project. The pair runs in canonical order: RequireProjectTokenScope binds
	// an emt_* token to its project, RequireProjectMember asserts session
	// membership in the project's owning org.
	e.POST("/api/projects/:projectId/tokens", h.Create,
		authMiddleware.RequireAuth(),
		authMiddleware.RequireProjectTokenScope(),
		authMiddleware.RequireProjectMember())

	// Scoped per-device credential mint (see CreateDeviceToken). The scope set is
	// hardcoded server-side; the route enforces token↔project binding and
	// project membership like the token-mint route it sits beside (issue #870).
	e.POST("/api/projects/:projectId/device-tokens", h.CreateDeviceToken,
		authMiddleware.RequireAuth(),
		authMiddleware.RequireProjectTokenScope(),
		authMiddleware.RequireProjectMember())

	// Scoped webhook trigger credential mint (see CreateWebhookTriggerToken). The
	// scope set is hardcoded server-side; the route enforces token↔project
	// binding and project membership like the device-token mint it mirrors
	// (issue #870), so a non-member cannot mint a credential for a project UUID
	// they know or guess.
	e.POST("/api/projects/:projectId/webhook-trigger-tokens", h.CreateWebhookTriggerToken,
		authMiddleware.RequireAuth(),
		authMiddleware.RequireProjectTokenScope(),
		authMiddleware.RequireProjectMember())

	g.GET("", h.List)
	g.GET("/:tokenId", h.Get)
	g.PATCH("/:tokenId", h.UpdateScopes)
	g.DELETE("/:tokenId", h.Revoke)
	g.POST("/:tokenId/regenerate", h.RegenerateToken)

	// Account-level token routes (not bound to a project)
	ag := e.Group("/api/tokens")
	ag.Use(authMiddleware.RequireAuth())

	ag.POST("", h.CreateAccountToken)
	ag.GET("", h.ListAccountTokens)
	ag.GET("/:tokenId", h.GetAccountToken)
	ag.PATCH("/:tokenId", h.UpdateAccountTokenScopes)
	ag.DELETE("/:tokenId", h.RevokeAccountToken)
	ag.POST("/:tokenId/regenerate", h.RegenerateAccountToken)
}
