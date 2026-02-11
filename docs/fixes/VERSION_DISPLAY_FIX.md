# Version Display Fix (Emergent CLI v0.4.11)

**Date**: 2026-02-11  
**Issue**: Inconsistent version prefix display in `emergent upgrade` command  
**Status**: ✅ Fixed

---

## Problem Description

The `emergent upgrade` command displayed inconsistent version formats:

```
Current CLI version: 0.4.11    ← No "v" prefix
Latest version: v0.4.11        ← Has "v" prefix
```

While the **comparison logic was correct** (it normalized both versions before comparing), the visual inconsistency was confusing to users.

---

## Root Cause

**Build Process**:

- GitHub workflow extracts version from git tag: `v0.4.11`
- Build script strips "v" before setting ldflags: `-X ...cmd.Version=0.4.11`
- Result: Binary has `Version = "0.4.11"` (no prefix)

**GitHub Release**:

- Release tagged as `v0.4.11` (with prefix)
- API returns `release.TagName = "v0.4.11"`

**Display Code** (before fix):

- Line 170: `fmt.Printf("Current CLI version: %s\n", Version)` → Prints `"0.4.11"`
- Line 171: `fmt.Printf("Latest version: %s\n", release.TagName)` → Prints `"v0.4.11"`

---

## Solution

**Normalize ALL user-facing version strings** by stripping the "v" prefix consistently.

### Changes Made

#### 1. Main Display Block (Lines 168-171)

```go
// Show what will be upgraded (normalize display - strip "v" prefix for consistency)
fmt.Println()
displayCurrent := strings.TrimPrefix(Version, "v")
displayLatest := strings.TrimPrefix(release.TagName, "v")
fmt.Printf("Current CLI version: %s\n", displayCurrent)
fmt.Printf("Latest version: %s\n", displayLatest)
```

#### 2. Dev Version Skip Message (Lines 137-143)

```go
if Version == "dev" && !upgradeFlags.force {
	displayLatest := strings.TrimPrefix(release.TagName, "v")
	fmt.Println("You are running a development version. Upgrade skipped.")
	fmt.Printf("Latest release: %s\n", displayLatest)
	fmt.Println("Use --force to upgrade anyway.")
	return
}
```

#### 3. Already Latest Message (Lines 147-151)

```go
if !cliNeedsUpgrade && !serverInstalled {
	displayCurrent := strings.TrimPrefix(Version, "v")
	fmt.Printf("You are already using the latest version: %s\n", displayCurrent)
	return
}
```

#### 4. Docker Images Warning (Lines 152-157)

```go
if serverInstalled && !upgradeFlags.cliOnly && !release.ImagesReady {
	displayLatest := strings.TrimPrefix(release.TagName, "v")
	fmt.Println()
	fmt.Println("⚠️  Warning: Docker images for this release are still being built")
	fmt.Printf("Latest release: %s\n", displayLatest)
	fmt.Println()
```

#### 5. Upgrade Action Messages (Lines 179-185)

```go
if cliNeedsUpgrade {
	if Version == "dev" {
		fmt.Printf("Will upgrade CLI from dev version to %s\n", displayLatest)
	} else if latestVersion == currentVersion {
		fmt.Printf("Will reinstall CLI %s\n", displayLatest)
	} else {
		fmt.Printf("Will upgrade CLI: %s → %s\n", displayCurrent, displayLatest)
	}
} else {
	fmt.Println("CLI is up to date")
}
```

#### 6. Success Message (Line 218-220)

```go
displayLatest := strings.TrimPrefix(release.TagName, "v")
fmt.Printf("✓ CLI upgraded to %s\n", displayLatest)
```

---

## Verification

### Build Test

```bash
cd /root/emergent/tools/emergent-cli
go build -o /tmp/emergent-cli ./cmd
# ✅ Build successful
```

### Expected Output (After Fix)

```
Current CLI version: 0.4.11
Latest version: 0.4.11

Will upgrade CLI: 0.4.11 → 0.4.12
```

All version strings now display without the "v" prefix consistently.

---

## Comparison Logic (Unchanged)

The comparison logic was **already correct** and remains unchanged:

```go
// Line 124-127: Comparison logic (CORRECT - not modified)
latestVersion := strings.TrimPrefix(release.TagName, "v")     // "v0.4.11" → "0.4.11"
currentVersion := strings.TrimPrefix(Version, "v")            // "0.4.11" → "0.4.11"
// Comparison: "0.4.11" == "0.4.11" ✅
```

This fix **only changes display formatting**, not comparison behavior.

---

## Prevention

**Best Practice**: Always normalize version strings for user-facing output.

```go
// ✅ CORRECT: Normalize before display
displayVersion := strings.TrimPrefix(rawVersion, "v")
fmt.Printf("Version: %s\n", displayVersion)

// ❌ WRONG: Display raw version strings
fmt.Printf("Version: %s\n", rawVersion)
```

**Why This Matters**:

- Internal version format can change (build scripts, ldflags)
- External APIs may use different conventions (GitHub tags)
- Users expect consistent formatting throughout the application
- Visual consistency reduces confusion and support requests

---

## Files Modified

- `tools/emergent-cli/internal/cmd/upgrade.go`
  - Lines 137-143 (dev skip message)
  - Lines 147-151 (already latest message)
  - Lines 152-157 (docker images warning)
  - Lines 168-171 (main display block)
  - Lines 179-185 (upgrade action messages)
  - Lines 218-220 (success message)

---

## Related Work

**v0.4.11 Deployment** (completed prior to this fix):

- ✅ Docker images published: `ghcr.io/emergent-company/emergent-server-go:0.4.11`
- ✅ CLI upgraded on remote machine (`mcj@100.123.170.53`)
- ✅ MCP fix verified working (32 tools accessible)
- ✅ `emergent doctor` now passes MCP Server check

**Primary Objective** (v0.4.11): Fix "must call initialize before tools/list" error

- Fixed by adding X-API-Key header support in `extractToken()`
- Deployed and verified on production-like remote machine

**This Fix** (version display): Quality-of-life improvement for user experience
