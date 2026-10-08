## Why

The Mac connector's Sparkle feed is published as GitHub **release assets** of
`emergent-company/emergent.memory`, but that repository is **private**. Release
assets in a private repo are not reachable without credentials — an
unauthenticated fetch of any release asset (including long-standing ones such as
`v0.86.0/memory-cli-darwin-arm64.tar.gz`) returns **404**. Sparkle fetches
`SUFeedURL` with no credentials, so the app can never read the feed and every
update check fails with "An error occurred in retrieving update information".

This is why the pipeline fixes (#1672/#1674/#1677) produced a correctly built
and signed feed that the app still cannot consume. The feed must be hosted on a
**public** static host.

The repo already has a public static host: the docs site at
`https://emergent-company.github.io/emergent.memory/` (GitHub Pages, served from
the `gh-pages` branch, `public: true`).

## What Changes

- Publish the signed Sparkle feed (`appcast.xml` + the update DMG(s)) to the
  public GitHub Pages site under `appcast/stable/` and `appcast/dev/`, via a new
  `.github/scripts/publish-appcast.sh` invoked by `mac-release.yml` and
  `mac-dev.yml`.
- Point the app's feeds (`SUFeedURL`, `MemoryUpdateFeedURLStable`,
  `MemoryUpdateFeedURLDev`) at those public URLs.
- Keep the private `memory-appcast`/`mac-dev` releases only as an archive store
  for `generate_appcast` (previous archives for deltas/ordering); they are no
  longer the feed URL.

## Capabilities

### Modified Capabilities

- `mac-connector-update`: the feed and update archives are hosted on a public
  static host (GitHub Pages), reachable without credentials; the feed URL is
  permanent and not a mutable "latest release" alias.

## Impact

- `.github/scripts/publish-appcast.sh` (new), `.github/workflows/mac-release.yml`,
  `.github/workflows/mac-dev.yml`, `apps/connector.mac/MemoryConnector/Info.plist`.
- GitHub Pages (`gh-pages` branch) content; the release workflows now push to it
  (with rebase-retry to tolerate concurrent docs deploys).
- No server/API changes.
