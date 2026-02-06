# Image Build Strategy Analysis

## Question: What Should We Build vs. Use Upstream?

### Current Situation

**What we have**:

1. **Custom Postgres Image** (`docker/Dockerfile.postgres`)

   - Base: `pgvector/pgvector:pg16` (723MB)
   - Adds: `init.sql` script (26 lines)
   - Purpose: Creates pgvector extension + app_rls role

2. **Third-party services**:

   - Kreuzberg: `goldziher/kreuzberg:latest`
   - MinIO: `minio/minio:latest`
   - Tailscale: `tailscale/tailscale:latest`

3. **Our code**:
   - Go backend: `apps/server-go/Dockerfile`

---

## Analysis: Should We Build Custom Images?

### PostgreSQL + pgvector

**Option 1: Build Custom Image** ✅ RECOMMENDED

```dockerfile
FROM pgvector/pgvector:pg16
COPY init.sql /docker-entrypoint-initdb.d/00-init.sql
```

**Pros**:

- ✅ Initializes extensions automatically (`vector`, `pgcrypto`)
- ✅ Creates `app_rls` role automatically
- ✅ Works out-of-the-box (no manual setup)
- ✅ Only adds 4KB to base image (init.sql is tiny)
- ✅ Reproducible across installations

**Cons**:

- ❌ Extra ~10 seconds build time in CI
- ❌ Need to maintain our own image

**Size Impact**: 723MB → 723MB (negligible difference)

---

**Option 2: Use Upstream pgvector/pgvector:pg16 Directly**

User would need to:

```bash
docker exec emergent-db psql -U emergent -c "CREATE EXTENSION vector;"
docker exec emergent-db psql -U emergent -c "CREATE EXTENSION pgcrypto;"
docker exec emergent-db psql -U emergent -c "CREATE ROLE app_rls;"
```

**Pros**:

- ✅ No custom image to maintain
- ✅ Direct upstream updates

**Cons**:

- ❌ Manual setup required after first start
- ❌ Easy to forget steps → broken installation
- ❌ Poor user experience
- ❌ Install script becomes more complex

**Verdict**: Build custom Postgres image. The init.sql is trivial (26 lines) and provides huge UX benefit.

---

### Kreuzberg (Document Extraction)

**Current**: `goldziher/kreuzberg:latest` (2.62GB)

**Should we build our own?** ❌ NO

**Rationale**:

- ❌ Complex Rust project with many dependencies
- ❌ We don't modify it
- ❌ Upstream is actively maintained
- ❌ Building would add significant CI time (~10 minutes)
- ✅ Upstream image works perfectly as-is

**Action**: Use `goldziher/kreuzberg:latest` directly in docker-compose.yml

---

### MinIO (S3 Storage)

**Current**: `minio/minio:latest` (~227MB)

**Should we build our own?** ❌ NO

**Rationale**:

- ❌ Official MinIO image is production-ready
- ❌ We don't modify it
- ❌ Upstream is well-maintained by MinIO team
- ❌ No customization needed
- ✅ Bucket initialization handled by init container

**Action**: Use `minio/minio:latest` directly in docker-compose.yml

---

### Tailscale

**Current**: `tailscale/tailscale:latest` (~200MB estimated)

**Should we build our own?** ❌ NO

**Rationale**:

- ❌ Official Tailscale image
- ❌ Maintained by Tailscale team
- ❌ Security-sensitive (better to use official)
- ❌ No customization needed
- ✅ All config via environment variables

**Action**: Use `tailscale/tailscale:latest` directly in docker-compose.yml

---

### Go Backend Server

**Current**: `apps/server-go/Dockerfile` (builds to ~18MB)

**Should we build our own?** ✅ YES - THIS IS OUR CODE

**Rationale**:

- ✅ This is our application
- ✅ We control the source code
- ✅ Need to update with every feature release
- ✅ Optimized multi-stage build (18MB final)
- ✅ Uses distroless base for security

**Action**: Build and publish `ghcr.io/emergent-company/emergent-server-go:VERSION`

---

## Recommended Build Strategy

### What We Build and Publish to GHCR

**1. Go Backend Server** (REQUIRED)

```
Image: ghcr.io/emergent-company/emergent-server-go:latest
Base: gcr.io/distroless/static-debian12:nonroot
Size: ~18MB
Build time: ~3 minutes
Reason: Our application code
```

**2. Custom PostgreSQL** (RECOMMENDED)

```
Image: ghcr.io/emergent-company/emergent-postgres:latest
Base: pgvector/pgvector:pg16
Size: ~723MB (same as base + 4KB)
Build time: ~10 seconds
Reason: Auto-initialization improves UX significantly
```

**Total custom images**: 2 images, ~741MB, ~3 min build time

---

### What We Use Upstream (No Custom Build)

**3. Kreuzberg** - Use `goldziher/kreuzberg:latest`
**4. MinIO** - Use `minio/minio:latest`
**5. Tailscale** - Use `tailscale/tailscale:latest`

---

## Total Download Size for Users

```
User runs: curl -fsSL https://install.emergent.ai/minimal | bash

Downloads:
├── ghcr.io/emergent-company/emergent-server-go:latest    ~18MB
├── ghcr.io/emergent-company/emergent-postgres:latest     ~723MB
├── goldziher/kreuzberg:latest                            ~2.6GB
├── minio/minio:latest                                     ~227MB
├── minio/mc:latest (init container)                       ~50MB
└── tailscale/tailscale:latest                            ~200MB

Total: ~3.8GB uncompressed
       ~1.2GB compressed (typical 3:1 compression ratio)

First install time: ~5 minutes (on 50 Mbps connection)
```

---

## Docker Compose Configuration

### Before (Build Everything)

```yaml
services:
  db:
    build:
      context: ../../docker
      dockerfile: Dockerfile.postgres

  server:
    build:
      context: ../..
      dockerfile: apps/server-go/Dockerfile

  kreuzberg:
    image: goldziher/kreuzberg:latest

  minio:
    image: minio/minio:latest

  tailscale:
    image: tailscale/tailscale:latest
```

**User impact**:

- ❌ Build time: ~5 minutes (Go compilation)
- ❌ Needs Git checkout of full repo
- ❌ Inconsistent (different Go versions, network issues)

---

### After (Use Pre-built Images)

```yaml
services:
  db:
    image: ghcr.io/emergent-company/emergent-postgres:${VERSION:-latest}
    pull_policy: always

  server:
    image: ghcr.io/emergent-company/emergent-server-go:${VERSION:-latest}
    pull_policy: always

  kreuzberg:
    image: goldziher/kreuzberg:latest

  minio:
    image: minio/minio:latest

  tailscale:
    image: tailscale/tailscale:latest
```

**User impact**:

- ✅ Download time: ~3 minutes (just pull images)
- ✅ No Git clone needed (script downloads docker-compose.yml)
- ✅ Consistent (same tested images for everyone)

---

## CI/CD Build Matrix

### GitHub Actions Workflow

**Job 1: Build Server-Go** (~3 minutes)

```yaml
- name: Build server-go
  platforms: linux/amd64,linux/arm64
  tags: |
    ghcr.io/emergent-company/emergent-server-go:latest
    ghcr.io/emergent-company/emergent-server-go:v1.0.0
```

**Job 2: Build Postgres** (~10 seconds)

```yaml
- name: Build postgres with init
  platforms: linux/amd64,linux/arm64
  tags: |
    ghcr.io/emergent-company/emergent-postgres:latest
    ghcr.io/emergent-company/emergent-postgres:v1.0.0
```

**Total CI time**: ~3 minutes (builds run in parallel)

**Trigger**:

- On git tag: `v*` (e.g., `v1.0.0`)
- Weekly schedule (rebuild for security patches)
- Manual workflow dispatch

---

## Alternative: Single "All-in-One" Image?

Some projects bundle everything into one mega-image. Should we?

**Option: Build emergent-all:latest with everything**

**Pros**:

- ✅ Single docker pull
- ✅ Simplest for users

**Cons**:

- ❌ HUGE image size (~4GB+)
- ❌ Can't update components independently
- ❌ Violates Docker best practices (one process per container)
- ❌ No health checks per service
- ❌ No independent scaling
- ❌ Complex init system needed (supervisord/systemd)
- ❌ Harder to debug issues

**Verdict**: ❌ Don't do this. Multi-container with Docker Compose is cleaner.

---

## Recommended Approach: Hybrid

### Build Our Code Only

**What we build**:

1. ✅ `emergent-server-go` - Our Go backend (18MB)
2. ✅ `emergent-postgres` - Postgres + our init script (723MB)

**What we reference**: 3. ✅ `goldziher/kreuzberg:latest` - Upstream 4. ✅ `minio/minio:latest` - Upstream 5. ✅ `tailscale/tailscale:latest` - Upstream

**Why this is optimal**:

- ✅ Fast CI builds (~3 min total)
- ✅ Small maintenance burden (only 2 images)
- ✅ Leverage upstream maintenance for third-party services
- ✅ Auto-init for Postgres (great UX)
- ✅ User gets tested, reproducible images
- ✅ Can pin versions: `minio:RELEASE.2024-01-01T00-00-00Z`

---

## Version Pinning Strategy

### Production Stability

For minimal deployment, we should pin third-party images to specific versions:

```yaml
services:
  kreuzberg:
    image: goldziher/kreuzberg:latest # Or pin: kreuzberg:v2.0.0

  minio:
    image: minio/minio:RELEASE.2024-02-01T00-00-00Z # Pin to specific release

  tailscale:
    image: tailscale/tailscale:v1.58.2 # Pin to version
```

**Trade-off**:

- ✅ More stable (no surprise breakage)
- ✅ Reproducible deployments
- ❌ Need to manually update pins
- ❌ Miss security patches if we forget to update

**Recommendation**:

- Use `:latest` for development
- Pin versions for production releases
- Document upgrade process: "Update pinned versions quarterly"

---

## Summary Table

| Component  | Build Custom? | Publish to GHCR? | Reason                             |
| ---------- | ------------- | ---------------- | ---------------------------------- |
| Go Server  | ✅ YES        | ✅ YES           | Our application code               |
| PostgreSQL | ✅ YES        | ✅ YES           | Tiny init script, big UX win       |
| Kreuzberg  | ❌ NO         | ❌ NO            | Complex upstream, use as-is        |
| MinIO      | ❌ NO         | ❌ NO            | Official image works perfectly     |
| Tailscale  | ❌ NO         | ❌ NO            | Official image, security-sensitive |

**Images to build**: 2  
**Build time**: ~3 minutes  
**Maintenance**: Low (only update on our code changes)

---

## Updated Implementation Plan

### Phase 1: GitHub Actions

Create `.github/workflows/publish-images.yml`:

```yaml
name: Publish Docker Images

on:
  push:
    tags:
      - 'v*'

jobs:
  server-go:
    runs-on: ubuntu-latest
    steps:
      - uses: docker/build-push-action@v5
        with:
          file: apps/server-go/Dockerfile
          platforms: linux/amd64,linux/arm64
          push: true
          tags: |
            ghcr.io/emergent-company/emergent-server-go:latest
            ghcr.io/emergent-company/emergent-server-go:${{ github.ref_name }}

  postgres:
    runs-on: ubuntu-latest
    steps:
      - uses: docker/build-push-action@v5
        with:
          context: docker
          file: docker/Dockerfile.postgres
          platforms: linux/amd64,linux/arm64
          push: true
          tags: |
            ghcr.io/emergent-company/emergent-postgres:latest
            ghcr.io/emergent-company/emergent-postgres:${{ github.ref_name }}
```

### Phase 2: Update docker-compose.yml

```yaml
services:
  db:
    image: ghcr.io/emergent-company/emergent-postgres:${VERSION:-latest}

  server:
    image: ghcr.io/emergent-company/emergent-server-go:${VERSION:-latest}

  kreuzberg:
    image: goldziher/kreuzberg:latest

  minio:
    image: minio/minio:latest

  tailscale:
    image: tailscale/tailscale:latest
```

### Phase 3: Installation Script

Script automatically downloads docker-compose.yml and runs:

```bash
docker compose pull  # Pulls all images
docker compose up -d # Starts services
```

No build step needed!

---

## Final Recommendation

**Build and publish**:

1. ✅ `emergent-server-go` (our code)
2. ✅ `emergent-postgres` (pgvector + our 26-line init script)

**Use upstream**: 3. ✅ `kreuzberg:latest` 4. ✅ `minio:latest` 5. ✅ `tailscale:latest`

This gives us the best balance of:

- Fast installation (\<5 minutes)
- Low maintenance (only 2 images to build)
- Great UX (auto-init database)
- Leverage upstream expertise
- Secure (official images for third-party services)
