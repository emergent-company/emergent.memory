# Deploy topology rename — prod systemd + /opt paths + checkout dirs

**Status:** proposed
**Created:** 2026-09-04
**Source:** [2026-09-04-rename-alfred-to-memory](../sessions/2026-09-04-rename-alfred-to-memory.md)

## What

Rename the remaining deployment-topology references that were intentionally left as-is during the product rename:
- **home2 systemd units**: `alfred.service` → `memory.service`, `EnvironmentFile=/opt/alfred/.env` → `/opt/memory/.env` (out-of-repo; documented as an external action item in the rename change).
- **On-disk checkout dirs**: `/root/alfred` (this server), `/Users/mcj/alfred` (Mac iOS dir), `~/code/alftred` (Mac build checkout).
- Corresponding `deploy.sh` / `tools/ios-*-mac.sh` / plist path literals (`/root/alfred`, `/Users/mcj/alfred`, `__HOME__/alfred`).

## Why

Deferred from the rename (design Non-Goals): directory paths are deployment topology, not product naming. Renaming them is a flag-day deploy change with no product benefit, but the literals now look stale.

## Depends on

none

## Notes

- Local dev unit already renamed (`alfred-dev.service` → `memory-dev.service`). This task covers **production** (home2) and the on-disk checkouts.
- `deploy.sh` still hardcodes `/root/alfred` (remote) and `/Users/mcj/alfred` (Mac) — update in the same deploy.
- Note the Mac checkout is `~/code/alftred` (typo'd variant), not `/Users/mcj/alfred` — reconcile while here.
