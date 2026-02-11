# API Versioning Strategy

The Go server uses **unified versioning** aligned with CLI and Docker images.

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
cd apps/server-go
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
curl http://localhost:5300/health | jq '.version'
# → "dev"

curl http://localhost:5300/openapi.json | jq '.info.version'
# → "0.4.9"
```

### Production Build (nx run server-go:build)

```bash
nx run server-go:build
```

**Build process:**

1. Generates Swagger spec from annotations
2. Builds binary with version injection:
   ```bash
   VERSION=$(git describe --tags --always)
   COMMIT=$(git rev-parse --short HEAD)
   go build -ldflags="-X github.com/emergent/emergent-core/internal/version.Version=$VERSION \
                      -X github.com/emergent/emergent-core/internal/version.GitCommit=$COMMIT"
   ```
3. Binary embeds git tag at compile time

**Resulting binary:**

```bash
./dist/server &
curl http://localhost:5300/health | jq '.version'
# → "v0.4.9"

curl http://localhost:5300/openapi.json | jq '.info.version'
# → "0.4.9"
```

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

### Step 1: Update OpenAPI Spec Version

```bash
# Edit cmd/server/main.go
# Change: // @version 0.4.9
# To:     // @version 0.5.0
```

### Step 2: Regenerate Swagger Docs

```bash
cd apps/server-go
nx run server-go:swagger
```

### Step 3: Commit Changes

```bash
git add apps/server-go/cmd/server/main.go apps/server-go/docs/swagger/
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
echo "API Health: $(curl -s http://localhost:5300/health | jq -r '.version')"
echo "OpenAPI Spec: $(curl -s http://localhost:5300/openapi.json | jq -r '.info.version')"

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
- Requires full build: `nx run server-go:build`
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

A: Build and run the binary:

```bash
nx run server-go:build
./apps/server-go/dist/server
```

**Q: Can I automate the OpenAPI version update?**

A: Yes, but it requires parsing/modifying Go source:

```bash
# Example (would need testing):
sed -i 's/@version [0-9.]\\+/@version 0.5.0/' apps/server-go/cmd/server/main.go
```

However, manual update ensures you review the change.
