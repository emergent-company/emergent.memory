package tracing

import (
	"context"
	"log/slog"
	"net/url"
	"path"
	"strings"

	"github.com/labstack/echo/v4"
	"go.opentelemetry.io/contrib/instrumentation/github.com/labstack/echo/otelecho"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.43.0"
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

	// A host-only value (`tempo:4318`) parses as scheme "tempo" with an empty
	// host, so the exporter cannot use it. Say so at startup instead of leaving
	// the next person to find it in an export-error log line. Schemes are
	// case-insensitive, so compare the parsed, lowercased one: `HTTPS://collector`
	// is valid and used to warn (#2360).
	if u, err := url.Parse(oc.ExporterEndpoint); err != nil || !isHTTPScheme(u.Scheme) {
		log.Warn("OTEL_EXPORTER_OTLP_ENDPOINT has no http(s) scheme; expected a URL such as http://tempo:4318",
			slog.String("endpoint", oc.ExporterEndpoint))
	}

	// No WithInsecure(): WithEndpointURL already derives the scheme, and forcing
	// insecure downgraded an https:// collector to cleartext (#2340).
	exp, err := otlptracehttp.New(
		context.Background(),
		otlptracehttp.WithEndpointURL(signalEndpoint(oc.ExporterEndpoint)),
	)
	if err != nil {
		return tracerProviderResult{}, err
	}

	res, err := newResource(context.Background(), oc.ServiceName)
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

// newResource builds the OTel resource the provider attaches to every span.
//
// The schema URL is deliberately *not* pinned. resource.New merges the detectors
// below, and each reports the semconv version it was built against; pinning a
// different version with resource.WithSchemaURL makes that merge conflict, so
// resource.New returns ErrSchemaURLConflict and a *schemaless* resource
// (SchemaURL() == ""), which the caller logs as "OTel resource detection failed"
// on every healthy start — a false alarm that sent the #2337 investigation
// looking for a tracing fault that was not there (#2339).
//
// The attributes survive the conflict: the merge keeps both sets. Measured on
// otel/sdk v1.46.0 with OTEL_RESOURCE_ATTRIBUTES set, the pinned and unpinned
// resources carry the same attributes; the conflicted merge degrades
// SchemaURL() to empty, and that plus the error is all the pin costs. So what the
// pin costs is the false alarm and the schema annotation, not the resource data.
func newResource(ctx context.Context, serviceName string) (*resource.Resource, error) {
	return resource.New(ctx,
		resource.WithAttributes(semconv.ServiceName(serviceName)),
		resource.WithFromEnv(),
		resource.WithProcess(),
	)
}

// isHTTPScheme reports whether s is http or https, case-insensitively.
func isHTTPScheme(s string) bool {
	switch strings.ToLower(s) {
	case "http", "https":
		return true
	default:
		return false
	}
}

// signalEndpoint resolves the URL the OTLP/HTTP exporter POSTs spans to.
//
// OTEL_EXPORTER_OTLP_ENDPOINT is a *base* URL — our compose files, deploy
// heredoc, ansible group_vars and preview env all set http://tempo:4318 — but
// otlptracehttp.WithEndpointURL sends to the URL exactly as given and does not
// append the signal path. A bare base URL therefore exported to "/" and Tempo
// answered 404 ("traces export: failed to send to http://tempo:4318/: 404 Not
// Found"), so no span ever landed. Append /v1/traces when the endpoint carries
// no path of its own, and join an explicit prefix with the signal path the way
// the OTLP spec does (path.Join): http://host/otlp exports to
// /otlp/v1/traces, not to /otlp — a prefix left "untouched" was the same 404
// class this function exists to fix (#2340). A path that already ends in
// /v1/traces is left alone, so the full signal URL the SDK documents is not
// doubled.
func signalEndpoint(endpoint string) string {
	u, err := url.Parse(endpoint)
	if err != nil || u.Host == "" {
		return endpoint
	}
	if strings.HasSuffix(strings.TrimSuffix(u.Path, "/"), "/v1/traces") {
		u.Path = strings.TrimSuffix(u.Path, "/")
	} else {
		u.Path = path.Join(u.Path, "/v1/traces")
	}
	return u.String()
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
