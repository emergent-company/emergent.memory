# Combined workspace base image for Memory agent workspaces.
# Provides BOTH Python and Go SDK execution tooling so that run_python and
# run_go work out of the box in the default sandbox image.
#
# Base: golang:1.26-bookworm. The repo root go.work declares "go 1.26.6", and
# official golang:* images set GOTOOLCHAIN=local, so the workspace toolchain must
# be >= that or `go test ./...` inside /workspace fails before it starts (#392).
# sdk/go/Dockerfile (golang:1.25-bookworm) and apps/cli/Dockerfile
# (golang:1.25-alpine) stay on 1.25: they build apps/server/pkg/sdk, whose go.mod
# declares "go 1.25.0", and never load the root go.work.
FROM golang:1.26-bookworm

# Install essential dev tools and AI agent-friendly utilities, plus Python.
RUN apt-get update && apt-get install -y --no-install-recommends \
    # Core shell and version control
    bash \
    git \
    # Network tools
    curl \
    wget \
    # SSL certificates
    ca-certificates \
    # JSON processing
    jq \
    # Fast search tools (ripgrep is rg command)
    ripgrep \
    # Text processing
    grep \
    sed \
    gawk \
    # File utilities
    findutils \
    tree \
    # Compression
    tar \
    gzip \
    unzip \
    # Basic build tools (for installing packages / native deps if needed)
    build-essential \
    # Python runtime + package manager (for run_python and the Python SDK)
    python3 \
    python3-pip \
    && rm -rf /var/lib/apt/lists/*

# rtk — token-optimizing CLI proxy (https://github.com/rtk-ai/rtk) used by the
# agent rig to compress tool output before it reaches the model. Pinned to the
# rig version and checksum-verified. amd64 only: publish-workspace-image.yml
# builds linux/amd64.
ARG RTK_VERSION=1.0.1
RUN curl -fsSL -o /tmp/rtk.tgz \
      "https://github.com/rtk-ai/rtk/releases/download/v${RTK_VERSION}/rtk-x86_64-unknown-linux-musl.tar.gz" \
    && echo "5b919c3bccb799b67a61866d0e617410a4fcdf173e2614440e59de2fa2003436  /tmp/rtk.tgz" | sha256sum -c - \
    && tar -xzf /tmp/rtk.tgz -C /usr/local/bin rtk \
    && chmod 0755 /usr/local/bin/rtk \
    && rm -f /tmp/rtk.tgz \
    && rtk --version

# Install ast-grep for the workspace_ast_grep tool (#393). Pinned release: the
# tool validates `ast-grep --version`, and Debian bookworm's /usr/bin/sg is the
# shadow-utils group tool, not ast-grep. Install ONLY the `ast-grep` binary so
# /usr/bin/sg stays untouched; the archive also contains an `sg`, do not use it.
ARG AST_GREP_VERSION=0.51.1
ARG TARGETARCH
RUN case "${TARGETARCH:-amd64}" in \
        amd64) ast_grep_arch=x86_64-unknown-linux-gnu ;; \
        arm64) ast_grep_arch=aarch64-unknown-linux-gnu ;; \
        *) echo "unsupported TARGETARCH: ${TARGETARCH}" >&2; exit 1 ;; \
    esac && \
    curl -fsSL -o /tmp/ast-grep.zip \
        "https://github.com/ast-grep/ast-grep/releases/download/${AST_GREP_VERSION}/app-${ast_grep_arch}.zip" && \
    unzip -q -j /tmp/ast-grep.zip ast-grep -d /usr/local/bin && \
    rm -f /tmp/ast-grep.zip && \
    chmod +x /usr/local/bin/ast-grep && \
    ast-grep --version

# Install the emergent-memory Python SDK from the memory repo.
# --break-system-packages is required on Debian bookworm (PEP 668 externally
# managed environment). requests/pydantic/python-dateutil are common deps.
#
# An optional BuildKit secret (id=github_token) carries a GitHub token: CI
# passes secrets.GITHUB_TOKEN (see publish-workspace-image.yml) and the CLI
# passes GH_TOKEN/GITHUB_TOKEN when present. The clone URL below is a literal
# pointing at emergent-company/emergent.memory, and no ARG or --build-arg can
# retarget it, so whether the credential is needed is a property of that
# repository: required while it is private, unnecessary once it is public
# (#2197). The token is written to ~/.netrc only for the duration of this RUN
# and removed before the layer ends, so it never persists into the image.
RUN --mount=type=secret,id=github_token,required=false \
    if [ -f /run/secrets/github_token ]; then \
        printf 'machine github.com login x-access-token password %s\n' "$(cat /run/secrets/github_token)" > ~/.netrc && chmod 600 ~/.netrc; \
    fi; \
    pip install --no-cache-dir --break-system-packages \
        "git+https://github.com/emergent-company/emergent.memory.git#subdirectory=sdk/python" \
        requests \
        pydantic \
        python-dateutil; \
    rc=$?; \
    rm -f ~/.netrc; \
    exit $rc

# Install pyrunner daemon for zero-cold-start script execution.
# Inlined because this Dockerfile is built with no COPY context.
RUN cat > /usr/local/bin/pyrunner.py << 'PYEOF'
#!/usr/bin/env python3
"""pyrunner - pre-forking Python daemon for zero-cold-start script execution."""
import json, os, signal, sys, time, traceback

FIFO_IN  = "/tmp/pyrunner.in"
FIFO_OUT = "/tmp/pyrunner.out"
MAX_OUTPUT = 50 * 1024

try:
    import emergent
except ImportError:
    pass

def _make_fifo(path):
    try:
        os.unlink(path)
    except FileNotFoundError:
        pass
    os.mkfifo(path)

def _run_child(script_path, env_vars, stdout_fd, stderr_fd):
    try:
        os.dup2(stdout_fd, 1)
        os.dup2(stderr_fd, 2)
        if env_vars:
            os.environ.update(env_vars)
        signal.signal(signal.SIGTERM, signal.SIG_DFL)
        signal.signal(signal.SIGINT, signal.SIG_DFL)
        with open(script_path) as f:
            code = f.read()
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

def _read_pipe(fd, max_bytes=MAX_OUTPUT):
    chunks = []
    total = 0
    while True:
        chunk = os.read(fd, 4096)
        if not chunk:
            break
        total += len(chunk)
        if total > max_bytes:
            keep = max_bytes - (total - len(chunk))
            if keep > 0:
                chunks.append(chunk[:keep])
            break
        chunks.append(chunk)
    os.close(fd)
    return b"".join(chunks).decode("utf-8", errors="replace")

def _write_response(resp):
    with open(FIFO_OUT, "w") as fout:
        fout.write(json.dumps(resp) + "\n")
        fout.flush()

def _daemon_loop():
    while True:
        with open(FIFO_IN, "r") as fin:
            line = fin.readline().strip()
        if not line:
            continue
        try:
            req = json.loads(line)
        except json.JSONDecodeError:
            _write_response({"exit_code": 1, "duration_ms": 0, "stdout": "", "stderr": "pyrunner: invalid JSON request\n"})
            continue
        script_path = req.get("script", "")
        env_vars = req.get("env", {})
        if not script_path or not os.path.isfile(script_path):
            _write_response({"exit_code": 1, "duration_ms": 0, "stdout": "", "stderr": f"pyrunner: script not found: {script_path}\n"})
            continue
        stdout_r, stdout_w = os.pipe()
        stderr_r, stderr_w = os.pipe()
        t0 = time.monotonic()
        pid = os.fork()
        if pid == 0:
            os.close(stdout_r)
            os.close(stderr_r)
            _run_child(script_path, env_vars, stdout_w, stderr_w)
        else:
            os.close(stdout_w)
            os.close(stderr_w)
            stdout_data = _read_pipe(stdout_r)
            stderr_data = _read_pipe(stderr_r)
            _, status = os.waitpid(pid, 0)
            duration_ms = int((time.monotonic() - t0) * 1000)
            if os.WIFEXITED(status):
                exit_code = os.WEXITSTATUS(status)
            elif os.WIFSIGNALED(status):
                exit_code = 128 + os.WTERMSIG(status)
            else:
                exit_code = 1
            _write_response({"exit_code": exit_code, "duration_ms": duration_ms, "stdout": stdout_data, "stderr": stderr_data})

def main():
    signal.signal(signal.SIGCHLD, signal.SIG_DFL)
    _make_fifo(FIFO_IN)
    _make_fifo(FIFO_OUT)
    _daemon_loop()

if __name__ == "__main__":
    main()
PYEOF
RUN chmod +x /usr/local/bin/pyrunner.py

# Clone the Memory repo and extract the Go SDK source, using a sparse checkout
# to avoid pulling the entire repo. Same optional github_token secret as the pip
# step above (netrc credential removed before the layer ends).
RUN --mount=type=secret,id=github_token,required=false \
    if [ -f /run/secrets/github_token ]; then \
        printf 'machine github.com login x-access-token password %s\n' "$(cat /run/secrets/github_token)" > ~/.netrc && chmod 600 ~/.netrc; \
    fi; \
    git clone --depth 1 --filter=blob:none --sparse \
        https://github.com/emergent-company/emergent.memory.git /repo && \
    cd /repo && \
    git sparse-checkout set apps/server/pkg/sdk && \
    cp -r /repo/apps/server/pkg/sdk /sdk && \
    rm -rf /repo; \
    rc=$?; \
    rm -f ~/.netrc; \
    exit $rc

# Create a template module with a stub main.go so go mod tidy keeps the
# dependency (without any .go files the require line would be stripped).
# Agent scripts are injected as main.go at run time by the run_go tool.
RUN mkdir -p /sdk-template && \
    cd /sdk-template && \
    go mod init agent && \
    printf 'require github.com/emergent-company/emergent.memory/apps/server/pkg/sdk v0.0.0\n' >> go.mod && \
    printf 'replace github.com/emergent-company/emergent.memory/apps/server/pkg/sdk => /sdk\n' >> go.mod && \
    printf 'package main\nimport _ "github.com/emergent-company/emergent.memory/apps/server/pkg/sdk"\nfunc main() {}\n' > main.go && \
    go mod tidy && \
    # Pre-cache all SDK dependencies into the module cache
    go build . && \
    # Remove the stub — agent scripts will replace main.go at run time
    rm main.go && \
    go clean -testcache

# Create workspace directory
RUN mkdir -p /workspace
WORKDIR /workspace

# Entrypoint: start pyrunner daemon in background, then exec CMD.
# gVisor overrides CMD but preserves ENTRYPOINT, so the daemon starts
# automatically at container boot regardless of CMD.
ENTRYPOINT ["sh", "-c", "python3 /usr/local/bin/pyrunner.py & exec \"$@\"", "--"]

# Keep container running (gVisor provider overrides CMD with ["sleep", "infinity"])
CMD ["sleep", "infinity"]
