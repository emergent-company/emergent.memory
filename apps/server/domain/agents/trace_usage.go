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
	Name       string           `json:"name"`
	Attributes []tempoAttribute `json:"attributes"`
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

// ── Token aggregation from trace ─────────────────────────────────────────────

// GetTokenUsageFromTrace fetches a trace from Tempo by traceID, finds all
// call_llm spans, and aggregates input/output token counts into a RunTokenUsage.
// Returns nil, nil when Tempo is unreachable or the trace has no call_llm spans.
func GetTokenUsageFromTrace(ctx context.Context, tempoBaseURL, traceID string) (*RunTokenUsage, error) {
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

	var totalInput, totalOutput int64

	for _, batch := range trace.Batches {
		for _, ss := range batch.ScopeSpans {
			for _, span := range ss.Spans {
				if span.Name != "call_llm" {
					continue
				}
				totalInput += tempoAttrInt(span.Attributes, "memory.llm.response.input_tokens")
				totalOutput += tempoAttrInt(span.Attributes, "memory.llm.response.output_tokens")
			}
		}
	}

	if totalInput == 0 && totalOutput == 0 {
		return nil, nil
	}

	return &RunTokenUsage{
		TotalInputTokens:  totalInput,
		TotalOutputTokens: totalOutput,
	}, nil
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
