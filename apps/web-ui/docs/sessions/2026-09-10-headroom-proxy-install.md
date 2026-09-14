# 2026-09-10 — Headroom proxy install (opencode)

## Goal
Install Headroom (local context-compression proxy) so opencode LLM traffic from `alfred-dev` to the
litellm gateway is compressed before it reaches the model, and add a human-friendly in-repo stats
script.

## Outcome
Done for **new** opencode processes; two follow-ups parked.

- Headroom CLI 0.37.0 installed on `alfred-dev` (`uv tool install --python 3.13 "headroom-ai[proxy,mcp]"`).
  Proxy runs persistently as systemd `headroom-proxy.service` on `127.0.0.1:8787`, upstream
  `http://100.113.48.6:4000/v1` (tailnet host `litellm`).
- opencode global config `provider.litellm.options.baseURL` switched to the local proxy; newly started
  opencode processes route through it (verified with a fresh `opencode run`).
- `tools/headroom-stats.py` + an `INFRASTRUCTURE.md` section shipped in PR #9.
- Existing long-running `opencode serve` daemons (and the Paseo subagent lanes they spawn) still bypass
  the proxy — restart pending (see tasks). User chose not to restart mid-session.
- A cosmetic fix for the stats script (empty display-session block) was written but never committed:
  the shared checkout got locked by a parallel session's unresolved conflicts, then the branch name was
  reused by an unrelated PR. Patch preserved at `/tmp/opencode/headroom-stats-fix.patch` (ephemeral);
  task tracks re-landing it.

## Decisions
- **Local proxy on alfred-dev, not a LiteLLM-gateway callback** — the gateway plausibly serves
  non-dev/product traffic; keep lossy compression off that path. (User-selected.)
- **Lean install `[proxy,mcp]`, not `[all]`** — `[all]` pulls a multi-GB CUDA/vision stack; the
  JSON/AST/cache compressors already cover agent tool output.
- **Upstream via `OPENAI_TARGET_API_URL`** (Headroom's OpenAI-compatible-gateway path) instead of the
  transport plugin — no per-process env/plugin wiring required.
- **Route by editing global opencode config `baseURL`** — avoids project/global provider deep-merge
  uncertainty; pre-change backup kept.
- **Defer native LiteLLM guardrail scoping** — the `litellm` host runs v1.88.0; the native Headroom
  guardrail requires ≥1.92.
- **Do not restart `opencode serve` daemons during the session** — would drop live sessions, including
  the one doing the work.

## Changes
- `tools/headroom-stats.py` (new) — human-friendly stats from proxy `/stats-history`: lifetime tokens
  and $ saved, prompt-cache reads, last active session, recent activity; friendly error + `systemctl`
  hint when the proxy is unreachable. (Merged via PR #9.)
- `INFRASTRUCTURE.md` — new "Headroom proxy (dev opencode LLM compression)" section: install, service,
  routing, scope caveat, stats surfaces, tuning, revert. (Merged via PR #9.)
- Non-repo (box `alfred-dev`): `/etc/systemd/system/headroom-proxy.service`; `headroom` CLI at
  `/root/.local/bin/headroom`; `~/.config/opencode/opencode.json` `baseURL` →
  `http://127.0.0.1:8787/v1` (backup `opencode.json.headroom-prebak`).

## Verification
- `curl http://127.0.0.1:8787/v1/chat/completions` (model `deepseek-v4-flash`, bearer passthrough) —
  200 completion via the gateway; response `x-litellm-model-api-base: https://api.deepseek.com/beta`.
- Proxy `/stats` after test traffic — 2 compressed of 4 requests; best `6,404 → 5,683` tokens (11.3%);
  lifetime 3,359 tokens saved, $0.0056.
- Fresh headless `opencode run` — proxy `api_requests` 2→4 and tokens saved 721→3,359, proving new
  processes route through the proxy.
- `headroom doctor` — 0 failures, proxy healthy v0.37.0.
- `tools/headroom-stats.py` — correct live output; exit 1 with hint when the proxy is down.

## Open questions / follow-ups
- Restart the `opencode serve` daemons (and the Paseo lanes) so existing sessions compress —
  see `headroom-route-existing-opencode-sessions`.
- Land the parked stats-script fix — see `headroom-stats-empty-session-fix`.
- Optional proxy tuning: `HEADROOM_OUTPUT_SHAPER=1` (response trimming, off by default) and
  `HEADROOM_BUDGET` spend cap, both set in the systemd unit.
- Revisit scoped compression (per key/model) if the `litellm` host is upgraded to ≥1.92 —
  see `headroom-litellm-native-guardrail-scoping`.
- Shared-checkout collisions: parallel sessions caused an index-wide commit lock (`UU` paths)
  mid-session; prefer worktree isolation for future writer lanes.
- Stray branch `omos/headroom-stats-fix` was reused by unrelated PRs (#15/#17) and is merged — safe to
  delete when convenient.

## Tasks
- [headroom-route-existing-opencode-sessions](../tasks/headroom-route-existing-opencode-sessions.md) —
  restart serve daemons so existing sessions compress
- [headroom-stats-empty-session-fix](../tasks/headroom-stats-empty-session-fix.md) — land the parked
  stats display fix
- [headroom-litellm-native-guardrail-scoping](../tasks/headroom-litellm-native-guardrail-scoping.md) —
  evaluate the native litellm guardrail after ≥1.92
