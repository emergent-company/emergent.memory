package tracing

import (
	"encoding/json"
	"sort"
	"strconv"
	"strings"
)

// otlpTraceResponse mirrors the OTLP JSON shape returned by Tempo's
// /api/traces/:id endpoint (protobuf JSON marshaled).
type otlpTraceResponse struct {
	Batches []otlpBatch `json:"batches"`
}
type otlpBatch struct {
	Resource   otlpResource     `json:"resource"`
	ScopeSpans []otlpScopeSpans `json:"scopeSpans"`
}
type otlpResource struct {
	Attributes []tempoAttr `json:"attributes"`
}
type otlpScopeSpans struct {
	Spans []otlpSpan `json:"spans"`
}
type otlpSpan struct {
	TraceID           string          `json:"traceId"`
	SpanID            string          `json:"spanId"`
	ParentSpanID      string          `json:"parentSpanId"`
	Name              string          `json:"name"`
	Kind              json.RawMessage `json:"kind"`
	StartTimeUnixNano string          `json:"startTimeUnixNano"`
	EndTimeUnixNano   string          `json:"endTimeUnixNano"`
	Attributes        []tempoAttr     `json:"attributes"`
	Status            otlpStatus      `json:"status"`
}
type otlpStatus struct {
	Code    json.RawMessage `json:"code"`
	Message string          `json:"message"`
}
type tempoAttr struct {
	Key   string         `json:"key"`
	Value tempoAttrValue `json:"value"`
}
type tempoAttrValue struct {
	StringValue string `json:"stringValue"`
	IntValue    string `json:"intValue"`
}

type structuredSpan struct {
	SpanID            string            `json:"spanId"`
	ParentSpanID      string            `json:"parentSpanId"`
	Name              string            `json:"name"`
	StartTimeUnixNano string            `json:"startTimeUnixNano"`
	EndTimeUnixNano   string            `json:"endTimeUnixNano"`
	DurationMs        float64           `json:"durationMs"`
	StatusCode        string            `json:"statusCode"`
	StatusMessage     string            `json:"statusMessage"`
	Attributes        map[string]string `json:"attributes"`
}
type structuredTrace struct {
	TraceID   string           `json:"traceId"`
	Spans     []structuredSpan `json:"spans"`
	RunID     string           `json:"runId,omitempty"`
	ProjectID string           `json:"projectId,omitempty"`
}

// attrValue returns the value of the attribute with the given key, preferring
// StringValue over IntValue. Returns "" when the key is not present.
func attrValue(attrs []tempoAttr, key string) string {
	for _, a := range attrs {
		if a.Key == key {
			if a.Value.StringValue != "" {
				return a.Value.StringValue
			}
			return a.Value.IntValue
		}
	}
	return ""
}

// toStructuredTrace flattens an OTLP trace response into a normalized,
// time-ordered span list with merged spans removed.
func toStructuredTrace(resp *otlpTraceResponse) *structuredTrace {
	var spans []otlpSpan
	for _, b := range resp.Batches {
		for _, ss := range b.ScopeSpans {
			spans = append(spans, ss.Spans...)
		}
	}

	// Drop spans produced by span merging.
	filtered := spans[:0]
	for _, s := range spans {
		if !strings.Contains(s.Name, "(merged)") {
			filtered = append(filtered, s)
		}
	}
	spans = filtered

	// Order spans by start time so the flattened list reads top-down.
	sort.Slice(spans, func(i, j int) bool {
		return parseNanos(spans[i].StartTimeUnixNano) < parseNanos(spans[j].StartTimeUnixNano)
	})

	st := &structuredTrace{Spans: make([]structuredSpan, 0, len(spans))}
	for _, s := range spans {
		startNs := parseNanos(s.StartTimeUnixNano)
		endNs := parseNanos(s.EndTimeUnixNano)

		attrs := make(map[string]string, len(s.Attributes))
		for _, a := range s.Attributes {
			if a.Value.StringValue != "" {
				attrs[a.Key] = a.Value.StringValue
			} else {
				attrs[a.Key] = a.Value.IntValue
			}
		}

		st.Spans = append(st.Spans, structuredSpan{
			SpanID:            s.SpanID,
			ParentSpanID:      s.ParentSpanID,
			Name:              s.Name,
			StartTimeUnixNano: s.StartTimeUnixNano,
			EndTimeUnixNano:   s.EndTimeUnixNano,
			DurationMs:        float64(endNs-startNs) / 1e6,
			StatusCode:        strings.Trim(string(s.Status.Code), `"`),
			StatusMessage:     s.Status.Message,
			Attributes:        attrs,
		})

		if st.TraceID == "" && s.TraceID != "" {
			st.TraceID = s.TraceID
		}
	}

	// Fall back across the whole trace: prefer the memory.* attribute key, then
	// the emergent.* key, scanning spans in order for each.
	st.RunID = firstAttrValue(spans, "memory.agent.run_id")
	if st.RunID == "" {
		st.RunID = firstAttrValue(spans, "emergent.agent.run_id")
	}
	st.ProjectID = firstAttrValue(spans, "memory.project.id")
	if st.ProjectID == "" {
		st.ProjectID = firstAttrValue(spans, "emergent.project.id")
	}

	return st
}

// firstAttrValue returns the first non-empty value for key found by scanning
// the spans in order, or "" if none.
func firstAttrValue(spans []otlpSpan, key string) string {
	for _, s := range spans {
		if v := attrValue(s.Attributes, key); v != "" {
			return v
		}
	}
	return ""
}

// parseNanos parses a unix-nano timestamp, falling back to 0 on error.
func parseNanos(s string) int64 {
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0
	}
	return n
}
