package tracing

import (
	"context"
	"strings"

	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

// attrRenames maps third-party OTel attribute keys (emitted by the Google ADK)
// to memory.* equivalents so all span attributes use a consistent namespace.
var attrRenames = map[string]string{
	// gcp.vertex.agent.* → memory.llm.*
	"gcp.vertex.agent.llm_request":    "memory.llm.request",
	"gcp.vertex.agent.llm_response":   "memory.llm.response",
	"gcp.vertex.agent.tool_call_args": "memory.llm.tool_call_args",
	"gcp.vertex.agent.tool_response":  "memory.llm.tool_response",
	"gcp.vertex.agent.event_id":       "memory.llm.event_id",
	"gcp.vertex.agent.invocation_id":  "memory.llm.invocation_id",
	"gcp.vertex.agent.session_id":     "memory.llm.session_id",

	// gen_ai.* → memory.llm.*
	"gen_ai.operation.name":                      "memory.llm.operation",
	"gen_ai.system":                              "memory.llm.system",
	"gen_ai.request.model":                       "memory.llm.request.model",
	"gen_ai.request.max_tokens":                  "memory.llm.request.max_tokens",
	"gen_ai.request.top_p":                       "memory.llm.request.top_p",
	"gen_ai.response.finish_reason":              "memory.llm.response.finish_reason",
	"gen_ai.response.prompt_token_count":         "memory.llm.response.input_tokens",
	"gen_ai.response.candidates_token_count":     "memory.llm.response.output_tokens",
	"gen_ai.response.cached_content_token_count": "memory.llm.response.cached_tokens",
	"gen_ai.response.total_token_count":          "memory.llm.response.total_tokens",
	"gen_ai.tool.name":                           "memory.llm.tool.name",
	"gen_ai.tool.description":                    "memory.llm.tool.description",
	"gen_ai.tool.call.id":                        "memory.llm.tool.call_id",
}

// AttrRewriteProcessor is an OTel SpanProcessor that renames third-party
// attribute keys (Google ADK gcp.vertex.agent.* and gen_ai.*) to the
// memory.llm.* namespace before spans are exported.
//
// It wraps a delegate processor and must be registered on both the server's
// sdktrace.TracerProvider and on the ADK's internal tracer via
// adktelemetry.RegisterSpanProcessor so that all ADK-emitted spans are rewritten.
type AttrRewriteProcessor struct {
	delegate sdktrace.SpanProcessor
}

// NewAttrRewriteProcessor wraps delegate with attribute rewriting.
func NewAttrRewriteProcessor(delegate sdktrace.SpanProcessor) *AttrRewriteProcessor {
	return &AttrRewriteProcessor{delegate: delegate}
}

func (p *AttrRewriteProcessor) OnStart(parent context.Context, s sdktrace.ReadWriteSpan) {
	p.delegate.OnStart(parent, s)
}

func (p *AttrRewriteProcessor) OnEnd(s sdktrace.ReadOnlySpan) {
	p.delegate.OnEnd(rewriteSpan(s))
}

func (p *AttrRewriteProcessor) Shutdown(ctx context.Context) error {
	return p.delegate.Shutdown(ctx)
}

func (p *AttrRewriteProcessor) ForceFlush(ctx context.Context) error {
	return p.delegate.ForceFlush(ctx)
}

// rewriteSpan returns a wrapper that presents renamed attributes to the exporter.
func rewriteSpan(s sdktrace.ReadOnlySpan) sdktrace.ReadOnlySpan {
	orig := s.Attributes()
	renamed := make([]attribute.KeyValue, 0, len(orig))
	changed := false
	for _, kv := range orig {
		k := string(kv.Key)
		if newKey, ok := attrRenames[k]; ok {
			renamed = append(renamed, attribute.KeyValue{
				Key:   attribute.Key(newKey),
				Value: kv.Value,
			})
			changed = true
		} else if strings.HasPrefix(k, "gcp.vertex.") || strings.HasPrefix(k, "gen_ai.") {
			// Unknown future ADK attributes: move to memory.llm.unknown.* rather
			// than dropping them, so we don't silently lose data.
			suffix := k
			if after, ok2 := strings.CutPrefix(k, "gcp.vertex.agent."); ok2 {
				suffix = after
			} else if after2, ok3 := strings.CutPrefix(k, "gen_ai."); ok3 {
				suffix = after2
			}
			renamed = append(renamed, attribute.KeyValue{
				Key:   attribute.Key("memory.llm.unknown." + suffix),
				Value: kv.Value,
			})
			changed = true
		} else {
			renamed = append(renamed, kv)
		}
	}
	if !changed {
		return s
	}
	return &rewrittenSpan{ReadOnlySpan: s, attrs: renamed}
}

// rewrittenSpan wraps a ReadOnlySpan and overrides Attributes().
type rewrittenSpan struct {
	sdktrace.ReadOnlySpan
	attrs []attribute.KeyValue
}

func (r *rewrittenSpan) Attributes() []attribute.KeyValue {
	return r.attrs
}
