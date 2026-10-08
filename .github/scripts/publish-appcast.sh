#!/usr/bin/env bash
# Publish a built Sparkle feed (appcast.xml + update archives) to the public
# GitHub Pages site.
#
# Why Pages and not the GitHub release: the repository is PRIVATE, so release
# assets are NOT reachable without credentials — an unauthenticated fetch of
# https://github.com/<repo>/releases/download/<tag>/<asset> returns 404. Sparkle
# fetches SUFeedURL with no credentials, so the feed MUST live on a public host.
# This repo's docs site (https://<org>.github.io/<repo>/) is public and served
# from the `gh-pages` branch, so the feed is published under
# `appcast/<channel>/` there.
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

[ -f "${source_dir}/appcast.xml" ] || { echo "error: ${source_dir}/appcast.xml not found" >&2; exit 1; }

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
cp "${dmg[@]}" "${dest_rel}/"
deltas=( "${source_dir}"/*.delta )
[ "${#deltas[@]}" -gt 0 ] && cp "${deltas[@]}" "${dest_rel}/"

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
