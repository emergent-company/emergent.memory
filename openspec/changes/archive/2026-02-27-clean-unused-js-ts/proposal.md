## Why

The project contains unused JavaScript and TypeScript files and dependencies which bloat the repository, increase build times, and add unnecessary complexity. Cleaning this up will simplify the codebase, and migrating critical remaining JS/TS components to Go (where appropriate) will unify the backend ecosystem, leveraging Go's performance and static typing for better maintainability.

## What Changes

- Identify and remove unused JavaScript and TypeScript files across the project.
- Remove orphaned NPM dependencies from `package.json` files.
- Evaluate remaining JS/TS backend scripts or services and port them to Go if they are critical and better suited for the Go ecosystem.
- Update build scripts and CI/CD pipelines to reflect the removal of JS/TS components.

## Capabilities

### New Capabilities
- `js-ts-cleanup`: Defines the process and criteria for identifying and removing unused JS/TS assets.
- `go-porting`: Guidelines and requirements for porting legacy JS/TS services to Go.

### Modified Capabilities

## Impact

- Significant reduction in the number of JS/TS files and dependencies.
- Potential performance and maintainability improvements by porting to Go.
- Changes to `package.json`, `tsconfig.json`, and build scripts.
- Developers will need to adapt to the Go-centric backend where applicable.
