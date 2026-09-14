# Route existing opencode sessions through the Headroom proxy

**Status:** proposed
**Created:** 2026-09-10
**Source:** [2026-09-10-headroom-proxy-install](../sessions/2026-09-10-headroom-proxy-install.md)

## What
Restart the long-running `opencode serve` daemons on `alfred-dev` (ports 4096 and 45517) — and any
Paseo-managed opencode workers/lanes — so they load the updated provider config and route LLM traffic
through the local Headroom proxy at `127.0.0.1:8787`.

## Why
opencode reads config at startup. Only processes started after the `baseURL` switch route through
Headroom; the daemons that were already running (and the subagent lanes they spawn) still talk to the
litellm gateway directly, so real compression savings do not accrue for them yet.

## Depends on
- Headroom proxy service described in `INFRASTRUCTURE.md` → "Headroom proxy".

## Notes
- Restarting the daemons drops in-flight sessions; do it when no work is active.
- After restart, confirm routing via `tools/headroom-stats.py` (request count should grow) and/or
  `journalctl -u headroom-proxy.service`.
- Revert path if needed: restore `~/.config/opencode/opencode.json` from
  `opencode.json.headroom-prebak` and restart the daemons again.
