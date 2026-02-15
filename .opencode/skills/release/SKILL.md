---
name: release
description: Create a versioned release — commit, tag, push, and deploy to servers. Use when the user wants to release, deploy, publish, or push a new version.
license: MIT
compatibility: opencode
metadata:
  author: emergent
  version: '1.0'
  trigger: release, deploy, push release, publish, bump version, tag and push
---

# Release Manager Skill

Create and deploy versioned releases of the Emergent platform. Handles version bumping, OpenAPI spec updates, git tagging, CI triggering, and server upgrades.

## Architecture Overview

All components share a **single version** derived from the git tag:

```
Git tag v0.X.Y
├── CLI binary v0.X.Y (built by CI)
├── Docker image emergent-server-with-cli:v0.X.Y (built by CI)
├── API /health returns "v0.X.Y" (ldflags at build time)
└── OpenAPI spec info.version = "0.X.Y" (annotation in main.go)
```

**CI Workflows triggered by `v*` tag push:**

1. `emergent-cli.yml` — Builds CLI binaries for 8 platforms, creates GitHub Release
2. `publish-minimal-images.yml` — Builds & pushes Docker image, uploads `images-ready.txt` sentinel to release

## Release Process

### Step 1: Determine the new version

Check the current version:

```bash
git tag --sort=-v:refname | head -3   # Latest tags
cat VERSION                            # VERSION file
```

Bump accordingly (patch for fixes, minor for features).

### Step 2: Stage and commit changes

**IMPORTANT:** Only stage files related to the changes being released. Do NOT include unrelated uncommitted work.

```bash
git add <changed-files>
git commit -m "fix/feat: description of changes"
```

### Step 3: Update version references

Three places need updating:

1. **VERSION file** (root):

   ```bash
   echo "0.X.Y" > VERSION
   ```

2. **OpenAPI spec annotation** in `apps/server-go/cmd/server/main.go`:

   ```go
   // @version 0.X.Y    ← line 4
   ```

3. **Regenerate Swagger docs** (if OpenAPI version changed):

   ```bash
   nx run server-go:swagger
   ```

   If swagger generation fails or is unavailable, skip it — the version annotation is what matters for the tag, and CI will regenerate docs during the Docker build.

4. **Commit the version bump:**
   ```bash
   git add VERSION apps/server-go/cmd/server/main.go apps/server-go/docs/swagger/
   git commit -m "chore: bump version to 0.X.Y"
   ```

### Step 4: Tag and push

```bash
git tag v0.X.Y
git push origin main --tags
```

This triggers CI which takes ~5-10 minutes to:

- Build CLI binaries and create GitHub Release
- Build and push Docker image to `ghcr.io/emergent-company/emergent-server-with-cli`
- Upload `images-ready.txt` sentinel when Docker image is ready

### Step 5: Deploy to servers (optional)

If the user wants to deploy to a specific server:

**Option A: SSH and run upgrade command** (if `emergent` CLI is in PATH on the server):

```bash
ssh root@<server> "emergent upgrade"
```

**Option B: SSH and pull directly** (if CLI is not installed on the host):

```bash
ssh root@<server> "cd ~/.emergent/docker && docker compose pull && docker compose up -d"
```

**Option C: Wait for user to upgrade manually**

> **NOTE:** The `emergent upgrade` command checks for the `images-ready.txt` sentinel in the GitHub Release before proceeding. If CI hasn't finished yet, the upgrade will fail. Either wait or use `--force` flag.

### Step 6: Verify deployment

```bash
ssh root@<server> "curl -s http://localhost:3002/health | jq '.version'"
# Should return "v0.X.Y"
```

## Known Servers

| Server       | SSH                     | Notes                                                                   |
| ------------ | ----------------------- | ----------------------------------------------------------------------- |
| mcj-emergent | `ssh root@mcj-emergent` | Standalone deployment, DB container: `emergent-db`, DB user: `emergent` |

## Quick Reference

| Item               | Location                                            |
| ------------------ | --------------------------------------------------- |
| VERSION file       | `/root/emergent/VERSION`                            |
| OpenAPI annotation | `apps/server-go/cmd/server/main.go` line 4          |
| Swagger docs       | `apps/server-go/docs/swagger/`                      |
| CI: CLI + Release  | `.github/workflows/emergent-cli.yml`                |
| CI: Docker image   | `.github/workflows/publish-minimal-images.yml`      |
| Dockerfile         | `deploy/minimal/Dockerfile.server-with-cli`         |
| Upgrade command    | `tools/emergent-cli/internal/cmd/upgrade.go`        |
| Versioning docs    | `apps/server-go/VERSIONING.md`                      |
| Docker registry    | `ghcr.io/emergent-company/emergent-server-with-cli` |

## Checklist

- [ ] Changes committed (only relevant files staged)
- [ ] VERSION file updated
- [ ] OpenAPI `@version` annotation updated in `main.go`
- [ ] Swagger docs regenerated (if possible)
- [ ] Version bump committed
- [ ] Tag created: `git tag v0.X.Y`
- [ ] Pushed: `git push origin main --tags`
- [ ] CI completed (check GitHub Actions)
- [ ] Server upgraded (if requested)
- [ ] Health check verified on target server
