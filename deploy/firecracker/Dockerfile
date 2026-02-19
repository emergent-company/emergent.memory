# Dockerfile for Firecracker MicroVM Manager
#
# This container runs the Firecracker binary and manages microVMs for agent
# workspaces. It requires /dev/kvm access (KVM-capable host) and runs in
# privileged mode for TAP device and iptables management.
#
# The container includes:
#   - Firecracker binary (Apache 2.0)
#   - vmlinux kernel for microVMs
#   - Base rootfs.ext4 with vm-agent, git, and build tools
#   - iptables for VM NAT networking
#   - iproute2 for TAP device management
#
# Build context: REPOSITORY ROOT (.)
# Usage: docker build -f deploy/firecracker/Dockerfile .
#
# NOTE: This image is only needed when using the Firecracker provider.
# The gVisor provider (default) does not require this container.

ARG FIRECRACKER_VERSION=1.10.1

# ──────────────────────────────────────────────────────────────────────
# Stage 1: Download Firecracker binary
# ──────────────────────────────────────────────────────────────────────
FROM alpine:3.20 AS firecracker-downloader

ARG FIRECRACKER_VERSION
ARG TARGETARCH=amd64

RUN apk add --no-cache curl tar

# Download official Firecracker release and verify checksum
RUN ARCH=$(case "$TARGETARCH" in arm64) echo "aarch64";; *) echo "x86_64";; esac) && \
    TARBALL="firecracker-v${FIRECRACKER_VERSION}-${ARCH}.tgz" && \
    curl -fsSL -o "/tmp/${TARBALL}" \
    "https://github.com/firecracker-microvm/firecracker/releases/download/v${FIRECRACKER_VERSION}/${TARBALL}" && \
    curl -fsSL -o "/tmp/${TARBALL}.sha256.txt" \
    "https://github.com/firecracker-microvm/firecracker/releases/download/v${FIRECRACKER_VERSION}/${TARBALL}.sha256.txt" && \
    (cd /tmp && sha256sum -c "${TARBALL}.sha256.txt") && \
    tar xzf "/tmp/${TARBALL}" && \
    mv release-v${FIRECRACKER_VERSION}-${ARCH}/firecracker-v${FIRECRACKER_VERSION}-${ARCH} /usr/local/bin/firecracker && \
    chmod +x /usr/local/bin/firecracker && \
    rm -rf /tmp/${TARBALL} /tmp/${TARBALL}.sha256.txt

# Download compatible kernel (6.1.102 from CI - supports virtio-rng and modern features)
RUN ARCH=$(case "$TARGETARCH" in arm64) echo "aarch64";; *) echo "x86_64";; esac) && \
    curl -fsSL -o /vmlinux \
    "https://s3.amazonaws.com/spec.ccfc.min/firecracker-ci/v1.10/${ARCH}/vmlinux-6.1.102" && \
    chmod +r /vmlinux

# ──────────────────────────────────────────────────────────────────────
# Stage 2: Build vm-agent binary
# ──────────────────────────────────────────────────────────────────────
FROM golang:1.24-alpine AS agent-builder

RUN apk add --no-cache git

WORKDIR /build
COPY apps/server-go/go.mod apps/server-go/go.sum ./
RUN go mod download

COPY apps/server-go/ ./

RUN CGO_ENABLED=0 GOOS=linux go build \
    -ldflags="-s -w -extldflags '-static'" \
    -o /vm-agent \
    ./cmd/vm-agent/main.go

# ──────────────────────────────────────────────────────────────────────
# Stage 3: Build rootfs image
# ──────────────────────────────────────────────────────────────────────
FROM alpine:3.20 AS rootfs-builder

RUN apk add --no-cache e2fsprogs

# Install packages for rootfs
RUN mkdir -p /rootfs && \
    for d in bin sbin usr/bin usr/sbin lib etc dev proc sys tmp run var/log var/run workspace usr/lib usr/share; do \
        mkdir -p /rootfs/$d; \
    done

# Copy Alpine base into rootfs
RUN mkdir -p /rootfs/etc/apk && \
    cp /etc/apk/repositories /rootfs/etc/apk/repositories && \
    cp -r /etc/apk/keys /rootfs/etc/apk/ && \
    (apk add --no-cache --no-scripts --root /rootfs --initdb \
    alpine-baselayout \
    busybox \
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
    make \
    patch \
    diffutils || true)

# Copy vm-agent
COPY --from=agent-builder /vm-agent /rootfs/usr/bin/vm-agent
RUN chmod +x /rootfs/usr/bin/vm-agent

# Install BusyBox symlinks (Alpine 3.20 doesn't create them with --no-scripts)
RUN chroot /rootfs /bin/busybox --install -s

# Set up CA certificates (--no-scripts skips the post-install hook that creates cert.pem)
RUN chroot /rootfs /usr/sbin/update-ca-certificates 2>/dev/null || \
    (cat /rootfs/usr/share/ca-certificates/mozilla/*.crt > /rootfs/etc/ssl/cert.pem 2>/dev/null && \
     ln -sf /etc/ssl/cert.pem /rootfs/etc/ssl/certs/ca-certificates.crt) || true

# Create /sbin/init script (BusyBox on Alpine 3.20 doesn't include init applet)
# Remove any existing busybox symlink first to avoid "Text file busy" error
RUN rm -f /rootfs/sbin/init && cat > /rootfs/sbin/init <<'INITEOF'
#!/bin/sh
# Firecracker VM init script
# NOTE: rootfs may be mounted read-only (shared across VMs)
mount -t proc proc /proc
mount -t sysfs sysfs /sys
mount -t devtmpfs devtmpfs /dev 2>/dev/null || true
mkdir -p /dev/pts /dev/shm
mount -t devpts devpts /dev/pts 2>/dev/null || true
mount -t tmpfs tmpfs /dev/shm
mount -t tmpfs tmpfs /run
mount -t tmpfs tmpfs /tmp

# Try to remount root read-write (works if disk is rw, harmless if not)
mount -o remount,rw / 2>/dev/null

# Set hostname (may fail on read-only rootfs, non-fatal)
hostname emergent-vm 2>/dev/null || true

# Bring up loopback
ip link set lo up

# Bring up eth0 (IP configured by kernel boot args)
ip link set eth0 up

# Wait briefly for network interface
sleep 0.3

# Configure DNS — use tmpfs overlay on /etc if rootfs is read-only
if ! echo "nameserver 8.8.8.8" > /etc/resolv.conf 2>/dev/null; then
    # Rootfs is read-only, create a tmpfs overlay for /etc
    mkdir -p /tmp/etc-overlay
    cp -a /etc/* /tmp/etc-overlay/ 2>/dev/null || true
    mount -t tmpfs tmpfs /etc
    cp -a /tmp/etc-overlay/* /etc/ 2>/dev/null || true
    rm -rf /tmp/etc-overlay
    echo "nameserver 8.8.8.8" > /etc/resolv.conf
    echo "nameserver 8.8.4.4" >> /etc/resolv.conf
else
    echo "nameserver 8.8.4.4" >> /etc/resolv.conf
fi

# Mount data disk if present
if [ -e /dev/vdb ]; then
    mkdir -p /workspace 2>/dev/null || true
    mount /dev/vdb /workspace
fi

# Start vm-agent
echo "Starting Emergent VM agent..."
exec /usr/bin/vm-agent
INITEOF

RUN chmod +x /rootfs/sbin/init

RUN echo "nameserver 8.8.8.8" > /rootfs/etc/resolv.conf && \
    echo "nameserver 8.8.4.4" >> /rootfs/etc/resolv.conf

RUN echo "root:x:0:0:root:/root:/bin/bash" > /rootfs/etc/passwd && \
    echo "root:x:0:" > /rootfs/etc/group && \
    mkdir -p /rootfs/workspace && chmod 777 /rootfs/workspace

# Create ext4 image (512MB sparse) — uses mkfs.ext4 -d to populate without mount
RUN truncate -s 512M /rootfs.ext4 && \
    mkfs.ext4 -F -L rootfs -d /rootfs /rootfs.ext4

# ──────────────────────────────────────────────────────────────────────
# Stage 4: Runtime image
# ──────────────────────────────────────────────────────────────────────
FROM alpine:3.20

RUN apk add --no-cache \
    iptables \
    iproute2 \
    curl \
    e2fsprogs \
    ca-certificates \
    && rm -rf /var/cache/apk/*

# Create directory structure
RUN mkdir -p \
    /var/lib/firecracker/data/sockets \
    /var/lib/firecracker/data/snapshots

# Copy Firecracker binary
COPY --from=firecracker-downloader /usr/local/bin/firecracker /usr/local/bin/firecracker

# Copy kernel
COPY --from=firecracker-downloader /vmlinux /var/lib/firecracker/vmlinux

# Copy rootfs
COPY --from=rootfs-builder /rootfs.ext4 /var/lib/firecracker/rootfs.ext4

# Environment defaults (overridden by docker-compose)
ENV FIRECRACKER_BIN=/usr/local/bin/firecracker \
    FIRECRACKER_KERNEL=/var/lib/firecracker/vmlinux \
    FIRECRACKER_ROOTFS=/var/lib/firecracker/rootfs.ext4 \
    FIRECRACKER_DATA_DIR=/var/lib/firecracker/data \
    FIRECRACKER_MAX_VMS=10 \
    FIRECRACKER_DEFAULT_VCPUS=2 \
    FIRECRACKER_DEFAULT_MEM_MB=512 \
    LOG_LEVEL=info

# Data volume for VM block devices and snapshots
VOLUME ["/var/lib/firecracker/data"]

# Health check — verify firecracker binary and KVM access
HEALTHCHECK --interval=30s --timeout=10s --retries=3 \
    CMD firecracker --version > /dev/null 2>&1 && test -w /dev/kvm

# The container stays running — the Go server communicates with Firecracker
# directly via the binary and socket files in the shared data volume.
# This container just provides the Firecracker binary, kernel, and rootfs.
#
# In production, the Go server's FirecrackerProvider spawns firecracker
# processes directly. This container serves as the environment for that.
ENTRYPOINT ["sleep", "infinity"]
