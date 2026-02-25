## Context

The backend migrated fully from NestJS (TypeScript) to Go (`apps/server-go`). The `apps/server` directory no longer exists. `openspec/project.md` is the primary AI context file loaded when AI agents create artifacts — it still describes the old NestJS stack, which causes agents to reference wrong frameworks, paths, and tools. The `.opencode/instructions.md` has one stale NestJS reference in the hot-reload timing comment.

## Goals / Non-Goals

**Goals:**
- Replace `openspec/project.md` backend section to accurately describe Go/Echo/Bun/fx/ADK-Go stack
- Correct monorepo structure (`apps/server` → `apps/server-go`)
- Update testing section to reflect Go testify suites and tasks CLI
- Fix git branch reference (`master` → `main`)
- Remove stale NestJS timing comment from `.opencode/instructions.md`
- Verify `AGENTS.md` is consistent

**Non-Goals:**
- Rewriting frontend docs (React/Vite section is accurate)
- Touching code files
- Updating the many docs under `docs/` (separate effort if needed)
- Adding new documentation sections beyond what already exists

## Decisions

**D1: Edit in-place vs rewrite**
Rewrite `openspec/project.md` backend sections wholesale. The NestJS content is extensive and scattered — a targeted edit risks missing sections. A clean rewrite of affected sections is safer and clearer.

**D2: Scope to AI context files only**
Focus on `openspec/project.md`, `.opencode/instructions.md`, and `AGENTS.md` — the files AI agents actively read. Broader doc cleanup (README, RUNBOOK, CONTRIBUTING) is out of scope for this pass.

**D3: Keep structure, replace content**
Preserve the existing heading structure of `openspec/project.md` so readers can navigate it the same way. Only replace content within sections.

## Risks / Trade-offs

- **Risk**: Missing a NestJS reference in `openspec/project.md` → Mitigation: Search for "NestJS", "TypeORM", "apps/server" strings after editing
- **Risk**: Overwriting accurate content → Mitigation: Frontend sections, auth, domain context are still accurate — only touch Backend and Testing sections

## Migration Plan

1. Update `openspec/project.md`: Tech Stack (Backend), Monorepo Structure, Backend Architecture, Testing Strategy (Backend), Git branch
2. Update `.opencode/instructions.md`: Remove "(NestJS)" from hot-reload comment
3. Verify `AGENTS.md`: Confirm it's accurate (it already references `apps/server-go/AGENT.md`)
4. Search for any residual stale references across all three files
