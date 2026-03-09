---
name: upgrade-envs
description: Upgrade all server installations to the latest tagged version. Use when the user says "upgrade both", "upgrade all envs", "upgrade installations", or "upgrade both installations".
license: MIT
compatibility: opencode
metadata:
  author: emergent
  version: '1.1'
  trigger: upgrade both, upgrade all envs, upgrade installations, upgrade both installations, upgrade all installations
---

# Upgrade Envs Skill

Upgrades all known server installations to the latest git-tagged Docker image version.

## Known Installations

| Name | SSH | Health port | Upgrade mechanism |
|---|---|---|---|
| production | `ssh -J root@hosting root@10.10.10.220` | `3012` | `SERVER_VERSION` in `/root/emergent/.env` + docker compose |
| mcj-emergent | `ssh root@mcj-emergent` | `3002` | `memory server upgrade --force` CLI |

## Process

### Step 1: Find the latest version

```bash
git tag --sort=-v:refname | head -3
```

Take the top tag (e.g. `v0.30.16` → version string is `0.30.16`).

### Step 2: Capture pre-upgrade state

Run all four in parallel to snapshot the baseline:

```bash
# production
ssh -J root@hosting root@10.10.10.220 "curl -s http://localhost:3012/health"

# mcj-emergent
ssh root@mcj-emergent "memory version"
ssh root@mcj-emergent "memory server ctl status 2>&1"
ssh root@mcj-emergent "curl -s http://localhost:3002/health"
```

Parse and show:
```
Pre-upgrade state:
  production:   v<version> (<status>)
  mcj-emergent: v<version> (<container states>)
```

If both are already on the latest version, stop and report — nothing to do.

### Step 3: Upgrade both in parallel

**Production** — update `SERVER_VERSION` in `.env`, then pull and recreate in the background (no `memory` CLI on this host):

```bash
ssh -J root@hosting root@10.10.10.220 "
  sed -i 's/SERVER_VERSION=.*/SERVER_VERSION=NEW/' /root/emergent/.env &&
  nohup bash -c 'cd /root/emergent && docker compose pull server && docker compose up -d --force-recreate server' > /tmp/upgrade.log 2>&1 &
  echo 'Production upgrade started in background'
"
```

**mcj-emergent** — use the `memory` CLI which handles image pull, container recreate, and CLI self-upgrade:

```bash
ssh -o ServerAliveInterval=30 -o ServerAliveCountMax=20 \
    root@mcj-emergent \
    'memory server upgrade --force 2>&1'
```

Run both SSH commands at the same time (parallel tool calls).

**Timeout**: 15 minutes — Docker image pulls can be slow on the first run.

Watch mcj-emergent output for these indicators:

| Pattern | Meaning |
|---------|---------|
| `Checking for updates...` | Upgrade started |
| `Will upgrade CLI: X → Y` | CLI version bump detected |
| `✓ CLI upgraded to X.Y.Z` | CLI done |
| `Pulling` | Docker image pull in progress |
| `✓ Upgrade complete!` | Full success |
| `Docker images for this release are still being built` | Images not ready yet — stop and report |
| `Error` / `failed` | Failure — capture full context |

If output contains `Docker images for this release are still being built`, stop and report:
> "Docker images for the latest release are not ready yet. Wait a few minutes and try again."

### Step 4: Wait and verify

After mcj-emergent upgrade completes, wait ~50s for production to finish pulling, then verify both:

```bash
# mcj-emergent
ssh root@mcj-emergent "memory server ctl status 2>&1 && curl -s http://localhost:3002/health | python3 -m json.tool"

# production (after ~50s)
ssh -J root@hosting root@10.10.10.220 "curl -s http://localhost:3012/health | python3 -m json.tool"
```

Both should report `"status": "healthy"` and `"version": "vNEW"`.

### Step 5: Report

Present a summary table:

```
| Server       | Before   | After    | Status    |
|---|---|---|---|
| production   | vA.B.C   | vX.Y.Z   | healthy ✓ |
| mcj-emergent | vA.B.C   | vX.Y.Z   | healthy ✓ |
```

## Failure Modes

**SSH to `hosting` jump host refused** — retry after a moment; if it persists, try `ssh -J root@zoidberg2 root@10.10.10.220` as an alternative jump host. Report to user if both are unreachable.

**mcj-emergent upgrade fails midway** — SSH back in and check state:
```bash
ssh root@mcj-emergent "memory server ctl status && memory server doctor"
```
If partial, re-run `memory server upgrade --force` to complete.

**Container starts but health check fails** — check logs:
```bash
ssh -J root@hosting root@10.10.10.220 "docker logs emergent-server-1 --tail 50"
ssh root@mcj-emergent "docker logs memory-server --tail 50"
```

**Image not yet available** — if pull fails with a 404/manifest error, CI image build may not be complete. Wait a few minutes and retry.

**Old version still running after production recreate** — the `--force-recreate` flag is required; without it Docker Compose won't replace a running container even after a pull.
