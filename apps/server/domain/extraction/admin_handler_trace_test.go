package extraction

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/domain/agents"
)

// fakePricing is a table-backed modelPricing for the enrichment degrade-path tests.
type fakePricing struct {
	prices map[string][2]float64 // model -> {textInput, output} per 1M tokens
}

func (f *fakePricing) LookupModelPricing(_ context.Context, model string) (float64, float64, bool) {
	p, ok := f.prices[model]
	if !ok {
		return 0, 0, false
	}
	return p[0], p[1], true
}

// traceBody is an OTLP JSON trace with two call_llm spans (different models).
// Per-span tokens live in memory.llm.usage.*; model in memory.llm.request.model.
const traceBody = `{
  "batches": [
    {
      "scopeSpans": [
        {
          "spans": [
            {
              "spanId": "0000000000000001",
              "name": "extraction.object_extraction",
              "startTimeUnixNano": "1700000000000000000",
              "endTimeUnixNano": "1700000003000000000",
              "attributes": []
            },
            {
              "spanId": "0000000000000002",
              "parentSpanId": "0000000000000001",
              "name": "call_llm",
              "startTimeUnixNano": "1700000001000000000",
              "endTimeUnixNano": "1700000002000000000",
              "attributes": [
                {"key": "memory.llm.usage.input_tokens", "value": {"intValue": "1000"}},
                {"key": "memory.llm.usage.output_tokens", "value": {"intValue": "500"}},
                {"key": "memory.llm.request.model", "value": {"stringValue": "deepseek-v4-flash"}}
              ]
            },
            {
              "spanId": "0000000000000003",
              "parentSpanId": "0000000000000001",
              "name": "call_llm",
              "startTimeUnixNano": "1700000002000000000",
              "endTimeUnixNano": "1700000003000000000",
              "attributes": [
                {"key": "memory.llm.usage.input_tokens", "value": {"intValue": "2000"}},
                {"key": "memory.llm.usage.output_tokens", "value": {"intValue": "1000"}},
                {"key": "memory.llm.request.model", "value": {"stringValue": "deepseek-v4-pro"}}
              ]
            }
          ]
        }
      ]
    }
  ]
}`

func tempoServer(body string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(body))
	}))
}

func traceIDPtr(v string) *string { return &v }

func TestEnrichTrace_DegradePaths(t *testing.T) {
	fullPricing := &fakePricing{prices: map[string][2]float64{
		"deepseek-v4-flash": {0.14, 0.28},
		"deepseek-v4-pro":   {1.74, 3.48},
	}}
	// Pricing table with no entry for any model in the trace.
	noMatchPricing := &fakePricing{prices: map[string][2]float64{
		"some-other-model": {0.10, 0.20},
	}}

	tests := []struct {
		name      string
		traceID   *string
		tempoURL  string
		pricing   modelPricing
		wantSpans int
		wantCost  *float64
	}{
		{
			name:      "no trace id",
			traceID:   nil,
			tempoURL:  "http://tempo",
			pricing:   fullPricing,
			wantSpans: 0,
		},
		{
			name:      "empty trace id",
			traceID:   traceIDPtr(""),
			tempoURL:  "http://tempo",
			pricing:   fullPricing,
			wantSpans: 0,
		},
		{
			name:      "tempo disabled",
			traceID:   traceIDPtr("abcdef0123456789"),
			tempoURL:  "",
			pricing:   fullPricing,
			wantSpans: 0,
		},
		{
			name:      "trace fetch failure",
			traceID:   traceIDPtr("abcdef0123456789"),
			tempoURL:  "http://127.0.0.1:1", // connection refused
			pricing:   fullPricing,
			wantSpans: 0,
		},
		{
			name:      "model without pricing",
			traceID:   traceIDPtr("abcdef0123456789"),
			tempoURL:  "", // overridden below to a live server
			pricing:   noMatchPricing,
			wantSpans: 3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := &AdminHandler{tempoBaseURL: tt.tempoURL, pricing: tt.pricing}

			if tt.name == "model without pricing" {
				srv := tempoServer(traceBody)
				defer srv.Close()
				h.tempoBaseURL = srv.URL
			}

			dto := &ExtractionJobDTO{TraceID: tt.traceID}
			h.enrichTrace(context.Background(), dto)

			require.Len(t, dto.Spans, tt.wantSpans)
			require.Nil(t, dto.TraceCostUSD)
		})
	}
}

func TestEnrichTrace_HappyPath(t *testing.T) {
	srv := tempoServer(traceBody)
	defer srv.Close()

	pricing := &fakePricing{prices: map[string][2]float64{
		"deepseek-v4-flash": {0.14, 0.28},
		"deepseek-v4-pro":   {1.74, 3.48},
	}}

	h := &AdminHandler{tempoBaseURL: srv.URL, pricing: pricing}
	dto := &ExtractionJobDTO{TraceID: traceIDPtr("abcdef0123456789")}

	h.enrichTrace(context.Background(), dto)

	require.Len(t, dto.Spans, 3)
	require.NotNil(t, dto.TraceCostUSD)

	// span1: 1000/1e6*0.14 + 500/1e6*0.28 = 0.00014 + 0.00014 = 0.00028
	// span2: 2000/1e6*1.74 + 1000/1e6*3.48 = 0.00348 + 0.00348 = 0.00696
	// total = 0.00724
	require.InDelta(t, 0.00724, *dto.TraceCostUSD, 1e-9)

	// Spans preserve the flattened RunTraceSpan shape, including token counts
	// and model, so the gateway can render a waterfall.
	var callLLM []agents.RunTraceSpan
	for _, s := range dto.Spans {
		if s.Name == "call_llm" {
			callLLM = append(callLLM, s)
		}
	}
	require.Len(t, callLLM, 2)
	require.Equal(t, "deepseek-v4-flash", callLLM[0].Model)
	require.Equal(t, int64(1000), callLLM[0].InputTokens)
	require.Equal(t, int64(500), callLLM[0].OutputTokens)
	require.Equal(t, "deepseek-v4-pro", callLLM[1].Model)
}

func TestEnrichTrace_NilPricingLeavesCostEmpty(t *testing.T) {
	srv := tempoServer(traceBody)
	defer srv.Close()

	h := &AdminHandler{tempoBaseURL: srv.URL, pricing: nil}
	dto := &ExtractionJobDTO{TraceID: traceIDPtr("abcdef0123456789")}

	h.enrichTrace(context.Background(), dto)

	require.Len(t, dto.Spans, 3)
	require.Nil(t, dto.TraceCostUSD)
}

// vendorPrefixedTrace is an OTLP trace whose call_llm spans carry vendor-prefixed
// model attributes, as emitted by the model factory.
const vendorPrefixedTrace = `{
  "batches": [
    {
      "scopeSpans": [
        {
          "spans": [
            {
              "spanId": "0000000000000001",
              "name": "call_llm",
              "startTimeUnixNano": "1700000001000000000",
              "endTimeUnixNano": "1700000002000000000",
              "attributes": [
                {"key": "memory.llm.usage.input_tokens", "value": {"intValue": "1000"}},
                {"key": "memory.llm.usage.output_tokens", "value": {"intValue": "500"}},
                {"key": "memory.llm.request.model", "value": {"stringValue": "openai/deepseek-v4-flash"}}
              ]
            },
            {
              "spanId": "0000000000000002",
              "parentSpanId": "0000000000000001",
              "name": "call_llm",
              "startTimeUnixNano": "1700000002000000000",
              "endTimeUnixNano": "1700000003000000000",
              "attributes": [
                {"key": "memory.llm.usage.input_tokens", "value": {"intValue": "2000"}},
                {"key": "memory.llm.usage.output_tokens", "value": {"intValue": "1000"}},
                {"key": "memory.llm.request.model", "value": {"stringValue": "google/deepseek-v4-pro"}}
              ]
            }
          ]
        }
      ]
    }
  ]
}`

func TestModelCandidates(t *testing.T) {
	tests := []struct {
		name  string
		model string
		want  []string
	}{
		{name: "bare", model: "deepseek-v4-flash", want: []string{"deepseek-v4-flash"}},
		{name: "vendor prefixed", model: "openai/deepseek-v4-flash", want: []string{"openai/deepseek-v4-flash", "deepseek-v4-flash"}},
		{name: "nested prefix", model: "google/vertex/gemini-2.5-flash", want: []string{"google/vertex/gemini-2.5-flash", "gemini-2.5-flash", "vertex/gemini-2.5-flash"}},
		{name: "trailing slash", model: "openai/", want: []string{"openai/"}},
		{name: "empty", model: "", want: nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, modelCandidates(tt.model))
		})
	}
}

func TestEnrichTrace_NormalizesVendorPrefixedModel(t *testing.T) {
	srv := tempoServer(vendorPrefixedTrace)
	defer srv.Close()

	pricing := &fakePricing{prices: map[string][2]float64{
		"deepseek-v4-flash": {0.14, 0.28},
		"deepseek-v4-pro":   {1.74, 3.48},
	}}

	h := &AdminHandler{tempoBaseURL: srv.URL, pricing: pricing}
	dto := &ExtractionJobDTO{TraceID: traceIDPtr("abcdef0123456789")}

	h.enrichTrace(context.Background(), dto)

	require.Len(t, dto.Spans, 2)
	require.NotNil(t, dto.TraceCostUSD)
	// Same arithmetic as the happy path: 0.00028 + 0.00696 = 0.00724.
	require.InDelta(t, 0.00724, *dto.TraceCostUSD, 1e-9)
}
