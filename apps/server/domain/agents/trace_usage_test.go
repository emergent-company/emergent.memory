package agents

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

// cannedTempoTrace is a realistic OTLP JSON body as served by Tempo's
// GET /api/traces/{traceID}. It spans two batches (nested batches) and covers:
//   - intValue attributes encoded as strings
//   - a root span with no parentSpanId
//   - spans with missing timing fields
//   - call_llm spans carrying both memory.llm.usage.* (per-span tokens) and
//     memory.llm.response.* (aggregate tokens) plus a model attribute
//   - non call_llm spans (agent.run, execute_tool) that must still be flattened
const cannedTempoTrace = `{
  "batches": [
    {
      "resource": {
        "attributes": [
          {"key": "service.name", "value": {"stringValue": "em-server"}}
        ]
      },
      "scopeSpans": [
        {
          "scope": {"name": "em-agent"},
          "spans": [
            {
              "traceId": "abcdef0123456789",
              "spanId": "0000000000000001",
              "name": "agent.run",
              "kind": 2,
              "startTimeUnixNano": "1700000000000000000",
              "endTimeUnixNano": "1700000001000000000",
              "attributes": [
                {"key": "memory.agent.run_status", "value": {"stringValue": "completed"}}
              ]
            },
            {
              "traceId": "abcdef0123456789",
              "spanId": "0000000000000002",
              "parentSpanId": "0000000000000001",
              "name": "call_llm",
              "kind": 2,
              "startTimeUnixNano": "1700000000000001000",
              "endTimeUnixNano": "1700000001000001000",
              "attributes": [
                {"key": "memory.llm.usage.input_tokens", "value": {"intValue": "123"}},
                {"key": "memory.llm.usage.output_tokens", "value": {"intValue": "456"}},
                {"key": "memory.llm.response.input_tokens", "value": {"intValue": "200"}},
                {"key": "memory.llm.response.output_tokens", "value": {"intValue": "500"}},
                {"key": "memory.llm.request.model", "value": {"stringValue": "deepseek-v4-flash"}}
              ]
            },
            {
              "traceId": "abcdef0123456789",
              "spanId": "0000000000000003",
              "parentSpanId": "0000000000000002",
              "name": "execute_tool",
              "kind": 3,
              "attributes": []
            }
          ]
        }
      ]
    },
    {
      "resource": {"attributes": []},
      "scopeSpans": [
        {
          "scope": {"name": "em-agent"},
          "spans": [
            {
              "traceId": "abcdef0123456789",
              "spanId": "0000000000000004",
              "parentSpanId": "0000000000000001",
              "name": "call_llm",
              "kind": 2,
              "startTimeUnixNano": "1700000001000000000",
              "endTimeUnixNano": "1700000002000000000",
              "attributes": [
                {"key": "memory.llm.usage.input_tokens", "value": {"intValue": "10"}},
                {"key": "memory.llm.usage.output_tokens", "value": {"intValue": "20"}},
                {"key": "memory.llm.response.input_tokens", "value": {"intValue": "30"}},
                {"key": "memory.llm.response.output_tokens", "value": {"intValue": "40"}}
              ]
            }
          ]
        }
      ]
    }
  ]
}`

// tempoServer spins up an httptest server serving body for every request.
func tempoServer(body string, status int) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
}

func TestGetTraceSpans_FlattensNestedBatches(t *testing.T) {
	srv := tempoServer(cannedTempoTrace, http.StatusOK)
	defer srv.Close()

	spans, usage, err := GetTraceSpans(context.Background(), srv.URL, "abcdef0123456789")

	require.NoError(t, err)
	require.NotNil(t, usage)
	require.Len(t, spans, 4)

	// Root span: no parentSpanId → empty; timing parsed from string nanos;
	// no token/model attributes → zeros/empty.
	require.Equal(t, RunTraceSpan{
		SpanID:        "0000000000000001",
		ParentSpanID:  "",
		Name:          "agent.run",
		StartUnixNano: 1700000000000000000,
		EndUnixNano:   1700000001000000000,
	}, spans[0])

	// call_llm span: per-span tokens from memory.llm.usage.* (intValue as
	// string), model from memory.llm.request.model, parent links preserved.
	require.Equal(t, RunTraceSpan{
		SpanID:        "0000000000000002",
		ParentSpanID:  "0000000000000001",
		Name:          "call_llm",
		StartUnixNano: 1700000000000001000,
		EndUnixNano:   1700000001000001000,
		InputTokens:   123,
		OutputTokens:  456,
		Model:         "deepseek-v4-flash",
	}, spans[1])

	// Non call_llm spans are flattened too; missing timings parse to 0.
	require.Equal(t, RunTraceSpan{
		SpanID:       "0000000000000003",
		ParentSpanID: "0000000000000002",
		Name:         "execute_tool",
	}, spans[2])

	// Second batch: tokens extracted, model absent → empty.
	require.Equal(t, RunTraceSpan{
		SpanID:        "0000000000000004",
		ParentSpanID:  "0000000000000001",
		Name:          "call_llm",
		StartUnixNano: 1700000001000000000,
		EndUnixNano:   1700000002000000000,
		InputTokens:   10,
		OutputTokens:  20,
	}, spans[3])

	// Aggregate usage comes from memory.llm.response.* across call_llm spans.
	require.Equal(t, int64(230), usage.TotalInputTokens)  // 200 + 30
	require.Equal(t, int64(540), usage.TotalOutputTokens) // 500 + 40
	require.Equal(t, "deepseek-v4-flash", usage.Model)
}

func TestGetTraceSpans_NoCallLLMSpansReturnsNilUsage(t *testing.T) {
	body := `{"batches":[{"scopeSpans":[{"spans":[
		{"spanId":"a","name":"agent.run","startTimeUnixNano":"1700000000000000000"}
	]}]}]}`
	srv := tempoServer(body, http.StatusOK)
	defer srv.Close()

	spans, usage, err := GetTraceSpans(context.Background(), srv.URL, "trace-1")

	require.NoError(t, err)
	require.Len(t, spans, 1)
	require.Equal(t, "a", spans[0].SpanID)
	require.Nil(t, usage)
}

func TestGetTraceSpans_GracefulNilOnEmptyBaseURLOrTraceID(t *testing.T) {
	ctx := context.Background()

	spans, usage, err := GetTraceSpans(ctx, "", "trace-1")
	require.NoError(t, err)
	require.Nil(t, spans)
	require.Nil(t, usage)

	spans, usage, err = GetTraceSpans(ctx, "http://localhost:3200", "")
	require.NoError(t, err)
	require.Nil(t, spans)
	require.Nil(t, usage)
}

func TestGetTraceSpans_GracefulNilOnTempoUnreachable(t *testing.T) {
	// Server closed immediately → connection refused.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	url := srv.URL
	srv.Close()

	spans, usage, err := GetTraceSpans(context.Background(), url, "trace-1")
	require.NoError(t, err)
	require.Nil(t, spans)
	require.Nil(t, usage)
}

func TestGetTraceSpans_GracefulNilOnNon200(t *testing.T) {
	srv := tempoServer(`{"status":"error"}`, http.StatusInternalServerError)
	defer srv.Close()

	spans, usage, err := GetTraceSpans(context.Background(), srv.URL, "trace-1")
	require.NoError(t, err)
	require.Nil(t, spans)
	require.Nil(t, usage)
}

func TestGetTraceSpans_GracefulNilOnInvalidBody(t *testing.T) {
	srv := tempoServer(`this is not json`, http.StatusOK)
	defer srv.Close()

	spans, usage, err := GetTraceSpans(context.Background(), srv.URL, "trace-1")
	require.NoError(t, err)
	require.Nil(t, spans)
	require.Nil(t, usage)
}

// TestGetTokenUsageFromTrace_DelegatesToSingleFetch ensures the legacy entry
// point still returns aggregate usage through the shared fetch path.
func TestGetTokenUsageFromTrace_DelegatesToSingleFetch(t *testing.T) {
	srv := tempoServer(cannedTempoTrace, http.StatusOK)
	defer srv.Close()

	usage, err := GetTokenUsageFromTrace(context.Background(), srv.URL, "abcdef0123456789")

	require.NoError(t, err)
	require.NotNil(t, usage)
	require.Equal(t, int64(230), usage.TotalInputTokens)
	require.Equal(t, int64(540), usage.TotalOutputTokens)
}

// TestRunTraceSpanJSONShape locks the serialized contract for a flattened span.
func TestRunTraceSpanJSONShape(t *testing.T) {
	b, err := json.Marshal(RunTraceSpan{
		SpanID:        "0000000000000002",
		ParentSpanID:  "0000000000000001",
		Name:          "call_llm",
		StartUnixNano: 1700000000000000000,
		EndUnixNano:   1700000001000000000,
		InputTokens:   123,
		OutputTokens:  456,
		Model:         "deepseek-v4-flash",
	})
	require.NoError(t, err)

	var m map[string]any
	require.NoError(t, json.Unmarshal(b, &m))
	require.Equal(t, "0000000000000002", m["spanId"])
	require.Equal(t, "0000000000000001", m["parentSpanId"])
	require.Equal(t, "call_llm", m["name"])
	require.Equal(t, float64(1700000000000000000), m["startUnixNano"])
	require.Equal(t, float64(1700000001000000000), m["endUnixNano"])
	require.Equal(t, float64(123), m["inputTokens"])
	require.Equal(t, float64(456), m["outputTokens"])
	require.Equal(t, "deepseek-v4-flash", m["model"])
}
