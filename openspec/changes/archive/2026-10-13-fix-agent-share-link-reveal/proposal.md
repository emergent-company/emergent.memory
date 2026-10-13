## Why

On an agent's share page (`/agents/:id/share`), the "Copy link" button on an
existing share link fails silently. Two latent client-side bugs compound:

1. The `copyText` helper in `share_manage.templ` calls only
   `navigator.clipboard.writeText` and no-ops when the Clipboard API is
   unavailable or rejects — so nothing is copied and no error is surfaced.
2. `scopeAttrRe` in `scope.go` does not include `data-reveal-url`, so on a
   scoped page (`/o/{org}/p/{proj}/agents/...`) the reveal fetch uses an
   unscoped URL and 404s.

The `not_recoverable` case is already surfaced correctly in the reveal UI, but
the copy step itself is silently dishonest: it reports "Copied" regardless of
whether the write actually succeeded.

## What Changes

- `share_manage.templ`: `copyText` now takes success and failure callbacks and
  falls back to a textarea + `document.execCommand('copy')` when the Clipboard
  API is unavailable or rejects (mirroring `api_tokens.templ`). The
  `data-copy-link` and `data-reveal-url` paths report "Copied" only on real
  success and "Copy failed" when both copy paths fail. No new dependencies; no
  `innerHTML` (label updates use `textContent`).
- `scope.go`: add `data-reveal-url` to `scopeAttrRe` so the reveal fetch URL is
  scope-rewritten under `/o/{org}/p/{proj}`.
- `scope_test.go`: assert `data-reveal-url` is rewritten under a base and passes
  through unchanged on global pages.

## Capabilities

### Modified Capabilities

- `web-agent-public-share`: add requirements that the owner copy-link reveal
  works at any moment for a recoverable link (with a Clipboard-API fallback and
  honest labels), that a non-recoverable link surfaces a clear rotate-to-recover
  message, that the reveal URL is scope-aware, and that reveal recovery requires
  the server's token encryption to be configured.

## Impact

- `apps/web-ui/gateway/share_manage.templ`: `copyText` fallback + honest copy
  labels.
- `apps/web-ui/gateway/scope.go`: `scopeAttrRe` includes `data-reveal-url`.
- `apps/web-ui/gateway/scope_test.go`: `data-reveal-url` rewrite + global
  pass-through assertions.
- `openspec/changes/fix-agent-share-link-reveal/`: new change (proposal, tasks,
  delta spec).

Note: the root cause of the `not_recoverable` failures — share-link API tokens
stored with `core.api_tokens.token_encrypted = NULL` because
`INTEGRATION_ENCRYPTION_KEY` is unset — is provisioned in a separate infra
change. This PR documents that prerequisite and hardens the client; it does not
change server encryption.
