## ADDED Requirements

### Requirement: Daemon runs inside the sandbox container
The workspace container image SHALL include a `pyrunner.py` daemon script that starts automatically at container boot. The daemon SHALL pre-import the `emergent` SDK and any other standard SDK dependencies so they are resident in memory before the first script execution request arrives.

#### Scenario: Daemon starts at container boot
- **WHEN** a sandbox container is started
- **THEN** `pyrunner.py` SHALL be running as a background process within 5 seconds of container start

#### Scenario: Daemon survives container idle period
- **WHEN** a container has been running in the warm pool for any duration
- **THEN** the daemon SHALL still be running and ready to accept requests

### Requirement: Scripts execute in isolated forked child processes
The daemon SHALL fork a child process for each script execution request. Each child SHALL run the script in full OS-level isolation with its own address space.

#### Scenario: Child process isolation from daemon
- **WHEN** a script calls `sys.exit(1)` or raises an unhandled exception
- **THEN** the daemon process SHALL remain running and ready for subsequent requests

#### Scenario: Child process crash does not affect daemon
- **WHEN** a script triggers a fatal signal (e.g. segfault in a C extension) inside the child
- **THEN** the daemon process SHALL remain alive and process the next request normally

#### Scenario: Child inherits pre-loaded module cache
- **WHEN** the daemon forks a child for script execution
- **THEN** the child SHALL have the `emergent` SDK already importable without re-importing it from disk (inherited via copy-on-write fork)

### Requirement: Per-invocation environment variable injection
The daemon SHALL accept per-call environment variables as part of the execution request and apply them to the child process only, without affecting the daemon's own environment or any other concurrent or future child.

#### Scenario: Session credentials injected into child
- **WHEN** a run_python request includes `MEMORY_API_KEY` and `MEMORY_API_URL` values
- **THEN** the child process SHALL have those values in its environment when the script runs

#### Scenario: Daemon environment remains unpolluted
- **WHEN** a child completes execution
- **THEN** the daemon's own `os.environ` SHALL NOT contain the injected credentials from that call

#### Scenario: Different sessions use different credentials
- **WHEN** two successive script executions are sent with different `MEMORY_API_KEY` values
- **THEN** each child process SHALL use only its own injected key and not the other session's key

### Requirement: FIFO-based IPC between Go tool layer and daemon
The daemon SHALL communicate with the Go tool layer via two named FIFOs inside the container: `/tmp/pyrunner.in` (request) and `/tmp/pyrunner.out` (response). The request SHALL be a single JSON line containing the script path and env vars. The response SHALL be a single JSON line containing exit code and duration.

#### Scenario: Successful script execution reported via FIFO
- **WHEN** a script completes successfully
- **THEN** the daemon SHALL write a JSON response to `/tmp/pyrunner.out` with `exit_code: 0` and a `duration_ms` value

#### Scenario: Non-zero exit code reported via FIFO
- **WHEN** a script exits with a non-zero code
- **THEN** the daemon SHALL write a JSON response to `/tmp/pyrunner.out` with the actual `exit_code`

#### Scenario: FIFO files present after daemon starts
- **WHEN** the daemon starts
- **THEN** both `/tmp/pyrunner.in` and `/tmp/pyrunner.out` SHALL exist as named FIFOs in the container filesystem

### Requirement: run_python tool dispatches to daemon with cold-start fallback
The `buildRunPythonTool` Go function SHALL attempt to dispatch script execution through the daemon FIFO. If the daemon FIFO is not present or does not respond within the configured timeout, the tool SHALL fall back to the existing cold `python3` invocation path.

#### Scenario: Daemon path used when FIFO is present
- **WHEN** `/tmp/pyrunner.in` exists in the container and the daemon is responsive
- **THEN** the script SHALL be executed via daemon fork, not via a fresh `python3` process

#### Scenario: Fallback to cold python3 when FIFO absent
- **WHEN** `/tmp/pyrunner.in` does not exist in the container
- **THEN** the tool SHALL execute the script by spawning `python3 /tmp/_agent_run.py` directly (existing behaviour)

#### Scenario: Fallback on daemon timeout
- **WHEN** the daemon FIFO write or read exceeds the configured invocation timeout
- **THEN** the tool SHALL return an error to the agent (timeout behaviour consistent with current cold-path timeout)

#### Scenario: Script output (stdout/stderr) still captured
- **WHEN** a script produces output on stdout or stderr
- **THEN** the tool SHALL return that output to the agent caller, regardless of whether the daemon path or fallback path was used
