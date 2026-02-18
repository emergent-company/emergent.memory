# Minimal workspace base image for Emergent agent workspaces
# Optimized for AI agent operations with essential tools
FROM alpine:3.19

# Install essential dev tools and AI agent-friendly utilities
RUN apk add --no-cache \
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
    # Basic build tools (for installing packages if needed)
    build-base \
    && rm -rf /var/cache/apk/*

# Create workspace directory
RUN mkdir -p /workspace
WORKDIR /workspace

# Keep container running
CMD ["sleep", "infinity"]
