## REMOVED Requirements

### Requirement: Hierarchical Tracing
**Reason**: This requirement was specific to the Langfuse integration, which is being removed. The new Tempo-based observability stack has its own methods for hierarchical tracing.
**Migration**: No migration is needed as this is a removal of an internal observability feature.

### Requirement: Span Context Propagation
**Reason**: This requirement was specific to the Langfuse integration and its concept of Spans and Observations. The new OpenTelemetry-based system uses a different context propagation mechanism.
**Migration**: No migration is needed.

### Requirement: Timeline-Driven Tracing
**Reason**: This requirement was for automatically creating Langfuse spans from internal timeline events. This is no longer applicable.
**Migration**: No migration is needed.
