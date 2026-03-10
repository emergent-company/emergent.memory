# observability Specification

## Purpose
Observability for the Emergent platform. LLM tracing is provided by Grafana Tempo via OpenTelemetry (opt-in via `OTEL_ENABLED=true`). Langfuse and Signoz integrations have been removed.

## Requirements

<!-- All prior Langfuse-specific requirements (Hierarchical Tracing, Span Context Propagation, Timeline-Driven Tracing) were removed as part of the remove-signals-and-langfuse change. -->
