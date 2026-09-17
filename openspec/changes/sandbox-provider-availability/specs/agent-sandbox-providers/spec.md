## ADDED Requirements

### Requirement: The providers endpoint SHALL report every known provider with availability and a reason

The `GET /api/v1/agent/sandboxes/providers` endpoint SHALL return an entry for every known provider type (`gvisor`, `firecracker`, `e2b`), in a stable order, regardless of whether it was successfully registered at startup. Registered providers SHALL report their live health; unregistered providers SHALL be reported as unhealthy with a human-readable reason.

#### Scenario: Unavailable providers are reported, not omitted
- **GIVEN** agent sandboxes are enabled
- **AND** Firecracker is skipped at startup because `/dev/kvm` is missing
- **AND** E2B is skipped because `E2B_API_KEY` is unset
- **WHEN** a client calls `GET /api/v1/agent/sandboxes/providers`
- **THEN** the response SHALL include entries for `gvisor`, `firecracker`, and `e2b`
- **AND** the `firecracker` entry SHALL have `"registered": false`, `"healthy": false`, and a reason mentioning KVM
- **AND** the `e2b` entry SHALL have `"registered": false`, `"healthy": false`, and reason `"E2B_API_KEY not set"`
- **AND** unregistered entries SHALL omit `capabilities`

#### Scenario: Registered providers report live health
- **GIVEN** a provider is registered at startup
- **WHEN** a client calls `GET /api/v1/agent/sandboxes/providers`
- **THEN** the entry SHALL have `"registered": true`
- **AND** the entry SHALL include the provider's live health, message, and capabilities

### Requirement: Provider health SHALL be refreshed periodically for the whole process lifetime

The health monitor SHALL keep checking provider health on a fixed interval for the entire process lifetime, independent of the startup context. Health SHALL NOT freeze at the boot-time value.

#### Scenario: Health monitor survives startup context cancellation
- **GIVEN** the health monitor is started with the fx OnStart hook context
- **AND** that context is canceled ~15s after startup
- **WHEN** 30 seconds or more have elapsed since startup
- **THEN** the monitor SHALL still run and re-check provider health on each interval
- **AND** a provider that became unhealthy after boot SHALL eventually be reported unhealthy

#### Scenario: Health monitoring stops only when explicitly requested
- **GIVEN** the health monitor is running
- **WHEN** `StopHealthMonitoring` is called
- **THEN** the monitor goroutine SHALL terminate
- **AND** calling `StopHealthMonitoring` again SHALL be safe (idempotent, no panic)

### Requirement: Provider selection errors SHALL enumerate rejection reasons

When no provider can be selected, the returned error SHALL explain why each candidate provider was rejected, in a deterministic order, while preserving the existing error prefix.

#### Scenario: Selection failure lists every rejection reason
- **GIVEN** gVisor is registered but unhealthy
- **AND** Firecracker is unregistered (KVM missing)
- **AND** E2B is unregistered (API key unset)
- **WHEN** automatic provider selection runs and no healthy provider exists
- **THEN** the error SHALL contain `"no healthy providers available"`
- **AND** the error SHALL enumerate `gvisor: unhealthy`, `firecracker: not registered: KVM not available`, and `e2b: not registered: E2B_API_KEY not set`
