// Package tracing provides a shared OTel tracer helper for all domain packages.
//
// When no TracerProvider is registered (e.g. in tests or local dev without OTel),
// the global no-op provider is used automatically and all calls are inert with
// zero overhead. Domain packages should call tracing.Start rather than using the
// OTel API directly.
package tracing

import (
	"context"
	"fmt"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
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
//	    attribute.String("memory.job.id", job.ID),
//	    attribute.String("memory.project.id", job.ProjectID),
//	)
//	defer span.End()
func Start(ctx context.Context, spanName string, attrs ...attribute.KeyValue) (context.Context, trace.Span) {
	return otel.Tracer(tracerName).Start(ctx, spanName, trace.WithAttributes(attrs...))
}

// StartLinked creates a new root span (no parent) that carries a link to the
// span active in ctx, if any. Use this for background operations that are
// causally related to a request span but should not be nested inside it.
func StartLinked(ctx context.Context, spanName string, attrs ...attribute.KeyValue) (context.Context, trace.Span) {
	var opts []trace.SpanStartOption
	opts = append(opts, trace.WithAttributes(attrs...))
	if sc := trace.SpanFromContext(ctx).SpanContext(); sc.IsValid() {
		opts = append(opts, trace.WithLinks(trace.Link{SpanContext: sc}))
	}
	return otel.Tracer(tracerName).Start(context.Background(), spanName, opts...)
}

// RecordErrorWithType records err on span, sets the span status to Error, and
// attaches a memory.error.type attribute with the Go type name of the error.
func RecordErrorWithType(span trace.Span, err error) {
	if err == nil {
		return
	}
	span.RecordError(err)
	span.SetStatus(codes.Error, err.Error())
	span.SetAttributes(attribute.String("memory.error.type", fmt.Sprintf("%T", err)))
}
