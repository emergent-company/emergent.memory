package server

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"go.uber.org/fx"

	"github.com/emergent-company/emergent/internal/config"
	"github.com/emergent-company/emergent/pkg/apperror"
	"github.com/emergent-company/emergent/pkg/logger"
)

var Module = fx.Module("server",
	fx.Provide(NewEcho),
	fx.Invoke(StartServer),
)

// EchoParams are the dependencies for creating an Echo instance
type EchoParams struct {
	fx.In

	Config     *config.Config
	Log        *slog.Logger
	HTTPLogger *logger.HTTPLogger
}

// NewEcho creates and configures an Echo instance
func NewEcho(p EchoParams) *echo.Echo {
	cfg := p.Config
	log := p.Log
	httpLogger := p.HTTPLogger

	e := echo.New()

	// Configure Echo
	e.Debug = cfg.Debug
	e.HideBanner = true
	e.HidePort = !cfg.Debug

	// Custom error handler matching NestJS format
	e.HTTPErrorHandler = apperror.HTTPErrorHandler(log)

	// Pre-middleware
	e.Pre(middleware.RemoveTrailingSlash())

	// Middleware stack
	e.Use(
		// CORS - AllowOriginFunc returns the requesting origin to support credentials
		middleware.CORSWithConfig(middleware.CORSConfig{
			AllowOriginFunc: func(origin string) (bool, error) {
				// Allow all origins but return the specific origin (not wildcard)
				// This is required when AllowCredentials is true
				return true, nil
			},
			AllowCredentials: true,
			AllowMethods:     []string{http.MethodGet, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete, http.MethodOptions},
			AllowHeaders:     []string{echo.HeaderOrigin, echo.HeaderContentType, echo.HeaderAccept, echo.HeaderAuthorization, echo.HeaderCacheControl, "X-Project-ID", "X-View-As-User-ID"},
		}),

		// Request ID
		middleware.RequestID(),

		// Request logging (skip health endpoints)
		middleware.RequestLoggerWithConfig(middleware.RequestLoggerConfig{
			Skipper: func(c echo.Context) bool {
				path := c.Request().URL.Path
				return path == "/health" || path == "/healthz" || path == "/ready"
			},
			LogURI:       true,
			LogStatus:    true,
			LogLatency:   true,
			LogError:     true,
			LogMethod:    true,
			LogRequestID: true,
			LogValuesFunc: func(c echo.Context, v middleware.RequestLoggerValues) error {
				attrs := []any{
					slog.String("method", v.Method),
					slog.String("uri", v.URI),
					slog.Int("status", v.Status),
					slog.Duration("latency", v.Latency),
					slog.String("request_id", v.RequestID),
				}
				if v.Error != nil {
					attrs = append(attrs, logger.Error(v.Error))
					log.Error("request failed", attrs...)
				} else {
					log.Info("request", attrs...)
				}

				// Also log to HTTP log file
				req := c.Request()
				ip := c.RealIP()
				userAgent := req.UserAgent()
				httpLogger.LogRequest(ip, v.Method, v.URI, v.Status, v.Latency, userAgent, v.RequestID)

				return nil
			},
		}),

		// Recover from panics
		middleware.RecoverWithConfig(middleware.RecoverConfig{
			LogErrorFunc: func(c echo.Context, err error, stack []byte) error {
				log.Error("panic recovered",
					logger.Error(err),
					slog.String("stack", string(stack)),
				)
				return nil
			},
		}),
	)

	return e
}

// StartServer starts the HTTP server with graceful shutdown
func StartServer(lc fx.Lifecycle, e *echo.Echo, cfg *config.Config, log *slog.Logger) {
	log = log.With(logger.Scope("server"))

	server := &http.Server{
		Addr:         fmt.Sprintf("%s:%d", cfg.ServerAddress, cfg.ServerPort),
		ReadTimeout:  cfg.ReadTimeout,
		WriteTimeout: cfg.WriteTimeout,
		IdleTimeout:  cfg.IdleTimeout,
	}

	lc.Append(fx.Hook{
		OnStart: func(ctx context.Context) error {
			log.Info("starting HTTP server",
				slog.String("address", server.Addr),
				slog.String("environment", cfg.Environment),
			)

			// Start server in goroutine
			go func() {
				if err := e.StartServer(server); err != nil && err != http.ErrServerClosed {
					log.Error("server error", logger.Error(err))
				}
			}()

			return nil
		},
		OnStop: func(ctx context.Context) error {
			log.Info("shutting down HTTP server")

			// Create shutdown context with timeout
			shutdownCtx, cancel := context.WithTimeout(ctx, cfg.ShutdownTimeout)
			defer cancel()

			return e.Shutdown(shutdownCtx)
		},
	})
}
