## ADDED Requirements

### Requirement: Owner copy-link reveal is honest and works without the Clipboard API

The gateway's owner share-link list SHALL let an owner copy a recoverable link's full URL at any time — including on-demand recovery of an existing link whose key is not present at list time — without depending on the asynchronous Clipboard API. The copy action SHALL fall back to a textarea + `document.execCommand('copy')` when `navigator.clipboard.writeText` is unavailable or rejects, and SHALL report success ("Copied") only when a copy path actually succeeded, reporting "Copy failed" when both paths fail. The on-demand reveal fetch SHALL use a scope-aware URL (`data-reveal-url`), so on a scoped page (`/o/{org}/p/{proj}/agents/...`) the fetch stays in the page's context rather than hitting an unscoped path.

#### Scenario: Copy works at any moment for a recoverable link

- **WHEN** an owner clicks "Copy link" on an existing recoverable link whose key is not present at list time
- **THEN** the gateway fetches the scoped reveal endpoint and copies the returned full URL, even when the Clipboard API is unavailable (via the textarea + `execCommand` fallback)

#### Scenario: Copy reports failure honestly

- **WHEN** both the Clipboard API and the `execCommand` fallback fail
- **THEN** the button label reads "Copy failed" (and reverts), never silently "Copied"

#### Scenario: Reveal URL is scoped on org/project pages

- **WHEN** the owner list renders under `/o/{org}/p/{proj}/agents/{id}/share`
- **THEN** the row's `data-reveal-url` carries the scope base, so the reveal fetch resolves in context

#### Scenario: Non-recoverable link surfaces a clear message

- **WHEN** the reveal endpoint returns 422 `not_recoverable` (the link's token cannot be decrypted)
- **THEN** the row shows "Not recoverable — rotate this link to issue a new one." and the button reads "Rotate to recover"

### Requirement: Reveal recovery requires token encryption to be configured

The on-demand reveal SHALL recover a key only when the server's token encryption is configured (`INTEGRATION_ENCRYPTION_KEY`). When it is unset, share-link API tokens are stored with `core.api_tokens.token_encrypted = NULL`, so reveal returns 422 `not_recoverable` and the owner is told to rotate rather than recover.

#### Scenario: Unconfigured encryption key makes links non-recoverable

- **WHEN** `INTEGRATION_ENCRYPTION_KEY` is unset and an owner reveals an existing link
- **THEN** the reveal returns 422 `not_recoverable` and the UI surfaces the rotate hint (the key cannot be recovered client-side)

#### Scenario: The share e2e suite cannot catch encryption-key failures

- **WHEN** the Playwright share suite (`apps/web-ui/tests/e2e/share.config.ts`) runs against `apps/web-ui/tests/e2e/mock-memory.mjs`
- **THEN** the mock always returns the key for the reveal endpoint, so the non-recoverable path (and the copy-failure path) is never exercised — this class of failure must be covered by unit tests and manual verification, not the share e2e suite
