## 1. Update openspec/project.md

- [x] 1.1 Replace Tech Stack → Backend section: swap NestJS/TypeORM/Passport.js for Go/Echo/Bun/fx/ADK-Go, update runtime from Node.js to Go 1.25
- [x] 1.2 Update Monorepo Structure: change `apps/server` to `apps/server-go`, remove NestJS description
- [x] 1.3 Rewrite Backend Architecture section: replace NestJS module/controller/service/repository/TypeORM patterns with Go fx module/handler/store/Bun patterns
- [x] 1.4 Update Testing Strategy → Backend Tests: replace NestJS/Vitest backend tests with Go testify suite descriptions and tasks CLI commands
- [x] 1.5 Update Test Commands code block: change `nx run server:test` → `nx run server-go:test`, etc.
- [x] 1.6 Update Git Workflow → Branch Strategy: change `master` → `main`
- [x] 1.7 Grep for any remaining "NestJS", "TypeORM", "apps/server", "master" references and fix

## 2. Update .opencode/instructions.md

- [x] 2.1 Remove "(NestJS)" from hot-reload timing comment (line ~68: "~1.5 minutes (NestJS)")

## 3. Verify AGENTS.md

- [x] 3.1 Confirm AGENTS.md correctly references `apps/server-go/AGENT.md` (not apps/server)
- [x] 3.2 Confirm build/lint/test commands match actual nx targets for server-go

## 4. Validation

- [x] 4.1 Grep all three files for stale references ("NestJS", "TypeORM", "apps/server[^-]", "master")
- [x] 4.2 Confirm `openspec/project.md` Tech Stack section lists Go as the backend runtime
