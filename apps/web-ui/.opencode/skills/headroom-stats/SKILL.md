---
name: headroom-stats
description: Check and report Headroom proxy compression stats (lifetime tokens saved, prompt-cache reads, $ savings, recent requests). Use when asked for headroom stats, compression savings, token/cost savings from the proxy, or to verify opencode traffic is routing through Headroom.
---

# Headroom Stats

Report the local **Headroom** compression proxy's savings (opencode → litellm gateway). See `INFRASTRUCTURE.md` § "Headroom proxy".

## When to use

- User asks for "headroom stats", "compression savings", token/`$` savings, or request counts.
- Verifying opencode traffic actually routes through the proxy (request count grows).
- Periodic check that the proxy is healthy and saving.

## Quick start

```bash
tools/headroom-stats.py            # lifetime + last active session + recent 5 rows
tools/headroom-stats.py -n 10      # show last 10 request rows
tools/headroom-stats.py --raw      # dump raw /stats-history JSON
```

Run from repo root (`/root/alfred`). Exit 0 = ok, 1 = proxy unreachable/bad response.

For a one-line health gate before reporting: `systemctl status headroom-proxy --no-pager`.

## Report format

Relay the script's output compactly, preserving these fields:

```
requests            : <N>
input tokens billed : <N>
tokens saved        : <N>  (<pct>%)
cache reads         : <N> ($<x>)
total savings       : $<x>
```

Then note the last active session (requests / tokens saved / start / last activity) and, if useful, the recent per-request rows. State the model(s) seen and whether savings are trending up.

## Reading the output

- **tokens saved %** is `tokens_saved / input tokens billed`. Small % is normal for Headroom (aggressive compression is not the goal).
- **prompt-cache reads** are provider-side cache hits, billed at a discount — separate from compression savings.
- **total $ savings** = compression + cache + output-token savings.
- **Scope caveat:** opencode reads config at startup. Only processes started after routing was enabled go through the proxy; long-running `opencode serve` daemons keep the old baseURL until restarted. Low/zero recent counts usually mean no fresh process, not a broken proxy.

## Failures

- Script exits 1 with a "cannot reach Headroom proxy" hint → proxy down or wrong port.
  - `systemctl status headroom-proxy` → `systemctl start headroom-proxy` / `restart`.
  - Logs: `journalctl -u headroom-proxy.service -f`.
  - Health: `headroom doctor`.
- Proxy up but empty history → no traffic routed yet (see Scope caveat).

## Other surfaces

- `curl http://127.0.0.1:8787/stats` (live) · `/stats-history` (durable, `~/.headroom/proxy_savings.json`) · `/metrics` (Prometheus).
- `headroom dashboard` (web UI) · `/root/.local/bin/headroom savings` (CLI).
