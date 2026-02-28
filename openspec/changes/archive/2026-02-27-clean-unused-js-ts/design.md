## Context

The project has accumulated unused JavaScript and TypeScript files and dependencies over time. This bloats the repository, increases build times, and adds unnecessary complexity to the codebase. Concurrently, there is an effort to unify the backend ecosystem by porting suitable legacy JS/TS services to Go.

## Goals / Non-Goals

**Goals:**
- Identify and remove unused JS/TS files.
- Remove orphaned NPM dependencies.
- Port critical, remaining JS/TS backend components to Go where beneficial.
- Update build pipelines to reflect these removals.

**Non-Goals:**
- Rewrite the entire frontend or core services that are actively maintained in JS/TS.
- Add new features to the existing JS/TS or Go services.

## Decisions

- **Unused Code Detection**: We will use tools like `ts-prune` or `knip` to systematically identify unused exports and files in the JS/TS projects.
- **Dependency Audit**: `npm-check` or similar tools will be used to identify unused dependencies in `package.json`.
- **Go Porting Strategy**: Only services that are small, standalone, or require performance improvements will be prioritized for porting to Go. Complex React/frontend code will remain untouched.

## Risks / Trade-offs

- **Risk**: Accidentally removing code that is used dynamically (e.g., via reflection or dynamic imports).
  - **Mitigation**: Thoroughly test the application after cleanup and rely on integration/e2e tests.
- **Risk**: Regressions introduced during the Go porting process.
  - **Mitigation**: Ported services must have equivalent test coverage in Go before the JS/TS versions are retired.
