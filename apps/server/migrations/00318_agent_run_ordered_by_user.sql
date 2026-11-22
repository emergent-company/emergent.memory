-- +goose Up
-- +goose StatementBegin

-- Who ordered an agent run. When a person starts a run, the run records that
-- person here: the agent acts on their order, so a graph write the run makes can
-- name the human behind it instead of only the agent that performed it (#1076
-- follow-up, object provenance v2).
--
-- NULL is the honest answer for a run nobody ordered — a GitHub source trigger, a
-- scheduled job, an extraction or indexing pass — and for every row written
-- before this column existed. A reader must then claim no orderer rather than
-- infer one from trigger_metadata, which a model can set.
--
-- A delegated run (an agent spawning another agent) carries the orderer of the
-- work it belongs to, and a resume by a principal that is not a person keeps the
-- orderer it resumed: attribution is never erased by an agent or a share link,
-- and an agent is never recorded as an orderer itself.
--
-- No foreign key: user identities live in the identity provider (Zitadel), not in
-- this database, which is also why kb.chat_conversations.owner_user_id and the
-- graph write stamp's actor_id carry bare UUIDs.
ALTER TABLE kb.agent_runs
    ADD COLUMN IF NOT EXISTS ordered_by_user_id UUID;

COMMENT ON COLUMN kb.agent_runs.ordered_by_user_id IS
    'Authenticated user who ordered this run; NULL when no person did.';

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

ALTER TABLE kb.agent_runs DROP COLUMN IF EXISTS ordered_by_user_id;

-- +goose StatementEnd
