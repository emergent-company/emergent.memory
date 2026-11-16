#!/usr/bin/env bash
# The committed Astro port must match docs/site (#954).
#
# docs-astro/ is the published docs site, and docs-astro/src/content/docs/ is
# GENERATED from docs/site by docs-astro/scripts/port_mkdocs.py and committed.
# Nothing compared the two: editing docs/site is the normal way to change docs,
# the MkDocs build is happy with the new text, the Astro build is happy with its
# old snapshot, and a PR merged green while the site kept publishing the stale
# copy. #940 rewrote docs/site/developer-guide/schema.md and the port only caught
# up when an unrelated PR happened to re-run the generator (#953).
#
# `--check` renders the whole port in memory and diffs it against the tree
# without writing anything, so four things fail here: a stale page, a page the
# port writes but the tree lost, an orphan the converter no longer generates (a
# renamed/deleted source, or a page dropped from PAGES), and a hand edit to a
# generated page. docs/site is authoritative, so the fix is always "regenerate
# the port", never "edit the MDX". The five hand-written pages are a literal
# named set in the script (HANDWRITTEN), so a page cannot leave the gate by
# quietly falling out of the conversion list — the orphan it leaves fails too.
#
# It lives in preflight rather than a new workflow step because preflight
# already runs in the branch-protection-required `ci` job AND in the lanes
# before a push, so a stale port is caught by the person changing docs/site
# instead of by whoever merges next. The cost is ~0.3 s of stdlib python3 (no
# Node, no npm: the converter reads only committed files), which is why the gate
# does not need a path filter.
#
# Run from anywhere; resolves the repo root itself.
set -euo pipefail

root="$(git rev-parse --show-toplevel)"
cd "$root"

python3 docs-astro/scripts/port_mkdocs.py --check

# The gate's own regression test. Its fixtures are a throwaway source tree, so
# it neither reads the real docs nor depends on the port being current, and at
# ~0.4 s it is cheap enough to run alongside the gate — which matters because
# this check is the only thing between docs/site and a stale published site: a
# silent bug in it would be invisible until someone happened to notice the drift
# again (#954).
python3 docs-astro/scripts/port_mkdocs_test.py
