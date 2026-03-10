## Why

Several live secrets—Zitadel RSA private keys, a Langfuse secret key, and a Context7
API key—are committed to the current HEAD of the public `emergent.memory` repository,
and GitHub Secret Scanning is disabled across all 15 org repositories so they went
undetected. They must be removed from HEAD and git history, and the credentials rotated,
before they can be exploited.

## What Changes

- **Rotate credentials** (out-of-band, before any git history rewrite):
  - Context7 API key (`ctx7sk-77ad3f0a-...`) — live in HEAD in 3 tracked files
  - Langfuse secret key (`sk-lf-4793a6ae...`) — live in HEAD in `.env`
  - Zitadel PAT, two RSA service-account private keys, and master key — live in HEAD
    in `docker/.env` (verify whether reused in production before rotating)

- **`docker/.env`** — replace all real credential values with obvious dummy defaults so
  docker-compose still works out of the box; add `docker/.env` to `.gitignore`; real
  local-dev overrides supplied via `docker/.env.local` (already pattern-matched by
  `**/.env.local` in `.gitignore`)

- **`.env`** — remove `LANGFUSE_SECRET_KEY` (moves to `.env.local` only); keep
  `LANGFUSE_PUBLIC_KEY` in `.env` as a semi-public project identifier

- **`opencode.jsonc`** — replace the hardcoded `CONTEXT7_API_KEY` header value with
  `"{env:CONTEXT7_API_KEY}"` (opencode's documented env-var substitution syntax)

- **`.vscode/mcp.json`** — replace the hardcoded Context7 CLI `--api-key` argument and
  DaisyUI license key with env-var references; file stays tracked in git

- **`docs/integrations/mcp/MCP_INSPECTOR_QUICKSTART.md`** — replace the live key value
  with a `<YOUR_CONTEXT7_API_KEY>` placeholder

- **`.gitignore`** — add `docker/.env` entry

- **Git history rewrite** — run `git-filter-repo` on `emergent.memory` to scrub all
  committed secret values from history; force-push to remote; all active contributors
  must re-clone

- **`emergent.strategy` history** — separately scrub the SigNoz and Brave API keys
  committed to that repo's history

- **GitHub Secret Scanning** — enable org-wide across all 15 repositories

## Capabilities

### New Capabilities

- `secret-hygiene`: Convention, `.gitignore` rules, and developer guidance ensuring all
  secrets live only in `.env.local` files (already gitignored), never in committed env
  files. Covers which files are safe to commit, how to populate `.env.local` for local
  development, and how `docker/.env.local` overrides docker-compose defaults.

### Modified Capabilities

_(none — this is a security/ops remediation, not a behavior change)_

## Impact

- **Files modified in HEAD**: `docker/.env`, `.env`, `.vscode/mcp.json`,
  `opencode.jsonc`, `docs/integrations/mcp/MCP_INSPECTOR_QUICKSTART.md`, `.gitignore`
- **Git history rewritten**: `emergent.memory` main branch (force-push required) and
  `emergent.strategy` (separate operation)
- **Credentials to rotate**: Context7 API key, Langfuse secret key, Zitadel PAT + two
  RSA service-account keypairs + master key
- **No API, schema, or application behavior changes** — purely security/config
  remediation
- **No frontend (`emergent.memory.ui`) changes required**
