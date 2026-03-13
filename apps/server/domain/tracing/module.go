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

	"github.com/emergent-company/emergent.memory/internal/config"
	pkgtracing "github.com/emergent-company/emergent.memory/pkg/tracing"
)

// Module wires OTel tracing into the fx app.
// OtelConfig is read from config.Config.Otel.
// It installs a TracerProvider (OTLP or no-op) and registers the Echo middleware.
var Module = fx.Module("tracing",
	fx.Provide(NewTracerProvider),
	fx.Provide(NewHandler),
	fx.Invoke(RegisterTracingLifecycle),
	fx.Invoke(RegisterEchoMiddleware),
	fx.Invoke(RegisterRoutes),
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
		// Non-fatal — partial resource may still be returned despite the error
		// (e.g. conflicting schema URLs from detectors). Use it if non-nil so
		// that service.name is preserved; fall back to a minimal resource only
		// when nothing was returned at all.
		log.Warn("OTel resource detection failed", slog.String("error", err.Error()))
		if res == nil {
			res, _ = resource.New(context.Background(),
				resource.WithAttributes(semconv.ServiceName(oc.ServiceName)),
			)
		}
	}

	var sampler sdktrace.Sampler
	if oc.SamplingRate >= 1.0 {
		sampler = sdktrace.AlwaysSample()
	} else {
		sampler = sdktrace.TraceIDRatioBased(oc.SamplingRate)
	}

	// Wrap the batcher in an AttrRewriteProcessor so that third-party attribute
	// keys emitted by the Google ADK (gcp.vertex.agent.*, gen_ai.*) are renamed
	// to the memory.llm.* namespace before export.
	rewritingBatcher := pkgtracing.NewAttrRewriteProcessor(sdktrace.NewBatchSpanProcessor(exp))

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithSpanProcessor(rewritingBatcher),
		sdktrace.WithResource(res),
		sdktrace.WithSampler(sampler),
	)

	otel.SetTracerProvider(tp)

	// NOTE: we intentionally do NOT call adktelemetry.RegisterSpanProcessor here.
	//
	// The Google ADK emits every span twice: once via its own local TracerProvider
	// (populated via RegisterSpanProcessor) and once via otel.GetTracerProvider()
	// (the global provider). Registering a processor on both paths causes every
	// ADK span to be exported twice with distinct spanIDs, polluting Tempo with
	// 100% duplicate spans.
	//
	// Since we already set the global provider above (otel.SetTracerProvider(tp)),
	// and that provider wraps the exporter in an AttrRewriteProcessor, ADK spans
	// are correctly rewritten and exported through the global path alone.

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
