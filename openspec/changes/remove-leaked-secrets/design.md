## Context

Several secrets are committed to the current HEAD of the public `emergent.memory`
repository and have been there since the initial commit (2026-02-06):

- **`docker/.env`**: Two Zitadel RSA-2048 service-account private keys, a PAT, and a
  master key. These are consumed by the local Zitadel Docker container on bootstrap;
  the Go server does not read them directly.
- **`.env`**: A Langfuse secret key (`sk-lf-4793a6ae...`) used by the server for LLM
  tracing. The Langfuse public key in the same file is semi-public by design.
- **`opencode.jsonc`**: A Context7 API key in an MCP server header (`CONTEXT7_API_KEY`).
  opencode supports env-var substitution via `"{env:VAR_NAME}"` syntax.
- **`.vscode/mcp.json`**: The same Context7 key as a CLI `--api-key` argument, plus a
  DaisyUI Blueprint commercial license key and personal email in an `env` block.
- **`docs/integrations/mcp/MCP_INSPECTOR_QUICKSTART.md`**: The live Context7 key in a
  copy-pasteable usage example.

GitHub Secret Scanning is disabled on all 15 org repositories, which is why these were
not caught automatically.

The `.gitignore` already correctly excludes `.env.local` and `**/.env.local`.
The gap is that `docker/.env` has no ignore entry, and the three tool config files
were never treated as secret-bearing.

## Goals / Non-Goals

**Goals:**

- Remove all live secret values from HEAD and scrub them from git history
- Establish `docker/.env` as a safe-defaults file that is gitignored and cannot be
  accidentally committed with real credentials
- Replace hardcoded secrets in tool configs (`opencode.jsonc`, `.vscode/mcp.json`) with
  env-var references so the files can safely remain tracked
- Replace the live key in docs with a placeholder
- Enable GitHub Secret Scanning org-wide as a detection safety net
- Rotate all affected credentials before or alongside the git history rewrite

**Non-Goals:**

- Migrating to a secrets manager (Infisical, Vault) — out of scope for this change
- Changing how the Go server or any application reads secrets at runtime
- Touching `emergent.memory.ui` — no secrets are committed there
- Fixing pre-existing LSP compile errors in unrelated packages

## Decisions

### Decision 1: Gitignore `docker/.env`, keep as safe-defaults template

**Choice:** Add `docker/.env` to `.gitignore`. Replace all real credential values with
clearly inert placeholders (e.g., `REPLACE_WITH_REAL_KEY`, `your-master-key-32-chars`).
Developers who need real Zitadel credentials create `docker/.env.local` which overrides
the defaults via the existing `**/.env.local` gitignore pattern.

**Alternative considered:** Remove `docker/.env` entirely and require all developers to
copy from `docker/.env.example`. Rejected because it breaks the zero-config docker-compose
experience for new contributors — the stack should start without any manual file setup.

**Alternative considered:** Replace with clearly fake but structurally valid RSA keys.
Rejected — generating fake RSA keys adds complexity without benefit; the Zitadel
container will fail to start with invalid keys and display a clear error, which is
acceptable since the stack won't be usable without real credentials anyway.

**Note on `docker-compose.dev.yml`:** The compose file does not use `env_file:` for
`docker/.env` — the Zitadel container reads these variables from the shell environment
or from Docker's `--env-file` flag passed by bootstrap scripts. The compose file at
root level already loads `.env` and `.env.local` via Taskfile. No compose file changes
are needed; the bootstrap scripts that call `--env-file docker/.env` will simply receive
placeholder values until a `docker/.env.local` override is present.

### Decision 2: Use `"{env:VAR_NAME}"` substitution in `opencode.jsonc`

**Choice:** Replace the literal `CONTEXT7_API_KEY` value with `"{env:CONTEXT7_API_KEY}"`.
This is opencode's documented env-var substitution syntax (confirmed in opencode docs
MCP servers page). Developers set `CONTEXT7_API_KEY` in their shell or `.env.local`.

**Alternative considered:** Move the entire context7 MCP entry to a gitignored
`opencode.local.jsonc`. opencode does not support a local override config file,
so this is not feasible.

### Decision 3: Keep `.vscode/mcp.json` tracked; use VS Code `${env:VAR}` for Context7, env block for DaisyUI

**Choice:** Replace the `--api-key ctx7sk-...` argument with `"${input:context7ApiKey}"`
and add an `inputs` array entry, OR use the env block pattern that VS Code MCP supports.
VS Code MCP servers support an `env` block for environment variable injection. Move the
`--api-key` to an `env` block using `${env:CONTEXT7_API_KEY}`. For the DaisyUI license,
replace the literal value with `${env:DAISYUI_LICENSE}` and `${env:DAISYUI_EMAIL}`.
Developers set these in their shell environment or in a `.env.local` that they source.

**Alternative considered:** Gitignore `.vscode/mcp.json` entirely. Rejected — the file
configures shared team tooling (postgres MCP, chrome-devtools, gh_grep, etc.). Removing
it from git would degrade the developer experience for the whole team.

### Decision 4: Full git history rewrite via `git-filter-repo`

**Choice:** Use `git-filter-repo --replace-text <replacements-file>` to rewrite all
commits in `emergent.memory` that contain the leaked secret values, replacing them with
`[REDACTED]` strings. Then force-push to `main`. Repeat for `emergent.strategy`.

**Alternative considered:** Forward-only fix (HEAD cleanup + credential rotation, leave
history dirty). Rejected by user — the repo is public and any attacker who already
cloned it has the history. Scrubbing history eliminates the risk for anyone who clones
after the rewrite.

**Coordination requirement:** All active contributors must re-clone after the force-push.
Any branch with the old history that is merged after the rewrite will re-introduce the
secrets. Communicate the force-push before executing it.

### Decision 5: Rotate credentials before the history rewrite

**Choice:** Rotate all affected credentials as the first step, before any git changes.
Even if the history rewrite is not yet complete, the rotated credentials are useless to
an attacker who already has them from git.

**Order of operations:**
1. Rotate Context7 API key → update `.env.local` with new value
2. Rotate Langfuse secret key → update `.env.local` with new value
3. Verify Zitadel credentials are not reused in production → rotate PAT + RSA keypairs
4. Redact HEAD files (the old values are now inert)
5. Run `git-filter-repo` history rewrite
6. Force-push

### Decision 6: Enable GitHub Secret Scanning via org settings (not per-repo)

**Choice:** Enable Secret Scanning at the organization level, which applies to all
current and future repositories. This is a one-time org settings change in
GitHub → Settings → Code security → Secret scanning. No per-repo configuration needed.

## Risks / Trade-offs

- **Force-push disrupts active branches** → Mitigate by communicating to all contributors
  before executing. Any in-flight PRs with the old history base must be rebased after
  the rewrite.

- **`docker-compose` stops working for existing clones** → After the force-push,
  `docker/.env` in the old history had real keys; in the new history it has placeholders.
  Developers with existing `docker/.env.local` are unaffected. Developers relying on the
  committed credentials will need to create `docker/.env.local` with their own values.

- **VS Code `${env:CONTEXT7_API_KEY}` may not resolve if env is not set** → The MCP
  server will fail to start with an auth error rather than silently, which is
  acceptable. Document the required env vars in `docker/.env.example` or AGENTS.md.

- **`git-filter-repo` cannot be undone on the remote** → Mitigate by creating a backup
  tag (`pre-secret-cleanup`) before the rewrite and keeping it locally until the rewrite
  is verified.

- **emergent.strategy is a separate repo** → Requires a separate `git-filter-repo` run
  and force-push. The leaked keys there (SigNoz, Brave) have already been removed from
  HEAD but remain in history.

## Migration Plan

1. **Out-of-band (no git changes yet):** Rotate all 5+ credentials. Update `.env.local`
   on all developer machines with new values.

2. **HEAD cleanup commit** (single commit, no --amend):
   - Replace secrets in `docker/.env` with placeholders
   - Add `docker/.env` to `.gitignore`
   - Remove `LANGFUSE_SECRET_KEY` from `.env`
   - Update `opencode.jsonc` → `"{env:CONTEXT7_API_KEY}"`
   - Update `.vscode/mcp.json` → env-var references
   - Update `docs/integrations/mcp/MCP_INSPECTOR_QUICKSTART.md` → placeholder

3. **Verify HEAD is clean:** `git log -p | grep ctx7sk` and similar for each key —
   should return no matches in the latest commit.

4. **Create local backup:** `git tag pre-secret-cleanup`

5. **Run `git-filter-repo` on `emergent.memory`:**
   ```
   git-filter-repo --replace-text replacements.txt --force
   ```
   Where `replacements.txt` maps each old secret value to `[REDACTED]`.

6. **Force-push:** `git push origin main --force`

7. **Notify contributors** to re-clone. Delete any stale remote branches that predate
   the rewrite.

8. **Repeat steps 4-7 for `emergent.strategy`** (SigNoz + Brave keys).

9. **Enable GitHub Secret Scanning** at the org level.

10. **Verify:** Clone fresh copy, run `git log --all -p | grep <old-key-fragments>` —
    confirm no matches.

## Open Questions

- **Are the Zitadel RSA keypairs reused in production?** If yes, the production Zitadel
  instance must be updated with new keypairs before the docker keys are changed.
  Verify before rotating.
- **Does the DaisyUI Blueprint license need to be replaced or just masked?** If the
  license is per-developer, each dev sets `DAISYUI_LICENSE` in their environment.
  If it is a shared team license that can be public, it can stay in the file.
- **Who has already cloned the public repo?** GitHub does not provide clone analytics.
  Assume the credentials are already known to any motivated attacker and treat rotation
  as mandatory regardless.
