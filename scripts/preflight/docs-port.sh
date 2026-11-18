#!/usr/bin/env bash
# Fail when the committed Astro docs port disagrees with its source.
#
# `docs-astro/src/content/docs` holds 77 MDX pages GENERATED from `docs/site` by
# `docs-astro/scripts/port_mkdocs.py`, and they are committed. That makes the
# port a snapshot: a PR that edits only `docs/site` — the normal way anyone
# changes docs — builds green in both sites, and the published Astro site keeps
# serving the previous text. Nothing ran the converter, so the drift was only
# noticed when an unrelated PR re-ran `shots:port` and carried the regenerated
# pages in its diff (#954, after #940 and #953).
#
# The work happens in `port_mkdocs.py --check`: it ports into a temp directory,
# diffs the committed tree and names every page that is stale (behind
# `docs/site`, or hand-edited — the source is authoritative, so the remedy is
# always "regenerate the port"), missing, or an orphan the port would no longer
# generate. The five hand-written pages are excluded by name in the script.
#
# `--self-test` runs the same cases against throwaway copies (clean tree,
# hand-written exclusion, hand edit, deleted page, leftover page, source-ahead,
# missing source), so the guard's own decisions are pinned.
#
# This wrapper exists so it is reachable from preflight/all.sh — which the
# branch-protection-required `ci` job runs — and from the local lane, without a
# caller needing to know where the converter lives. It reads only committed
# files (the screenshot manifest is committed), needs no Node/npm, and costs
# ~0.3 s for the check plus ~1 s for the self-test.
set -euo pipefail

root="$(git rev-parse --show-toplevel)"
cd "$root"

converter="docs-astro/scripts/port_mkdocs.py"

if [ ! -f "$converter" ]; then
  # Fail closed: the generated tree is committed, so a missing converter means
  # the gate was deleted or renamed, and PASS would silently disable it.
  echo "docs-port: $converter is missing; the port can no longer be verified" >&2
  exit 1
fi

python3 "$converter" --check
python3 "$converter" --self-test
