#!/usr/bin/env python3
"""Human-friendly stats for the local Headroom compression proxy.

Headroom (http://127.0.0.1:8787) compresses opencode -> litellm-gateway LLM
traffic on this box (see INFRASTRUCTURE.md, "Headroom proxy"). This script
pulls the proxy's durable savings history and prints it in a readable form.

Usage:
    tools/headroom-stats.py                # lifetime + recent requests
    tools/headroom-stats.py -n 10          # show the last 10 request rows
    tools/headroom-stats.py --raw          # dump the raw JSON instead

Exit codes: 0 = ok, 1 = proxy unreachable or bad response.
"""

import argparse
import json
import sys
import urllib.error
import urllib.request

DEFAULT_URL = "http://127.0.0.1:8787"


def fetch(url: str) -> dict:
    try:
        with urllib.request.urlopen(url, timeout=5) as resp:
            return json.load(resp)
    except urllib.error.HTTPError as exc:
        sys.exit(f"error: proxy answered {exc.code} at {url}")
    except (urllib.error.URLError, OSError) as exc:
        sys.exit(
            "error: cannot reach Headroom proxy at "
            f"{url} ({exc.reason if hasattr(exc, 'reason') else exc})\n"
            "  is it running? try:  systemctl status headroom-proxy\n"
            "                      systemctl start headroom-proxy"
        )
    except ValueError as exc:
        sys.exit(f"error: non-JSON response from {url}: {exc}")


def money(value: float) -> str:
    decimals = 4 if value < 1 else 2
    return f"${value:,.{decimals}f}"


def human(num: float) -> str:
    return f"{num:,.0f}"


def main() -> int:
    ap = argparse.ArgumentParser(description=__doc__)
    ap.add_argument("--url", default=DEFAULT_URL, help=f"proxy base URL (default {DEFAULT_URL})")
    ap.add_argument("-n", "--history", type=int, default=5, help="recent request rows to show")
    ap.add_argument("--raw", action="store_true", help="dump raw JSON and exit")
    args = ap.parse_args()

    data = fetch(f"{args.url}/stats-history")
    if args.raw:
        print(json.dumps(data, indent=2))
        return 0

    life = data.get("lifetime", {})
    session = data.get("display_session", {})
    history = data.get("history", [])

    print("Headroom compression proxy  (openCode -> litellm gateway)")
    print("-" * 62)

    # Lifetime row
    saved = life.get("tokens_saved", 0)
    billed = life.get("total_input_tokens", 0)
    pct = f"  ({saved / billed:.1%} of billed input)" if billed else ""
    print("Lifetime:")
    print(f"  requests               : {human(life.get('requests', 0))}")
    print(f"  input tokens billed    : {human(billed)}")
    print(f"  tokens saved           : {human(saved)}{pct}")
    comp_cost = life.get("compression_savings_usd", 0)
    out_saved = life.get("output_tokens_saved", 0)
    out_cost = life.get("output_savings_usd", 0)
    attr = "compression/output" if out_cost else "compression"
    print(f"  total $ savings        : {money(comp_cost + out_cost)}  ({attr} only)")
    if out_saved:
        print(f"  output tokens saved    : {human(out_saved)}  ({money(out_cost)})")
    # Provider prefix-cache reads are a provider-native discount: the provider
    # caches the prompt prefix whether or not Headroom sits in the path, so the
    # proxy's lifetime cache_savings_usd is NOT Headroom's doing. Report the
    # token count for visibility, but never add it to "$ savings" (the proxy's
    # own /stats cost block likewise reports 0 for these).
    cache = life.get("cache_read_tokens", 0)
    if cache:
        print(f"  provider cache reads   : {human(cache)}"
              "  (provider-native; not counted as savings)")

    # Current display session (rolling window of recent activity)
    if session and session.get("requests"):
        s_pct = session.get("savings_percent")
        print("\nLast active session:")
        print(f"  requests               : {session.get('requests', 0)}")
        print(f"  tokens saved           : {human(session.get('tokens_saved', 0))}"
              + (f"  ({s_pct:.1f}%)" if s_pct is not None else ""))
        print(f"  started                : {session.get('started_at', '?')} UTC")
        print(f"  last activity          : {session.get('last_activity_at', '?')} UTC")

    # Recent per-request rows
    if history:
        rows = history[-args.history :]
        print(f"\nRecent activity (last {len(rows)} history entries):")
        print(f"  {'time':<22}{'provider':<10}{'model':<20}{'saved':>8}  {'$':>9}")
        for row in rows:
            ts = (row.get("timestamp") or "?")[:16]
            model = (row.get("model") or "?")[:19]
            print(f"  {ts:<22}{row.get('provider', '?'):<10}{model:<20}"
                  f"{human(row.get('total_tokens_saved', 0)):>8}  "
                  f"{money(row.get('compression_savings_usd', 0)):>9}")
    else:
        print("\nNo request history yet — route some traffic through the proxy first.")

    print("\n(proxy /stats and /metrics also available; live view: `headroom dashboard`)")
    return 0


if __name__ == "__main__":
    sys.exit(main())
