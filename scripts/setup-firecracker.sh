#!/bin/bash
#
# Firecracker Setup Script for Emergent Workspace Provider
# Builds and installs all Firecracker artifacts on a KVM-capable host.
#
# Prerequisites:
#   - Docker installed (for building rootfs)
#   - /dev/kvm accessible (Intel VT-x or AMD-V enabled)
#   - Root access (for TAP device and iptables management)
#
# Usage: ./setup-firecracker.sh [DATA_DIR]
#
# This script:
#   1. Verifies KVM is available
#   2. Builds the Firecracker Docker image (multi-stage: binary + kernel + rootfs + vm-agent)
#   3. Extracts artifacts to DATA_DIR
#   4. Verifies the Firecracker binary works
#
# After running this script, set these env vars for the Go server:
#   WORKSPACE_DEFAULT_PROVIDER=firecracker  (or keep auto-select)
#
# The provider reads from /var/lib/emergent/firecracker/ by default.

set -e

DATA_DIR="${1:-/var/lib/emergent/firecracker}"

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

log_step() { echo -e "${YELLOW}→${NC} $1"; }
log_success() { echo -e "${GREEN}✓${NC} $1"; }
log_error() { echo -e "${RED}✗${NC} $1"; }

echo -e "${BLUE}═══════════════════════════════════════════════════════════${NC}"
echo -e "${BLUE}  Emergent Firecracker Provider Setup${NC}"
echo -e "${BLUE}═══════════════════════════════════════════════════════════${NC}"
echo ""

# Step 1: Verify KVM
log_step "Checking KVM availability..."
if [ ! -e /dev/kvm ]; then
    log_error "/dev/kvm not found. Enable Intel VT-x / AMD-V in BIOS."
    exit 1
fi
if [ ! -w /dev/kvm ]; then
    log_error "/dev/kvm is not writable. Run as root or add user to kvm group."
    exit 1
fi
log_success "KVM available at /dev/kvm"

# Step 2: Find repository root
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
if [ ! -f "$REPO_ROOT/deploy/firecracker/Dockerfile" ]; then
    log_error "Cannot find deploy/firecracker/Dockerfile. Run from the repo root."
    exit 1
fi
log_success "Repository root: $REPO_ROOT"

# Step 3: Build Docker image
log_step "Building Firecracker image (this may take a few minutes)..."
docker build \
    -f "$REPO_ROOT/deploy/firecracker/Dockerfile" \
    -t emergent-firecracker:latest \
    "$REPO_ROOT" 2>&1 | tail -5

log_success "Docker image built: emergent-firecracker:latest"

# Step 4: Create data directory
log_step "Creating data directory: $DATA_DIR"
mkdir -p "$DATA_DIR"/{sockets,disks,snapshots}

# Step 5: Extract artifacts from image
log_step "Extracting Firecracker binary..."
CONTAINER_ID=$(docker create emergent-firecracker:latest)

docker cp "$CONTAINER_ID:/usr/local/bin/firecracker" "$DATA_DIR/firecracker"
chmod +x "$DATA_DIR/firecracker"
log_success "Firecracker binary: $DATA_DIR/firecracker"

docker cp "$CONTAINER_ID:/var/lib/firecracker/vmlinux" "$DATA_DIR/vmlinux"
log_success "Kernel: $DATA_DIR/vmlinux"

docker cp "$CONTAINER_ID:/var/lib/firecracker/rootfs.ext4" "$DATA_DIR/rootfs.ext4"
log_success "Root filesystem: $DATA_DIR/rootfs.ext4"

docker rm "$CONTAINER_ID" > /dev/null
log_success "Cleanup: temporary container removed"

# Step 6: Install firecracker binary to PATH
if [ ! -f /usr/local/bin/firecracker ]; then
    log_step "Installing firecracker to /usr/local/bin..."
    cp "$DATA_DIR/firecracker" /usr/local/bin/firecracker
    chmod +x /usr/local/bin/firecracker
    log_success "Firecracker installed to /usr/local/bin/firecracker"
else
    log_success "Firecracker already in /usr/local/bin"
fi

# Step 7: Verify
log_step "Verifying installation..."
FC_VERSION=$(/usr/local/bin/firecracker --version 2>&1 || echo "FAILED")
echo "  Firecracker version: $FC_VERSION"
echo "  Kernel size: $(du -h "$DATA_DIR/vmlinux" | cut -f1)"
echo "  Rootfs size: $(du -h "$DATA_DIR/rootfs.ext4" | cut -f1)"

echo ""
echo -e "${GREEN}═══════════════════════════════════════════════════════════${NC}"
echo -e "${GREEN}  Setup Complete!${NC}"
echo -e "${GREEN}═══════════════════════════════════════════════════════════${NC}"
echo ""
echo "To enable Firecracker for the Go server, ensure:"
echo "  1. /dev/kvm is mounted into the server container:"
echo "     devices: [\"/dev/kvm:/dev/kvm\"]"
echo "  2. The data directory is mounted:"
echo "     volumes: [\"$DATA_DIR:/var/lib/emergent/firecracker\"]"
echo "  3. The server container has NET_ADMIN capability (for TAP devices):"
echo "     cap_add: [NET_ADMIN]"
echo "  4. The firecracker binary is in PATH inside the server container"
echo ""
echo "The provider will auto-register when KVM is detected."
