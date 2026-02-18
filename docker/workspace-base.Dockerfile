# Minimal workspace base image for Emergent agent workspaces
# Size: ~50MB (vs ubuntu:22.04 ~77MB)
FROM alpine:3.19

# Install essential dev tools
RUN apk add --no-cache \
    bash \
    git \
    curl \
    wget \
    ca-certificates \
    && rm -rf /var/cache/apk/*

# Create workspace directory
RUN mkdir -p /workspace
WORKDIR /workspace

# Keep container running
CMD ["sleep", "infinity"]
