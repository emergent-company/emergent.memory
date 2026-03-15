<!-- Baseline: go build ./... passes clean (no pre-existing failures) -->
<!-- Discovery: run_python uses `emergent-memory-python-sdk:latest` NOT `memory-workspace:latest`.
     The image is built from:
       - sdk/python/Dockerfile (local dev)
       - tools/cli/internal/installer/templates.go GetPythonSDKDockerfile() (production)
     gvisor_provider.go always overrides CMD with ["sleep", "infinity"] (line 165).
     pyrunner.py must be added to the Python SDK image with an ENTRYPOINT wrapper. -->

## 1. Locate workspace image Dockerfile

- [x] 1.1 Find which Dockerfile produces `memory-workspace:latest` (check `sdk/python/Dockerfile`, any compose files, and Makefile/Taskfile build targets)
- [x] 1.2 Confirm the image name constant in `gvisor_provider.go` and verify it matches the Dockerfile being modified

## 2. Add pyrunner.py to the workspace image

- [x] 2.1 Write `pyrunner.py` (~60 lines): create `/tmp/pyrunner.in` and `/tmp/pyrunner.out` FIFOs, pre-import `emergent` SDK, enter a read loop that forks a child per request
- [x] 2.2 Implement child-side logic: receive JSON request from FIFO, call `os.environ.update(injected_env)`, exec the script path, write JSON result (`exit_code`, `duration_ms`) to `/tmp/pyrunner.out`
- [x] 2.3 Implement parent-side daemon loop: after fork, `waitpid` on child, read child exit status, write response JSON to `/tmp/pyrunner.out`
- [x] 2.4 Add `pyrunner.py` into the workspace Dockerfile (`COPY pyrunner.py /usr/local/bin/pyrunner.py`)
- [x] 2.5 Update the container entrypoint in the Dockerfile to start `pyrunner.py` in background before the idle loop (e.g. `CMD ["sh", "-c", "python3 /usr/local/bin/pyrunner.py & sleep infinity"]`)

## 3. Update buildRunPythonTool in workspace_tools.go

- [x] 3.1 Add a helper function `daemonAvailable(ctx, provider, containerID) bool` that checks for `/tmp/pyrunner.in` existence via a fast `Exec` (e.g. `test -p /tmp/pyrunner.in`)
- [x] 3.2 Add a helper function `runViaDaemon(ctx, provider, containerID, scriptPath, sessionEnv, timeoutMs) (string, error)` that writes JSON request to `/tmp/pyrunner.in` via `Exec` and reads response from `/tmp/pyrunner.out`
- [x] 3.3 Modify `buildRunPythonTool` to call `daemonAvailable` first; if true dispatch via `runViaDaemon`, else fall through to the existing cold `python3` path
- [x] 3.4 Add a log line (warn level) when the fallback path is taken, noting that the daemon FIFO was not found
- [x] 3.5 Ensure stdout/stderr from the child script are captured and returned to the agent caller on both the daemon path and the fallback path

## 4. Testing

- [x] 4.1 Build the updated workspace Docker image locally (`docker build -t emergent-memory-python-sdk:latest sdk/python/`)
- [x] 4.2 Manually verify daemon starts: `docker run --rm emergent-memory-python-sdk:latest sh -c 'sleep 2 && test -p /tmp/pyrunner.in && echo OK'`
- [x] 4.3 Write and run a minimal Go unit test or integration test for `runViaDaemon` that sends a trivial script and asserts exit code 0 and non-empty output
- [x] 4.4 Test the fallback path: start a container without the daemon and confirm `run_python` still works via the cold path
- [x] 4.5 Test credential isolation: send two requests with different `MEMORY_API_KEY` values and verify each child receives the correct key (can be done with a script that prints `os.environ.get("MEMORY_API_KEY")`)

## 5. Cleanup and observability

- [x] 5.1 Add an optional `daemon_hit` boolean field to the `run_python` tool's log output so daemon vs. fallback usage is observable in server logs
- [x] 5.2 Confirm warm-pool containers cycle out and are replaced by newly-built containers after image rebuild (document or script this in the deployment notes)
