## Context

The `mcj-emergent` server is a critical semi-production environment used for testing with real data. Managing this server requires SSH access and specific knowledge of the `emergent` CLI for operations like upgrades and service management. The agent currently lacks a formal skill to perform these actions, risking manual errors and inconsistent deployments.

## Goals / Non-Goals

**Goals:**
- Create a dedicated agent skill (`agent-mcj-emergent-manager`) to standardize interactions with the `mcj-emergent` server.
- The skill must instruct the agent to use SSH for all remote commands.
- The skill must specify `emergent upgrade` as the standard procedure for updating the application.
- The skill must emphasize using the `emergent` CLI for all other management tasks (start, stop, status).

**Non-Goals:**
- Modifying the `emergent` CLI tool itself.
- Changing the SSH key setup or access control for the server.
- Automating the decision to upgrade; the agent will only execute the upgrade when instructed by the user.

## Decisions

- **Skill Location**: The skill will be located at `.agents/skills/agent-mcj-emergent-manager/SKILL.md`.
- **Interaction Model**: All interactions with the server will be funneled through `ssh mcj-emergent '<command>'`. This ensures that all commands are executed in the context of the remote server. The agent's SSH keys are pre-configured, so no password or interactive login is required.
- **CLI Focus**: The skill will explicitly forbid running raw docker commands or systemd commands directly, enforcing the use of the `emergent` CLI wrapper to ensure consistent and safe operations.

## Risks / Trade-offs

- **Risk: `emergent` CLI changes**: The commands or flags for the `emergent` CLI might change in future versions, making the skill outdated.
  - **Mitigation**: The skill will include a note advising the agent to run `ssh mcj-emergent 'emergent --help'` if it encounters unexpected errors, to dynamically learn the available commands.
- **Risk: Network Connectivity**: The agent's connection to the `mcj-emergent` server could fail.
  - **Mitigation**: The skill will instruct the agent to report connection errors clearly to the user and to not attempt repeated retries without user confirmation.
