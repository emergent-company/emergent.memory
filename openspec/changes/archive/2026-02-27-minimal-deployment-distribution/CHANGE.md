# Minimal Deployment Distribution System

**Status**: Draft  
**Priority**: High  
**Type**: Feature - Infrastructure  
**Scope**: Distribution, Deployment, Developer Experience

## Problem Statement

We need a production-ready distribution model for the Emergent minimal standalone deployment that allows users to install via a simple copy-paste command, similar to successful OSS projects like Supabase, PostHog, and Langfuse.

### Current State

- ✅ Docker Compose configuration exists (`deploy/minimal/docker-compose.yml`)
- ✅ Multi-stage Dockerfiles for Go backend, NestJS server, Admin UI
- ✅ GitHub Actions CI builds binaries but doesn't push images
- ❌ No pre-built Docker images published
- ❌ No one-line installation script
- ❌ Users must clone repo and build locally
- ❌ No versioning or update mechanism

### Desired State

Users can install Emergent minimal deployment with a single command:

```bash
curl -fsSL https://install.emergent.ai/minimal | bash
```

This script should:

1. Check prerequisites (Docker, Docker Compose)
2. Download docker-compose.yml and templates
3. Generate secrets automatically
4. Prompt for required configuration (Tailscale key, Google API key)
5. Pull pre-built images from registry
6. Start services
7. Display access information

## Proposed Solution

### Architecture Overview

```
┌─────────────────────────────────────────────────────────────┐
│                    Distribution Flow                         │
└─────────────────────────────────────────────────────────────┘

1. User runs install command
   ↓
2. Install script downloads from GitHub/CDN
   ↓
3. Script validates prerequisites
   ↓
4. Script prompts for configuration
   ↓
5. Script generates .env with secrets
   ↓
6. Docker Compose pulls pre-built images
   ↓
7. Services start with health checks
   ↓
8. Success message with access info
```

### Component 1: Docker Image Registry

**Choice**: GitHub Container Registry (ghcr.io)

**Rationale**:

- ✅ Free for public images
- ✅ Integrated with GitHub Actions
- ✅ No additional account needed
- ✅ Good performance
- ✅ Multi-architecture support

**Alternative Considered**: Docker Hub

- ❌ Rate limiting issues
- ❌ Separate account management
- ✅ More discoverable

**Images to Publish**:

```
ghcr.io/emergent-company/emergent-server-go:latest
ghcr.io/emergent-company/emergent-server-go:v1.2.3
ghcr.io/emergent-company/emergent-postgres:latest (with pgvector)
```

**Third-party images** (reference directly):

- `goldziher/kreuzberg:latest`
- `minio/minio:latest`
- `tailscale/tailscale:latest`

### Component 2: Installation Script

**File**: `scripts/install-minimal.sh`

**Script Structure** (~300 lines):

```bash
#!/usr/bin/env bash
set -euo pipefail

# 1. Banner and version info
# 2. Prerequisites check (docker, docker-compose)
# 3. Interactive prompts with validation
# 4. Secret generation
# 5. File download (docker-compose.yml, .env.example)
# 6. Docker Compose pull and up
# 7. Health checks with retries
# 8. Success message with credentials
```

**Key Features**:

- Non-interactive mode: `bash install-minimal.sh --non-interactive`
- Dry-run mode: `bash install-minimal.sh --dry-run`
- Custom install directory: `bash install-minimal.sh --dir=/opt/emergent`
- Update existing installation: `bash install-minimal.sh --upgrade`

**Security Considerations**:

- Script hosted on GitHub (versioned, auditable)
- HTTPS-only downloads
- Checksum validation for downloaded files
- Option to review before execution

### Component 3: GitHub Actions CI/CD

**Workflow**: `.github/workflows/publish-images.yml`

**Triggers**:

- On git tag push: `v*` (e.g., `v1.0.0`)
- Manual workflow dispatch
- Weekly schedule (rebuild latest)

**Build Matrix**:

```yaml
strategy:
  matrix:
    platform:
      - linux/amd64
      - linux/arm64
```

**Tagging Strategy**:

- `latest` - Most recent stable release
- `v1.2.3` - Specific version tag
- `v1.2` - Minor version alias
- `v1` - Major version alias
- `edge` - Latest commit on master (optional)

**Build Steps**:

1. Checkout code with full git history
2. Extract version from git tag
3. Set up Docker Buildx (multi-arch)
4. Login to GitHub Container Registry
5. Build and push server-go image
6. Build and push postgres image
7. Update image manifest for multi-arch

### Component 4: Update Mechanism

**File**: `scripts/update-minimal.sh`

Users can update with:

```bash
curl -fsSL https://install.emergent.ai/update | bash
```

**Update Process**:

1. Detect current version (from .env or docker-compose.yml)
2. Fetch latest version from GitHub API
3. Show changelog between versions
4. Prompt for confirmation
5. Backup current .env
6. Pull new images
7. Run database migrations if needed
8. Restart services with health checks
9. Rollback on failure

### Component 5: Documentation

**New Files**:

- `deploy/minimal/README.md` (✅ already created)
- `deploy/minimal/INSTALL.md` - Installation guide
- `deploy/minimal/UPGRADE.md` - Upgrade guide
- `deploy/minimal/TROUBLESHOOTING.md` - Common issues
- Website: `https://docs.emergent.ai/deployment/minimal`

## Implementation Plan

### Phase 1: Image Registry Setup (Week 1)

**Tasks**:

1. Create GitHub Actions workflow for image publishing
2. Configure GHCR authentication
3. Update Dockerfiles for multi-arch builds
4. Test image build and push locally
5. Publish initial images (v0.1.0)

**Files Created/Modified**:

- `.github/workflows/publish-images.yml` (new, ~150 lines)
- `apps/server-go/Dockerfile` (update for multi-arch)
- `docker/Dockerfile.postgres` (update for multi-arch)

**Success Criteria**:

- Images successfully pushed to ghcr.io
- Images work on both amd64 and arm64
- Image size optimized (\u003c25MB for Go server)

### Phase 2: Installation Script (Week 1-2)

**Tasks**:

1. Write installation script with interactive prompts
2. Add prerequisite checks
3. Implement secret generation
4. Add health check loops
5. Test on fresh Ubuntu/Debian/CentOS systems
6. Add non-interactive mode for automation

**Files Created/Modified**:

- `scripts/install-minimal.sh` (new, ~300 lines)
- `scripts/lib/colors.sh` (new, shared utilities)
- `scripts/lib/validators.sh` (new, input validation)

**Success Criteria**:

- Script works on Ubuntu 20.04+, Debian 11+, CentOS 8+
- Handles errors gracefully
- Provides clear feedback at each step
- Generates secure random secrets

### Phase 3: Docker Compose Updates (Week 2)

**Tasks**:

1. Update docker-compose.yml to use pre-built images
2. Add image version constraints
3. Configure image pull policy
4. Test with published images

**Files Modified**:

- `deploy/minimal/docker-compose.yml` (switch from build to image)

**Changes**:

```yaml
# Before (build locally)
services:
  server:
    build:
      context: ../..
      dockerfile: apps/server-go/Dockerfile

# After (use pre-built)
services:
  server:
    image: ghcr.io/emergent-company/emergent-server-go:${VERSION:-latest}
    pull_policy: always
```

### Phase 4: Update Script (Week 2)

**Tasks**:

1. Write update detection logic
2. Implement changelog fetching
3. Add backup/restore functionality
4. Test upgrade path from v0.1.0 to v0.2.0

**Files Created**:

- `scripts/update-minimal.sh` (new, ~200 lines)

### Phase 5: Testing \u0026 Documentation (Week 3)

**Tasks**:

1. Write integration tests for install script
2. Test on cloud providers (AWS, GCP, DigitalOcean)
3. Create video walkthrough
4. Update documentation website
5. Create troubleshooting guide

**Files Created**:

- `deploy/minimal/tests/test-install.sh` (integration tests)
- `deploy/minimal/INSTALL.md`
- `deploy/minimal/UPGRADE.md`
- `deploy/minimal/TROUBLESHOOTING.md`

## Technical Decisions

### Decision 1: Pre-built Images vs Build-on-Install

**Chosen**: Pre-built images from GHCR

**Rationale**:

- ✅ **Fast installation** - No compilation time (Go build takes ~2 minutes)
- ✅ **Reproducible** - Same image for all users
- ✅ **Tested** - Images built and tested in CI
- ✅ **Smaller download** - Multi-stage build removes build tools
- ✅ **Better UX** - User doesn't need Go toolchain

**Trade-off**:

- ❌ Users can't customize build
- ❌ Must trust our build process
- Mitigation: Provide Dockerfiles for those who want to build

### Decision 2: GHCR vs Docker Hub

**Chosen**: GitHub Container Registry (ghcr.io)

**Rationale**:

- ✅ No rate limiting for public images
- ✅ Integrated with GitHub Actions (no separate credentials)
- ✅ Same authentication as code repo
- ✅ Free for OSS projects
- ✅ Good multi-arch support

**Trade-off**:

- ❌ Less discoverable than Docker Hub
- Mitigation: Also publish to Docker Hub as mirror (optional)

### Decision 3: Install Script Distribution

**Chosen**: GitHub-hosted script via curl

**Rationale**:

- ✅ Industry standard (Docker, K8s, Homebrew all use this)
- ✅ Versioned (can pin to specific commit/tag)
- ✅ Auditable (users can review before running)
- ✅ CDN-backed (GitHub has global CDN)

**Security Measures**:

1. HTTPS-only downloads
2. Checksum validation
3. Script review recommendation
4. No automatic sudo escalation

**Alternative**: Package managers (apt, yum, brew)

- Requires packaging for each platform
- More overhead to maintain
- Better security/trust model
- Consider for Phase 2

### Decision 4: Multi-Architecture Support

**Chosen**: Support amd64 and arm64

**Rationale**:

- ✅ Covers 99% of deployments
- ✅ Raspberry Pi support (arm64)
- ✅ Apple Silicon Macs (arm64)
- ✅ Cloud ARM instances (cheaper)

**Build Approach**:

- Use Docker Buildx with QEMU
- Test both architectures in CI
- Publish multi-arch manifest

## File Structure

```
emergent/
├── .github/
│   └── workflows/
│       └── publish-images.yml           # NEW - Image publishing CI
├── deploy/
│   └── minimal/
│       ├── docker-compose.yml           # MODIFIED - Use pre-built images
│       ├── .env.example                 # EXISTS
│       ├── README.md                    # EXISTS
│       ├── INSTALL.md                   # NEW - Installation guide
│       ├── UPGRADE.md                   # NEW - Upgrade guide
│       ├── TROUBLESHOOTING.md           # NEW - Common issues
│       └── tests/
│           └── test-install.sh          # NEW - Integration tests
├── scripts/
│   ├── install-minimal.sh               # NEW - Installation script
│   ├── update-minimal.sh                # NEW - Update script
│   └── lib/
│       ├── colors.sh                    # NEW - Shared utilities
│       └── validators.sh                # NEW - Input validation
└── apps/
    ├── server-go/
    │   └── Dockerfile                   # MODIFIED - Multi-arch support
    └── docker/
        └── Dockerfile.postgres          # MODIFIED - Multi-arch support
```

## Environment Variables

**Required for CI** (GitHub Secrets):

```
GITHUB_TOKEN - Automatically provided by GitHub Actions
```

**Required for Installation** (user-provided):

```
TS_AUTHKEY - Tailscale authentication key
GOOGLE_API_KEY - Google Cloud API key
```

**Auto-generated** (by install script):

```
POSTGRES_PASSWORD - Database password
MINIO_ROOT_PASSWORD - MinIO admin password
STANDALONE_API_KEY - MCP authentication key
```

## Success Metrics

**Installation Experience**:

- ⏱️ Install time: \u003c5 minutes (vs current ~30 minutes)
- 📦 Download size: \u003c100MB total
- 🎯 Success rate: \u003e95% on supported platforms
- 📚 Documentation clarity: \u003c10 support questions per month

**Image Quality**:

- 📦 Server image size: \u003c25MB (Go distroless)
- 📦 Postgres image size: \u003c300MB
- 🏗️ Build time: \u003c5 minutes in CI
- 🔒 Security scan: No critical vulnerabilities

**Distribution**:

- 🚀 Image pull time: \u003c2 minutes on average connection
- 🌍 Multi-arch support: Works on amd64 and arm64
- 🔄 Update time: \u003c3 minutes

## Testing Plan

### Unit Tests

- Prerequisite detection functions
- Secret generation functions
- Version comparison logic

### Integration Tests

```bash
# Test fresh install on Ubuntu
./deploy/minimal/tests/test-install.sh --platform ubuntu:22.04

# Test fresh install on Debian
./deploy/minimal/tests/test-install.sh --platform debian:12

# Test upgrade path
./deploy/minimal/tests/test-install.sh --test-upgrade

# Test non-interactive mode
./deploy/minimal/tests/test-install.sh --non-interactive
```

### Manual Tests

- [ ] Install on AWS EC2 (Ubuntu 22.04, t3.medium)
- [ ] Install on GCP Compute (Debian 12, e2-medium)
- [ ] Install on DigitalOcean Droplet (Ubuntu 22.04, 2GB)
- [ ] Install on Raspberry Pi 4 (Ubuntu Server 22.04, arm64)
- [ ] Install on Apple Silicon Mac (Docker Desktop, arm64)
- [ ] Upgrade from v0.1.0 to v0.2.0
- [ ] Rollback after failed upgrade

## Rollout Plan

### Phase 1: Internal Testing (Week 1)

- Build and publish v0.1.0-alpha images
- Test install script internally
- Fix critical issues

### Phase 2: Beta Release (Week 2)

- Publish v0.1.0-beta
- Invite 5-10 beta testers
- Gather feedback
- Fix reported issues

### Phase 3: Public Release (Week 3)

- Publish v0.1.0 stable
- Announce on website, blog, social media
- Monitor installations
- Provide support

### Phase 4: Iteration (Ongoing)

- Weekly image rebuilds for security patches
- Monthly feature releases
- Quarterly major version bumps

## Risks \u0026 Mitigation

| Risk                                      | Impact | Probability | Mitigation                                               |
| ----------------------------------------- | ------ | ----------- | -------------------------------------------------------- |
| Image registry rate limiting              | High   | Low         | Use GHCR (no limits), mirror to Docker Hub               |
| Installation script breaks on new OS      | Medium | Medium      | CI tests on multiple platforms, version pinning          |
| Security vulnerability in base image      | High   | Medium      | Weekly rebuilds, automated scanning, security advisories |
| Breaking changes in docker-compose format | Medium | Low         | Pin docker-compose version in prerequisites              |
| User runs script with sudo unnecessarily  | Low    | High        | Explicit warning in script, documentation                |
| Tailscale auth key expires                | High   | Medium      | Clear error message, documentation on rotation           |

## Alternative Approaches Considered

### 1. Kubernetes Helm Chart

**Pros**: Better for production clusters, declarative
**Cons**: Overkill for minimal deployment, steep learning curve
**Decision**: Consider for "full" deployment, not minimal

### 2. Single Binary Distribution

**Pros**: Simplest for users, no Docker needed
**Cons**: Can't include PostgreSQL, Kreuzberg, MinIO easily
**Decision**: Not feasible for our multi-service stack

### 3. Package Managers (apt, yum, brew)

**Pros**: Better trust model, automatic updates
**Cons**: Complex to maintain, multiple platforms
**Decision**: Consider for Phase 2 after validating demand

### 4. Vagrant Box

**Pros**: Complete VM image, very reproducible
**Cons**: Large download (GB), slower, requires VirtualBox
**Decision**: Not suitable for modern cloud deployments

## Open Questions

1. **Versioning**: Should we support multiple major versions concurrently?
   - Proposal: Support N and N-1 major versions
2. **Telemetry**: Should install script send anonymous usage stats?
   - Proposal: Opt-in telemetry with clear privacy policy
3. **Branding**: Should images include branding/labels?
   - Proposal: Add OCI labels with metadata
4. **Mirror**: Should we mirror to Docker Hub for discoverability?

   - Proposal: Yes, publish to both registries

5. **Database migrations**: How to handle schema upgrades?
   - Proposal: Auto-migrate on container start (Goose)

## References

**Similar Projects**:

- Supabase: https://supabase.com/docs/guides/self-hosting
- PostHog: https://posthog.com/docs/self-host
- Langfuse: https://langfuse.com/docs/deployment/self-host
- Plausible: https://plausible.io/docs/self-hosting
- Ghost: https://ghost.org/docs/install/docker/

**GitHub Actions**:

- Docker Buildx: https://github.com/docker/build-push-action
- GHCR: https://docs.github.com/en/packages/working-with-a-github-packages-registry/working-with-the-container-registry

**Docker Best Practices**:

- Multi-stage builds: https://docs.docker.com/build/building/multi-stage/
- Multi-arch: https://docs.docker.com/build/building/multi-platform/

## Appendix A: Install Script Example

See `scripts/install-minimal.sh` (to be created in implementation phase)

Key sections:

1. Banner and prerequisites
2. Interactive prompts
3. Secret generation
4. File setup
5. Docker operations
6. Health checks
7. Success message

## Appendix B: GitHub Actions Workflow Example

See `.github/workflows/publish-images.yml` (to be created in implementation phase)

Key jobs:

1. Build server-go image
2. Build postgres image
3. Multi-arch manifest
4. Security scanning
5. Tag management
