## Why

Currently, to use the `emergent` CLI for project-specific operations, a user must manually pass a project token via a command-line flag. This is repetitive and cumbersome for developers working within a single project directory. By automatically reading the project token from `.env` or `.env.local` files, we can streamline the developer experience and make the CLI more intuitive to use.

## What Changes

- The `emergent` CLI will be modified to automatically search for and load `.env` and `.env.local` files in the current working directory upon execution.
- If `EMERGENT_PROJECT_TOKEN` and `EMERGENT_PROJECT_ID` variables are found in these files, their values will be used to configure the project context (token and project ID) for all subsequent CLI operations, eliminating the need for the `--project-id` flag on project-scoped commands.
- The CLI will print an informative message indicating which project is being used (e.g., using `EMERGENT_PROJECT_NAME` if available).
- The existing `--project-token` and `--project-id` flags will remain and will override any values found in the environment files if provided.
- A new CLI command `emergent projects set [project name or ID]` will be added. This command will allow users to select a project (interactively or via argument), generate/fetch a project token, and automatically save `EMERGENT_PROJECT_TOKEN`, `EMERGENT_PROJECT_ID`, and `EMERGENT_PROJECT_NAME` to a `.env.local` file.
- The `traces` commands will be updated to respect the project context. If a project context is active (via flag or environment file), the `traces` queries will automatically filter results to only show traces for that specific project.

## Capabilities

### New Capabilities
- `cli-auto-project-context`: Automatically configure the CLI's project context by reading a token from local `.env` files.
- `cli-project-set-command`: A new `emergent projects set` command to interactively select a project and configure the local environment with its token.
- `cli-traces-project-scoping`: Automatically scope `traces` commands to the active project context if one is set.

### Modified Capabilities
- No existing capabilities are being modified at the requirement level.

## Impact

This change will affect the initialization logic of the `emergent` CLI application. It will introduce a new dependency for loading `.env` files (e.g., `godotenv`). This is a non-breaking enhancement that improves usability.
