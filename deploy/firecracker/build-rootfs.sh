#!/bin/bash
# Build all Firecracker rootfs variants
#
# Usage: ./build-rootfs.sh [output_dir]
# 
# Builds 4 rootfs images:
#   - rootfs-base.ext4       (512MB, minimal Alpine)
#   - rootfs-coder.ext4      (1GB, with Python/Go/Node.js)
#   - rootfs-researcher.ext4 (512MB, with Python/jq/wget)
#   - rootfs-reviewer.ext4   (768MB, with Python linters)
#
# Output directory defaults to /var/lib/emergent/firecracker/

set -euo pipefail

# Configuration
OUTPUT_DIR="${1:-/var/lib/emergent/firecracker}"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
DOCKERFILE="$SCRIPT_DIR/Dockerfile.rootfs"

# Variants to build
VARIANTS=("base" "coder" "researcher" "reviewer")

echo "========================================"
echo "Firecracker Rootfs Builder"
echo "========================================"
echo "Repository root: $REPO_ROOT"
echo "Dockerfile:      $DOCKERFILE"
echo "Output dir:      $OUTPUT_DIR"
echo ""

# Ensure output directory exists
mkdir -p "$OUTPUT_DIR"

# Build each variant
for variant in "${VARIANTS[@]}"; do
    echo "----------------------------------------"
    echo "Building rootfs-${variant}..."
    echo "----------------------------------------"
    
    start_time=$(date +%s)
    
    # Create temporary directory for output
    tmp_output=$(mktemp -d)
    
    # Build with docker buildx to extract output
    docker buildx build \
        --target "rootfs-${variant}" \
        -f "$DOCKERFILE" \
        --output "type=local,dest=$tmp_output" \
        "$REPO_ROOT"
    
    # Move the extracted .ext4 file to output directory
    mv "$tmp_output/rootfs-${variant}.ext4" "$OUTPUT_DIR/"
    
    # Clean up temp directory
    rm -rf "$tmp_output"
    
    end_time=$(date +%s)
    elapsed=$((end_time - start_time))
    
    # Show file info
    file_size=$(du -h "$OUTPUT_DIR/rootfs-${variant}.ext4" | cut -f1)
    echo "✓ Built rootfs-${variant}.ext4 ($file_size) in ${elapsed}s"
    echo ""
done

echo "========================================"
echo "Build complete!"
echo "========================================"
echo "Output files in $OUTPUT_DIR:"
ls -lh "$OUTPUT_DIR"/rootfs-*.ext4
echo ""
echo "Next steps:"
echo "  1. Modify firecracker_provider.go to add resolveRootfsPath()"
echo "  2. Modify auto_provisioner.go to skip warm pool for specific images"
echo "  3. Update agent definitions with base_image values"
echo "  4. Restart server to pick up new rootfs images"
