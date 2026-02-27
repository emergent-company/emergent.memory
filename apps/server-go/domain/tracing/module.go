package tracing

import (
	"context"
	"log/slog"

	"github.com/labstack/echo/v4"
	"go.opentelemetry.io/contrib/instrumentation/github.com/labstack/echo/otelecho"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
	"go.opentelemetry.io/otel/trace/noop"
	"go.uber.org/fx"

	"github.com/emergent-company/emergent/internal/config"
)

// Module wires OTel tracing into the fx app.
// OtelConfig is read from config.Config.Otel.
// It installs a TracerProvider (OTLP or no-op) and registers the Echo middleware.
var Module = fx.Module("tracing",
	fx.Provide(NewTracerProvider),
	fx.Invoke(RegisterTracingLifecycle),
	fx.Invoke(RegisterEchoMiddleware),
)

// tracerProviderResult is returned by NewTracerProvider.
// It exposes the SDK provider (nil when disabled) for lifecycle management.
type tracerProviderResult struct {
	fx.Out

	// SDKProvider is non-nil only when OTLP is enabled.
	// Stored so RegisterTracingLifecycle can shut it down cleanly.
	SDKProvider *sdktrace.TracerProvider `name:"otelSDKProvider" optional:"true"`
}

// NewTracerProvider creates and globally registers a TracerProvider.
// When tracing is disabled it installs a no-op provider with zero overhead.
func NewTracerProvider(cfg *config.Config, log *slog.Logger) (tracerProviderResult, error) {
	oc := cfg.Otel

	if !oc.Enabled() {
		log.Info("OTel tracing disabled (OTEL_EXPORTER_OTLP_ENDPOINT not set)")
		otel.SetTracerProvider(noop.NewTracerProvider())
		return tracerProviderResult{}, nil
	}

	log.Info("OTel tracing enabled",
		slog.String("endpoint", oc.ExporterEndpoint),
		slog.String("service", oc.ServiceName),
		slog.Float64("sampling_rate", oc.SamplingRate),
	)

	exp, err := otlptracehttp.New(
		context.Background(),
		otlptracehttp.WithEndpointURL(oc.ExporterEndpoint),
		otlptracehttp.WithInsecure(),
	)
	if err != nil {
		return tracerProviderResult{}, err
	}

	res, err := resource.New(context.Background(),
		resource.WithSchemaURL(semconv.SchemaURL),
		resource.WithAttributes(
			semconv.ServiceName(oc.ServiceName),
		),
		resource.WithFromEnv(),
		resource.WithProcess(),
	)
	if err != nil {
		// Non-fatal — fall back to empty resource
		log.Warn("OTel resource detection failed", slog.String("error", err.Error()))
		res = resource.Empty()
	}

	var sampler sdktrace.Sampler
	if oc.SamplingRate >= 1.0 {
		sampler = sdktrace.AlwaysSample()
	} else {
		sampler = sdktrace.TraceIDRatioBased(oc.SamplingRate)
	}

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exp),
		sdktrace.WithResource(res),
		sdktrace.WithSampler(sampler),
	)

	otel.SetTracerProvider(tp)

	return tracerProviderResult{SDKProvider: tp}, nil
}

// sdkProviderParam lets RegisterTracingLifecycle receive the optional SDK provider.
type sdkProviderParam struct {
	fx.In
	SDKProvider *sdktrace.TracerProvider `name:"otelSDKProvider" optional:"true"`
}

// RegisterTracingLifecycle shuts the SDK provider down gracefully on app stop.
func RegisterTracingLifecycle(lc fx.Lifecycle, p sdkProviderParam, log *slog.Logger) {
	if p.SDKProvider == nil {
		return
	}
	lc.Append(fx.Hook{
		OnStop: func(ctx context.Context) error {
			log.Info("shutting down OTel TracerProvider")
			return p.SDKProvider.Shutdown(ctx)
		},
	})
}

// RegisterEchoMiddleware adds the otelecho middleware to the Echo instance.
// Skips health-check routes to avoid trace noise.
func RegisterEchoMiddleware(e *echo.Echo, cfg *config.Config) {
	if !cfg.Otel.Enabled() {
		return
	}
	e.Use(otelecho.Middleware(
		cfg.Otel.ServiceName,
		otelecho.WithSkipper(func(c echo.Context) bool {
			p := c.Request().URL.Path
			return p == "/health" || p == "/healthz" || p == "/ready"
		}),
	))
}
