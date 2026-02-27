// Package tracing provides a shared OTel tracer helper for all domain packages.
//
// When no TracerProvider is registered (e.g. in tests or local dev without OTel),
// the global no-op provider is used automatically and all calls are inert with
// zero overhead. Domain packages should call tracing.Start rather than using the
// OTel API directly.
package tracing

import (
	"context"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

const tracerName = "emergent"

// Start creates a new OTel span as a child of the span in ctx, or a root span
// when ctx carries no active span. The caller MUST call span.End() when the
// operation is done (typically via defer span.End()).
//
// Example:
//
//	ctx, span := tracing.Start(ctx, "extraction.document_parsing",
//	    attribute.String("emergent.job.id", job.ID),
//	    attribute.String("emergent.project.id", job.ProjectID),
//	)
//	defer span.End()
func Start(ctx context.Context, spanName string, attrs ...attribute.KeyValue) (context.Context, trace.Span) {
	return otel.Tracer(tracerName).Start(ctx, spanName, trace.WithAttributes(attrs...))
}
