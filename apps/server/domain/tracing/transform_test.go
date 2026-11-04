package tracing

import (
	"encoding/json"
	"testing"
)

func TestAttrValuePrefersStringValue(t *testing.T) {
	attrs := []tempoAttr{
		{Key: "service.name", Value: tempoAttrValue{StringValue: "api", IntValue: "7"}},
	}
	if got := attrValue(attrs, "service.name"); got != "api" {
		t.Fatalf("attrValue() = %q, want %q", got, "api")
	}
}

func TestAttrValueReturnsIntValueWhenStringEmpty(t *testing.T) {
	attrs := []tempoAttr{
		{Key: "http.status_code", Value: tempoAttrValue{IntValue: "200"}},
	}
	if got := attrValue(attrs, "http.status_code"); got != "200" {
		t.Fatalf("attrValue() = %q, want %q", got, "200")
	}
}

func TestAttrValueReturnsEmptyWhenKeyMissing(t *testing.T) {
	attrs := []tempoAttr{
		{Key: "service.name", Value: tempoAttrValue{StringValue: "api"}},
	}
	if got := attrValue(attrs, "missing.key"); got != "" {
		t.Fatalf("attrValue() = %q, want %q", got, "")
	}
}

func TestToStructuredTraceDropsMergedSpans(t *testing.T) {
	resp := traceWithSpans(
		mustSpan("span-a", "keep me", "trace-1", "100", "200", nil),
		mustSpan("span-b", "sql query (merged)", "trace-1", "150", "250", nil),
		mustSpan("span-c", "http call (merged) extra", "trace-1", "300", "400", nil),
	)
	out := toStructuredTrace(resp)
	if len(out.Spans) != 1 {
		t.Fatalf("got %d spans, want 1 (merged spans must be dropped)", len(out.Spans))
	}
	if out.Spans[0].SpanID != "span-a" {
		t.Fatalf("remaining span = %q, want %q", out.Spans[0].SpanID, "span-a")
	}
}

func TestToStructuredTraceComputesDurationMs(t *testing.T) {
	resp := traceWithSpans(
		mustSpan("span-a", "op", "trace-1", "1000000000", "2000000000", nil),
	)
	out := toStructuredTrace(resp)
	if got := out.Spans[0].DurationMs; got != 1000.0 {
		t.Fatalf("DurationMs = %v, want %v", got, 1000.0)
	}

	// Sanity check the unit: 1e6 ns == 1 ms.
	resp2 := traceWithSpans(
		mustSpan("span-a", "op", "trace-1", "5000000", "6000000", nil),
	)
	out2 := toStructuredTrace(resp2)
	if got := out2.Spans[0].DurationMs; got != 1.0 {
		t.Fatalf("DurationMs = %v, want %v", got, 1.0)
	}
}

func TestToStructuredTraceRunIDFallsBackToEmergent(t *testing.T) {
	resp := traceWithSpans(
		mustSpan("span-1", "op-a", "trace-1", "100", "200", []tempoAttr{
			{Key: "emergent.agent.run_id", Value: tempoAttrValue{StringValue: "run-emergent"}},
		}),
		mustSpan("span-2", "op-b", "trace-1", "300", "400", []tempoAttr{
			{Key: "memory.agent.run_id", Value: tempoAttrValue{StringValue: "run-memory"}},
		}),
	)
	if got := toStructuredTrace(resp).RunID; got != "run-memory" {
		t.Fatalf("RunID = %q, want %q (memory.agent.run_id preferred)", got, "run-memory")
	}

	// memory key absent anywhere -> fall back to emergent.agent.run_id.
	resp2 := traceWithSpans(
		mustSpan("span-1", "op-a", "trace-1", "100", "200", []tempoAttr{
			{Key: "emergent.agent.run_id", Value: tempoAttrValue{StringValue: "run-emergent"}},
		}),
	)
	if got := toStructuredTrace(resp2).RunID; got != "run-emergent" {
		t.Fatalf("RunID = %q, want %q (fallback to emergent.agent.run_id)", got, "run-emergent")
	}
}

func TestToStructuredTraceExtractsProjectID(t *testing.T) {
	resp := traceWithSpans(
		mustSpan("span-1", "op-a", "trace-1", "100", "200", []tempoAttr{
			{Key: "memory.project.id", Value: tempoAttrValue{StringValue: "proj-1"}},
		}),
	)
	if got := toStructuredTrace(resp).ProjectID; got != "proj-1" {
		t.Fatalf("ProjectID = %q, want %q", got, "proj-1")
	}

	// Fallback to emergent.project.id when memory key is missing.
	resp2 := traceWithSpans(
		mustSpan("span-1", "op-a", "trace-1", "100", "200", []tempoAttr{
			{Key: "emergent.project.id", Value: tempoAttrValue{StringValue: "proj-2"}},
		}),
	)
	if got := toStructuredTrace(resp2).ProjectID; got != "proj-2" {
		t.Fatalf("ProjectID = %q, want %q (fallback to emergent.project.id)", got, "proj-2")
	}
}

func TestToStructuredTraceSortsSpansByStartTime(t *testing.T) {
	resp := traceWithSpans(
		mustSpan("late", "op-late", "trace-1", "9000", "10000", nil),
		mustSpan("early", "op-early", "trace-1", "1000", "2000", nil),
		mustSpan("mid", "op-mid", "trace-1", "5000", "6000", nil),
	)
	out := toStructuredTrace(resp)
	got := []string{out.Spans[0].SpanID, out.Spans[1].SpanID, out.Spans[2].SpanID}
	want := []string{"early", "mid", "late"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("span order = %v, want %v", got, want)
		}
	}
}

func TestToStructuredTracePopulatesSpanFields(t *testing.T) {
	resp := traceWithSpans(
		mustSpan("span-a", "op", "trace-1", "1000", "3000", []tempoAttr{
			{Key: "db.system", Value: tempoAttrValue{StringValue: "postgres"}},
			{Key: "db.rows", Value: tempoAttrValue{IntValue: "42"}},
		}),
	)
	resp.Batches[0].ScopeSpans[0].Spans[0].Status = otlpStatus{
		Code:    json.RawMessage(`"STATUS_CODE_ERROR"`),
		Message: "boom",
	}
	out := toStructuredTrace(resp)
	s := out.Spans[0]
	if s.SpanID != "span-a" || s.ParentSpanID != "parent-span-a" || s.Name != "op" {
		t.Fatalf("span identity fields wrong: %+v", s)
	}
	if s.StatusCode != "STATUS_CODE_ERROR" {
		t.Fatalf("StatusCode = %q, want %q", s.StatusCode, "STATUS_CODE_ERROR")
	}
	if s.StatusMessage != "boom" {
		t.Fatalf("StatusMessage = %q, want %q", s.StatusMessage, "boom")
	}
	if s.Attributes["db.system"] != "postgres" {
		t.Fatalf("Attributes[db.system] = %q, want %q", s.Attributes["db.system"], "postgres")
	}
	if s.Attributes["db.rows"] != "42" {
		t.Fatalf("Attributes[db.rows] = %q, want %q", s.Attributes["db.rows"], "42")
	}
	if out.TraceID != "trace-1" {
		t.Fatalf("TraceID = %q, want %q", out.TraceID, "trace-1")
	}
}

// traceWithSpans wraps the given spans in a single batch/scope so they share a
// trace, mirroring Tempo's OTLP JSON layout.
func traceWithSpans(spans ...otlpSpan) *otlpTraceResponse {
	return &otlpTraceResponse{
		Batches: []otlpBatch{
			{
				ScopeSpans: []otlpScopeSpans{
					{Spans: spans},
				},
			},
		},
	}
}

// mustSpan builds a minimal OTLP span. ParentSpanID is set so identity fields
// are exercised, but merged/dropped spans never assert on it.
func mustSpan(spanID, name, traceID, start, end string, attrs []tempoAttr) otlpSpan {
	return otlpSpan{
		TraceID:           traceID,
		SpanID:            spanID,
		ParentSpanID:      "parent-" + spanID,
		Name:              name,
		Kind:              json.RawMessage(`1`),
		StartTimeUnixNano: start,
		EndTimeUnixNano:   end,
		Attributes:        attrs,
		Status: otlpStatus{
			Code: json.RawMessage(`2`),
		},
	}
}
