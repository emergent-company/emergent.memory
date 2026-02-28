## Context

The `emergent` CLI requires a project token for most of its commands to interact with the API. This token is currently passed via a `--project-token` flag. This design outlines how the CLI will automatically load this token from `.env` files to improve user experience.

## Goals / Non-Goals

**Goals:**
- Implement a mechanism in the Go-based CLI to load environment variables from `.env` and `.env.local` files.
- Automatically configure the project context if `EMERGENT_PROJECT_TOKEN` and `EMERGENT_PROJECT_ID` are found, so project-scoped commands don't require the `--project-id` argument.
- Inform the user which project is being used via `EMERGENT_PROJECT_NAME`.
- Ensure that explicit command-line flags take precedence over environment variables.
- Add an `emergent projects set` command to interactively or explicitly select a project.
- Automatically fetch or generate a project token and write the token, project ID, and project name to `.env.local` when `emergent projects set` is used.
- Scope `emergent traces` to the active project context automatically.

**Non-Goals:**
- This design does not cover completely managing all API tokens, but focuses on the `EMERGENT_PROJECT_TOKEN` and `EMERGENT_PROJECT_ID` variables for local development context.
- We will not overwrite `.env` if `.env.local` is preferred for machine-specific secrets.

## Decisions

- **Dependency**: We will use the `github.com/joho/godotenv` Go package to handle the loading of `.env` files. It is a well-maintained and widely used library for this purpose.
- **Loading Order**: The CLI will attempt to load `.env.local` first. If it doesn't exist or doesn't contain the token, it will then attempt to load `.env`. The `godotenv.Load()` function can be called multiple times, and it will not override variables that are already set, so we will call `godotenv.Load(".env.local")` followed by `godotenv.Load(".env")`. This effectively prioritizes `.env.local`.
- **Variable Names**: The specific environment variables the CLI will look for are `EMERGENT_PROJECT_TOKEN`, `EMERGENT_PROJECT_ID`, and `EMERGENT_PROJECT_NAME`.
- **Precedence Logic**:
    1. The CLI will first check for the `--project-id` flag (and `--project-token` or global auth context).
    2. If the flag is not provided, it will check for `EMERGENT_PROJECT_ID` and `EMERGENT_PROJECT_TOKEN` loaded from `.env.local` or `.env`.
    3. If neither is found, commands that require a project context will fail with an informative error message, as they do now.
- **Implementation Location**: This loading logic will be added to the root command's `PersistentPreRun` function (or equivalent initialization entry point) in the Cobra command structure of the CLI, ensuring it runs before any specific command logic. We'll also update `resolveProjectID` to transparently use the loaded `EMERGENT_PROJECT_ID`.
- **Project Set Command**: The `emergent projects set [project]` command will use the existing authentication and API to fetch a list of available projects (if no argument is provided) using an interactive prompt. Once a project is selected (or provided as an argument), it will fetch an existing active token or generate a new one via the API, and then write `EMERGENT_PROJECT_TOKEN=...`, `EMERGENT_PROJECT_ID=...`, and `EMERGENT_PROJECT_NAME=...` to the `.env.local` file in the current directory, appending or updating the variables if the file already exists.
- **User Notification**: When executing a project-scoped command and the CLI successfully reads the project context from an env file, it will print a concise message, e.g., `Using project context: My Project (from .env.local)` before outputting the command results.
- **Traces Project Scoping**: The `emergent traces` command will use `resolveProjectID` (or similar logic) to check if a project context is active. If active, it will automatically append `.project.id = "<id>"` to the TraceQL query to filter traces for that specific project.

## Risks / Trade-offs

- **Risk: User Confusion**: A user might have a token in their `.env` file and forget about it, leading to confusion about which project the CLI is targeting.
  - **Mitigation**: The CLI will print a notice (e.g., "Using project token from .env.local") when it successfully loads a token from a file. This provides transparency to the user.
- **Risk: Performance**: A negligible performance overhead will be added due to file system access on every CLI execution.
  - **Mitigation**: This is an acceptable trade-off for the significant improvement in user experience. The file I/O is minimal and will not be noticeable.
