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

### Requirement: The agent Sandbox settings UI SHALL derive provider choices from reported availability

The agent Sandbox settings page (`/agents/:id/sandbox`) SHALL render its provider `<select>` from the providers endpoint instead of a hardcoded list. `Auto` SHALL remain the default option. Providers reported as unavailable SHALL be rendered as non-selectable options labelled as unavailable, and the page SHALL show each provider's availability with the reported reason. The provider currently stored in the agent's sandbox config SHALL be the exception: its option SHALL remain selectable (still labelled unavailable) so that submitting an unchanged form preserves it, because browsers omit a disabled selected option from the submitted form data. A provider stored in the agent's sandbox config SHALL never be dropped from the form because the endpoint did not list it.

#### Scenario: Unavailable provider that is not stored is visible but not selectable
- **GIVEN** the providers endpoint reports `gvisor` as healthy
- **AND** reports `firecracker` as unregistered with reason `"KVM not available on this host"`
- **AND** the agent's stored provider is not `firecracker`
- **WHEN** a user opens the agent Sandbox settings page
- **THEN** the `gvisor` option SHALL be selectable
- **AND** the `firecracker` option SHALL be rendered `disabled` and labelled as unavailable
- **AND** the reported reason SHALL be exposed to the user (for example as the option's tooltip and in the availability status list)

#### Scenario: Stored unavailable provider is selectable and round-trips
- **GIVEN** an agent's sandbox config stores provider `firecracker`
- **AND** the providers endpoint reports `firecracker` as unavailable with a reason
- **WHEN** the user opens the Sandbox settings page and submits the unchanged form
- **THEN** the rendered `firecracker` option SHALL be `selected`, SHALL NOT be `disabled`, and SHALL be labelled as unavailable
- **AND** the stored provider SHALL remain `firecracker` after the update

#### Scenario: Stored provider is preserved when it is not reported
- **GIVEN** an agent's sandbox config stores provider `firecracker`
- **AND** the providers endpoint does not include a `firecracker` entry
- **WHEN** the user opens the Sandbox settings page and submits the unchanged form
- **THEN** the rendered form SHALL still contain a selected, selectable `firecracker` option marked unavailable
- **AND** the stored provider SHALL remain `firecracker` after the update

#### Scenario: An explicit Auto choice persists
- **GIVEN** the sandbox form renders a stored-but-unavailable provider
- **WHEN** the user selects `Auto` and submits, sending an empty provider value
- **THEN** no fallback SHALL re-inject the stored provider
- **AND** the stored provider SHALL be cleared (Auto)

#### Scenario: Provider availability cannot be determined
- **GIVEN** the providers request fails, or returns an empty list
- **WHEN** a user opens the agent Sandbox settings page
- **THEN** the page SHALL still render
- **AND** it SHALL display an inline warning that provider availability could not be determined
- **AND** it SHALL NOT present an unmarked list of selectable providers

#### Scenario: Availability status is shown per provider
- **GIVEN** the providers endpoint reports a mix of available and unavailable providers
- **WHEN** the Sandbox settings page is rendered
- **THEN** each reported provider SHALL be listed with an availability indicator
- **AND** every unavailable provider SHALL display its reason text
