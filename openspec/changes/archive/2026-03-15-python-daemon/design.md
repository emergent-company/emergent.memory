## Context

The `run_python` ADK tool in `apps/server/domain/agents/workspace_tools.go` currently works by:
1. Writing the script to `/tmp/_agent_run.py` via `provider.WriteFile` (one `docker exec` round-trip, base64-encode/decode)
2. Spawning `python3 /tmp/_agent_run.py` via `provider.Exec` (another `docker exec`)

Step 2 pays the full CPython cold-start cost on every call: VM init (~30–80ms), `site` module import (~20–40ms), and `emergent` SDK import (~150–300ms) before any user code runs. For an agent session that calls `run_python` five times, that is ~1–2 seconds of pure interpreter overhead.

The sandbox infrastructure already maintains a **warm pool** of pre-booted containers, so the container-level cold start is already eliminated. The remaining bottleneck is the per-call Python interpreter startup inside the container.

The container runs with `sleep infinity` as its CMD (or a custom cmd for MCP containers). The gVisor provider creates containers via Docker and runs commands inside them via `docker exec`. There is no persistent Python process in the container today.

## Goals / Non-Goals

**Goals:**
- Eliminate per-call Python VM init + SDK import overhead for `run_python`
- Maintain full process isolation between calls (each script runs in its own forked child process)
- Ensure a crash/segfault/`sys.exit()` in user code cannot kill the daemon or affect subsequent calls
- Per-invocation env var injection (`MEMORY_API_KEY`, `MEMORY_API_URL`) must still work correctly
- Graceful fallback to cold `python3` if the daemon is not running (e.g. immediately after container assignment from warm pool before daemon starts)
- No changes to the `Provider` interface, DB schema, or API surface
- No new Go dependencies

**Non-Goals:**
- Shared state between successive `run_python` calls (each call gets a clean fork — no shared globals)
- Supporting `pip install` inside the daemon
- Supporting interactive/REPL use cases
- Applying this optimisation to `run_go` (Go compilation dominates, startup is not the bottleneck)
- Supporting Firecracker or E2B providers (gVisor/Docker is the primary provider; daemon is image-level)

## Decisions

### D1: Fork-per-call, not exec()-per-call

**Decision**: The daemon uses `os.fork()` to create a child process for each script execution, rather than `exec()`ing code in the daemon process itself.

**Rationale**: `exec()` in the daemon's own process shares all global state (module caches, open file handles, class registries). A `sys.exit()` call or unhandled exception would kill the daemon. A segfault in a C extension would take down the entire daemon. Fork gives true OS-level isolation — child has its own address space (CoW), its own file descriptors, its own signal mask. The daemon's pre-loaded module cache is inherited read-only by the child (zero-copy due to CoW), so import time savings are fully realised without sharing mutable state.

**Alternative considered**: Socket-based server (nailgun-style). Rejected — more complex, requires serialising stdin/stdout over the socket, harder to handle timeouts and crashes cleanly.

**Alternative considered**: `exec()` with `importlib` state reset. Rejected — cannot fully reset C extension state; `sys.exit()` still kills daemon.

### D2: FIFO (named pipe) for daemon communication, not Unix sockets

**Decision**: Use two FIFOs inside the container (`/tmp/pyrunner.in`, `/tmp/pyrunner.out`) for request/response. The Go layer writes the script path + env vars to `.in`, reads exit code + timing from `.out`.

**Rationale**: FIFOs are available on all Linux container setups including gVisor (`runsc`). They require no additional Python dependencies. The protocol is trivially simple: one line in (JSON with `script` path and `env` dict), one line out (JSON with `exit_code` and `duration_ms`). `docker exec` can write to a FIFO with a simple shell command.

**Alternative considered**: Unix domain socket. More capable but also more complex — requires `socket` module, accept loop, framing protocol.

**Alternative considered**: Writing result to a temp file. Works but requires polling or inotify; FIFOs block cleanly.

### D3: Daemon started as part of container startup, not on first use

**Decision**: `pyrunner.py` is started in the background as part of the container entrypoint (`sleep infinity` replaced by a small shell wrapper that starts `pyrunner.py &` then `sleep infinity`), so the daemon is already running when a warm-pool container is assigned to an agent session.

**Rationale**: If we start it on first `run_python` call, the first call still pays a startup cost. Starting it at container creation time means by the time the container is assigned from the warm pool the daemon is warm and has pre-imported the SDK.

**Alternative considered**: Start daemon on first `run_python` call and pay startup cost once per session. Acceptable but wastes the warm-pool advantage.

### D4: Env var injection via FIFO request payload, not fork-inherited env

**Decision**: The Go tool layer sends `MEMORY_API_KEY` and `MEMORY_API_URL` as part of the FIFO request JSON. The daemon applies them to the child process environment before forking (using `os.environ` in the parent temporarily, then restoring, or applying in child before exec).

**Rationale**: The daemon's own process has no `MEMORY_API_KEY` at start time — credentials are injected per session via `sessionEnv` in `WorkspaceToolDeps`. If we relied on fork-inherited env the key would need to be in the daemon's env, which would mean different sessions running against the same container would share credentials — not acceptable. Sending the env per-call and applying it in the child before running user code keeps isolation correct.

**Implementation detail**: The child process calls `os.environ.update(injected_env)` before `exec()`ing the script, so the parent's environment is never polluted.

### D5: Fallback to cold python3 if daemon is not running

**Decision**: `buildRunPythonTool` first checks for the daemon FIFO at `/tmp/pyrunner.in`. If absent (daemon not yet started, or crashed), it falls back to the existing `python3 /tmp/_agent_run.py` cold-start path with a one-time log warning.

**Rationale**: Containers freshly created (not from warm pool, or just restarted) may not have the daemon running yet. A hard failure on missing daemon would be a regression. The fallback ensures zero behaviour change for the worst case.

## Risks / Trade-offs

**[Risk] gVisor fork() support** → `runsc` supports `fork()` in syscall filter. Verified: gVisor's seccomp profile allows `fork`/`clone`. If a future gVisor version restricts this, the fallback path covers it automatically.

**[Risk] FIFO deadlock if daemon crashes mid-write** → The Go layer sets a timeout on the FIFO read equal to `timeoutMs`. If the daemon dies, the FIFO read will block until timeout, then the tool returns an error. The fallback will be used on next call (daemon absent).

**[Risk] Multiple concurrent run_python calls to the same container** → The daemon is single-threaded and processes one fork at a time. Concurrent calls will queue at the FIFO. Since each `run_python` call within a single agent session is sequential (the LLM waits for the result before the next call), this is not a real-world concern for the current use case.

**[Risk] Daemon restart after crash** → If user code triggers a segfault in the child, only the child dies (fork isolation). But if the daemon itself crashes (OOM, signal), the FIFO disappears and the fallback kicks in for that call. The daemon does not auto-restart. A follow-up could add a supervisor, but given the fallback exists this is acceptable for v1.

**[Trade-off] Slightly more complex container entrypoint** → The entrypoint goes from `sleep infinity` to a two-line shell script. This is minimal complexity but worth noting as a change to the container image.

**[Trade-off] First call after container creation still pays daemon startup** → The daemon starts at container boot, not before. With warm pool enabled (default size=2), the container will have been running for seconds before assignment, so the daemon will be fully warm. Without warm pool, the first call still pays daemon startup (~300ms) on top of container creation, but this is a one-time cost per container lifetime.

## Migration Plan

1. Add `pyrunner.py` to the workspace image (new file, no breaking changes)
2. Update the container entrypoint in the Dockerfile to start `pyrunner.py &`
3. Rebuild the `memory-workspace:latest` image (or whichever image is configured)
4. Update `buildRunPythonTool` in `workspace_tools.go` to use the daemon path with fallback
5. Existing containers in the warm pool will not have the daemon (they were built with the old image) — they will use the fallback path automatically until the pool cycles through with newly-built containers
6. No rollback mechanism needed: the fallback is always present, so a broken daemon simply degrades to the previous behaviour

## Open Questions

- Should `pyrunner.py` be part of the Python SDK image (`sdk/python/Dockerfile`) or a separate workspace-specific image? Current structure has `memory-workspace:latest` as the default image — we should confirm which Dockerfile produces that image.
- Should we add a `run_python_fast: true` metric/log field so we can measure daemon hit rate in production logs?
