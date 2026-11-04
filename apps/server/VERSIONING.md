# API Versioning Strategy

The Go server uses **unified versioning** aligned with CLI and Docker images.

## Canonical Version Source

The **root `VERSION` file is the single canonical source** of the release
version. Two checked-in references must match it exactly:

- `apps/server/cmd/server/main.go`'s `// @version` annotation
- every `MARKETING_VERSION` in `apps/ios/VoiceAgent.xcodeproj/project.pbxproj`
  (VoiceAgent app, Debug + Release configs)

`apps/server/docs/swagger/` is a third, derived carrier: its `info.version` is
regenerated from the `@version` annotation by `task server:swagger`, so it must
also match `VERSION`.

A release bumps all of them together — the `release` task in `Taskfile.yml`
does this atomically, and CI enforces it with a version-drift guard
(`scripts/preflight/version-drift.sh`, wired into `.github/workflows/ci.yml`)
that fails when any of them diverge from `VERSION`.

## Version Components

| Component         | Version Source                       | Dev Mode | Production |
| ----------------- | ------------------------------------ | -------- | ---------- |
| **CLI**           | GitHub Release tag                   | N/A      | v0.4.9     |
| **Docker Images** | Built from git tag                   | latest   | v0.4.9     |
| **API `/health`** | `internal/version.Version` (ldflags) | "dev"    | v0.4.9     |
| **OpenAPI Spec**  | Swagger annotation in `main.go`      | 0.4.9    | 0.4.9      |

## How It Works

### Development Mode (go run / Air)

```bash
# Running with go run or Air
cd apps/server
go run ./cmd/server
# or
air
```

**Behavior:**

- Runtime version: `"dev"` (no ldflags injection)
- OpenAPI spec version: Hardcoded in `cmd/server/main.go` annotations
- Use case: Local development, hot reload

**Endpoints:**

```bash
curl http://localhost:3002/health | jq '.version'
# → "dev"

curl http://localhost:3002/openapi.json | jq '.info.version'
# → "0.4.9"
```

### Production Build (server image)

`task build` produces the local binary: it does not run `swag init` and injects
no version ldflags, so that binary reports `dev`. Release images come from
`deploy/self-hosted/Dockerfile.server`, which is the file CI builds and
publishes:

```bash
docker build -f deploy/self-hosted/Dockerfile.server -t memory-server \
  --build-arg VERSION="$(git describe --tags --always)" \
  --build-arg GIT_COMMIT="$(git rev-parse --short HEAD)" \
  .
```

(`apps/server/Dockerfile` is the local-dev image — same swag step and Go build,
but not the release path, and its version defaults to `dev` unless you pass the
same build args.)

**Build process:**

1. Verifies the module files are tidy — see [Module files must be tidy](#module-files-must-be-tidy)
2. Generates Swagger spec from annotations
3. Builds binary with version injection:
   ```bash
   VERSION=$(git describe --tags --always)
   COMMIT=$(git rev-parse --short HEAD)
   go build -ldflags="-X github.com/emergent-company/emergent.memory/internal/version.Version=$VERSION \
                      -X github.com/emergent-company/emergent.memory/internal/version.GitCommit=$COMMIT"
   ```
4. Binary embeds git tag at compile time

**Resulting image:**

The release image expects Postgres: `entrypoint.sh` waits for it, applies the
migrations, then starts the server on port 3002.
`deploy/self-hosted/docker-compose.yml` supplies both (`POSTGRES_HOST: db`).
The version it reports is the `VERSION` build arg, so a tagged build returns
`"v0.4.9"` from `/health` and the release version from `/openapi.json`.

### Module files must be tidy

Both build stages run `go mod tidy -diff` and fail the build if that prints a
diff, instead of tidying the tree at build time (#2194). The image used to run a
bare `go mod tidy`, so a tag whose `go.mod`/`go.sum` were out of sync still
produced a published image — built from a dependency graph the tag never
contained.

What that means in practice:

- A tag needs a tidy module set. `ci` enforces it before merge
  (`scripts/preflight/go-mod-tidy.sh`), so a failure means the tree you are
  building bypassed that gate, or you edited `go.mod`/`go.sum` and did not commit
  the tidy result.
- Your `go mod tidy` may have run on a different Go minor than this image's
  `1.26`. Tidy output moves with the Go minor, so a mismatch can pass `ci` and
  fail here; use the version the image pins (the comment at that step in
  `deploy/self-hosted/Dockerfile.server` records the coupling).
- The check applies to trees cut after #2196. Tags cut before it keep the bare
  `go mod tidy` from their own Dockerfile, so rebuilding them still works, and
  the images published for them are unaffected. Building *this* Dockerfile
  against a pre-#2190 tree (incomplete `apps/server/go.sum`) fails with a
  `go.sum` addition diff — tidy that tree and cut a new tag rather than moving
  an existing one.
- If the failure prints no diff, tidy never got as far as comparing: the module
  graph or an import failed to load (network, module proxy, a missing `replace`
  target), or a toolchain download failed. Go's own error is printed above;
  re-running the build only helps the network case.

## Version Alignment

All components share the same semantic version:

```
Git tag v0.4.9
├─→ CLI binary v0.4.9 (built by CI)
├─→ Docker image emergent-server:v0.4.9 (built by CI)
├─→ API /health endpoint returns "v0.4.9" (ldflags at build time)
└─→ API /openapi.json info.version = "0.4.9" (hardcoded annotation)
```

## Updating Versions for Release

**Before tagging a new release (e.g., v0.5.0):**

### Step 1: Bump the canonical version (all references)

```bash
# 1. Root VERSION — canonical source
echo "0.5.0" > VERSION

# 2. OpenAPI annotation — must match VERSION exactly
# Edit cmd/server/main.go
# Change: // @version 0.4.9
# To:     // @version 0.5.0

# 3. iOS marketing version — must match VERSION exactly
# apps/ios/VoiceAgent.xcodeproj/project.pbxproj (app + broadcast extension)
# Change every: MARKETING_VERSION = 0.4.9;
# To:           MARKETING_VERSION = 0.5.0;
```

All references must be updated together. The version-drift guard fails CI if
any of them diverge (see "Canonical Version Source" above).

### Step 2: Regenerate Swagger Docs

```bash
task swagger   # from the product repo root
```

### Step 3: Commit Changes

```bash
git add apps/server/cmd/server/main.go apps/server/docs/swagger/
git commit -m "chore(api): Update OpenAPI spec version to 0.5.0"
```

### Step 4: Tag and Push

```bash
git tag v0.5.0
git push origin main --tags
```

### Step 5: CI Automation

GitHub Actions automatically:

- ✅ Builds CLI binaries with version `v0.5.0` (from git tag)
- ✅ Builds Docker images tagged `v0.5.0` (from git tag)
- ✅ Creates GitHub release with all artifacts
- ✅ Publishes to package registries

## Verifying Version Alignment

```bash
# Check all versions match
echo "Git Tag: $(git describe --tags --always)"
echo "CLI Release: $(gh release view v0.5.0 --json tagName -q .tagName)"
echo "API Health: $(curl -s http://localhost:3002/health | jq -r '.version')"
echo "OpenAPI Spec: $(curl -s http://localhost:3002/openapi.json | jq -r '.info.version')"

# Expected output:
# Git Tag: v0.5.0
# CLI Release: v0.5.0
# API Health: v0.5.0
# OpenAPI Spec: 0.5.0
```

## Important Notes

### Development vs Production

**Development (dev):**

- Hot reload enabled via Air
- Version shows "dev" (expected behavior)
- Fast iteration, no build step
- OpenAPI spec manually updated

**Production (production builds):**

- Version injected from git tag via ldflags
- Built by `deploy/self-hosted/Dockerfile.server`, not `task build`
- Optimized binary with `-ldflags="-s -w"`
- Version alignment automatic

### Manual Steps Required

**Only the OpenAPI spec version requires manual update before releases.**

All other versions (CLI, Docker, API runtime) are automatically derived from the git tag.

### Why Two Version Sources?

**Q: Why not generate OpenAPI version dynamically?**

**A:** Swagger spec is generated at **build time**, not runtime:

1. `swag init` reads Go annotations and generates static `swagger.json`
2. The generated file is committed to git
3. The file is served as-is by the API

To inject runtime version, we would need to:

- Parse and modify JSON on every request (slow)
- Or embed a template and render at startup (complex)

**Current approach is simpler:**

- Update `@version` annotation before release
- Build process generates static spec
- No runtime overhead

## FAQs

**Q: Why does `/health` show "dev" in development?**

A: The `go run` and `air` commands don't use ldflags. This is expected. Production builds inject the version via ldflags.

**Q: How do I test with a real version locally?**

A: Inject the version yourself — `task build` does not set it:

```bash
cd apps/server
VERSION=$(git describe --tags --always)
go build -ldflags="-X github.com/emergent-company/emergent.memory/internal/version.Version=$VERSION" \
  -o dist/server ./cmd/server
./dist/server
```

**Q: Can I automate the OpenAPI version update?**

A: Yes, but it requires parsing/modifying Go source:

```bash
# Example (would need testing):
sed -i 's/@version [0-9.]\\+/@version 0.5.0/' apps/server/cmd/server/main.go
```

However, manual update ensures you review the change.
