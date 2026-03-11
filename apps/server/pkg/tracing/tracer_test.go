package tracing_test

import (
	"context"
	"errors"
	"testing"

	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/emergent-company/emergent.memory/pkg/tracing"
)

// setupTestTracer registers an in-memory span recorder as the global tracer
// provider. Returns the recorder and registers a cleanup to restore the
// original provider.
func setupTestTracer(t *testing.T) *tracetest.SpanRecorder {
	t.Helper()
	recorder := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithSpanProcessor(recorder),
	)
	orig := otel.GetTracerProvider()
	otel.SetTracerProvider(tp)
	t.Cleanup(func() { otel.SetTracerProvider(orig) })
	return recorder
}

func TestStartLinked_CreatesRootSpanWithLink(t *testing.T) {
	recorder := setupTestTracer(t)

	// Create a parent span in a "request" context
	parentCtx, parentSpan := tracing.Start(context.Background(), "parent.operation")
	parentSpanCtx := parentSpan.SpanContext()
	parentSpan.End()

	// StartLinked should create a root span linked to the parent
	bgCtx, linkedSpan := tracing.StartLinked(parentCtx, "background.operation")
	linkedSpan.End()

	if bgCtx == nil {
		t.Fatal("StartLinked returned nil context")
	}

	spans := recorder.Ended()
	if len(spans) < 2 {
		t.Fatalf("expected at least 2 spans, got %d", len(spans))
	}

	// Find the linked span (background.operation)
	var bgSpan sdktrace.ReadOnlySpan
	for _, s := range spans {
		if s.Name() == "background.operation" {
			bgSpan = s
			break
		}
	}
	if bgSpan == nil {
		t.Fatal("background.operation span not found")
	}

	// It should have a link to the parent span
	links := bgSpan.Links()
	if len(links) == 0 {
		t.Fatal("expected background span to have at least one link, got none")
	}
	linkTraceID := links[0].SpanContext.TraceID()
	if linkTraceID != parentSpanCtx.TraceID() {
		t.Errorf("link trace ID = %v, want %v", linkTraceID, parentSpanCtx.TraceID())
	}

	// The linked span should be a root span (no valid parent)
	if bgSpan.Parent().IsValid() {
		t.Errorf("background span should be a root span, but has parent %v", bgSpan.Parent())
	}
}

func TestStartLinked_NoParentSpan_CreatesRootSpanWithNoLinks(t *testing.T) {
	recorder := setupTestTracer(t)

	// Call StartLinked with a context that has no active span
	_, linkedSpan := tracing.StartLinked(context.Background(), "background.no_parent")
	linkedSpan.End()

	spans := recorder.Ended()
	if len(spans) == 0 {
		t.Fatal("expected at least 1 span")
	}

	var bgSpan sdktrace.ReadOnlySpan
	for _, s := range spans {
		if s.Name() == "background.no_parent" {
			bgSpan = s
			break
		}
	}
	if bgSpan == nil {
		t.Fatal("background.no_parent span not found")
	}

	// With no valid parent span context, there should be no links
	if len(bgSpan.Links()) != 0 {
		t.Errorf("expected no links when no parent span, got %d", len(bgSpan.Links()))
	}
}

func TestRecordErrorWithType_SetsAttributesAndStatus(t *testing.T) {
	recorder := setupTestTracer(t)

	_, span := tracing.Start(context.Background(), "test.operation")
	testErr := errors.New("something went wrong")
	tracing.RecordErrorWithType(span, testErr)
	span.End()

	spans := recorder.Ended()
	if len(spans) == 0 {
		t.Fatal("expected at least 1 span")
	}

	var s sdktrace.ReadOnlySpan
	for _, sp := range spans {
		if sp.Name() == "test.operation" {
			s = sp
			break
		}
	}
	if s == nil {
		t.Fatal("test.operation span not found")
	}

	// Should have error status
	status := s.Status()
	if status.Code.String() != "Error" {
		t.Errorf("status code = %q, want %q", status.Code.String(), "Error")
	}
	if status.Description != "something went wrong" {
		t.Errorf("status description = %q, want %q", status.Description, "something went wrong")
	}

	// Should have emergent.error.type attribute
	found := false
	for _, attr := range s.Attributes() {
		if string(attr.Key) == "memory.error.type" {
			found = true
			if attr.Value.AsString() == "" {
				t.Error("memory.error.type attribute is empty")
			}
		}
	}
	if !found {
		t.Error("memory.error.type attribute not found on span")
	}

	// Should have a recorded error event
	hasErrorEvent := false
	for _, event := range s.Events() {
		if event.Name == "exception" {
			hasErrorEvent = true
			break
		}
	}
	if !hasErrorEvent {
		t.Error("expected an exception event recorded on the span")
	}
}
