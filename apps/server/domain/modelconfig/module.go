package modelconfig

import (
	"log/slog"

	"github.com/labstack/echo/v4"
	"github.com/uptrace/bun"
	"go.uber.org/fx"

	"github.com/emergent-company/emergent.memory/domain/agents"
	"github.com/emergent-company/emergent.memory/domain/provider"
	"github.com/emergent-company/emergent.memory/pkg/adk"
	"github.com/emergent-company/emergent.memory/pkg/auth"
	"github.com/emergent-company/emergent.memory/pkg/embeddings"
)

// Module provides the modelconfig domain as an fx module.
var Module = fx.Module("modelconfig",
	fx.Provide(
		provideStore,
		provideService,
		provideADKModelResolverAdapter,
		provideEmbeddingResolverAdapter,
		NewHandler,
	),
	fx.Invoke(
		RegisterRoutes,
		wireAgentsModelResolver,
	),
)

func provideStore(db bun.IDB, log *slog.Logger) *Store {
	return NewStore(db, log)
}

func provideService(store *Store, log *slog.Logger) *Service {
	return NewService(store, log)
}

func provideADKModelResolverAdapter(svc *Service) adk.ModelResolver {
	return NewADKModelResolverAdapter(svc)
}

// provideEmbeddingResolverAdapter provides the embeddings.EmbeddingResolver backed
// by modelconfig (explicit project model selection) + provider credentials.
// This replaces the old provider.EmbeddingCredentialAdapter.
func provideEmbeddingResolverAdapter(svc *Service, credsvc *provider.CredentialService) embeddings.EmbeddingResolver {
	return NewEmbeddingResolverAdapter(svc, credsvc)
}

// wireAgentsModelResolver injects the model resolver into the agents handler
// so agent definition GET responses include an effectiveModel field.
func wireAgentsModelResolver(agentsHandler *agents.Handler, mr adk.ModelResolver) {
	agentsHandler.WithModelResolver(mr)
}

// RegisterRoutes wires model config routes into Echo.
//
//	GET    /api/v1/projects/:projectId/model-config           — get project model config
//	PUT    /api/v1/projects/:projectId/model-config           — set project model config
//	DELETE /api/v1/projects/:projectId/model-config           — clear project model config
//	GET    /api/v1/projects/:projectId/model-config/effective — get resolved effective models
func RegisterRoutes(e *echo.Echo, h *Handler, authMiddleware *auth.Middleware) {
	api := e.Group("/api/v1")
	api.Use(authMiddleware.RequireAuth())

	// Project model config
	proj := api.Group("/projects/:projectId/model-config")
	proj.GET("", h.GetProjectModelConfig)
	proj.PUT("", h.UpsertProjectModelConfig)
	proj.DELETE("", h.DeleteProjectModelConfig)
	proj.GET("/effective", h.GetEffectiveModelConfig)
}
