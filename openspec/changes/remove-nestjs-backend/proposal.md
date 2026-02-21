## Why

The project has successfully migrated its primary backend services to Go (`apps/server-go`). The legacy NestJS backend (`apps/server`) is now obsolete and unused. Removing it will significantly reduce repository size, simplify the dependency tree, speed up CI/CD pipelines, and eliminate architectural confusion for developers.

## What Changes

- Delete the `apps/server` directory and all its contents entirely.
- Remove NestJS-specific packages from the root `package.json` (e.g., `@nestjs/*`, `class-validator`, `class-transformer`, `@nestjs/passport`, etc.).
- Update workspace configurations (`nx.json`, workspace scripts) to remove targets related to the old `server` project.
- Remove the NestJS service from Docker configurations (`docker-compose.yml`, `Dockerfile`s) and CI/CD workflows.
- Identify and remove any shared libraries in `libs/` that were exclusively consumed by the NestJS backend.
- Clean up any lingering references to `@server` or `apps/server` in the `admin` frontend or other workspace packages.

## Capabilities

### New Capabilities

- `nestjs-cleanup`: Defines the technical requirements for removing the NestJS framework, including dependency cleanup, workspace configuration updates, and infrastructure changes.

### Modified Capabilities

- `environment-configuration`: Update environment setup and scripts to only spin up the Go backend and remove NestJS environment variables.

## Impact

- **Codebase:** Removes thousands of lines of legacy TypeScript backend code.
- **Dependencies:** Significantly shrinks the `node_modules` size by dropping heavy NestJS and related libraries.
- **Infrastructure:** Simplifies local development scripts (e.g., `pnpm run workspace:start`) and Docker compositions.
- **CI/CD:** Reduces build and test times by eliminating the Node.js backend matrix.
