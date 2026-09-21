## Purpose

Guarantees that every sandbox container and volume created by the server — warm-pool containers included — has a durable owner and is reclaimed once no live process or active workspace is responsible for it, so host container and volume counts converge to configured targets across restarts and crashes instead of growing without bound.

## ADDED Requirements

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

The server SHALL run a reconciliation pass that destroys sandbox containers which are neither owned by the current process nor referenced by a live workspace record, together with the volume named by their `workspace.volume` label. Containers that are still owned by the current process, referenced by a workspace record that is not stopped or errored, or marked persistent SHALL NOT be destroyed. A container created more recently than the configured grace period SHALL NOT be destroyed.

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

### Requirement: Reconciliation SHALL run at startup and on the cleanup interval

The server SHALL run reconciliation once after providers are registered at startup and thereafter on the existing cleanup interval. Both SHALL be gated on agent sandboxes being enabled.

#### Scenario: Startup reconciliation reclaims the predecessor's leftovers
- **GIVEN** agent sandboxes are enabled
- **AND** orphaned labelled sandbox containers exist from a previous process
- **WHEN** the server completes startup
- **THEN** reconciliation SHALL have run without waiting for the cleanup interval

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

The grace period before an ownerless sandbox resource may be destroyed SHALL be configurable, and reconciliation SHALL be independently disableable, without requiring a schema change.

#### Scenario: Configured grace period is honoured
- **GIVEN** the grace period is configured to a non-default value
- **WHEN** reconciliation evaluates an ownerless container
- **THEN** the configured grace period SHALL be applied to the destruction decision

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
