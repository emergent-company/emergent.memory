# Verify relay node badges after deploy

**Status:** proposed
**Created:** 2026-09-13
**Source:** [2026-09-13-memory-mac-connector](../sessions/2026-09-13-memory-mac-connector.md)

## What
Once GitHub Actions billing is fixed, publish the `memory-web-ui` image and get
infra to deploy it, then verify on the hosted web UI (Settings → MCP nodes):
live node shows a green `Connected` badge; a disconnected node stays listed with
a gray `Disconnected` badge + `last seen <rel>`; offline tools come from the
snapshot; the confirmed **Remove** action deletes a stale entry.

## Why
The registry UI shipped to master, but the image build (`Build & Publish`) cannot
start — GitHub billing/spending limit — so the deployed UI predates the change.

## Depends on
- [ci-actions-billing-blocker](ci-actions-billing-blocker.md) (same root cause).
- `emergent.memory.infra` deploying the new image.

## Notes
- Trigger: `gh workflow run build-publish.yml` (or re-run the failed run) after
  billing is resolved; watch it to completion.
- Removing a still-connected node is expected to reappear on refresh.
