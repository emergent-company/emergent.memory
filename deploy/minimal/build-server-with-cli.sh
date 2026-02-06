#!/bin/bash
# Build script for emergent-server-with-cli Docker image
# This creates a single image containing both the server and CLI

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"

# Configuration
IMAGE_NAME="${IMAGE_NAME:-emergent-server-with-cli}"
TAG="${TAG:-latest}"
FULL_IMAGE="$IMAGE_NAME:$TAG"

# Build info
VERSION="${VERSION:-dev}"
GIT_COMMIT=$(git rev-parse --short HEAD 2>/dev/null || echo "unknown")
BUILD_TIME=$(date -u +"%Y-%m-%dT%H:%M:%SZ")

echo "Building $FULL_IMAGE..."
echo "  Version: $VERSION"
echo "  Commit: $GIT_COMMIT"
echo "  Build Time: $BUILD_TIME"
echo

# Build from repository root (needed for COPY paths to work)
cd "$REPO_ROOT"

docker build \
  --file deploy/minimal/Dockerfile.server-with-cli \
  --tag "$FULL_IMAGE" \
  --build-arg "VERSION=$VERSION" \
  --build-arg "GIT_COMMIT=$GIT_COMMIT" \
  --build-arg "BUILD_TIME=$BUILD_TIME" \
  .

echo
echo "✅ Build complete: $FULL_IMAGE"
echo
echo "Usage:"
echo "  # Start server (default)"
echo "  docker run -p 3002:3002 $FULL_IMAGE"
echo
echo "  # Run CLI commands"
echo "  docker run --rm $FULL_IMAGE emergent-cli --help"
echo
echo "  # Access CLI in running container"
echo "  docker exec -it emergent-server emergent-cli projects list"
echo
