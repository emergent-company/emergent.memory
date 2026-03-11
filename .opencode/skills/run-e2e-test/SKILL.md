---
name: run-e2e-test
description: Run the e2e test suite against mcj-emergent or another environment. Use when the user wants to run e2e tests, verify a feature end-to-end, or run a specific test by name.
license: MIT
metadata:
  author: openspec
  version: "1.0"
---

Run e2e tests using the `test` script in `/root/emergent.memory.e2e/`. All env vars,
server URLs, auth tokens, and API keys are already wired in `.env.mcj-emergent` —
no manual variable setup needed.

## Quick Reference

```bash
# Run ALL tests against mcj-emergent (default)
bash .opencode/skills/run-e2e-test/scripts/run.sh

# Run a specific test against mcj-emergent
bash .opencode/skills/run-e2e-test/scripts/run.sh TestAINewsBlueprint_InstallAndRun

# Run against a different env (localhost, etc.)
bash .opencode/skills/run-e2e-test/scripts/run.sh localhost TestCLIInstalled_Version
```

## Available Test Names

| Test | What it covers |
|------|---------------|
| `TestAINewsBlueprint_InstallAndRun` | Full AI news pipeline: install blueprint, run all skill agents, classifier, digest-writer. Verifies AIDigest + `included_in` relationships. ~3-5 min. |
| `TestCLIInstalled_*` | CLI install and basic command tests |
| `TestProduction_*` | Production smoke tests (skipped unless `MEMORY_PROD_TEST_TOKEN` is set) |

## How It Works

The `test` script in `/root/emergent.memory.e2e/test`:
1. Loads `.env` as base config
2. Merges `.env.mcj-emergent` (or whichever overlay is named) on top
3. Shell-exported variables always win over file values
4. Passes everything to `go test` with `-v`

The `run.sh` wrapper:
- Defaults env to `mcj-emergent`
- Detects `TestAINews*` / `TestBlueprint*` filters and adds `-timeout 60m` automatically

## Env Overlays

| File | Target |
|------|--------|
| `.env` | Base defaults (Docker Compose stack) |
| `.env.mcj-emergent` | mcj-emergent test server (auth + token pre-configured) |
| `.env.localhost` | Local server at `http://localhost:3012` |

## Guardrails

- Do NOT manually export `MEMORY_TEST_SERVER`, `MEMORY_TEST_TOKEN`, etc. — the overlay files handle this
- The AI news blueprint test creates and deletes ephemeral projects — it is safe to re-run at any time
- If a test SKIPs, check the skip message — it usually means a required env var or API key is missing
- Logs for each run are saved to `/root/emergent.memory.e2e/logs/<timestamp>-<TestName>/run.log`
