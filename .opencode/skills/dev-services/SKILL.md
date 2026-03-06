---
name: dev-services
description: Start, stop, restart, and check status of local development services. Use when you need to restart the server after code changes, check if services are running, or recover from a broken dev environment.
license: MIT
metadata:
  author: openspec
  version: "1.0"
---

Manage local development services for this project.

**Input**: Optionally specify an action (`start`, `stop`, `restart`, `status`) and a service name. If omitted, defaults to restarting all services.

## Decision Guide

| Situation | Action |
|-----------|--------|
| Changed backend/server code | `restart` server |
| Changed worker/queue code | `restart` worker |
| Frontend hot-reload handles it | no restart needed |
| Everything broken / fresh start | `restart --clean` |
| Just need to check if running | `status` |
| Need to stop everything | `stop` |

## How to Run

All commands are in `.opencode/skills/dev-services/scripts/`. Run from the project root.

```bash
bash .opencode/skills/dev-services/scripts/status.sh       # check what's running
bash .opencode/skills/dev-services/scripts/restart.sh      # restart all services
bash .opencode/skills/dev-services/scripts/restart.sh --clean  # clean restart
bash .opencode/skills/dev-services/scripts/stop.sh         # stop all services
bash .opencode/skills/dev-services/scripts/start.sh        # start from stopped state
```

## After Making Code Changes

```bash
bash .opencode/skills/dev-services/scripts/restart.sh
bash .opencode/skills/dev-services/scripts/status.sh
```

## Guardrails

- Always run `status.sh` after a restart to confirm services came up
- If a port is in use, `restart.sh` handles it — do not run ad-hoc `pkill` commands
- If `restart.sh` fails twice, run `stop.sh` then `start.sh` instead
- Check log files (listed in `status.sh` output) before concluding a service is broken
