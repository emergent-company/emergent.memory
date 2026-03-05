---
name: release
description: Cut a new versioned release — bump VERSION, update swagger annotation, commit, tag (v* + SDK tag), and push to trigger CI. Use when the user says "release", "cut a release", "bump version", or "tag a release".
metadata:
  author: emergent
  version: "1.0"
---

Cut a new release: bump version files, commit, tag, and push to trigger CI (CLI binaries + Docker images).

**Input**: Optionally accepts a version string (e.g. `/release 0.28.0`). If omitted, ask the user.

---

## Steps

### 1. Determine the new version

Read the current version:
```bash
cat VERSION
```

If the user provided a version as an argument, use it. Otherwise, use **AskUserQuestion** to ask:
> "Current version is X.Y.Z. What should the new version be? (e.g. patch → X.Y.Z+1, minor → X.Y+1.0, major → X+1.0.0)"

Validate the input is a bare semver (no leading `v`): must match `^\d+\.\d+\.\d+$`.
Reject if new version is not strictly greater than current.

### 2. Confirm before doing anything destructive

Show a summary and ask the user to confirm:

```
About to release:
  Current: <current>
  New:     <new>

Steps:
  1. Update VERSION → <new>
  2. Update apps/server-go/cmd/server/main.go @version → <new>
  3. git commit -m "chore: bump version to <new>"
  4. git tag v<new>
  5. git tag apps/server-go/pkg/sdk/v<new>
  6. git push origin main
  7. git push origin v<new> apps/server-go/pkg/sdk/v<new>

CI will then build CLI binaries (all platforms) and Docker images.
Proceed?
```

Use **AskUserQuestion** with Yes / No options. Stop if the user says No.

### 3. Run the release script

If `scripts/release.sh` exists, invoke it:
```bash
bash scripts/release.sh <new-version>
```

Otherwise execute steps manually:

```bash
# Step 1: Version files
CURRENT=$(cat VERSION)
NEW=<new-version>
echo "$NEW" > VERSION
sed -i "s|// @version $CURRENT|// @version $NEW|" apps/server-go/cmd/server/main.go

# Step 2: Commit
git add VERSION apps/server-go/cmd/server/main.go
git commit -m "chore: bump version to $NEW"

# Step 3: Tags
git tag "v$NEW"
git tag "apps/server-go/pkg/sdk/v$NEW"

# Step 4: Push
git push origin main
git push origin "v$NEW" "apps/server-go/pkg/sdk/v$NEW"
```

### 4. Report outcome

On success, output:

```
Released v<new>

Tags pushed:
  v<new>                          → triggers emergent-cli.yml (CLI binaries + GitHub Release)
  apps/server-go/pkg/sdk/v<new>  → Go module proxy

CI will build:
  - Cross-platform CLI binaries (linux, darwin, windows, freebsd × amd64/arm64)
  - Docker images (ghcr.io/emergent-company/emergent-server-with-cli:<new>)
  - Docker images (ghcr.io/emergent-company/emergent-cli:<new>)

Monitor at: https://github.com/emergent-company/emergent/actions
```

---

## Guardrails

- **Never skip the confirmation step** — pushing tags is irreversible
- **Never force-push or delete tags** without explicit user instruction
- The working tree must be clean (`git status`) before bumping; warn the user and stop if it is not
- Only bump from `main` branch; warn and stop if on any other branch
- The new version must be strictly greater than the current one (semver ordering)
- If `apps/server-go/cmd/server/main.go` doesn't contain `// @version <current>`, warn and stop — do not corrupt the file
