# Workspace Optimization Guide

## Current Performance: 35 seconds

### Breakdown:

- **Docker container creation**: 21.5 seconds (62%) ⚠️ **BOTTLENECK**
- **Git clone**: 8.7 seconds (25%)
- **Container startup**: 3.1 seconds (9%)
- **Other operations**: 1.5 seconds (4%)

---

## Optimization 1: Shallow Git Clone (~7 second improvement)

### What is Shallow Clone?

Instead of downloading the entire git history, only download the latest commit.

**Normal Clone (current):**

```bash
git clone https://github.com/emergent-company/emergent.git
# Downloads: All commits, all branches, full history (~500MB)
# Time: ~8.7 seconds
```

**Shallow Clone:**

```bash
git clone --depth=1 https://github.com/emergent-company/emergent.git
# Downloads: Only latest commit, minimal .git directory (~60-80MB)
# Time: ~1-2 seconds
# Saves: ~7 seconds per workspace
```

### Trade-offs:

| Feature                 | Normal Clone | Shallow Clone          |
| ----------------------- | ------------ | ---------------------- |
| Download size           | ~500MB       | ~60-80MB (8x smaller)  |
| Clone time              | ~8.7 sec     | ~1-2 sec               |
| `git log`               | Full history | Only latest commit     |
| `git blame`             | Full blame   | Works on current files |
| Checkout old commits    | ✅ Yes       | ❌ No                  |
| **Good for AI agents?** | ✅ Yes       | ✅ **Perfect**         |

### When to Use:

✅ **Use Shallow Clone for:**

- AI agent workspaces (agents need current code, not history)
- CI/CD builds
- Quick code analysis
- One-time tasks

❌ **Use Full Clone for:**

- Development environments
- When you need `git blame` history
- Debugging "when did this break?"
- Long-lived workspaces

### Implementation:

**File**: `apps/server-go/domain/workspace/checkout.go`

```go
// Add ShallowClone option to CheckoutOptions
type CheckoutOptions struct {
    RepoURL      string
    Branch       string
    ShallowClone bool  // NEW: Enable shallow clone
}

// In Clone() method, modify git clone command:
func (s *CheckoutService) Clone(ctx context.Context, opts CheckoutOptions) error {
    args := []string{"clone"}

    // Add shallow clone flag
    if opts.ShallowClone {
        args = append(args, "--depth=1")
    }

    args = append(args, opts.RepoURL, "/workspace")
    // ... rest of implementation
}
```

**Expected Result**: 35 sec → **~28 seconds** (20% improvement)

---

## Optimization 2: Enable KVM for Firecracker (~20 second improvement)

### What is KVM?

**KVM (Kernel-based Virtual Machine)** is Linux's native virtualization technology. It allows running lightweight VMs (like Firecracker) that start in milliseconds instead of seconds.

### Current Status on mcj-emergent:

✅ **KVM is already available!**

```bash
$ ls -l /dev/kvm
crw-rw-rw- 1 root kvm 10, 232 Feb 12 22:08 /dev/kvm
                   ^^^
                   KVM device exists

$ grep vmx /proc/cpuinfo
vmx flags: vnmi preemption_timer invvpid ept_x_only ...
^^^
Intel VT-x virtualization enabled
```

### Performance Comparison:

| Technology           | Startup Time  | Isolation         | Notes              |
| -------------------- | ------------- | ----------------- | ------------------ |
| **Docker** (current) | ~25 seconds   | Process-level     | What we use now    |
| **gVisor runsc**     | ~5-10 seconds | Syscall intercept | Better than Docker |
| **Firecracker**      | **~125ms**    | MicroVM (KVM)     | **200x faster!**   |

### How to Enable:

#### Step 1: Pass KVM device to server container

**File**: `/root/.emergent/docker/docker-compose.yml` (on mcj-emergent)

```yaml
services:
  server:
    image: ghcr.io/emergent-company/emergent-server-with-cli:0.16.4
    container_name: emergent-server
    volumes:
      - emergent_cli_config:/root/.emergent
      - /var/run/docker.sock:/var/run/docker.sock
    devices:
      - /dev/kvm:/dev/kvm # ADD THIS LINE
    environment:
      ENABLE_AGENT_WORKSPACES: 'true'
```

#### Step 2: Restart server

```bash
cd /root/.emergent/docker
docker-compose up -d server
```

#### Step 3: Install Firecracker binary (if not already in image)

Firecracker is a static binary (~15MB). Either:

**Option A**: Add to Dockerfile (best)

```dockerfile
FROM alpine:3.19
RUN wget https://github.com/firecracker-microvm/firecracker/releases/download/v1.7.0/firecracker-v1.7.0-x86_64.tgz \
    && tar -xzf firecracker-v1.7.0-x86_64.tgz \
    && mv release-v1.7.0-x86_64/firecracker-v1.7.0-x86_64 /usr/local/bin/firecracker \
    && chmod +x /usr/local/bin/firecracker
```

**Option B**: Install at runtime (temporary testing)

```bash
docker exec emergent-server bash -c "
  wget https://github.com/firecracker-microvm/firecracker/releases/download/v1.7.0/firecracker-v1.7.0-x86_64.tgz &&
  tar -xzf firecracker-v1.7.0-x86_64.tgz &&
  mv release-v1.7.0-x86_64/firecracker-v1.7.0-x86_64 /usr/local/bin/firecracker &&
  chmod +x /usr/local/bin/firecracker
"
```

#### Step 4: Implement Firecracker provider (requires code)

This is more complex and requires implementing a new provider in Go:

**File**: `apps/server-go/domain/workspace/firecracker_provider.go` (NEW)

This would be similar to `gvisor_provider.go` but using Firecracker SDK instead of Docker SDK.

**Firecracker advantages:**

- 125ms boot time (vs 25 seconds)
- Better isolation than containers
- Lower memory overhead
- Purpose-built for serverless workloads

**Firecracker challenges:**

- More complex implementation
- Requires custom kernel and rootfs images
- Need to manage networking manually
- Less mature tooling than Docker

### Architecture Comparison:

```
┌─────────────────────────────────────────────────────────┐
│                     Current (Docker)                     │
├─────────────────────────────────────────────────────────┤
│  Host Linux Kernel                                       │
│    ├─ Docker Engine                                      │
│    │   ├─ emergent-server container                      │
│    │   │   ├─ workspace container 1 (25s startup)        │
│    │   │   ├─ workspace container 2 (25s startup)        │
│    │   │   └─ workspace container 3 (25s startup)        │
└─────────────────────────────────────────────────────────┘
         Process isolation, shared kernel

┌─────────────────────────────────────────────────────────┐
│                  With Firecracker                        │
├─────────────────────────────────────────────────────────┤
│  Host Linux Kernel                                       │
│    ├─ KVM                                                │
│    │   ├─ emergent-server container                      │
│    │   │   ├─ Firecracker microVM 1 (125ms startup) 🚀   │
│    │   │   ├─ Firecracker microVM 2 (125ms startup) 🚀   │
│    │   │   └─ Firecracker microVM 3 (125ms startup) 🚀   │
└─────────────────────────────────────────────────────────┘
         VM isolation, separate kernels, much faster
```

**Expected Result**: 35 sec → **~10 seconds** (with Firecracker + shallow clone)

---

## Optimization 3: Smaller Base Image (~2 second improvement)

### Current Image: 349MB

**Location**: `docker/workspace-base.Dockerfile`

**Issue**: Includes `build-base` package (~220MB) which adds:

- gcc, g++, make
- Build tools most agents don't need

### Solution: Create Image Variants

#### Minimal Image (~130MB):

```dockerfile
FROM alpine:3.19
RUN apk add --no-cache \
    bash \
    git \
    curl \
    ca-certificates \
    jq \
    ripgrep \
    sed \
    gawk \
    findutils \
    tar \
    gzip
```

#### Full Image (~349MB) - Keep current for agents that need to compile:

```dockerfile
FROM alpine:3.19
RUN apk add --no-cache \
    bash \
    git \
    curl \
    ca-certificates \
    jq \
    ripgrep \
    sed \
    gawk \
    findutils \
    tar \
    gzip \
    build-base  # gcc, make, etc.
```

### Implementation:

Build both variants:

```bash
cd docker
docker build -f workspace-base.Dockerfile --target minimal -t emergent-workspace:minimal .
docker build -f workspace-base.Dockerfile --target full -t emergent-workspace:full .
```

Let user specify in workspace creation:

```json
{
  "repository_url": "https://github.com/example/repo.git",
  "base_image": "emergent-workspace:minimal" // or "full"
}
```

**Expected Result**: 35 sec → **~33 seconds** (less extraction time)

---

## Optimization 4: Warm Pool (~25 second improvement)

### Concept: Pre-create idle containers

Instead of creating containers on-demand, keep 1-3 containers "warm" and ready.

**How it works:**

1. Background service maintains N warm containers
2. When workspace requested, assign an existing container
3. Immediately start creating replacement warm container
4. Result: Instant workspace provisioning (for first N requests)

### Implementation:

**File**: `apps/server-go/domain/workspace/warm_pool.go` (already exists!)

Check current setting:

```bash
# In environment or config
WORKSPACE_WARM_POOL_SIZE=0  # Currently disabled
```

Enable warm pool:

```bash
# In docker-compose.yml
environment:
  WORKSPACE_WARM_POOL_SIZE: '2'  # Keep 2 containers warm
```

**Trade-offs:**

- ✅ Near-instant workspace creation (for first 2 requests)
- ✅ Better user experience
- ❌ Uses resources when idle (2 containers × 4GB RAM = 8GB reserved)
- ❌ Slightly more complex

**Expected Result**: First 2 workspaces → **~9 seconds** (just git clone)

---

## Optimization 5: Parallel Operations (~3 second improvement)

### Current: Sequential operations

```
1. Create volume (0.8s)
2. Create container (21.5s)  ← Wait for volume
3. Start container (3.1s)     ← Wait for create
4. Clone repository (8.7s)    ← Wait for start
Total: 34.1 seconds
```

### Optimized: Parallel where possible

```
1. Create volume (0.8s) ┐
                         ├─→ 2. Create container (21.5s)
                         │
3. Pre-fetch git refs (2s) ← Can start during container creation
                         │
                         └─→ 4. Start container (3.1s)
                              5. Clone repository (6.7s) ← Faster with pre-fetch
Total: ~28 seconds
```

**Expected Result**: 35 sec → **~28 seconds**

---

## Summary: Combined Optimizations

| Optimization    | Time Saved | Difficulty         | Total Time          |
| --------------- | ---------- | ------------------ | ------------------- |
| **Baseline**    | -          | -                  | **35 sec**          |
| + Shallow clone | -7 sec     | ⭐ Easy            | **28 sec**          |
| + Smaller image | -2 sec     | ⭐ Easy            | **26 sec**          |
| + Parallel ops  | -3 sec     | ⭐⭐ Medium        | **23 sec**          |
| + gVisor runsc  | -10 sec    | ⭐⭐⭐ Hard        | **13 sec**          |
| + Firecracker   | -20 sec    | ⭐⭐⭐⭐ Very Hard | **10 sec**          |
| + Warm pool     | -9 sec     | ⭐⭐ Medium        | **1 sec** (first N) |

### Recommended Approach:

#### Phase 1: Quick Wins (1-2 hours implementation)

1. ✅ **Enable shallow clone** → 28 seconds
2. ✅ **Build minimal image** → 26 seconds
3. ✅ **Add parallel operations** → 23 seconds

**Result**: 34% improvement with minimal effort

#### Phase 2: Warm Pool (2-3 hours)

4. ✅ **Enable warm pool (size=2)** → 9 seconds (first 2 workspaces)

**Result**: 74% improvement for typical usage

#### Phase 3: Advanced (1-2 weeks)

5. ⚠️ **Implement Firecracker provider** → 3-5 seconds

**Result**: 86-91% improvement, production-grade

---

## Next Steps

### Immediate (Today):

1. **Test shallow clone:**

```bash
# Edit apps/server-go/domain/workspace/checkout.go
# Add --depth=1 flag to git clone command
# Test with: bash /tmp/test-workspace-comprehensive.sh ...
```

2. **Build minimal image:**

```bash
cd docker
# Create workspace-base-minimal.Dockerfile
docker build -f workspace-base-minimal.Dockerfile -t emergent-workspace:minimal .
# Test provisioning time
```

### Short-term (This Week):

3. **Enable warm pool:**

```bash
# Update docker-compose.yml
WORKSPACE_WARM_POOL_SIZE: '2'
# Monitor resource usage
```

### Long-term (Next Month):

4. **Research Firecracker integration:**

- Study Firecracker SDK for Go
- Design rootfs image for workspaces
- Implement proof-of-concept provider
- Benchmark vs Docker

---

## Testing Performance

After each optimization, run benchmark:

```bash
#!/bin/bash
# benchmark-workspace.sh

for i in {1..10}; do
  START=$(date +%s%3N)

  WORKSPACE_ID=$(curl -s -X POST http://localhost:3002/api/v1/agent/workspaces \
    -H "X-API-Key: $API_KEY" \
    -H "Content-Type: application/json" \
    -d '{
      "repository_url": "https://github.com/emergent-company/emergent.git",
      "branch": "main"
    }' | jq -r '.id')

  # Wait for ready
  while true; do
    STATUS=$(curl -s http://localhost:3002/api/v1/agent/workspaces/$WORKSPACE_ID \
      -H "X-API-Key: $API_KEY" | jq -r '.status')
    [[ "$STATUS" == "ready" ]] && break
    sleep 1
  done

  END=$(date +%s%3N)
  DURATION=$((END - START))
  echo "Run $i: ${DURATION}ms"

  # Cleanup
  curl -s -X DELETE http://localhost:3002/api/v1/agent/workspaces/$WORKSPACE_ID \
    -H "X-API-Key: $API_KEY"
done
```

Compare before/after results to measure improvement.
