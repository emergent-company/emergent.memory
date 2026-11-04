-- +goose Up
-- +goose StatementBegin
-- Stop storing a sandbox git token in env_vars (issue #1844, part b).
--
-- kb.agent_definitions.sandbox_config previously held a literal GH_TOKEN (or
-- GITHUB_TOKEN) inside its env_vars map. That put a plaintext credential both in
-- the database and in every sandbox container's environment (env_vars is injected
-- verbatim). This migration relocates the effective literal out of env_vars into a
-- top-level `git_token` field that the server resolves only at clone time, so the
-- token no longer lands in the container environment. The literal value is
-- preserved — it is never destroyed — and stays resolvable via the documented
-- legacy-literal precedence until the operator rotates it to a server-side secret
-- referenced by `git_token_secret_ref` (#1844, external rotation action).
--
-- Precedence GH_TOKEN > GITHUB_TOKEN mirrors ResolveGitToken: only the preferred
-- key is removed from env_vars, so a row that held BOTH keys keeps the
-- non-preferred value in env_vars and loses no data (the down migration restores
-- the preferred key only).
UPDATE kb.agent_definitions
SET sandbox_config = sandbox_config
    || jsonb_build_object(
        'git_token',
        COALESCE(
            sandbox_config #> '{env_vars,GH_TOKEN}',
            sandbox_config #> '{env_vars,GITHUB_TOKEN}'
        )
    )
    || jsonb_build_object(
        'env_vars',
        COALESCE(sandbox_config #> '{env_vars}', '{}'::jsonb)
        - CASE
            WHEN (sandbox_config #> '{env_vars,GH_TOKEN}') IS NOT NULL THEN 'GH_TOKEN'
            ELSE 'GITHUB_TOKEN'
          END
    )
WHERE (sandbox_config #> '{env_vars,GH_TOKEN}') IS NOT NULL
   OR (sandbox_config #> '{env_vars,GITHUB_TOKEN}') IS NOT NULL;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
-- Restore the relocated literal git token back into env_vars.GH_TOKEN and drop the
-- top-level git_token field, returning to the pre-migration shape. A row that
-- already carried a non-preferred GITHUB_TOKEN in env_vars (kept by the up
-- migration) is unaffected: only GH_TOKEN is written here.
UPDATE kb.agent_definitions
SET sandbox_config = (
        sandbox_config
        || jsonb_build_object(
            'env_vars',
            COALESCE(sandbox_config #> '{env_vars}', '{}'::jsonb)
            || jsonb_build_object('GH_TOKEN', sandbox_config #> '{git_token}')
        )
    ) - 'git_token'
WHERE sandbox_config ? 'git_token';
-- +goose StatementEnd
