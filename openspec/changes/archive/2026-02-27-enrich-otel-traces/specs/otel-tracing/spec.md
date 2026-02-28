## ADDED Requirements

### Requirement: Shared tracer helper package
The server SHALL provide a shared `pkg/tracing` helper package that wraps `otel.Tracer()` and provides a consistent `Start(ctx, spanName, attrs ...attribute.KeyValue) (context.Context, trace.Span)` function. Domain packages SHALL use this helper rather than calling the OTel API directly.

#### Scenario: Domain package starts a span via helper
- **WHEN** any domain package needs to create a span
- **THEN** it SHALL call `tracing.Start(ctx, "operation.name", attrs...)` from `pkg/tracing`
- **AND** the returned span SHALL be a child of the span in `ctx` if one is present, or a root span otherwise

### Requirement: Span naming convention
All custom spans SHALL use dot-separated lowercase names following the pattern `<domain>.<operation>` (e.g. `extraction.document_parsing`, `agent.run`, `search.execute`). HTTP spans from otelecho follow their own convention and are excluded.

#### Scenario: Span names are consistent
- **WHEN** traces are queried in Tempo using a service name filter
- **THEN** all custom spans SHALL match the pattern `^[a-z]+\.[a-z_]+$`

### Requirement: Attribute payload safety rule
Span attributes SHALL be limited to: IDs (UUIDs as strings), counts (integers), durations (already captured by span timing), strategy/type enums (short strings), and error messages. Variable-length user content (document text, query text, prompt text, LLM completions, embeddings) SHALL NEVER appear in span attributes or span events.

#### Scenario: Attribute size stays bounded
- **WHEN** any span is exported
- **THEN** no individual attribute value SHALL exceed 256 characters
- **AND** attributes carrying entity names, document titles, or query text SHALL NOT be set
