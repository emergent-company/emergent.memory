## Why

Currently, the AI agent interacts with the project's local development environment services manually. To streamline this process and prevent errors or misinterpretations, we need a dedicated skill that teaches the agent how to start, stop, restart, and check the status of local services. This ensures the agent is aware of configuration (ports, etc.) and uses the correct commands to manage the environment effectively.

## What Changes

- Propose a standard agent skill for managing local development services.
- Document how to check if services are running, how to start, stop, or restart them.
- Define what commands and configuration files the agent needs to rely on (e.g., Taskfile.yml, package.json).

## Capabilities

### New Capabilities
- `agent-dev-manager`: A new skill teaching the agent how to manage local development environment services (start, stop, status, restart) and how to identify running configuration like ports.

### Modified Capabilities

## Impact

This new skill will reside in `.agents/skills/agent-dev-manager/SKILL.md`. It improves agent autonomy and stability without affecting any production application code.
