# Test container for the memory CLI installation tests.
#
# The memory CLI is built from the checked-out repo source in a Docker stage —
# it is NOT downloaded from GitHub. The repository is private, so the previous
# approach (curl the install script from raw.githubusercontent.com and download
# the latest GitHub release) failed unauthenticated with `curl: (22) ... 404`.
#
# Stage 1 (cli-builder) compiles apps/cli using the same source layout as
# deploy/self-hosted/Dockerfile.server. The test-runner stage stages the binary
# at /opt/memory/memory; entrypoint.sh installs it into $HOME/.memory/bin — the
# same destination install.sh uses — so the install step and PATH wiring stay
# faithful while the private-repo network dependency is gone.
#
# The container runs against a live Memory server injected via MEMORY_TEST_SERVER
# at runtime (see docker-compose.yml).
#
# Build from the REPO ROOT — the context must include apps/cli and
# apps/server/pkg/sdk:
#   docker build -f e2e/Dockerfile -t emergent-cli-test .
#   docker run --rm \
#     -e MEMORY_TEST_SERVER=http://host.docker.internal:3002 \
#     emergent-cli-test

# ── Stage 1: build the memory CLI from the checked-out source ───────────────
FROM golang:1.25-alpine AS cli-builder

WORKDIR /build/cli

# The SDK must be staged at the path apps/cli/go.mod's replace directive expects.
COPY apps/server/pkg/sdk/ /apps/server/pkg/sdk/

# Dependency cache layer.
COPY apps/cli/go.mod apps/cli/go.sum ./
RUN go mod download

# CLI source.
COPY apps/cli/ ./

# -ldflags stamps a version so `memory version` prints real values.
RUN GOWORK=off CGO_ENABLED=0 go build \
    -ldflags="-s -w \
      -X github.com/emergent-company/emergent.memory/apps/cli/internal/cmd.Version=e2e \
      -X github.com/emergent-company/emergent.memory/apps/cli/internal/cmd.Commit=e2e \
      -X github.com/emergent-company/emergent.memory/apps/cli/internal/cmd.BuildDate=e2e" \
    -o /out/memory ./cmd

# ── Stage 2: test runner ────────────────────────────────────────────────────
FROM golang:1.25-alpine AS test-runner

# Runtime deps: bash for scripts; curl/tar/git for test tooling and workspace
# setup; libstdc++/libgcc are required by the opencode binary on Alpine (musl libc)
RUN apk --no-cache add \
    bash \
    curl \
    tar \
    git \
    ca-certificates \
    jq \
    libstdc++ \
    libgcc

# The memory CLI built from source; entrypoint.sh installs it to ~/.memory/bin.
COPY --from=cli-builder /out/memory /opt/memory/memory
RUN chmod +x /opt/memory/memory

WORKDIR /tests

# Copy the Go test module and pre-download dependencies so the test run is fast.
COPY e2e/go.mod e2e/go.sum* ./
RUN go mod download

# Copy test source
COPY e2e/ ./

# Install opencode CLI (pinned release for linux/amd64, musl build for Alpine).
# This is a test infrastructure dependency, not the subject under test.
#
# Pinned to a specific version instead of resolving `latest` via the GitHub API:
#   - resolving `latest` is a moving target and hits api.github.com, which is
#     unauthenticated rate-limited (60 req/hr/IP) and 403s on shared CI runners;
#   - the repo was renamed sst/opencode -> anomalyco/opencode; the old name 301s
#     and, without -L, the version resolved empty -> asset URL 404s.
# Downloaded directly from the release asset URL with retries + backoff so a
# transient 403/404 (CDN/rate-limit flakiness) cannot fail the stack build. The
# Docker layer cache also caches the binary until the version changes.
ARG OPENCODE_VERSION=v1.18.32
RUN set -eux; \
    url="https://github.com/anomalyco/opencode/releases/download/${OPENCODE_VERSION}/opencode-linux-x64-musl.tar.gz"; \
    ok=0; \
    for attempt in 1 2 3 4 5; do \
        if curl -fsSL -o /tmp/opencode.tar.gz "${url}"; then ok=1; break; fi; \
        echo "opencode fetch attempt ${attempt}/5 failed; backing off" >&2; \
        sleep "$((attempt * 5))"; \
    done; \
    if [ "${ok}" -ne 1 ]; then \
        echo "INFRASTRUCTURE ERROR: could not fetch opencode ${OPENCODE_VERSION} from ${url} after 5 attempts" >&2; \
        exit 1; \
    fi; \
    test -s /tmp/opencode.tar.gz; \
    tar -xzf /tmp/opencode.tar.gz -C /usr/local/bin opencode; \
    rm -f /tmp/opencode.tar.gz; \
    chmod +x /usr/local/bin/opencode; \
    opencode --version

ENV PATH="/usr/local/bin:${HOME}/.memory/bin:${PATH}"
ENV HOME="/root"

# Log directory for test session output
RUN mkdir -p /test-logs

# MEMORY_TEST_SERVER must be injected at runtime (e.g. by docker-compose.yml).
# The server runs in a separate container — there is no localhost fallback.
# docker-compose.yml sets:  MEMORY_TEST_SERVER=http://test-emergent-server:3002

# Copy the entrypoint which installs the memory CLI from the staged binary then
# runs tests.
COPY e2e/entrypoint.sh /entrypoint.sh
RUN chmod +x /entrypoint.sh

ENTRYPOINT ["/entrypoint.sh"]
