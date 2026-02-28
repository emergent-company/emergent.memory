## Why

The agent needs a standardized way to interact with the `mcj-emergent` semi-production server. Currently, operations like deploying updates, checking status, or restarting services on this environment are not codified in a skill, leading to ad-hoc bash commands or missing the correct usage of the `emergent` CLI. Having a dedicated skill ensures the agent reliably connects via SSH and manages the deployment correctly.

## What Changes

- Propose a new agent skill (`agent-mcj-emergent-manager`) to manage the `mcj-emergent` server.
- The skill will document how to connect to the server via SSH (since keys are already installed).
- The skill will document that upgrades should be performed using the `emergent upgrade` command.
- The skill will specify that most operations should use the `emergent` CLI on the remote host.

## Capabilities

### New Capabilities
- `agent-mcj-emergent-manager`: A new skill teaching the agent how to manage the `mcj-emergent` semi-production server (connect, upgrade, start, stop, check status via `emergent` CLI over SSH).

### Modified Capabilities

## Impact

This new skill will be placed in `.agents/skills/agent-mcj-emergent-manager/SKILL.md`. It improves agent autonomy and reliability when interacting with the semi-production environment, without changing any application code.
