package installer

import (
	"fmt"
	"strings"
)

const (
	// ServerImageRepo is the Docker image repository for the Memory server
	ServerImageRepo = "ghcr.io/emergent-company/memory-server"

	// PostgresImage is the pgvector-enabled PostgreSQL image used for all deployments.
	// Bumping this constant is the single source of truth for the postgres version.
	PostgresImage = "pgvector/pgvector:pg17"

	// PostgresMajorVersion is the expected major version after install/upgrade.
	// Used by the upgrade flow to decide whether pg_upgrade is needed.
	PostgresMajorVersion = 17

	// WorkspaceBaseImage is the default agent sandbox base image (alpine + dev tools).
	// Must match docker/workspace-base.Dockerfile, which the publish workflow builds.
	WorkspaceBaseImage = "memory-workspace:latest"

	// KreuzbergImage is the pinned image for the Kreuzberg document extraction service.
	// Bumping this constant is the single source of truth for the Kreuzberg version.
	// The static copies in deploy/self-hosted/*.yml and install-online.sh MUST be bumped
	// together with this constant; the CLI tests assert the rendered template and the
	// cli.yml CI workflow greps the static copies, so drift fails CI.
	KreuzbergImage = "ghcr.io/kreuzberg-dev/kreuzberg-full:4.10.3"

	// ObjectStoreImage is the pinned SeaweedFS S3-compatible object store.
	//
	// MinIO archived its community edition and privatised its images (issue
	// #23); SeaweedFS (chrislusf/seaweedfs, Apache-2.0) is the maintained,
	// freely pullable replacement. The single-node command
	// (`server -dir=/data -s3`) runs master + volume + filer + S3 in one
	// process; the S3 API listens on 8333.
	//
	// Pinned BY DIGEST (multi-arch index for chrislusf/seaweedfs:4.47). Bumping
	// this constant is the single source of truth for the object-store version.
	// The static copies in deploy/self-hosted/*.yml and install-online.sh MUST
	// be bumped together with it; the CLI tests assert the rendered template
	// and the cli.yml CI workflow greps the static copies, so drift fails CI.
	ObjectStoreImage = "chrislusf/seaweedfs@sha256:ce9e796f1fe6f06968f4c04bdaf8f678dad9c8acdfef3d244133d71bfa6bf882"

	// StorageInitImage is the image that carries the bucket-bootstrap binary.
	// It is the server image itself: deploy/self-hosted/Dockerfile.server builds
	// and copies `emergent-storage-init` into it, so the one-shot storage-init
	// service reuses the same S3 client and versioned image as the server.
	StorageInitImage = ServerImageRepo

	// StorageInitEntrypoint is the path to the bucket-bootstrap binary baked
	// into the server image.
	StorageInitEntrypoint = "/usr/local/bin/emergent-storage-init"
)

// GetDockerComposeTemplate returns the docker-compose template with :latest tag.
// Used for fresh installs. RAM-based PostgreSQL tuning is applied automatically.
func GetDockerComposeTemplate() string {
	return GetDockerComposeTemplateWithVersion("latest")
}

// GetDockerComposeTemplateWithVersion returns the docker-compose template with a specific
// image version tag. This is the primary way compose files are generated — both for fresh
// installs (tag="latest") and for upgrades (tag="0.7.3" etc).
//
// By always regenerating from the template, we ensure upgrades pick up:
//   - New services added to the template
//   - New environment variables
//   - Changed healthchecks, resource limits, volume mounts
//   - Corrected image names/repos
//   - PostgreSQL major version bumps
//   - RAM-tuned PostgreSQL configuration for the host machine
func GetDockerComposeTemplateWithVersion(version string) string {
	imageTag := strings.TrimPrefix(version, "v")
	serverImage := fmt.Sprintf("%s:%s", ServerImageRepo, imageTag)

	tuning := computePgTuning()

	return `services:
  db:
    image: ` + PostgresImage + `
    container_name: memory-db
    restart: unless-stopped
    command:
      - "postgres"
      - "-c"
      - "shared_buffers=` + tuning.SharedBuffers + `"
      - "-c"
      - "effective_cache_size=` + tuning.EffectiveCacheSize + `"
      - "-c"
      - "maintenance_work_mem=` + tuning.MaintenanceWorkMem + `"
      - "-c"
      - "work_mem=` + tuning.WorkMem + `"
      - "-c"
      - "max_wal_size=` + tuning.MaxWalSize + `"
      - "-c"
      - "checkpoint_timeout=` + tuning.CheckpointTimeout + `"
      - "-c"
      - "max_parallel_workers=` + fmt.Sprintf("%d", tuning.MaxParallelWorkers) + `"
      - "-c"
      - "max_parallel_workers_per_gather=` + fmt.Sprintf("%d", tuning.MaxParallelWorkers/2) + `"
    environment:
      POSTGRES_USER: ${POSTGRES_USER:-emergent}
      POSTGRES_PASSWORD: ${POSTGRES_PASSWORD:-changeme}
      POSTGRES_DB: ${POSTGRES_DB:-emergent}
    volumes:
      - postgres_data:/var/lib/postgresql/data
      - ./init.sql:/docker-entrypoint-initdb.d/00-init.sql:ro
    ports:
      - '${POSTGRES_PORT:-5432}:5432'
    healthcheck:
      test: ['CMD-SHELL', 'pg_isready -U ${POSTGRES_USER:-emergent} -d ${POSTGRES_DB:-emergent}']
      interval: 5s
      timeout: 5s
      retries: 10
    networks:
      - memory

  kreuzberg:
    # Pinned: Kreuzberg v4 LTS (GHCR). Do not revert to the old floating :latest tag (frozen at 4.0.7).
    image: ` + KreuzbergImage + `
    container_name: memory-kreuzberg
    restart: unless-stopped
    ports:
      - '${KREUZBERG_PORT:-8000}:8000'
    environment:
      - LOG_LEVEL=${KREUZBERG_LOG_LEVEL:-info}
    healthcheck:
      test: ['CMD', 'curl', '-f', 'http://localhost:8000/health']
      interval: 30s
      timeout: 10s
      retries: 3
    deploy:
      resources:
        limits:
          memory: 2G
        reservations:
          memory: 512M
    networks:
      - memory

  whisper-server:
    image: onerahmet/openai-whisper-asr-webservice:latest
    container_name: memory-whisper
    restart: unless-stopped
    ports:
      - '${WHISPER_PORT:-9000}:9000'
    environment:
      - ASR_MODEL=${WHISPER_MODEL:-base}
      - ASR_ENGINE=faster_whisper
    deploy:
      resources:
        limits:
          memory: 4G
        reservations:
          memory: 1G
    networks:
      - memory

  seaweedfs:
    image: ` + ObjectStoreImage + `
    container_name: memory-seaweedfs
    restart: unless-stopped
    # Single node: master + volume + filer + S3 in one process. S3 API on 8333.
    command: server -dir=/data -s3
    environment:
      # Fallback admin credentials. With these set, the installer-generated
      # secret is authoritative and unmatched access keys are rejected.
      AWS_ACCESS_KEY_ID: ${OBJECT_STORE_ACCESS_KEY:-emergent}
      AWS_SECRET_ACCESS_KEY: ${OBJECT_STORE_SECRET_KEY:-changeme}
    ports:
      - '${OBJECT_STORE_API_PORT:-9000}:8333'
    volumes:
      - object_store_data:/data
    healthcheck:
      # Master cluster-status endpoint (returns {"IsLeader":true}) — the S3
      # port alone does not indicate a ready cluster. 127.0.0.1 is used
      # explicitly because localhost resolves to IPv6 in the image.
      test: ['CMD', 'wget', '--no-verbose', '--tries=1', '--spider', 'http://127.0.0.1:9333/cluster/status']
      interval: 30s
      timeout: 10s
      retries: 5
      start_period: 10s
    networks:
      - memory

  storage-init:
    image: ` + serverImage + `
    container_name: memory-storage-init
    # One-shot bucket bootstrap from the server image (same S3 client as the
    # server). Exits non-zero on failure, blocking server start.
    entrypoint: ['` + StorageInitEntrypoint + `']
    restart: 'no'
    depends_on:
      seaweedfs:
        condition: service_healthy
    environment:
      STORAGE_PROVIDER: seaweedfs
      STORAGE_ENDPOINT: http://seaweedfs:8333
      STORAGE_ACCESS_KEY: ${OBJECT_STORE_ACCESS_KEY:-emergent}
      STORAGE_SECRET_KEY: ${OBJECT_STORE_SECRET_KEY:-changeme}
      STORAGE_REGION: ${STORAGE_REGION:-us-east-1}
      STORAGE_BUCKET_DOCUMENTS: documents
      STORAGE_BUCKET_TEMP: document-temp
    networks:
      - memory

  tempo:
    image: grafana/tempo:2.6.1
    container_name: memory-tempo
    restart: unless-stopped
    command: ["-config.file=/etc/tempo.yaml"]
    volumes:
      - ./tempo/tempo.yaml:/etc/tempo.yaml:ro
      - tempo_data:/var/tempo
    ports:
      - "127.0.0.1:3200:3200"
    healthcheck:
      test: ['CMD', 'wget', '--no-verbose', '--tries=1', '--spider', 'http://localhost:3200/ready']
      interval: 10s
      timeout: 5s
      retries: 5
    networks:
      - memory

  server:
    image: ` + serverImage + `
    container_name: memory-server
    restart: unless-stopped
    ports:
      - '${SERVER_PORT:-3002}:3002'
    volumes:
      - memory_cli_config:/root/.memory
      - /var/run/docker.sock:/var/run/docker.sock
    environment:
      STANDALONE_MODE: 'true'
      STANDALONE_API_KEY: ${STANDALONE_API_KEY}
      STANDALONE_USER_EMAIL: ${STANDALONE_USER_EMAIL}
      STANDALONE_ORG_NAME: ${STANDALONE_ORG_NAME}
      STANDALONE_PROJECT_NAME: ${STANDALONE_PROJECT_NAME}
      POSTGRES_HOST: db
      POSTGRES_PORT: 5432
      POSTGRES_USER: ${POSTGRES_USER:-emergent}
      POSTGRES_PASSWORD: ${POSTGRES_PASSWORD:-changeme}
      POSTGRES_DB: ${POSTGRES_DB:-emergent}
      PORT: 3002
      GO_ENV: production
      KREUZBERG_SERVICE_URL: http://kreuzberg:8000
      KREUZBERG_ENABLED: 'true'
      WHISPER_ENABLED: ${WHISPER_ENABLED:-false}
      WHISPER_SERVICE_URL: http://whisper-server:9000
      WHISPER_MODEL: ${WHISPER_MODEL:-base}
      WHISPER_LANGUAGE: ${WHISPER_LANGUAGE:-}
      WHISPER_SERVICE_TIMEOUT: ${WHISPER_SERVICE_TIMEOUT:-600000}
      WHISPER_MAX_FILE_SIZE_MB: ${WHISPER_MAX_FILE_SIZE_MB:-500}
      STORAGE_PROVIDER: seaweedfs
      STORAGE_ENDPOINT: http://seaweedfs:8333
      STORAGE_ACCESS_KEY: ${OBJECT_STORE_ACCESS_KEY:-emergent}
      STORAGE_SECRET_KEY: ${OBJECT_STORE_SECRET_KEY:-changeme}
      STORAGE_BUCKET_DOCUMENTS: documents
      STORAGE_BUCKET_TEMP: document-temp
      STORAGE_REGION: ${STORAGE_REGION:-us-east-1}
      STORAGE_USE_SSL: 'false'
      GOOGLE_API_KEY: ${GOOGLE_API_KEY:-}
      GITHUB_APP_ENCRYPTION_KEY: ${GITHUB_APP_ENCRYPTION_KEY:-}
      LLM_ENCRYPTION_KEY: ${LLM_ENCRYPTION_KEY:-}
      EMBEDDING_DIMENSION: ${EMBEDDING_DIMENSION:-768}
      DB_AUTOINIT: 'true'
      SCOPES_DISABLED: 'true'
      OTEL_EXPORTER_OTLP_ENDPOINT: ${OTEL_EXPORTER_OTLP_ENDPOINT:-http://tempo:4318}
      OTEL_TEMPO_URL: ${OTEL_TEMPO_URL:-http://localhost:3200}
    depends_on:
      db:
        condition: service_healthy
      kreuzberg:
        condition: service_healthy
      seaweedfs:
        condition: service_healthy
      storage-init:
        condition: service_completed_successfully
      tempo:
        condition: service_healthy
    healthcheck:
      test: ['CMD', 'curl', '-f', 'http://localhost:3002/health']
      interval: 30s
      timeout: 10s
      retries: 3
    networks:
      - memory

volumes:
  postgres_data:
  object_store_data:
  tempo_data:
  memory_cli_config:

networks:
  memory:
`
}

// GetTempoConfigTemplate returns the Grafana Tempo configuration file content.
// Retention is set to 720h (30 days).
func GetTempoConfigTemplate() string {
	return `server:
  http_listen_port: 3200
  log_level: warn

distributor:
  receivers:
    otlp:
      protocols:
        grpc:
          endpoint: 0.0.0.0:4317
        http:
          endpoint: 0.0.0.0:4318

ingester:
  max_block_duration: 5m

compactor:
  compaction:
    block_retention: 720h

storage:
  trace:
    backend: local
    local:
      path: /var/tempo/traces
    wal:
      path: /var/tempo/wal
`
}

// GetWorkspaceBaseDockerfile returns the Dockerfile used to build the
// memory-workspace base sandbox image. Content mirrors docker/workspace-base.Dockerfile
// (kept in sync manually; the file is used by the ghcr publish workflow).
func GetWorkspaceBaseDockerfile() string {
	return `FROM alpine:3.19

# Install essential dev tools and AI agent-friendly utilities
RUN apk add --no-cache \
    bash \
    git \
    curl \
    wget \
    ca-certificates \
    jq \
    ripgrep \
    grep \
    sed \
    gawk \
    findutils \
    tree \
    tar \
    gzip \
    unzip \
    build-base \
    && rm -rf /var/cache/apk/*

# Create workspace directory
RUN mkdir -p /workspace
WORKDIR /workspace

# Keep container running
CMD ["sleep", "infinity"]
`
}

// GetPythonSDKDockerfile returns the Dockerfile used to build the
// emergent-memory-python-sdk sandbox image. The content is embedded here so
// that every `memory server install` and `memory server upgrade` can
// (re)build the image without needing the source repository present.
func GetPythonSDKDockerfile() string {
	return `FROM python:3.12-slim

# Install system deps needed by pip/git and common packages
RUN apt-get update && apt-get install -y --no-install-recommends \
        git \
        curl \
        ca-certificates \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /workspace

# Install the emergent-memory Python SDK from the public GitHub repository
RUN pip install --no-cache-dir \
        "git+https://github.com/emergent-company/emergent.memory.git#subdirectory=sdk/python"

# Pre-install common helper packages agents are likely to import
RUN pip install --no-cache-dir \
        requests \
        pydantic \
        python-dateutil

# Install pyrunner daemon for zero-cold-start script execution.
# Inlined because this Dockerfile is built from a temp dir with no COPY context.
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

# Entrypoint: start pyrunner daemon in background, then exec CMD.
# gvisor_provider.go overrides CMD with ["sleep", "infinity"] so the
# daemon starts automatically at container boot regardless of CMD.
ENTRYPOINT ["sh", "-c", "python3 /usr/local/bin/pyrunner.py & exec \"$@\"", "--"]

# Default working directory for agent scripts
WORKDIR /workspace

CMD ["sleep", "infinity"]
`
}

// GetGoSDKDockerfile returns the Dockerfile used to build the
// emergent-memory-go-sdk sandbox image. The content is embedded here so
// that every `memory server install` and `memory server upgrade` can
// (re)build the image without needing the source repository present.
//
// The Dockerfile clones the SDK from the public GitHub repository so it is
// fully self-contained (no local COPY required). A stub main.go is added
// before go mod tidy so that the require line is not stripped, enabling
// proper pre-caching of dependencies. The stub is removed after the build
// cache is populated.
func GetGoSDKDockerfile() string {
	return `FROM golang:1.24-bookworm

# Install system deps
RUN apt-get update && apt-get install -y --no-install-recommends \
        git \
        curl \
        ca-certificates \
    && rm -rf /var/lib/apt/lists/*

# Clone the Memory repo and extract the Go SDK source.
# Using sparse checkout to avoid pulling the entire repo.
RUN git clone --depth 1 --filter=blob:none --sparse \
        https://github.com/emergent-company/emergent.memory.git /repo && \
    cd /repo && \
    git sparse-checkout set apps/server/pkg/sdk && \
    cp -r /repo/apps/server/pkg/sdk /sdk && \
    rm -rf /repo

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

# Default working directory for agent scripts
WORKDIR /workspace

CMD ["/bin/bash"]
`
}

func GetInitSQLTemplate() string {
	return `-- PostgreSQL Initialization Script for Memory Standalone
-- Creates required extensions and roles

CREATE EXTENSION IF NOT EXISTS vector;
CREATE EXTENSION IF NOT EXISTS pgcrypto;

DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'app_rls') THEN
        CREATE ROLE app_rls WITH NOLOGIN;
    END IF;
END
$$;
`
}
