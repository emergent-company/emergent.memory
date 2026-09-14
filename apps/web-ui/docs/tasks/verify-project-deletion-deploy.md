# Verify project pending-deletion on dev after deploy

**Status:** proposed
**Created:** 2026-09-10
**Source:** [2026-09-10-project-pending-deletion](../sessions/2026-09-10-project-pending-deletion.md)

## What
Run the end-to-end project pending-deletion flow against the **deployed dev environment**
after the memory-server and memory-web-ui `:dev` images are rebuilt and `/opt/emergent-dev`
compose is restarted (migration `00147` applied):

1. Org landing → select projects → Delete → confirmation dialog → confirm shows busy state and
   the "N projects scheduled for deletion." flash.
2. Rows render the "Scheduled for deletion" badge with a purge time, are excluded from delete
   selection, and the page auto-refreshes while pending.
3. "Cancel deletion" restores a project (`POST /projects/restore`, "Deletion cancelled." flash).
4. With a short `PROJECT_DELETION_GRACE_PERIOD` on dev, confirm the sweep purges an expired
   project and its row disappears.
5. Run `tests/e2e/specs/projects/projects-table-ui.spec.ts` against dev.

## Why
The change is merged but not yet deployed/verified on dev. Local build/tests are green and the
e2e spec was updated, but neither the pending badge/restore nor the sweep has run against a
deployed memory-server.

## Depends on
- Memory PR #421 (`4822a7a`) and gateway PR #64 (`ae57b18`) deployed to dev.
- [project-delete-authorization](project-delete-authorization.md) is independent and not required
  for this verification.

## Notes
- Dev compose: `/opt/emergent-dev/docker-compose.yml` with `SERVER_VERSION=dev`, `WEBUI_VERSION=dev`;
  images `ghcr.io/emergent-company/memory-server:dev` / `memory.web-ui:dev`.
- Gateway `Build & Publish` is currently blocked by the Actions billing issue
  ([ci-actions-billing-blocker](ci-actions-billing-blocker.md)); the `:dev` image may need a manual build/push.
- Memory env knobs: `PROJECT_DELETION_GRACE_PERIOD` (default `1h`), `PROJECT_DELETION_SWEEP_INTERVAL`
  (default `1m`), `PROJECT_DELETION_SWEEP_SCHEDULE`.
