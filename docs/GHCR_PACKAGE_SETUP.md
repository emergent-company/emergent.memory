# Making GHCR Packages Public

## Problem

Docker images published to `ghcr.io` default to **Private** visibility. Anonymous users cannot pull them.

## Solution

Make the package public via GitHub UI (one-time action).

## Steps

1. **Navigate to Packages**

   - Go to: https://github.com/emergent-company/emergent/packages
   - Or: Repository → Packages (right sidebar)

2. **Find the Package**

   - Look for `emergent-server-with-cli`
   - Click on the package name

3. **Open Package Settings**

   - Click "Package settings" (bottom right)

4. **Change Visibility**

   - Scroll to "Danger Zone"
   - Click "Change visibility"
   - Select "Public"
   - Confirm the action

5. **Verify**
   ```bash
   docker pull ghcr.io/emergent-company/emergent-server-with-cli:latest
   ```
   Should succeed without authentication.

## Packages to Make Public

For the minimal installer to work, make these public:

- ✅ `emergent-server-with-cli` (main server + CLI)
- ✅ `emergent-kreuzberg` (vector DB)
- ✅ `emergent-minio` (object storage)

## Alternative: Using GitHub Token in Workflow

**Not recommended** - Adds complexity for users. Making packages public is simpler.

<details>
<summary>Advanced: Auto-public via API (not implemented)</summary>

```yaml
# Would require PAT with write:packages scope
- name: Make package public
  run: |
    gh api graphql -f query='
      mutation {
        updatePackageVersion(input: {
          packageVersionId: "${{ env.PACKAGE_ID }}"
          visibility: PUBLIC
        }) {
          success
        }
      }'
```

Not implemented because:

- Requires additional PAT management
- UI action is simpler (one-time)
- Packages should be public by default for OSS

</details>
