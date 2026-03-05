---
name: deploy-mcj
description: Upgrade the mcj-emergent test server via SSH. Runs "emergent upgrade", monitors output, then runs "emergent ctl status" and "emergent doctor" to verify the deployment. Use when the user says "upgrade mcj", "deploy to mcj", "update the test server", or "upgrade mcj-emergent".
metadata:
  author: emergent
  version: "1.0"
---

Upgrade the `mcj-emergent` test server: SSH in, run the upgrade, verify containers are healthy, run diagnostics, and report.

**Input**: Optional `--cli-only` flag to skip server upgrade and only update the CLI binary.

---

## Server details

| Field | Value |
|-------|-------|
| SSH host | `root@mcj-emergent` |
| Emergent install | `~/.emergent` |
| Upgrade command | `emergent upgrade --force` |
| Status command | `emergent ctl status` |
| Diagnostics command | `emergent doctor` |

---

## Steps

### 1. Capture pre-upgrade state

Run these two commands in parallel via SSH to snapshot the baseline:

```bash
ssh root@mcj-emergent 'emergent version'
ssh root@mcj-emergent 'emergent ctl status 2>&1'
```

Parse and show:
```
Pre-upgrade state on mcj-emergent:
  CLI version:  <version>
  Containers:   <running list from ctl status>
```

If SSH connection fails entirely, stop and report:
> "Cannot reach mcj-emergent. Check that the host is up and SSH key is loaded."

### 2. Check if images are ready for the latest release

Before running the upgrade, verify that Docker images are ready (the `images-ready.txt` sentinel on the latest GitHub release):

```bash
ssh root@mcj-emergent 'emergent upgrade --force 2>&1 | head -5'
```

If the output contains `Docker images for this release are still being built`, stop and report:
> "Docker images for the latest release are not ready yet. Wait a few minutes and try again."

Otherwise proceed.

### 3. Run the upgrade

```bash
ssh -o ServerAliveInterval=30 -o ServerAliveCountMax=20 \
    root@mcj-emergent \
    'emergent upgrade --force 2>&1'
```

**Timeout**: 15 minutes — Docker image pulls can be slow on the first run.

Stream the output and watch for these indicators in real-time:

| Pattern | Meaning |
|---------|---------|
| `Checking for updates...` | Upgrade started |
| `Will upgrade CLI: X → Y` | CLI version bump detected |
| `Downloading emergent-cli-linux-amd64.tar.gz` | Downloading new binary |
| `✓ CLI upgraded to X.Y.Z` | CLI done |
| `Upgrading server with new CLI...` | Server upgrade starting |
| `Pulling` | Docker image pull in progress |
| `✓ Upgrade complete!` | Full success |
| `Upgrade canceled.` | --force not working, investigate |
| `Error` / `failed` | Failure — capture full context |

If `--cli-only` was passed, use: `emergent upgrade --force --cli-only`

Show live output as it streams. If the command takes longer than 15 minutes or the SSH connection drops, report the timeout and skip to step 5.

### 4. Wait for server to come up

After a full upgrade (not `--cli-only`), the server container restarts. Wait up to 90 seconds for it to become healthy before running diagnostics:

```bash
ssh root@mcj-emergent '
  for i in $(seq 1 9); do
    status=$(curl -sf http://localhost:3002/health 2>&1)
    if echo "$status" | grep -q "healthy\|ok\|status"; then
      echo "Server is up after ${i}0s"
      exit 0
    fi
    echo "Waiting... (${i}0s elapsed)"
    sleep 10
  done
  echo "Server did not come up within 90s"
  exit 1
'
```

If the server doesn't come up, continue to diagnostics anyway — `emergent doctor` will capture why.

### 5. Run post-upgrade diagnostics

Run these in parallel:

```bash
ssh root@mcj-emergent 'emergent ctl status 2>&1'
ssh root@mcj-emergent 'emergent doctor 2>&1'
```

### 6. Parse and report

Parse `emergent doctor` output. The summary line looks like:
```
Checks: N passed[, M warnings][, P failed]
```

Individual check lines use prefix icons:
- `✓` — pass
- `⚠` — warning
- `✗` — fail

**Build and show the full report:**

```
## mcj-emergent Upgrade Report

### Version
  Before:  <pre_version>
  After:   <post_version from ctl status or emergent version>

### Container Status (emergent ctl status)
<formatted table of containers and their states>

### Diagnostics (emergent doctor)
<pass/warn/fail lines from doctor>

### Summary
  Checks: N passed, M warnings, P failed

### Verdict
  ✓ UPGRADE SUCCESSFUL  — or —  ✗ UPGRADE FAILED — see issues below
```

**Verdict rules:**
- `SUCCESSFUL` — `emergent doctor` summary shows 0 failed AND the server container is listed as running
- `DEGRADED` — doctor has only warnings, no failures, server is up
- `FAILED` — any `✗` checks in doctor, or server container is not running after the wait

**For each failed or warned check**, include a short note about what it means and what to do:

| Doctor check | Common cause | Suggested action |
|-------------|--------------|-----------------|
| Server Connectivity FAILED | Container not yet up | `ssh root@mcj-emergent 'emergent ctl restart'` |
| Docker Containers — VERSION MISMATCH | Old container still running | `ssh root@mcj-emergent 'emergent upgrade server --force'` |
| Docker Containers — NOT RUNNING | Crash on start | `ssh root@mcj-emergent 'docker logs emergent-server --tail 50'` |
| API Access FAILED | Server up but not accepting requests yet | Wait 30s and re-run `emergent doctor` |
| Google API Key NOT SET | Config not migrated | Check `~/.emergent/config/.env.local` on the server |

---

## Guardrails

- **Never run `docker rm` or `docker volume rm`** without explicit user instruction — data loss risk
- **Never modify `.env.local`** on the server without explicit user instruction
- **Never force-pull an unbuilt image** — if `images-ready.txt` is not in the release assets, the upgrade will pull the wrong tag
- If upgrade fails midway (partial CLI upgrade, server not restarted), report the partial state clearly and suggest `ssh root@mcj-emergent 'emergent upgrade server --force'` to complete only the server portion
- If the SSH connection times out during the image pull, don't assume failure — SSH back in and run `ssh root@mcj-emergent 'emergent ctl status && emergent doctor'` to check the actual state
