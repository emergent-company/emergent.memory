# Test container for emergent CLI installation tests.
#
# This image deliberately does NOT pre-install the emergent CLI at build time.
# Instead, the entrypoint script runs the exact one-liner a real user would use:
#
#   curl -fsSL https://raw.githubusercontent.com/.../install.sh | bash
#
# This ensures the test exercises the full install path — download, extract,
# PATH setup — exactly as a real user experiences it, not a cached artefact.
#
# The container is designed to run against a live Emergent server injected via
# MEMORY_TEST_SERVER at runtime (see docker-compose.yml).
#
# Usage (standalone):
#   docker build -t emergent-cli-test .
#   docker run --rm \
#     -e MEMORY_TEST_SERVER=http://host.docker.internal:5300 \
#     emergent-cli-test

FROM golang:1.24-alpine AS test-runner

# Runtime deps: curl + tar for install.sh; bash for scripts; git for workspace setup
RUN apk --no-cache add \
    bash \
    curl \
    tar \
    git \
    ca-certificates \
    jq

WORKDIR /tests

# Copy the Go test module and pre-download dependencies so the test run is fast.
COPY go.mod go.sum* ./
RUN go mod download

# Copy test source
COPY . .

# Install opencode CLI (latest release for linux/amd64).
# This is a test infrastructure dependency, not the subject under test.
RUN OPENCODE_VERSION=$(curl -fsSL https://api.github.com/repos/sst/opencode/releases/latest \
        | grep '"tag_name":' | sed -E 's/.*"([^"]+)".*/\1/') \
    && echo "Installing opencode ${OPENCODE_VERSION}" \
    && curl -fsSL "https://github.com/sst/opencode/releases/download/${OPENCODE_VERSION}/opencode_Linux_x86_64.tar.gz" \
       -o /tmp/opencode.tar.gz \
    && tar -xzf /tmp/opencode.tar.gz -C /usr/local/bin \
    && rm /tmp/opencode.tar.gz \
    && chmod +x /usr/local/bin/opencode \
    && opencode --version

ENV PATH="/usr/local/bin:${HOME}/.emergent/bin:${PATH}"
ENV HOME="/root"

# Log directory for test session output
RUN mkdir -p /test-logs

# MEMORY_TEST_SERVER must be injected at runtime (e.g. by docker-compose.yml).
# The server runs in a separate container — there is no localhost fallback.
# docker-compose.yml sets:  MEMORY_TEST_SERVER=http://test-emergent-server:5300

# Copy the entrypoint which installs the emergent CLI at runtime then runs tests.
COPY entrypoint.sh /entrypoint.sh
RUN chmod +x /entrypoint.sh

ENTRYPOINT ["/entrypoint.sh"]
