---
name: upgrade-envs
description: Upgrade all server installations to the latest tagged version. Use when the user says "upgrade both", "upgrade all envs", "upgrade installations", or "upgrade both installations".
license: MIT
compatibility: opencode
metadata:
  author: emergent
  version: '1.0'
  trigger: upgrade both, upgrade all envs, upgrade installations, upgrade both installations, upgrade all installations
---

# Upgrade Envs Skill

Upgrades all known server installations to the latest git-tagged Docker image version.

## Known Installations

| Name | SSH | Compose path | Health port | Version mechanism |
|---|---|---|---|---|
| production | `ssh -J root@hosting root@10.10.10.220` | `/root/emergent/` | `3012` | `SERVER_VERSION` in `/root/emergent/.env` |
| mcj-emergent | `ssh root@mcj-emergent` | `~/.memory/docker/` | `3002` | image tag pinned in `docker-compose.yml` |

## Process

### Step 1: Find the latest version

```bash
git tag --sort=-v:refname | head -3
```

Take the top tag (e.g. `v0.30.16` → version string is `0.30.16`).

### Step 2: Check what's currently running

```bash
# production
ssh -J root@hosting root@10.10.10.220 "curl -s http://localhost:3012/health"

# mcj-emergent
ssh root@mcj-emergent "curl -s http://localhost:3002/health"
```

If both are already on the latest version, stop and report — nothing to do.

### Step 3: Upgrade both in parallel

**Production** — update `SERVER_VERSION` in `.env`, then pull and recreate in the background:

```bash
ssh -J root@hosting root@10.10.10.220 "
  sed -i 's/SERVER_VERSION=OLD/SERVER_VERSION=NEW/' /root/emergent/.env &&
  nohup bash -c 'cd /root/emergent && docker compose pull server && docker compose up -d --force-recreate server' > /tmp/upgrade.log 2>&1 &
"
```

**mcj-emergent** — update the pinned image tag in `docker-compose.yml`, then pull and recreate (foreground, fast enough):

```bash
ssh root@mcj-emergent "
  sed -i 's|memory-server:OLD|memory-server:NEW|g' ~/.memory/docker/docker-compose.yml &&
  cd ~/.memory/docker &&
  docker compose pull server &&
  docker compose up -d --force-recreate server
"
```

Run both SSH commands at the same time (parallel tool calls or background `&`).

### Step 4: Verify both

Wait ~50s for production to finish pulling, then check health on both:

```bash
# mcj-emergent (instant)
ssh root@mcj-emergent "curl -s http://localhost:3002/health | python3 -m json.tool"

# production (after ~50s)
ssh -J root@hosting root@10.10.10.220 "curl -s http://localhost:3012/health | python3 -m json.tool"
```

Both should report `"status": "healthy"` and `"version": "vNEW"`.

### Step 5: Report

Present a summary table:

```
| Server       | Version  | Status    |
|---|---|---|
| production   | vX.Y.Z   | healthy ✓ |
| mcj-emergent | vX.Y.Z   | healthy ✓ |
```

## Failure Modes

**SSH to `hosting` jump host refused** — retry after a moment; if it persists, try `ssh -J root@zoidberg2 root@10.10.10.220` as an alternative jump host. Report to user if both are unreachable.

**Container starts but health check fails** — check logs:
```bash
ssh -J root@hosting root@10.10.10.220 "docker logs emergent-server-1 --tail 50"
ssh root@mcj-emergent "docker logs memory-server --tail 50"
```

**Image not yet available** — if `docker compose pull` fails with a 404/manifest error, the CI image build may not be complete yet. Wait a few minutes and retry.

**Old version still running after recreate** — the `--force-recreate` flag is required; without it Docker Compose won't replace a running container even after a pull.
