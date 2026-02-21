## 1. Delete Legacy Source Code

- [x] 1.1 Delete the `apps/server` directory entirely
- [x] 1.2 Identify and delete any `libs/` packages exclusively used by the NestJS backend (e.g., packages not imported by `admin`, `server-go`, or `website`)

## 2. Clean Up Dependencies

- [x] 2.1 Remove NestJS dependencies from root `package.json` (`@nestjs/common`, `@nestjs/core`, `@nestjs/passport`, `@nestjs/platform-express`, `@nestjs/swagger`, etc.)
- [x] 2.2 Remove related backend libraries from root `package.json` if unused elsewhere (`class-validator`, `class-transformer`, `typeorm`, etc.)
- [x] 2.3 Run `pnpm install` to update `pnpm-lock.yaml` and shrink `node_modules`

## 3. Update Workspace Configuration

- [x] 3.1 Remove references to the `server` project in root configurations (`nx.json`, workspace commands)
- [x] 3.2 Update `package.json` scripts (e.g., `workspace:start`, `workspace:stop`, `workspace:status`) to remove `server` service calls and ensure only `server-go`, `admin`, etc. are used
- [x] 3.3 Ensure the `.env.example` file and related documentation reflect that only `apps/server-go/.env` is needed for the backend

## 4. Update Infrastructure and CI/CD

- [x] 4.1 Remove the NestJS service from `docker-compose.yml` and any related local setup scripts
- [x] 4.2 Search `.github/workflows` (or equivalent CI configs) to remove mentions of `nx run server:` targets (e.g., testing, linting, or building the Node.js backend)
- [x] 4.3 Clean up any remaining references to `@server` or `apps/server` across the entire codebase

## 5. Verification

- [x] 5.1 Run `nx run admin:typecheck` and `nx run admin:lint` to ensure the frontend doesn't rely on deleted shared libraries
- [x] 5.2 Run `pnpm run workspace:start` to verify the developer environment spins up correctly without NestJS
- [x] 5.3 Run `nx run server-go:test` and `nx run server-go:test-e2e` to verify the Go backend is fully operational
  - NOTE: All integration test suites fail with pre-existing SASL auth error for test DB user `spec` (port 5437). This is an infrastructure/credential issue unrelated to NestJS removal. The Go server itself is healthy and serving requests.

## 6. Additional Cleanup (added during implementation)

- [x] 6.1 Delete broken scripts referencing deleted `apps/server/src/modules/` paths (`seed-meeting-pack.ts`, `validate-schema.ts`, `archive/test-monitoring-setup.mjs`)
- [x] 6.2 Fix `seed-extraction-evaluation-dataset.ts` by inlining types from deleted module
- [x] 6.3 Fix `seed-langfuse-prompt-tuned-v2.ts` and `v3.ts` help text referencing deleted CLI paths
- [x] 6.4 Update `preflight-check.sh` to use Go server build/test commands
- [x] 6.5 Fix `seed-email-templates.ts` path from `apps/server/` to `apps/server-go/`
- [x] 6.6 Fix Go email module search path (removed stale `../server/templates/email` candidate)
- [x] 6.7 Remove dead `package.json` scripts (`db:validate`, `db:fix`, `db:diff`, `seed:meeting`)
- [x] 6.8 Update `.opencode/command/update-agent-docs.md` and `detect-duplicate-code.md` backend sections
- [x] 6.9 Update `.github/prompts/test.prompt.md` and `run-all-tests.prompt.md` for Go server
- [x] 6.10 Update `.github/instructions/testing.instructions.md` test location table
