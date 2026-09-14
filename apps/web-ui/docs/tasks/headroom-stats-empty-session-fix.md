# Land parked fix: hide empty display-session block in headroom-stats

**Status:** done
**Created:** 2026-09-10
**Landed:** 2026-09-10 (one-line guard in `tools/headroom-stats.py`; verified with zero-request and active-session stubs plus live proxy)
**Source:** [2026-09-10-headroom-proxy-install](../sessions/2026-09-10-headroom-proxy-install.md)

## What
Re-apply and commit the small cosmetic fix to `tools/headroom-stats.py` so the "Last active session"
block is skipped when the proxy reports zero requests (after the inactivity rollover), instead of
printing `requests: 0` with `None UTC` timestamps.

## Why
Current merged script (PR #9) shows a useless empty session block after the proxy's display session
rolls over. The fix was written but never committed — the shared checkout was locked by a parallel
session's unresolved merge conflicts and the branch was later reused by an unrelated PR.

## Depends on
none

## Notes
- Patch was saved at `/tmp/opencode/headroom-stats-fix.patch` but `/tmp` is ephemeral; the change is
  one line and is inlined here for reference:

```diff
     # Current display session (rolling window of recent activity)
-    if session:
+    if session and session.get("requests"):
         s_pct = session.get("savings_percent")
```

- Verify by letting the display session roll over (60 min inactivity) or by stubbing
  `/stats-history` with `display_session.requests == 0`.
