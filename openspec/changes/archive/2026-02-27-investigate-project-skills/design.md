## Context

Currently, the AI agent interacts with project commands via generic bash execution without pre-configured understanding of the environment's specific capabilities. The project utilizes `task` (Taskfile.yml) for essential project actions like building, testing, linting, migration, and querying OpenTelemetry traces. Additionally, there are numerous debugging scripts under `scripts/`. Creating agent skills corresponding to these processes reduces tool call friction, standardizes interactions, and ensures correct arguments are supplied when debugging or managing the backend database.

## Goals / Non-Goals

**Goals:**
- Provide the agent with specialized instructions for managing common project workflows (tests, migrations, traces, scripting).
- Ensure skills encompass instructions on using existing Taskfile commands and `scripts/` correctly.

**Non-Goals:**
- Refactoring the underlying Taskfile or scripts.
- Modifying production code or application logic.

## Decisions

- **Implement Agent Skills Using Built-in Tools**: The agent will leverage `run_shell_command` natively within the skills to perform these actions, but the skills will provide the "how-to" knowledge required.
- **Categorize Skills by Domain**: We will create discrete skills (`agent-test-runner`, `agent-db-migrations`, `agent-trace-viewer`, `agent-script-runner`) to keep contexts isolated and manageable.
- **Trace Viewer Workflow**: Ensure the `agent-trace-viewer` understands that `emergent traces list` requires CLI args and might output large trace logs, providing guidance on how to filter and read traces properly.

## Risks / Trade-offs

- **Risk: Obsolete Skills**: If `Taskfile.yml` changes significantly or `scripts/` are refactored, the agent skills might become outdated.
  - *Mitigation*: The skills should primarily instruct the agent to inspect the current `Taskfile.yml` and `scripts/` first to learn dynamic arguments before executing them blindly.
- **Trade-off: Setup Overhead**: Setting up these skills takes initial time, but pays off in repetitive diagnostic/testing workflows.
