<!-- Baseline failures (pre-existing, not introduced by this change):
- apps/server/pkg/tracing/tracer_test.go: compile errors — tracing.StartLinked and tracing.RecordErrorWithType undefined
- apps/server/domain/provider/catalog_test.go: compile errors — staticModels undefined
- These are pre-existing and unrelated to this change; go build ./... is clean
-->

## 1. Credential Rotation (do this first, out-of-band)

- [ ] 1.1 Verify whether the Zitadel RSA keypairs and PAT in `docker/.env` are reused in any production or staging Zitadel instance
- [ ] 1.2 Rotate the Context7 API key at https://context7.com — get a new `ctx7sk-...` value
- [ ] 1.3 Rotate the Langfuse secret key at `langfuse.dev.emergent-company.ai` — get a new `sk-lf-...` value
- [ ] 1.4 Rotate the Zitadel PAT (`nPn3djEyet1NlWGpE2WCC...`) in the local Zitadel admin console
- [ ] 1.5 Rotate the two Zitadel RSA service-account keypairs (`keyId: 348011771132379139` and `keyId: 348011770712948739`) in the local Zitadel admin console
- [ ] 1.6 Update `.env.local` (root) with the new Context7 API key and Langfuse secret key
- [ ] 1.7 Update `docker/.env.local` with the new Zitadel PAT, RSA keypairs, and master key

## 2. HEAD Cleanup — `.gitignore` and `docker/.env`

- [x] 2.1 Add `docker/.env` to `.gitignore` (under the "Environment files with secrets" section)
- [x] 2.2 In `docker/.env`, replace the `ZITADEL_API_KEY` JSON blob (full RSA private key) with `REPLACE_WITH_REAL_ZITADEL_API_KEY_JSON`
- [x] 2.3 In `docker/.env`, replace the `ZITADEL_CLIENT_JWT` JSON blob (full RSA private key) with `REPLACE_WITH_REAL_ZITADEL_CLIENT_JWT_JSON`
- [x] 2.4 In `docker/.env`, replace `ZITADEL_PAT` value with `REPLACE_WITH_REAL_ZITADEL_PAT`
- [x] 2.5 In `docker/.env`, replace `ZITADEL_MASTERKEY` value with `REPLACE_WITH_32_CHAR_MASTER_KEY_HERE`
- [x] 2.6 In `docker/.env`, replace `ZITADEL_FIRSTINSTANCE_ORG_HUMAN_PASSWORD` value with `REPLACE_WITH_REAL_PASSWORD`
- [x] 2.7 In `docker/.env`, replace `ZITADEL_DATABASE_POSTGRES_USER_PASSWORD` value with `REPLACE_WITH_REAL_DB_PASSWORD`
- [x] 2.8 Verify `docker/.env` still has all required variable names so `docker-compose` does not fail with missing key errors

## 3. HEAD Cleanup — `.env` (root)

- [x] 3.1 Remove the `LANGFUSE_SECRET_KEY=sk-lf-4793a6ae...` line from `.env`
- [x] 3.2 Add a comment above `LANGFUSE_PUBLIC_KEY` noting that the secret key must be set in `.env.local`
- [x] 3.3 Verify `.env` still loads correctly (no broken references)

## 4. HEAD Cleanup — `opencode.jsonc`

- [x] 4.1 Replace the literal `"[REDACTED_CONTEXT7_KEY]"` value in the `context7` MCP `headers` block with `"{env:CONTEXT7_API_KEY}"`
- [x] 4.2 Start opencode locally and confirm the context7 MCP server connects using the env var (or fails with an auth error rather than a missing-key panic)

## 5. HEAD Cleanup — `.vscode/mcp.json`

- [x] 5.1 Replace the `--api-key ctx7sk-...` argument in the `context7` server `args` array — move it to an `env` block: `"env": { "CONTEXT7_API_KEY": "${env:CONTEXT7_API_KEY}" }` and remove the `--api-key` arg
- [x] 5.2 Replace `"LICENSE": "[REDACTED_DAISYUI_LICENSE]"` in the `daisyui-blueprint` env block with `"${env:DAISYUI_LICENSE}"`
- [x] 5.3 Replace `"EMAIL": "maciej@kucharz.net"` in the `daisyui-blueprint` env block with `"${env:DAISYUI_EMAIL}"`
- [x] 5.4 Confirm in VS Code that the context7 MCP server entry is valid (no syntax errors in the JSON)

## 6. HEAD Cleanup — Documentation

- [x] 6.1 In `docs/integrations/mcp/MCP_INSPECTOR_QUICKSTART.md`, replace `--api-key [REDACTED_CONTEXT7_KEY]` (line 71) with `--api-key <YOUR_CONTEXT7_API_KEY>`
- [x] 6.2 In the same file, replace the full key in the table entry (line 264) with `<YOUR_CONTEXT7_API_KEY>`
- [x] 6.3 Search the entire repo for any remaining occurrences of `ctx7sk-77ad3f0a` and `sk-lf-4793a6ae`: `git grep -r "ctx7sk-77ad3f0a\|sk-lf-4793a6ae"` — fix any that remain

## 7. Commit HEAD Changes

- [x] 7.1 Stage all modified files: `docker/.env`, `.env`, `.gitignore`, `opencode.jsonc`, `.vscode/mcp.json`, `docs/integrations/mcp/MCP_INSPECTOR_QUICKSTART.md`
- [x] 7.2 Create commit: "remove committed secrets and replace with env-var references"
- [x] 7.3 Verify with `git show HEAD` that no secret values appear in the diff

## 8. Git History Rewrite — `emergent.memory`

- [x] 8.1 Install `git-filter-repo` if not present: `pip install git-filter-repo` or `brew install git-filter-repo`
- [x] 8.2 Create a `replacements.txt` file mapping each old secret value to `[REDACTED]`:
  - The full `ZITADEL_API_KEY` RSA JSON blob (old value before rotation)
  - The full `ZITADEL_CLIENT_JWT` RSA JSON blob (old value before rotation)
  - The old Zitadel PAT value `nPn3djEyet1NlWGpE2WCC...`
  - The old Context7 key `[REDACTED_CONTEXT7_KEY]`
  - The old Langfuse secret key `sk-lf-4793a6ae...`
  - The DaisyUI license key `[REDACTED_DAISYUI_LICENSE]`
  - Two Vertex AI Express API keys (`AQ.Ab8RN6IPwK...`, `AQ.Ab8RN6KJRRd...`) — found via GitHub Secret Scanning alerts, removed in round 4
- [x] 8.3 Create a local backup tag: `git tag pre-secret-cleanup`
- [x] 8.4 Run the rewrite: `git filter-repo --replace-text replacements.txt --force` (4 passes total)
- [x] 8.5 Verify no secrets remain in any commit — confirmed zero matches for all known secret patterns
- [x] 8.6 Notify all active contributors that a force-push is imminent and they must re-clone
- [x] 8.7 Force-push: `git push origin --force --all && git push origin --force --tags`
- [x] 8.8 Delete the local `replacements.txt` file (it contains the old secret values)

## 9. Git History Rewrite — `emergent.strategy`

- [x] 9.1 Clone or switch to the `emergent.strategy` repository
- [x] 9.2 Create a `replacements.txt` for that repo mapping the old SigNoz key, old Brave API key, 4 Google API keys, LangSmith key, GitHub OAuth token, Vertex AI Express key, and Google service account private key to `[REDACTED]`
- [x] 9.3 Create backup tag: `git tag pre-secret-cleanup`
- [x] 9.4 Run `git filter-repo --replace-text replacements.txt --force` (2 passes total)
- [x] 9.5 Verify: confirmed zero matches for all known secret patterns
- [x] 9.6 Force-push: `git push origin main --force`
- [x] 9.7 Delete the local `replacements.txt` file

## 10. GitHub Secret Scanning

- [x] 10.1 Navigate to GitHub org settings → Security → Code security → Secret scanning
- [x] 10.2 Enable "Secret scanning" at the organization level — enabled on all 11 public repos via API (4 private repos require GHAS license)
- [x] 10.3 Enable "Push protection" to block future pushes containing secrets — enabled on same 11 repos
- [x] 10.4 Verified setting applies to: `emergent.memory`, `emergent.memory.ui`, `emergent.memory.e2e`, and 8 other public org repos
- [x] 10.5 Checked Secret Scanning alerts — found and remediated 2 Vertex AI Express keys (emergent.memory) and 8 additional secrets (emergent.strategy, including Google API keys, LangSmith key, GitHub OAuth token, GCP service account credentials)

## 11. Verification

- [x] 11.1 Do a fresh clone of `emergent.memory` and run `git log --all -p | grep -E "ctx7sk-77ad3f0a|sk-lf-4793a6ae|nPn3djEyet1N|MIIEpAIBAAKCAQEA"` — confirm zero matches
- [x] 11.2 Confirm `docker/.env` in the fresh clone contains only placeholder values
- [x] 11.3 Confirm `.env` in the fresh clone does not contain `LANGFUSE_SECRET_KEY`
- [x] 11.4 Confirm `opencode.jsonc` shows `"{env:CONTEXT7_API_KEY}"` not the literal key
- [x] 11.5 Confirm `.vscode/mcp.json` shows no literal API keys
- [x] 11.6 Confirm `docs/integrations/mcp/MCP_INSPECTOR_QUICKSTART.md` shows only `<YOUR_CONTEXT7_API_KEY>` placeholders
- [ ] 11.7 Test that opencode starts and the context7 MCP server is usable with `CONTEXT7_API_KEY` set in the environment (manual — do this once credential rotation is complete)
