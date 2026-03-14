FROM golang:1.24-bookworm

# Install system deps
RUN apt-get update && apt-get install -y --no-install-recommends \
        git \
        curl \
        ca-certificates \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /workspace

# Copy the SDK source into the image and set up a module cache.
# The SDK is vendored as a local replace directive so scripts can import it
# without network access at run time.
COPY sdk/ /sdk/

# Create a template module that pre-fetches the SDK's dependencies.
# Agent scripts will be injected into this module at run time.
RUN mkdir -p /sdk-template && \
    cd /sdk-template && \
    go mod init agent && \
    echo 'require github.com/emergent-company/emergent.memory/apps/server/pkg/sdk v0.0.0' >> go.mod && \
    echo 'replace github.com/emergent-company/emergent.memory/apps/server/pkg/sdk => /sdk' >> go.mod && \
    go mod tidy && \
    # Pre-cache all dependencies so run_go doesn't need network access
    go build github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/... && \
    go clean -cache

# Default working directory for agent scripts
WORKDIR /workspace

CMD ["/bin/bash"]
