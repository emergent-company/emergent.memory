## ADDED Requirements

### Requirement: Observability services available as opt-in Docker Compose profile
The monorepo deployment SHALL support an `observability` Docker Compose profile that adds trace collection infrastructure without affecting default deployments. Services under this profile SHALL NOT start unless explicitly requested.

#### Scenario: Default compose does not start Tempo
- **WHEN** a developer runs `docker compose up` without `--profile observability`
- **THEN** the Tempo service SHALL NOT start
- **AND** the main server SHALL start normally without any tracing errors

#### Scenario: Observability profile starts Tempo
- **WHEN** a developer runs `docker compose --profile observability up`
- **THEN** Tempo SHALL start alongside the other services
- **AND** the server SHALL automatically send traces to Tempo when `OTEL_EXPORTER_OTLP_ENDPOINT` is set
