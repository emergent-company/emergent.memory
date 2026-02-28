## Context

The agent operates inside a workspace where local development services (like backend servers, databases, frontend apps) are managed via standard commands (`npm run workspace:*`, `task *`). Often, the agent doesn't know how to verify if a service is running or how to restart it if needed, leading to failures or incorrect manual actions.

## Goals / Non-Goals

**Goals:**
- Provide a standardized skill (`agent-dev-manager`) in `.agents/skills/` that the agent can read to understand how to interact with local services.
- Define explicit commands for starting, stopping, restarting, and checking the status of services using `Taskfile.yml` and `package.json`.
- Provide information on default ports and configurations (e.g., frontend on 5176, server on 3002).

**Non-Goals:**
- Modifying the underlying startup scripts or configurations.
- Creating new orchestration tools.

## Decisions

- **Location**: The skill will be placed in `.agents/skills/agent-dev-manager/SKILL.md` to align with the new structure.
- **Content Strategy**: The skill will instruct the agent to use `task status` (Go server status), `npm run workspace:start`, `npm run workspace:stop`, and other standard commands found in `GEMINI.md` and `AGENTS.md`.

## Risks / Trade-offs

- **Risk: Outdated Skill Content**: The project's tooling might change (e.g., from `npm` to `pnpm` or `task` modifications).
  - *Mitigation*: The skill will advise checking `Taskfile.yml` and `package.json` dynamically if standard commands fail.
