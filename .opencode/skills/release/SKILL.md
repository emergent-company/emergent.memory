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

Create and deploy versioned releases of the Memory platform. Handles version bumping, OpenAPI spec updates, git tagging, CI triggering, and server upgrades.

## Architecture Overview

All components share a **single version** derived from the git tag:

```
Git tag v0.X.Y
├── CLI binary v0.X.Y (built by CI)
├── Docker image memory-server:v0.X.Y (built by CI)
├── API /health returns "v0.X.Y" (ldflags at build time)
└── OpenAPI spec info.version = "0.X.Y" (annotation in main.go)
```

**CI Workflows triggered by `v*` tag push:**

1. `cli.yml` — Builds CLI binaries for 8 platforms, creates GitHub Release
2. `publish-self-hosted-images.yml` — Builds & pushes Docker image, uploads `images-ready.txt` sentinel to release

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

2. **OpenAPI spec annotation** in `apps/server/cmd/server/main.go`:

   ```go
   // @version 0.X.Y    ← line 4
   ```

3. **Regenerate Swagger docs** (if OpenAPI version changed):

   ```bash
   nx run server:swagger
   ```

   If swagger generation fails or is unavailable, skip it — the version annotation is what matters for the tag, and CI will regenerate docs during the Docker build.

4. **Commit the version bump:**
   ```bash
   git add VERSION apps/server/cmd/server/main.go apps/server/docs/swagger/
   git commit -m "chore: bump version to 0.X.Y"
   ```

### Step 3.5: Pre-tag verification

Before tagging, verify the build is clean:

```bash
go build ./...                        # must succeed — fix any compile errors first
git status --short | grep "^?"        # check for untracked files that should have been staged
```

If `go build ./...` fails, do not tag. Fix the errors and commit first.
If there are untracked files related to the release (e.g. generated docs), add and commit them now.

### Step 4: Tag and push

```bash
git tag v0.X.Y
git push origin main --tags
```

This triggers CI which takes ~5-10 minutes to:

- Build CLI binaries and **create the GitHub Release** (via `cli.yml` — the Release appears in GitHub only after CI completes, not immediately after `git tag`)
- Build and push Docker image to `ghcr.io/emergent-company/memory-server`
- Upload `images-ready.txt` sentinel when Docker image is ready

> **NOTE:** `git tag` + `git push --tags` does NOT immediately create a GitHub Release. The Release is created by the `cli.yml` CI workflow. Monitor progress with: `gh run watch`

### Step 5: Ask user to trigger prod deployment

**NEVER deploy to prod manually (no binary copy, no SSH docker commands).**

After pushing the tag, always stop and tell the user:

> "Tag `v0.X.Y` pushed. CI is building the image. Please trigger the prod deployment via GitHub Actions when the image is ready."

Wait for the user to confirm deployment is done before verifying.

### Step 6: Verify deployment (after user confirms)

```bash
curl -s https://memory.emergent-company.ai/api/health | jq '.version'
# Should return "v0.X.Y"
```

## Quick Reference

| Item               | Location                                            |
| ------------------ | --------------------------------------------------- |
| VERSION file       | `/root/emergent.memory/VERSION`                     |
| OpenAPI annotation | `apps/server/cmd/server/main.go` line 4          |
| Swagger docs       | `apps/server/docs/swagger/`                      |
| CI: CLI + Release  | `.github/workflows/cli.yml`                |
| CI: Docker image   | `.github/workflows/publish-self-hosted-images.yml`      |
| Dockerfile         | `deploy/self-hosted/Dockerfile.server`                  |
| Upgrade command    | `tools/cli/internal/cmd/upgrade.go`        |
| Versioning docs    | `apps/server/VERSIONING.md`                      |
| Docker registry    | `ghcr.io/emergent-company/memory-server`            |

## Checklist

- [ ] Changes committed (only relevant files staged)
- [ ] VERSION file updated
- [ ] OpenAPI `@version` annotation updated in `main.go`
- [ ] Swagger docs regenerated (if possible)
- [ ] Version bump committed
- [ ] `go build ./...` passes (no compile errors)
- [ ] No untracked release-related files (`git status --short | grep "^?"`)
- [ ] Tag created: `git tag v0.X.Y`
- [ ] Pushed: `git push origin main --tags`
- [ ] **User asked to trigger prod deployment via GitHub Actions** (never deploy manually)
- [ ] CI completed — GitHub Release created by CI (`gh run watch`)
- [ ] Health check verified after user confirms deployment done
