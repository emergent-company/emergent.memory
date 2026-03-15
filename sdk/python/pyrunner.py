#!/usr/bin/env python3
"""pyrunner — pre-forking Python daemon for zero-cold-start script execution.

Sits inside an agent sandbox container and pre-imports the emergent SDK.
Each script execution request is handled in a forked child process for full
OS-level isolation.  Communication with the Go host uses two named FIFOs:

  /tmp/pyrunner.in   — Go writes a JSON request  {script, env}
  /tmp/pyrunner.out  — daemon writes a JSON response
                       {exit_code, duration_ms, stdout, stderr}

Stdout/stderr of the child are captured via pipes and included in the
FIFO response so the Go layer can return them to the agent caller.
"""

import io
import json
import os
import signal
import sys
import time
import traceback

FIFO_IN = "/tmp/pyrunner.in"
FIFO_OUT = "/tmp/pyrunner.out"
MAX_OUTPUT = 50 * 1024  # 50KB — matches Go-side maxOutputBytes

# ---------------------------------------------------------------------------
# Pre-import the emergent SDK so it is warm in memory before any fork.
# ---------------------------------------------------------------------------
try:
    import emergent          # noqa: F401 — warm the import cache
except ImportError:
    pass  # SDK not installed; daemon still works, scripts just import normally


def _make_fifo(path: str) -> None:
    """Create a named FIFO, removing any stale file first."""
    try:
        os.unlink(path)
    except FileNotFoundError:
        pass
    os.mkfifo(path)


def _run_child(script_path: str, env_vars: dict,
               stdout_fd: int, stderr_fd: int) -> None:
    """Execute *script_path* inside the **child** process.

    Redirects stdout/stderr to the provided file descriptors (pipe write
    ends) so the parent can capture the output.

    This function never returns — it always calls ``os._exit``.
    """
    try:
        # Redirect stdout/stderr to the pipe write ends
        os.dup2(stdout_fd, 1)
        os.dup2(stderr_fd, 2)

        # Inject per-call environment variables
        if env_vars:
            os.environ.update(env_vars)

        # Reset signal handlers inherited from the daemon
        signal.signal(signal.SIGTERM, signal.SIG_DFL)
        signal.signal(signal.SIGINT, signal.SIG_DFL)

        # Execute the script
        with open(script_path) as f:
            code = f.read()

        # Use compile + exec so tracebacks show the real filename
        compiled = compile(code, script_path, "exec")
        exec(compiled, {"__name__": "__main__", "__file__": script_path})

        sys.stdout.flush()
        sys.stderr.flush()
        os._exit(0)
    except SystemExit as e:
        sys.stdout.flush()
        sys.stderr.flush()
        os._exit(e.code if isinstance(e.code, int) else 1)
    except Exception:
        traceback.print_exc()
        sys.stdout.flush()
        sys.stderr.flush()
        os._exit(1)


def _read_pipe(fd: int, max_bytes: int = MAX_OUTPUT) -> str:
    """Read all data from a pipe fd until EOF, up to *max_bytes*."""
    chunks = []
    total = 0
    while True:
        chunk = os.read(fd, 4096)
        if not chunk:
            break
        total += len(chunk)
        if total > max_bytes:
            # Truncate to limit
            keep = max_bytes - (total - len(chunk))
            if keep > 0:
                chunks.append(chunk[:keep])
            break
        chunks.append(chunk)
    os.close(fd)
    return b"".join(chunks).decode("utf-8", errors="replace")


def _daemon_loop() -> None:
    """Main loop: read requests from FIFO, fork + exec, write result."""
    while True:
        # Open FIFO for reading (blocks until a writer connects)
        with open(FIFO_IN, "r") as fin:
            line = fin.readline().strip()

        if not line:
            continue  # spurious empty open/close

        try:
            req = json.loads(line)
        except json.JSONDecodeError:
            _write_response({
                "exit_code": 1, "duration_ms": 0,
                "stdout": "", "stderr": "pyrunner: invalid JSON request\n",
            })
            continue

        script_path = req.get("script", "")
        env_vars = req.get("env", {})

        if not script_path or not os.path.isfile(script_path):
            _write_response({
                "exit_code": 1, "duration_ms": 0,
                "stdout": "", "stderr": f"pyrunner: script not found: {script_path}\n",
            })
            continue

        # Create pipes for stdout/stderr capture
        stdout_r, stdout_w = os.pipe()
        stderr_r, stderr_w = os.pipe()

        t0 = time.monotonic()

        pid = os.fork()
        if pid == 0:
            # --- Child process ---
            # Close read ends of pipes (parent reads them)
            os.close(stdout_r)
            os.close(stderr_r)
            _run_child(script_path, env_vars, stdout_w, stderr_w)
            # _run_child never returns
        else:
            # --- Parent process ---
            # Close write ends of pipes (child writes them)
            os.close(stdout_w)
            os.close(stderr_w)

            # Read child output from pipes
            stdout_data = _read_pipe(stdout_r)
            stderr_data = _read_pipe(stderr_r)

            # Wait for child to finish
            _, status = os.waitpid(pid, 0)
            duration_ms = int((time.monotonic() - t0) * 1000)

            if os.WIFEXITED(status):
                exit_code = os.WEXITSTATUS(status)
            elif os.WIFSIGNALED(status):
                exit_code = 128 + os.WTERMSIG(status)
            else:
                exit_code = 1

            _write_response({
                "exit_code": exit_code,
                "duration_ms": duration_ms,
                "stdout": stdout_data,
                "stderr": stderr_data,
            })


def _write_response(resp: dict) -> None:
    """Write a single JSON line to the output FIFO."""
    with open(FIFO_OUT, "w") as fout:
        fout.write(json.dumps(resp) + "\n")
        fout.flush()


def main() -> None:
    # We explicitly wait for each child with waitpid, so don't ignore SIGCHLD
    # (SIG_IGN would cause waitpid to fail with ECHILD on some systems).
    signal.signal(signal.SIGCHLD, signal.SIG_DFL)

    _make_fifo(FIFO_IN)
    _make_fifo(FIFO_OUT)

    _daemon_loop()


if __name__ == "__main__":
    main()
