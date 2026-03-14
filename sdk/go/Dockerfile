FROM golang:1.24-bookworm

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
