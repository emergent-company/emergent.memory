## Why

The project has fully migrated from NestJS to a Go backend (`apps/server-go`), but `openspec/project.md` — the primary AI context file — still describes NestJS, TypeORM, and `apps/server`. This causes AI agents to reference wrong patterns, wrong paths, and wrong tools when working on the backend. Additionally, `.opencode/instructions.md` has one stale NestJS reference.

## What Changes

- **Update `openspec/project.md`**: Replace entire backend tech stack description (NestJS/TypeORM → Go/Echo/Bun/fx/ADK-Go), update app path (`apps/server` → `apps/server-go`), update monorepo structure, update testing section (Go testify suites), update git branch (`master` → `main`), update architecture patterns
- **Update `.opencode/instructions.md`**: Remove stale "(NestJS)" timing reference in hot-reload section
- **Update `AGENTS.md`**: Verify and fix any remaining inconsistencies

## Capabilities

### New Capabilities
<!-- None - this is a documentation-only change -->

### Modified Capabilities
- `openspec-workflow`: The project context file (`openspec/project.md`) that AI agents use is being updated to accurately reflect the current Go backend

## Impact

- AI agents reading `openspec/project.md` will now get accurate backend architecture info
- No code changes — documentation only
- Affects: `openspec/project.md`, `.opencode/instructions.md`, `AGENTS.md`
