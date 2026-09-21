# agent-sandbox-lifecycle Specification

## Purpose
Guarantees that every sandbox container and volume created by the server — warm-pool containers included — has a durable owner and is reclaimed once no live process or active workspace is responsible for it, so host container and volume counts converge to configured targets across restarts and crashes instead of growing without bound.

## Requirements

### Requirement: Every sandbox container and volume SHALL carry a durable ownership label

The server SHALL apply a stable owning-process identity label to every sandbox container and volume it creates, in addition to the existing reconciliation labels (`memory.workspace`, `workspace.type`, `workspace.volume`). Container and volume identity SHALL be discoverable from the Docker daemon after the creating process has exited.

#### Scenario: Created sandbox resources are labelled and discoverable after process exit
- **GIVEN** agent sandboxes are enabled
- **WHEN** the server creates a sandbox container and its workspace volume
- **THEN** the container SHALL carry `memory.workspace=true`, its workspace type, its volume name, and the owning-process identity label
- **AND** the volume SHALL carry `memory.workspace=true` and the owning-process identity label
- **AND** after the creating process exits, both SHALL remain discoverable by filtering the Docker daemon on those labels

#### Scenario: Enumeration returns only labelled sandbox resources
- **GIVEN** the host runs labelled sandbox containers alongside unrelated containers
- **WHEN** the server enumerates sandbox resources by label
- **THEN** only the labelled sandbox containers and volumes SHALL be returned

### Requirement: Ownerless sandbox containers and volumes SHALL be reconciled

The server SHALL run a reconciliation pass that destroys sandbox containers which are neither owned by the current process, nor protected by a fresh owner liveness lease (see the peer-liveness requirement), nor referenced by a live workspace record, together with the volume named by their `workspace.volume` label. Containers that are still owned by the current process, referenced by a workspace record that is not stopped or errored, or marked persistent SHALL NOT be destroyed. A container created more recently than the configured grace period SHALL NOT be destroyed. If enumeration of sandbox containers fails, reconciliation SHALL abort before destroying any container or volume.

#### Scenario: Orphans left by an ungraceful restart are reclaimed automatically
- **GIVEN** a previous server process created warm-pool containers and exited without running its shutdown hook
- **AND** those containers have no workspace record and are past the grace period
- **WHEN** the server starts and runs its reconciliation pass
- **THEN** each such container SHALL be destroyed
- **AND** each container's labelled workspace volume SHALL be destroyed
- **AND** the remaining labelled container count SHALL equal the configured pool target

#### Scenario: The current process's own containers are never destroyed
- **GIVEN** the running process owns warm-pool containers labelled with its own process identity
- **WHEN** reconciliation runs
- **THEN** those containers SHALL NOT be destroyed

#### Scenario: Active and persistent workspaces are never destroyed
- **GIVEN** a workspace record exists for a container with status neither stopped nor errored
- **AND** a persistent MCP server container exists with no expiry
- **WHEN** reconciliation runs
- **THEN** neither container SHALL be destroyed

#### Scenario: Containers inside the grace period are spared
- **GIVEN** an ownerless sandbox container was created more recently than the configured grace period
- **WHEN** reconciliation runs
- **THEN** the container SHALL NOT be destroyed
- **AND** it SHALL be reconsidered on a later pass once it exceeds the grace period

#### Scenario: Orphan volumes without a container are reclaimed
- **GIVEN** a labelled sandbox volume exists with no corresponding running container
- **WHEN** reconciliation runs
- **THEN** the volume SHALL be destroyed

#### Scenario: A failing destroy does not abort the pass
- **GIVEN** several orphan containers are eligible for destruction
- **AND** destroying one of them fails
- **WHEN** reconciliation runs
- **THEN** the remaining eligible orphans SHALL still be destroyed
- **AND** the failure SHALL be logged

#### Scenario: Concurrent reconciliation passes do not overlap
- **GIVEN** a reconciliation pass is already running
- **WHEN** another reconciliation pass is triggered
- **THEN** the second pass SHALL be a no-op

#### Scenario: An unavailable reference store aborts before destruction
- **GIVEN** the workspace reference store is absent or cannot list active workspaces
- **WHEN** reconciliation runs
- **THEN** no container or volume SHALL be destroyed
- **AND** the failure SHALL be logged

#### Scenario: Container enumeration failure aborts before destruction
- **GIVEN** the Docker daemon cannot enumerate sandbox containers
- **AND** labelled workspace volumes exist, some belonging to live containers
- **WHEN** reconciliation runs
- **THEN** no container or volume SHALL be destroyed
- **AND** the failure SHALL be logged

### Requirement: A warm-pool container whose owner is still alive SHALL be spared

The server SHALL refresh a per-container liveness lease for every warm-pool container it tracks, at an interval of `WORKSPACE_OWNER_HEARTBEAT_MIN`. Reconciliation SHALL NOT destroy a warm-pool container whose lease is fresher than `3 × WORKSPACE_OWNER_HEARTBEAT_MIN`. A missing or stale lease SHALL mean the owner is presumed dead and the container SHALL be eligible for destruction subject to the grace period. A container the warm pool no longer tracks SHALL NOT be kept alive by the owner's lease. The lease representation SHALL be durable and readable after the owning process exits.

#### Scenario: A live peer's warm-pool container is spared
- **GIVEN** a peer process owns a warm-pool container older than the grace period
- **AND** its liveness lease is fresher than `3 × WORKSPACE_OWNER_HEARTBEAT_MIN`
- **WHEN** reconciliation runs
- **THEN** the container SHALL NOT be destroyed

#### Scenario: A stale heartbeat is reapable
- **GIVEN** a warm-pool container whose owner has stopped refreshing its lease
- **AND** its newest lease is older than `3 × WORKSPACE_OWNER_HEARTBEAT_MIN`
- **WHEN** reconciliation runs
- **THEN** the container SHALL be destroyed once past the grace period

#### Scenario: A missing heartbeat is treated as stale
- **GIVEN** a warm-pool container with no liveness lease
- **WHEN** reconciliation runs
- **THEN** the container SHALL be treated as ownerless and be eligible for destruction once past the grace period

#### Scenario: Dropped containers are not resurrected by a live owner
- **GIVEN** the warm pool no longer tracks a container it previously created
- **AND** the owning process is still running
- **WHEN** the dropped container's lease is no longer refreshed
- **THEN** reconciliation SHALL treat it as ownerless and destroy it once past the grace period

#### Scenario: Unparseable lease timestamps fail safe
- **GIVEN** a warm-pool container whose lease volume has an empty or unparseable creation time
- **WHEN** reconciliation evaluates the container
- **THEN** the lease SHALL be treated as unknown and the container SHALL be spared

#### Scenario: Concurrent lease refreshes never remove the only lease
- **GIVEN** a warm-pool container whose liveness lease is refreshed concurrently by more than one caller
- **WHEN** the concurrent refreshes complete
- **THEN** at least one lease for that container SHALL remain
- **AND** reconciliation SHALL still observe the owner as live

#### Scenario: Liveness is re-validated immediately before destruction
- **GIVEN** reconciliation's lease snapshot shows a warm-pool container's owner as dead
- **AND** the owner refreshes that container's lease before the container is destroyed
- **WHEN** reconciliation is about to destroy the container
- **THEN** it SHALL re-read the lease and spare the container
- **AND** the skip SHALL be recorded with a reason

#### Scenario: Normal teardown removes the container's leases
- **GIVEN** a warm-pool container is destroyed through the provider's normal destroy path
- **WHEN** the destroy completes and the container is gone
- **THEN** its liveness lease volumes SHALL be removed
- **AND** a later reconciliation pass SHALL find no lease for that container
- **AND** a container that could not be removed SHALL keep its lease

#### Scenario: Leases of dead containers are reclaimed
- **GIVEN** a liveness-lease volume exists whose container ID is absent from the container list
- **WHEN** reconciliation runs
- **THEN** the lease volume SHALL be destroyed and logged with a reason
- **AND** a repeated pass SHALL be a no-op for that lease (idempotent)

#### Scenario: Leases of live containers are never removed
- **GIVEN** a lease volume whose container ID is present in the container list
- **WHEN** reconciliation runs
- **THEN** the lease volume SHALL NOT be removed
- **AND** the workspace volume sweep SHALL NOT treat any lease volume as an orphan

### Requirement: Reconciliation SHALL run at startup and on the cleanup interval

The server SHALL run reconciliation once after providers are registered at startup and thereafter on the existing cleanup interval. Both SHALL be gated on agent sandboxes being enabled.

#### Scenario: Startup reconciliation reclaims the predecessor's leftovers
- **GIVEN** agent sandboxes are enabled
- **AND** orphaned labelled sandbox containers exist from a previous process whose owner lease is stale or absent
- **WHEN** the server completes startup
- **THEN** reconciliation SHALL have run without waiting for the cleanup interval
- **AND** those containers SHALL be reclaimed (subject to the grace period)

#### Scenario: Startup spares a just-crashed peer whose lease is still fresh
- **GIVEN** a peer process crashed moments ago and its warm-pool lease is still fresh
- **WHEN** the server runs its startup reconciliation
- **THEN** the peer's containers SHALL be spared as `peer_live`
- **AND** they SHALL become reapable on a later cycle once the lease exceeds `3 × WORKSPACE_OWNER_HEARTBEAT_MIN`

#### Scenario: Cleanup cycle reconciles and expires in one tick
- **GIVEN** the cleanup job is running on its configured interval
- **WHEN** a cycle executes
- **THEN** the cycle SHALL both reconcile ownerless sandbox resources and destroy DB-recorded expired workspaces

#### Scenario: Reconciliation is skipped when sandboxes are disabled
- **GIVEN** agent sandboxes are disabled via configuration
- **WHEN** the server starts and the cleanup cycle runs
- **THEN** no reconciliation pass SHALL run
- **AND** no sandbox container or volume SHALL be destroyed

### Requirement: Reconciliation behaviour SHALL be configurable

The grace period before an ownerless sandbox resource may be destroyed, and the owner heartbeat refresh interval, SHALL be configurable, and reconciliation SHALL be independently disableable, without requiring a schema change.

#### Scenario: Configured grace period is honoured
- **GIVEN** the grace period is configured to a non-default value
- **WHEN** reconciliation evaluates an ownerless container
- **THEN** the configured grace period SHALL be applied to the destruction decision

#### Scenario: Configured heartbeat interval sets the staleness threshold
- **GIVEN** `WORKSPACE_OWNER_HEARTBEAT_MIN` is configured to a non-default value
- **WHEN** reconciliation evaluates a warm-pool container's liveness lease
- **THEN** the staleness threshold SHALL be `3 × WORKSPACE_OWNER_HEARTBEAT_MIN`

### Requirement: Warm pool SHALL converge to its configured target

The warm pool SHALL keep the number of containers it manages equal to the configured target per managed image. Containers beyond the target, and containers discarded for image staleness, SHALL be destroyed rather than retained or orphaned.

#### Scenario: Surplus containers converge to target
- **GIVEN** managed images have more pre-booted containers than the configured target
- **WHEN** the warm pool starts
- **THEN** it SHALL destroy the surplus containers
- **AND** the retained count per managed image SHALL equal the configured target

#### Scenario: Stale container discard leaves no extra container
- **GIVEN** a warm container was booted from an image that has since been rebuilt
- **WHEN** the pool assigns a request and discards that stale container
- **THEN** the stale container SHALL be destroyed
- **AND** at most one replacement SHALL be created for it

#### Scenario: Steady-state pool size remains bounded
- **GIVEN** repeated acquire and replenish cycles occur
- **WHEN** the pool reaches steady state
- **THEN** the number of containers managed by the pool SHALL equal the configured target

### Requirement: The warm pool SHALL pre-boot only the reconciled provider

The warm pool SHALL create pre-booted containers only through the provider that supports label-scoped enumeration and per-container liveness leases. If that provider is unavailable, or does not implement those capabilities, the pool SHALL fail to create that warm container rather than pre-boot a container reconciliation cannot enumerate or keep alive.

#### Scenario: Warm containers are pre-booted through the reconciled provider
- **GIVEN** agent sandboxes are enabled and the warm pool has a positive target
- **WHEN** the pool creates a warm container
- **THEN** the container SHALL be created through the label-enumerating, lease-capable provider
- **AND** it SHALL be discoverable by the reconciliation label query

#### Scenario: A provider that cannot be reconciled is refused
- **GIVEN** the label-enumerating, lease-capable provider is unavailable
- **WHEN** the warm pool attempts to create a warm container
- **THEN** the attempt SHALL fail and be logged
- **AND** no container that reconciliation cannot enumerate SHALL be pre-booted

### Requirement: Clients SHALL NOT be able to supply a provider container reference

The provider container/VM reference persisted with a workspace SHALL be assigned by the server. A client-supplied provider container reference SHALL be ignored when creating a workspace, so an API caller cannot shield an unrelated container from reconciliation.

#### Scenario: A client-supplied provider container reference is ignored
- **GIVEN** a workspace creation request whose body sets a provider container reference
- **WHEN** the server decodes the request and persists the workspace
- **THEN** the persisted workspace SHALL NOT carry the client-supplied reference
- **AND** the container named by the client SHALL remain eligible for reconciliation

### Requirement: Reconciliation SHALL be observable

Reconciliation SHALL log each destruction with enough detail to identify the resource and the reason, and SHALL report counts of reconciled, skipped, and failed resources.

#### Scenario: Destroyed orphans are logged with reasons
- **GIVEN** reconciliation destroys an ownerless container
- **WHEN** the pass completes
- **THEN** a log entry SHALL identify the container and the reason it was considered ownerless
- **AND** the pass summary SHALL report reconciled, skipped, and failed counts

#### Scenario: Skipped resources are explained
- **GIVEN** a container is skipped because it is owned, active, persistent, or inside the grace period
- **WHEN** the pass completes
- **THEN** the skip reason SHALL be recorded at debug level
