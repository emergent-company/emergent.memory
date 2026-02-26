---
description: 'Instructions for workspace management, including logging, process management, and running scripts.'
applyTo: '**'
---
# Coding Agent Instructions

This document provides instructions for interacting with the workspace, including logging, process management, and running scripts.

## 1. Logging

Log files are stored in `logs/` (root directory):

```
logs/
├── server/
│   ├── server.log          # Main server log (INFO+)
│   ├── server.error.log    # Server errors only
│   └── server.debug.log    # Debug output (dev only)
```

Read log files directly or use the **SigNoz MCP** for observability.

## 2. Process Management

Services are managed via **Taskfile tasks**.

*   **Start server with hot reload (foreground):**
    ```bash
    task dev
    ```

*   **Start server in background:**
    ```bash
    task start
    ```

*   **Stop background server:**
    ```bash
    task stop
    ```

*   **Check server status:**
    ```bash
    task status
    curl http://localhost:3002/health
    ```

### Hot Reload

The Go server uses `air` for hot reload. **Do not restart after code changes** — changes are picked up automatically in ~2 seconds.

**Restart only when:**
- Adding new fx modules to `cmd/server/main.go`
- Changing environment variables
- After `go mod tidy`
- Server is not responding

## 3. Running Scripts and Tests

All backend build/test/lint tasks use `task` (Taskfile):

```bash
task build              # Build server binary
task test               # Unit tests
task test:e2e           # E2E tests
task lint               # Go linter
task migrate:up         # Run migrations
task migrate:status     # Check migration status
```

For frontend tasks, use `pnpm` in `/root/emergent.memory.ui`:

```bash
cd /root/emergent.memory.ui
pnpm run dev            # Start Vite dev server
pnpm run build          # Build for production
pnpm run test           # Unit tests
```
