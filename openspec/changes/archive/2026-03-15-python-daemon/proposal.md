## Why

Every `run_python` call inside an agent sandbox pays a full CPython cold-start cost (~200–400ms) to initialise the VM and import the `emergent` SDK before any user code runs. For agents that issue many sequential `run_python` calls this overhead accumulates significantly. We can eliminate it by keeping a pre-warmed Python interpreter running inside the container and forking a child process per invocation — giving full process isolation with zero import overhead.

## What Changes

- Add a `pyrunner.py` daemon (~50 lines) to the workspace Docker image that pre-imports the `emergent` SDK and waits on a FIFO for script paths to execute
- Modify the container entrypoint to start `pyrunner.py` as a background process alongside the existing idle loop
- Change `buildRunPythonTool` in `workspace_tools.go` to dispatch scripts through the daemon instead of spawning a fresh `python3` process per call
- Add a daemon health-check/fallback in `buildRunPythonTool` so a cold `python3` invocation is used if the daemon is not yet running (e.g. immediately after warm-pool checkout)
- Add a new `run_python_daemon` execution path in the `GVisorProvider.Exec` flow (or at tool level) that signals the daemon and reads results via a result FIFO

## Capabilities

### New Capabilities

- `python-daemon`: Pre-forking Python interpreter daemon that lives inside the sandbox container; pre-imports the `emergent` SDK; executes each script in an isolated fork with per-invocation env injection; communicates via in-container FIFOs

### Modified Capabilities

- (none — no existing spec-level requirements are changing; only the internal execution mechanism of `run_python` changes)

## Impact

- **workspace Docker image** (`sdk/python/Dockerfile` or a dedicated workspace image): new `pyrunner.py` added, entrypoint updated
- **`apps/server/domain/agents/workspace_tools.go`**: `buildRunPythonTool` updated to use daemon dispatch
- **`apps/server/domain/sandbox/gvisor_provider.go`**: no interface changes; only the command string sent via `Exec` changes
- No DB migrations, no new API endpoints, no breaking changes to the `Provider` interface
- Agents using `run_python` benefit automatically with no prompt or config changes required
