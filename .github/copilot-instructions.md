# Coding Agent Instructions

This document provides instructions for interacting with the workspace, including logging, process management, and running scripts.

## Domain-Specific Pattern Documentation

Before implementing new features, **always check** these domain-specific AGENT.md files to understand existing patterns and avoid recreating functionality:

| File                                                      | Domain              | Key Topics                                                                          |
| --------------------------------------------------------- | ------------------- | ----------------------------------------------------------------------------------- |
| `/root/emergent.memory.ui/src/components/AGENT.md`        | Frontend Components | Atomic design (atoms/molecules/organisms), DaisyUI + Tailwind, available components |
| `/root/emergent.memory.ui/src/hooks/AGENT.md`             | Frontend Hooks      | `useApi` (MUST use for all API calls), all 33+ hooks categorized                    |
| `apps/server-go/AGENT.md`                                 | Go Backend          | fx modules, Echo handlers, Bun ORM, job queues                                      |

> **Frontend repo**: The React admin lives at `/root/emergent.memory.ui` (remote: `emergent-company/emergent.memory.ui`). It is a standalone Vite project — not in this monorepo.

**When to read these files:**

- Before creating new components → Read `components/AGENT.md`
- Before creating new hooks → Read `hooks/AGENT.md`
- Before creating new API endpoints → Read `modules/AGENT.md`
- Before creating new database entities → Read `entities/AGENT.md`

## Primary References

- **`AGENTS.md`** (root) - Quick reference for build, lint, test, and pattern links
- **`.opencode/instructions.md`** - Workspace operations (logging, process management, testing)
- **`docs/testing/AI_AGENT_GUIDE.md`** - Comprehensive testing guidance

## 1. Logging

Log files are stored in `logs/` (root directory).

- **Server logs:** `logs/server/server.log`, `logs/server/server.error.log`

## 2. Process Management

Services use **Taskfile tasks** for process management.

- **Start with hot reload (foreground):**
  ```bash
  task dev
  ```

- **Start in background:**
  ```bash
  task start
  ```

- **Stop background server:**
  ```bash
  task stop
  ```

- **Check server status:**
  ```bash
  task status
  ```

The Go server uses `air` for hot reload. **Do not restart after code changes** — changes are picked up automatically.

## 3. Running Scripts and Tests

All backend tasks use `task` (Taskfile):

```bash
task build              # Build server binary
task test               # Unit tests
task test:e2e           # API e2e tests
task test:integration   # Integration tests
task test:coverage      # Tests with coverage
task lint               # Go linter
task migrate:up         # Run migrations
```

For frontend tasks, use `pnpm` in `/root/emergent.memory.ui`:

```bash
cd /root/emergent.memory.ui
pnpm run test           # Unit tests
pnpm run build          # Production build
```

For comprehensive testing guidance, refer to **`docs/testing/AI_AGENT_GUIDE.md`**.

## 4. EPF Working Directory

When working with the Emergent Product Framework (EPF), **all temporary working documents** (analysis, summaries, session notes, implementation docs) go in:

```
docs/EPF/.epf-work/
```

**Key Rules:**

- ✅ **DO** create subdirectories by session/topic: `docs/EPF/.epf-work/skattefunn-wizard-selection/`
- ✅ **DO** place ALL EPF-related working documents there (summaries, analysis, decisions, session notes)
- ❌ **DON'T** create `.epf-work/` at repository root
- ❌ **DON'T** create `.epf-work/` inside `_instances/`
- ❌ **DON'T** place working documents in canonical EPF directories (schemas, wizards, templates)

**Why one location?**

- Single source of truth for all EPF working documents
- Easy to find session notes and analysis
- Clear separation: `docs/EPF/` = canonical framework, `docs/EPF/.epf-work/` = temporary work
- Version-controlled with EPF for context preservation

See `docs/EPF/.github/copilot-instructions.md` for complete EPF contribution guidelines.
