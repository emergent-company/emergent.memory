# Distribution Model Recommendation

## TL;DR - One-Line Install

**Recommended approach**: Pre-built Docker images + installation script

```bash
curl -fsSL https://install.emergent.ai/minimal | bash
```

## Why This Approach?

### ✅ What We Chose: Pre-built Images from GitHub Container Registry

**User Experience**:

- Install in \u003c5 minutes (vs ~30 minutes building locally)
- No Go compiler needed
- Same tested image for everyone
- Works offline after initial pull

**Distribution**:

```
ghcr.io/emergent-company/emergent-server-go:latest    (~18MB)
ghcr.io/emergent-company/emergent-postgres:latest     (~300MB)
```

Plus third-party images:

- `goldziher/kreuzberg:latest`
- `minio/minio:latest`
- `tailscale/tailscale:latest`

**Total download**: ~100MB compressed

### Installation Flow

```
1. User runs: curl -fsSL https://install.emergent.ai/minimal | bash
                    ↓
2. Script checks: Docker, Docker Compose installed?
                    ↓
3. Script prompts:
   - Tailscale auth key
   - Google API key
   - Custom hostname (optional)
                    ↓
4. Script generates:
   - POSTGRES_PASSWORD (random)
   - MINIO_ROOT_PASSWORD (random)
   - STANDALONE_API_KEY (random)
                    ↓
5. Docker Compose:
   - Pulls pre-built images
   - Starts services
   - Runs health checks
                    ↓
6. Success! Display:
   - Tailscale hostname: http://emergent:3002
   - API key for MCP
   - MinIO console: http://localhost:9001
```

## Implementation Phases

### Phase 1: Build \u0026 Publish Images (Week 1)

**Create**: `.github/workflows/publish-images.yml`

- Triggered on git tag: `v1.0.0`
- Builds multi-arch images (amd64, arm64)
- Pushes to ghcr.io
- Takes ~5 minutes

**Update**: `deploy/minimal/docker-compose.yml`

```yaml
# Change from:
server:
  build: ../../apps/server-go/Dockerfile

# To:
server:
  image: ghcr.io/emergent-company/emergent-server-go:${VERSION:-latest}
```

### Phase 2: Installation Script (Week 1-2)

**Create**: `scripts/install-minimal.sh` (~300 lines)

Features:

- ✅ Interactive prompts with validation
- ✅ Auto-generate secrets
- ✅ Health checks with retries
- ✅ Non-interactive mode: `--non-interactive`
- ✅ Dry-run: `--dry-run`
- ✅ Custom directory: `--dir=/opt/emergent`

### Phase 3: Update Mechanism (Week 2)

**Create**: `scripts/update-minimal.sh`

```bash
curl -fsSL https://install.emergent.ai/update | bash
```

Features:

- Detect current version
- Show changelog
- Backup .env before upgrade
- Auto-migrate database
- Rollback on failure

## Comparison with Alternatives

| Approach                | Install Time | Download Size | Pros                       | Cons                    |
| ----------------------- | ------------ | ------------- | -------------------------- | ----------------------- |
| **Pre-built images** ✅ | 5 min        | 100MB         | Fast, tested, reproducible | Must trust build        |
| Build from source       | 30 min       | 50MB          | Customizable, auditable    | Slow, needs Go compiler |
| Single binary           | 2 min        | 50MB          | Simplest                   | Can't include DB/MinIO  |
| Package managers        | 1 min        | 100MB         | Best trust model           | Complex maintenance     |
| Vagrant box             | 10 min       | 2GB           | Complete VM                | Huge, slow, outdated    |

## Image Registry Comparison

| Registry    | Rate Limit   | Cost      | Multi-arch | Integration    |
| ----------- | ------------ | --------- | ---------- | -------------- |
| **GHCR** ✅ | None         | Free      | ✅         | GitHub Actions |
| Docker Hub  | 100 pulls/6h | Free tier | ✅         | Separate auth  |
| Quay.io     | None         | Free      | ✅         | Separate auth  |

**Chosen**: GitHub Container Registry (ghcr.io)

- No rate limits
- Integrated with our GitHub repo
- Free for OSS projects

## Success Metrics

**Installation**:

- ⏱️ Time: \u003c5 minutes (vs 30 min currently)
- 📦 Download: \u003c100MB
- 🎯 Success rate: \u003e95% on Ubuntu/Debian/CentOS

**Images**:

- 📦 Server size: \u003c25MB (Go distroless)
- 🏗️ Build time: \u003c5 minutes in CI
- 🔒 Security: Weekly rebuilds + scanning

## Security Considerations

**Install Script**:

- HTTPS-only downloads
- Hosted on GitHub (auditable)
- Recommend review before running
- No automatic sudo escalation

**Docker Images**:

- Built from scratch in CI (reproducible)
- Minimal distroless base (tiny attack surface)
- Weekly security patches
- Automated vulnerability scanning

**Secret Management**:

- Auto-generated random secrets (32 bytes)
- Never logged or transmitted
- Stored in .env (gitignored)

## Files Created

```
.github/workflows/
└── publish-images.yml          # NEW - CI/CD for images

deploy/minimal/
├── docker-compose.yml          # MODIFIED - Use pre-built images
├── INSTALL.md                  # NEW - Installation guide
├── UPGRADE.md                  # NEW - Upgrade guide
└── TROUBLESHOOTING.md          # NEW - Common issues

scripts/
├── install-minimal.sh          # NEW - Installation script (~300 LOC)
├── update-minimal.sh           # NEW - Update script (~200 LOC)
└── lib/
    ├── colors.sh               # NEW - UI utilities
    └── validators.sh           # NEW - Input validation
```

## Next Steps

1. ✅ **This document** - Recommendation complete
2. ⏭️ **Create OpenSpec proposal** - For review and approval
3. ⏭️ **Phase 1**: Build GitHub Actions workflow
4. ⏭️ **Phase 2**: Write installation script
5. ⏭️ **Phase 3**: Test on cloud providers
6. ⏭️ **Phase 4**: Documentation and launch

## Example Commands

**Install**:

```bash
curl -fsSL https://install.emergent.ai/minimal | bash
```

**Install (non-interactive)**:

```bash
curl -fsSL https://install.emergent.ai/minimal | bash -s -- \
  --non-interactive \
  --ts-authkey="tskey-auth-xxx" \
  --google-api-key="AIzaSyxxx"
```

**Update**:

```bash
cd ~/emergent-minimal
./update.sh
```

**Uninstall**:

```bash
cd ~/emergent-minimal
docker compose down -v
rm -rf ~/emergent-minimal
```

## References

- **Supabase**: https://github.com/supabase/supabase/blob/master/docker/docker-compose.yml
- **PostHog**: https://posthog.com/docs/self-host/deploy/docker
- **Langfuse**: https://langfuse.com/docs/deployment/self-host
- **GHCR Docs**: https://docs.github.com/en/packages/working-with-a-github-packages-registry/working-with-the-container-registry
