# Deployment Documentation

Deployment, infrastructure, and operations documentation.

## Structure

- **infrastructure/** - Infrastructure docs (ports, Docker)
- **DEPLOYMENT_GUIDE.md** - Main deployment guide
- **PORTABLE_DEPLOYMENT_COMPLETE.md** - Portable Docker deployment docs
- **migration-drift-runbook.md** - Runbook for goose migration drift (recorded-but-no-op
  migrations, `CONCURRENTLY` index swaps, out-of-order version merges) and local backlog catch-up

## Dev environment

There is **one** dev machine — see the canonical host/IP/alias table and the deploy trigger in
[`AGENTS.md` → Environment URLs](../../AGENTS.md#environment-urls). Do not target a separate
"legacy dev" host; it does not exist. Deploys are not scheduled — trigger
`.github/workflows/deploy-dev.yml` in `emergent-company/emergent.memory.infra` manually.
