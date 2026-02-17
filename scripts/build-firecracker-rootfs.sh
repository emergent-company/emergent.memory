#!/usr/bin/env bash
# build-firecracker-rootfs.sh — Builds the base rootfs.ext4 image for Firecracker microVMs.
#
# This script creates a minimal ext4 root filesystem containing:
#   - Alpine Linux base (musl libc, busybox)
#   - vm-agent binary (statically compiled Go HTTP server)
#   - git, openssh-client, curl, bash, coreutils
#   - init system (busybox init + inittab)
#   - Networking configured for DHCP-less static IP (set by Firecracker)
#
# Prerequisites:
#   - Docker (for building in a clean environment)
#   - Go 1.24+ (for cross-compiling vm-agent)
#
# Usage:
#   ./scripts/build-firecracker-rootfs.sh [--output /path/to/rootfs.ext4] [--size 512]
#
# Options:
#   --output PATH   Output path for the rootfs image (default: build/firecracker/rootfs.ext4)
#   --size MB       Image size in MB (default: 512)
#   --kernel        Also download a compatible vmlinux kernel
#   --no-cache      Build without Docker cache
#
# The resulting rootfs.ext4 can be placed at /var/lib/emergent/firecracker/rootfs.ext4
# on the host or inside the firecracker-manager container.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
SERVER_DIR="$REPO_ROOT/apps/server-go"
BUILD_DIR="$REPO_ROOT/build/firecracker"

# Defaults
OUTPUT=""
IMAGE_SIZE_MB=512
DOWNLOAD_KERNEL=false
NO_CACHE=""

# Firecracker-compatible kernel version
# Using 5.10 LTS — the most widely tested version with Firecracker
KERNEL_VERSION="5.10"
KERNEL_URL="https://s3.amazonaws.com/spec.ccfc.min/firecracker-ci/v1.10/x86_64/vmlinux-${KERNEL_VERSION}"

# Parse arguments
while [[ $# -gt 0 ]]; do
    case "$1" in
        --output)
            OUTPUT="$2"
            shift 2
            ;;
        --size)
            IMAGE_SIZE_MB="$2"
            shift 2
            ;;
        --kernel)
            DOWNLOAD_KERNEL=true
            shift
            ;;
        --no-cache)
            NO_CACHE="--no-cache"
            shift
            ;;
        -h|--help)
            head -27 "$0" | tail -23
            exit 0
            ;;
        *)
            echo "Unknown option: $1"
            exit 1
            ;;
    esac
done

if [[ -z "$OUTPUT" ]]; then
    OUTPUT="$BUILD_DIR/rootfs.ext4"
fi

echo "=== Firecracker Rootfs Builder ==="
echo "  Output:       $OUTPUT"
echo "  Image size:   ${IMAGE_SIZE_MB}MB"
echo "  Kernel:       $DOWNLOAD_KERNEL"
echo ""

# Create build directory
mkdir -p "$(dirname "$OUTPUT")"
mkdir -p "$BUILD_DIR/staging"

# ──────────────────────────────────────────────────────────────────────
# Step 1: Cross-compile vm-agent as a static binary
# ──────────────────────────────────────────────────────────────────────
echo ">>> Step 1: Building vm-agent (static, linux/amd64)..."

VM_AGENT_BIN="$BUILD_DIR/staging/vm-agent"

CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
    -ldflags="-s -w -extldflags '-static'" \
    -o "$VM_AGENT_BIN" \
    "$SERVER_DIR/cmd/vm-agent/main.go"

chmod +x "$VM_AGENT_BIN"
echo "    vm-agent built: $(du -h "$VM_AGENT_BIN" | cut -f1)"

# ──────────────────────────────────────────────────────────────────────
# Step 2: Build rootfs using Docker (multi-stage)
# ──────────────────────────────────────────────────────────────────────
echo ">>> Step 2: Building rootfs via Docker..."

# Create a temporary Dockerfile for rootfs construction
ROOTFS_DOCKERFILE="$BUILD_DIR/staging/Dockerfile.rootfs"
cat > "$ROOTFS_DOCKERFILE" <<'DOCKERFILE'
# Stage 1: Build the rootfs directory tree
FROM alpine:3.20 AS rootfs-builder

# Install packages that will be in the rootfs
RUN apk add --no-cache \
    alpine-baselayout \
    busybox \
    busybox-suid \
    musl \
    bash \
    coreutils \
    git \
    openssh-client \
    curl \
    ca-certificates \
    findutils \
    grep \
    sed \
    gawk \
    tar \
    gzip \
    xz \
    make \
    patch \
    diffutils \
    && rm -rf /var/cache/apk/*

# Create standard directories
RUN mkdir -p /rootfs && \
    for d in bin sbin usr/bin usr/sbin lib etc dev proc sys tmp run var/log var/run workspace; do \
        mkdir -p /rootfs/$d; \
    done

# Copy the entire Alpine filesystem into /rootfs
RUN cp -a /bin/* /rootfs/bin/ 2>/dev/null || true && \
    cp -a /sbin/* /rootfs/sbin/ 2>/dev/null || true && \
    cp -a /usr/bin/* /rootfs/usr/bin/ 2>/dev/null || true && \
    cp -a /usr/sbin/* /rootfs/usr/sbin/ 2>/dev/null || true && \
    cp -a /lib/* /rootfs/lib/ 2>/dev/null || true && \
    cp -a /usr/lib /rootfs/usr/ 2>/dev/null || true && \
    cp -a /etc/* /rootfs/etc/ 2>/dev/null || true && \
    cp -a /usr/share/ca-certificates /rootfs/usr/share/ 2>/dev/null || true && \
    cp -a /usr/libexec /rootfs/usr/ 2>/dev/null || true

# Configure init system (busybox init)
RUN cat > /rootfs/etc/inittab <<'EOF'
::sysinit:/etc/init.d/rcS
::respawn:/usr/bin/vm-agent
::ctrlaltdel:/sbin/reboot
::shutdown:/bin/umount -a -r
EOF

# Create init script
RUN mkdir -p /rootfs/etc/init.d && \
    cat > /rootfs/etc/init.d/rcS <<'EOF'
#!/bin/sh
# Mount essential filesystems
mount -t proc proc /proc
mount -t sysfs sysfs /sys
mount -t devtmpfs devtmpfs /dev

# Set up hostname
hostname emergent-vm

# Configure networking (IP is set by Firecracker network config)
# The kernel gets IP from Firecracker's static network configuration
ip link set lo up
ip link set eth0 up

# Wait for network interface
sleep 0.5

# Mount data device at /workspace if available
if [ -e /dev/vdb ]; then
    mkdir -p /workspace
    mount /dev/vdb /workspace
fi

# Ready
echo "Emergent VM agent starting..."
EOF
    chmod +x /rootfs/etc/init.d/rcS

# Set up DNS
RUN echo "nameserver 8.8.8.8" > /rootfs/etc/resolv.conf && \
    echo "nameserver 8.8.4.4" >> /rootfs/etc/resolv.conf

# Set up /etc/passwd and /etc/group
RUN echo "root:x:0:0:root:/root:/bin/bash" > /rootfs/etc/passwd && \
    echo "root:x:0:" > /rootfs/etc/group

# Create workspace directory
RUN mkdir -p /rootfs/workspace && chmod 777 /rootfs/workspace

# Stage 2: Create the ext4 image
FROM alpine:3.20 AS image-builder

RUN apk add --no-cache e2fsprogs

COPY --from=rootfs-builder /rootfs /rootfs

# Copy vm-agent binary
COPY vm-agent /rootfs/usr/bin/vm-agent
RUN chmod +x /rootfs/usr/bin/vm-agent

# Create the ext4 image
ARG IMAGE_SIZE_MB=512
RUN truncate -s ${IMAGE_SIZE_MB}M /rootfs.ext4 && \
    mkfs.ext4 -F -L rootfs /rootfs.ext4 && \
    mkdir -p /mnt/rootfs && \
    mount -o loop /rootfs.ext4 /mnt/rootfs && \
    cp -a /rootfs/* /mnt/rootfs/ && \
    umount /mnt/rootfs

# Final stage: extract the image
FROM scratch
COPY --from=image-builder /rootfs.ext4 /rootfs.ext4
DOCKERFILE

# Build the rootfs image using Docker
echo "    Building rootfs Docker image..."
docker build \
    $NO_CACHE \
    --build-arg "IMAGE_SIZE_MB=$IMAGE_SIZE_MB" \
    -f "$ROOTFS_DOCKERFILE" \
    -t emergent-rootfs-builder:latest \
    "$BUILD_DIR/staging/"

# Extract the rootfs.ext4 from the Docker image
echo "    Extracting rootfs.ext4..."
CONTAINER_ID=$(docker create emergent-rootfs-builder:latest)
docker cp "$CONTAINER_ID:/rootfs.ext4" "$OUTPUT"
docker rm "$CONTAINER_ID" > /dev/null

echo "    rootfs.ext4 created: $(du -h "$OUTPUT" | cut -f1)"

# ──────────────────────────────────────────────────────────────────────
# Step 3: Optionally download kernel
# ──────────────────────────────────────────────────────────────────────
if [[ "$DOWNLOAD_KERNEL" == "true" ]]; then
    KERNEL_OUTPUT="$(dirname "$OUTPUT")/vmlinux"
    echo ">>> Step 3: Downloading vmlinux kernel (${KERNEL_VERSION})..."
    curl -fsSL -o "$KERNEL_OUTPUT" "$KERNEL_URL"
    chmod +x "$KERNEL_OUTPUT"
    echo "    vmlinux downloaded: $(du -h "$KERNEL_OUTPUT" | cut -f1)"
fi

# ──────────────────────────────────────────────────────────────────────
# Cleanup
# ──────────────────────────────────────────────────────────────────────
echo ">>> Cleaning up staging files..."
rm -rf "$BUILD_DIR/staging"

echo ""
echo "=== Build Complete ==="
echo "  rootfs: $OUTPUT"
if [[ "$DOWNLOAD_KERNEL" == "true" ]]; then
    echo "  kernel: $(dirname "$OUTPUT")/vmlinux"
fi
echo ""
echo "To use:"
echo "  1. Copy to /var/lib/emergent/firecracker/"
echo "  2. Or mount into the firecracker-manager container"
echo ""
