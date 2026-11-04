package agents

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"
)

// ── Tempo OTLP JSON types (minimal subset for token aggregation) ─────────────

type tempoAttribute struct {
	Key   string `json:"key"`
	Value struct {
		StringValue string `json:"stringValue"`
		IntValue    string `json:"intValue"`
	} `json:"value"`
}

type tempoSpan struct {
	SpanID            string           `json:"spanId"`
	ParentSpanID      string           `json:"parentSpanId"`
	Name              string           `json:"name"`
	StartTimeUnixNano string           `json:"startTimeUnixNano"`
	EndTimeUnixNano   string           `json:"endTimeUnixNano"`
	Attributes        []tempoAttribute `json:"attributes"`
}

type tempoScopeSpans struct {
	Spans []tempoSpan `json:"spans"`
}

type tempoBatch struct {
	ScopeSpans []tempoScopeSpans `json:"scopeSpans"`
}

type tempoTraceResponse struct {
	Batches []tempoBatch `json:"batches"`
}

// ── Trace fetching ────────────────────────────────────────────────────────────

// RunTraceSpan is a flattened OTLP span from a run's Tempo trace. The tree is
// reconstructible client-side via ParentSpanID (empty for the root span).
type RunTraceSpan struct {
	SpanID        string `json:"spanId"`
	ParentSpanID  string `json:"parentSpanId"`
	Name          string `json:"name"`
	StartUnixNano int64  `json:"startUnixNano"`
	EndUnixNano   int64  `json:"endUnixNano"`
	InputTokens   int64  `json:"inputTokens"`
	OutputTokens  int64  `json:"outputTokens"`
	Model         string `json:"model"`
}

// fetchTempoTrace performs a single GET /api/traces/{traceID} against Tempo.
// Returns nil, nil when tempoBaseURL/traceID is empty, Tempo is unreachable,
// the response is non-200, or the body is not decodable — callers degrade
// gracefully instead of failing the enclosing API call.
func fetchTempoTrace(ctx context.Context, tempoBaseURL, traceID string) (*tempoTraceResponse, error) {
	if tempoBaseURL == "" || traceID == "" {
		return nil, nil
	}

	url := tempoBaseURL + "/api/traces/" + traceID

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("build tempo request: %w", err)
	}

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		// Tempo unreachable — degrade gracefully, don't fail the API call.
		return nil, nil
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, nil
	}

	var trace tempoTraceResponse
	if err := json.NewDecoder(resp.Body).Decode(&trace); err != nil {
		return nil, nil
	}
	return &trace, nil
}

// GetTraceSpans fetches a trace from Tempo by traceID ONCE and returns both the
// flattened span list (every span in the trace, tree-shaped via parentSpanId)
// and the aggregated RunTokenUsage (from call_llm spans). Returns nil, nil, nil
// when tempoBaseURL/traceID is empty, Tempo is unreachable, or the response is
// non-200.
func GetTraceSpans(ctx context.Context, tempoBaseURL, traceID string) ([]RunTraceSpan, *RunTokenUsage, error) {
	trace, err := fetchTempoTrace(ctx, tempoBaseURL, traceID)
	if trace == nil {
		return nil, nil, err
	}
	return flattenTraceSpans(trace), aggregateTokenUsage(trace), nil
}

// flattenTraceSpans converts every span in the trace into a flat RunTraceSpan
// list, preserving the order Tempo served them in. startTimeUnixNano/
// endTimeUnixNano come from OTLP as decimal strings; unparseable or absent
// timings become 0. inputTokens/outputTokens come from the
// memory.llm.usage.input_tokens / memory.llm.usage.output_tokens attributes
// (intValue is a string in OTLP; 0 if absent). model comes from
// memory.llm.request.model (empty if absent).
func flattenTraceSpans(trace *tempoTraceResponse) []RunTraceSpan {
	var out []RunTraceSpan
	for _, batch := range trace.Batches {
		for _, ss := range batch.ScopeSpans {
			for _, span := range ss.Spans {
				out = append(out, RunTraceSpan{
					SpanID:        span.SpanID,
					ParentSpanID:  span.ParentSpanID,
					Name:          span.Name,
					StartUnixNano: tempoUnixNano(span.StartTimeUnixNano),
					EndUnixNano:   tempoUnixNano(span.EndTimeUnixNano),
					InputTokens:   tempoAttrInt(span.Attributes, "memory.llm.usage.input_tokens"),
					OutputTokens:  tempoAttrInt(span.Attributes, "memory.llm.usage.output_tokens"),
					Model:         tempoAttrStr(span.Attributes, "memory.llm.request.model"),
				})
			}
		}
	}
	return out
}

// aggregateTokenUsage finds all call_llm spans in a fetched trace and aggregates
// input/output token counts into a RunTokenUsage. Returns nil when the trace has
// no call_llm spans with token data.
func aggregateTokenUsage(trace *tempoTraceResponse) *RunTokenUsage {
	var totalInput, totalOutput int64
	// Track model occurrence counts to pick the dominant model.
	modelCounts := map[string]int{}

	for _, batch := range trace.Batches {
		for _, ss := range batch.ScopeSpans {
			for _, span := range ss.Spans {
				if span.Name != "call_llm" {
					continue
				}
				totalInput += tempoAttrInt(span.Attributes, "memory.llm.response.input_tokens")
				totalOutput += tempoAttrInt(span.Attributes, "memory.llm.response.output_tokens")
				if m := tempoAttrStr(span.Attributes, "memory.llm.request.model"); m != "" {
					modelCounts[m]++
				}
			}
		}
	}

	if totalInput == 0 && totalOutput == 0 {
		return nil
	}

	// Pick the most-used model name across all call_llm spans.
	dominantModel := ""
	dominantCount := 0
	for m, c := range modelCounts {
		if c > dominantCount {
			dominantModel = m
			dominantCount = c
		}
	}

	return &RunTokenUsage{
		TotalInputTokens:  totalInput,
		TotalOutputTokens: totalOutput,
		Model:             dominantModel,
	}
}

// GetTokenUsageFromTrace fetches a trace from Tempo by traceID, finds all
// call_llm spans, and aggregates input/output token counts into a RunTokenUsage.
// Returns nil, nil when Tempo is unreachable or the trace has no call_llm spans.
// Thin wrapper over GetTraceSpans — both share a single Tempo fetch.
func GetTokenUsageFromTrace(ctx context.Context, tempoBaseURL, traceID string) (*RunTokenUsage, error) {
	_, usage, err := GetTraceSpans(ctx, tempoBaseURL, traceID)
	return usage, err
}

// tempoUnixNano parses an OTLP startTimeUnixNano/endTimeUnixNano value. OTLP
// JSON emits these as decimal strings; missing or invalid values yield 0.
func tempoUnixNano(s string) int64 {
	if s == "" {
		return 0
	}
	v, _ := strconv.ParseInt(s, 10, 64)
	return v
}

// tempoAttrInt extracts an integer attribute value from a Tempo span.
func tempoAttrInt(attrs []tempoAttribute, key string) int64 {
	for _, a := range attrs {
		if a.Key == key {
			s := a.Value.IntValue
			if s == "" {
				s = a.Value.StringValue
			}
			if s == "" {
				return 0
			}
			v, _ := strconv.ParseInt(s, 10, 64)
			return v
		}
	}
	return 0
}

// tempoAttrStr extracts a string attribute value from a Tempo span.
func tempoAttrStr(attrs []tempoAttribute, key string) string {
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
