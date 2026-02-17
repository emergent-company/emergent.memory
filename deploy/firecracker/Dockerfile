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
    curl -fsSL -o /tmp/SHA256SUMS \
    "https://github.com/firecracker-microvm/firecracker/releases/download/v${FIRECRACKER_VERSION}/SHA256SUMS" && \
    (cd /tmp && grep "${TARBALL}" SHA256SUMS | sha256sum -c -) && \
    tar xzf "/tmp/${TARBALL}" && \
    mv release-v${FIRECRACKER_VERSION}-${ARCH}/firecracker-v${FIRECRACKER_VERSION}-${ARCH} /usr/local/bin/firecracker && \
    chmod +x /usr/local/bin/firecracker && \
    rm -rf /tmp/${TARBALL} /tmp/SHA256SUMS

# Download compatible kernel
RUN ARCH=$(case "$TARGETARCH" in arm64) echo "aarch64";; *) echo "x86_64";; esac) && \
    curl -fsSL -o /vmlinux \
    "https://s3.amazonaws.com/spec.ccfc.min/firecracker-ci/v1.10/${ARCH}/vmlinux-5.10" && \
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
RUN apk add --no-cache --root /rootfs --initdb \
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
    diffutils

# Copy vm-agent
COPY --from=agent-builder /vm-agent /rootfs/usr/bin/vm-agent
RUN chmod +x /rootfs/usr/bin/vm-agent

# Configure init
RUN cat > /rootfs/etc/inittab <<'EOF'
::sysinit:/etc/init.d/rcS
::respawn:/usr/bin/vm-agent
::ctrlaltdel:/sbin/reboot
::shutdown:/bin/umount -a -r
EOF

RUN mkdir -p /rootfs/etc/init.d && \
    cat > /rootfs/etc/init.d/rcS <<'INITEOF'
#!/bin/sh
mount -t proc proc /proc
mount -t sysfs sysfs /sys
mount -t devtmpfs devtmpfs /dev
hostname emergent-vm
ip link set lo up
ip link set eth0 up
sleep 0.5
if [ -e /dev/vdb ]; then
    mkdir -p /workspace
    mount /dev/vdb /workspace
fi
echo "Emergent VM agent starting..."
INITEOF
    chmod +x /rootfs/etc/init.d/rcS

RUN echo "nameserver 8.8.8.8" > /rootfs/etc/resolv.conf && \
    echo "nameserver 8.8.4.4" >> /rootfs/etc/resolv.conf

RUN echo "root:x:0:0:root:/root:/bin/bash" > /rootfs/etc/passwd && \
    echo "root:x:0:" > /rootfs/etc/group && \
    mkdir -p /rootfs/workspace && chmod 777 /rootfs/workspace

# Create ext4 image (512MB sparse)
RUN truncate -s 512M /rootfs.ext4 && \
    mkfs.ext4 -F -L rootfs /rootfs.ext4 && \
    mkdir -p /mnt/rootfs && \
    mount -o loop /rootfs.ext4 /mnt/rootfs && \
    cp -a /rootfs/* /mnt/rootfs/ && \
    umount /mnt/rootfs

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
