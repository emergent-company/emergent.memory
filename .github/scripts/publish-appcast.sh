#!/usr/bin/env bash
# Publish a built Sparkle feed (appcast.xml + update archives) to the public
# GitHub Pages site.
#
# Why Pages and not a release asset: SUFeedURL is baked into every shipped
# build, so it must never change, and each channel needs one permanent path.
# `releases/latest/download/<asset>` skips prereleases, so it cannot serve the
# dev channel, and the rolling `mac-dev` release is deleted and recreated on
# every dev build, so its asset URL 404s in that window. The docs site
# (https://<org>.github.io/<repo>/) is served from `gh-pages` and gives one fixed
# path per channel, so the feed is published under `appcast/<channel>/` there.
# The `memory-appcast` and `mac-dev` releases are only an archive store for
# generate_appcast.
#
# Usage: publish-appcast.sh <channel: stable|dev> <marketing-version> <source-dir>
#   source-dir must contain appcast.xml and the Memory-*.dmg (+ optional *.delta)
#   archives to publish.
#
# Env: GH_TOKEN (contents:write), GITHUB_REPOSITORY (owner/repo),
#      PAGES_ORG (default: repo owner).
set -euo pipefail

channel="${1:?usage: publish-appcast.sh <stable|dev> <marketing> <source-dir>}"
marketing="${2:?marketing version required}"
source_dir="${3:?source directory required}"
repo="${GITHUB_REPOSITORY:?GITHUB_REPOSITORY not set}"
token="${GH_TOKEN:?GH_TOKEN not set}"
org="${PAGES_ORG:-${repo%%/*}}"
repo_name="${repo##*/}"
pages_url="https://${org}.github.io/${repo_name}"
dest_rel="appcast/${channel}"

# Canonicalise the source dir to an absolute path: we `cd` into the cloned pages
# branch below, so a relative source path would no longer resolve.
source_dir="$(cd "${source_dir}" && pwd)"
[ -f "${source_dir}/appcast.xml" ] || { echo "error: ${source_dir}/appcast.xml not found" >&2; exit 1; }

# Shared size guard (also enforced at build time in Scripts/build-dmg.sh): a DMG
# over the guard is committed to gh-pages, where GitHub's 100 MiB per-file push
# limit would reject it and silently 404 the feed's enclosure URL.
script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
check_pages_size() { bash "${script_dir}/check-pages-file-size.sh" "$@"; }

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

# Fresh shallow clone of the pages branch, isolated from the caller's checkout
# (which may be a shallow tag checkout that cannot host a worktree commit).
git clone --depth 1 --branch gh-pages \
  "https://x-access-token:${token}@github.com/${repo}.git" "${tmp}/pages" >/dev/null 2>&1
cd "${tmp}/pages"

mkdir -p "${dest_rel}"
cp "${source_dir}/appcast.xml" "${dest_rel}/"
# DMGs are required (the feed references them); deltas are optional.
shopt -s nullglob
dmg=( "${source_dir}"/Memory-*.dmg )
[ "${#dmg[@]}" -gt 0 ] || { echo "error: no Memory-*.dmg in ${source_dir}" >&2; exit 1; }
check_pages_size "${dmg[@]}"
cp "${dmg[@]}" "${dest_rel}/"
deltas=( "${source_dir}"/*.delta )
[ "${#deltas[@]}" -gt 0 ] && cp "${deltas[@]}" "${dest_rel}/"

# Stop the branch from growing: drop every archive in this channel that the new
# appcast.xml no longer references. Such a file is unreachable to Sparkle (no
# published feed points at it), so removing it cannot break an update, while a
# file the feed *does* reference is always kept.
bash "${script_dir}/prune-appcast-orphans.sh" "${channel}"

# Aggregate guard: check-pages-file-size.sh protects GitHub's 100 MiB per-file
# push limit; this protects the separate 1 GB limit on a *published* Pages site,
# above which GitHub stops building and the feed/docs silently go stale.
bash "${script_dir}/check-pages-tree-size.sh" .

git config user.name "github-actions[bot]"
git config user.email "github-actions[bot]@users.noreply.github.com"
git add "${dest_rel}"
if git diff --cached --quiet; then
  echo "no feed changes to publish"
  exit 0
fi
git commit -q -m "appcast(${channel}): ${marketing}"

# The docs site also deploys to gh-pages; retry with rebase to tolerate a
# concurrent deploy.
for attempt in 1 2 3 4 5; do
  if git push origin HEAD:gh-pages; then
    echo "published ${channel} feed to ${pages_url}/${dest_rel}/"
    exit 0
  fi
  echo "push rejected (attempt ${attempt}); rebasing on latest gh-pages" >&2
  git pull --rebase origin gh-pages >/dev/null 2>&1 || true
  sleep 3
done
echo "error: could not publish ${channel} feed to gh-pages" >&2
exit 1
