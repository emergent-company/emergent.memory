-- +goose Up

-- Rename the session thread tables, dropping the retired ACP vocabulary.
-- Foreign keys reference the renamed tables by OID, so they follow automatically;
-- only column and index names are updated below.
ALTER TABLE kb.acp_sessions RENAME TO sessions;
ALTER TABLE kb.acp_run_events RENAME TO run_events;

ALTER INDEX IF EXISTS kb.idx_acp_sessions_project_id RENAME TO idx_sessions_project_id;
ALTER INDEX IF EXISTS kb.idx_acp_sessions_is_archived RENAME TO idx_sessions_is_archived;
ALTER INDEX IF EXISTS kb.idx_acp_run_events_run_id_created RENAME TO idx_run_events_run_id_created;

-- Rename the child FK columns on every table that links to a session.
ALTER TABLE kb.agent_runs RENAME COLUMN acp_session_id TO session_id;
ALTER TABLE kb.chat_conversations RENAME COLUMN acp_session_id TO session_id;
ALTER TABLE kb.agent_share_sessions RENAME COLUMN acp_session_id TO session_id;

ALTER INDEX IF EXISTS kb.idx_agent_runs_acp_session_id RENAME TO idx_agent_runs_session_id;
ALTER INDEX IF EXISTS kb.idx_chat_conversations_acp_session_id_unique RENAME TO idx_chat_conversations_session_id_unique;
ALTER INDEX IF EXISTS kb.idx_agent_share_sessions_acp_link RENAME TO idx_agent_share_sessions_link;

-- kb.session_todos.session_id already carries the correct column name; its FK
-- target follows the table rename automatically.

-- +goose Down

ALTER INDEX IF EXISTS kb.idx_agent_share_sessions_link RENAME TO idx_agent_share_sessions_acp_link;
ALTER INDEX IF EXISTS kb.idx_chat_conversations_session_id_unique RENAME TO idx_chat_conversations_acp_session_id_unique;
ALTER INDEX IF EXISTS kb.idx_agent_runs_session_id RENAME TO idx_agent_runs_acp_session_id;

ALTER TABLE kb.agent_share_sessions RENAME COLUMN session_id TO acp_session_id;
ALTER TABLE kb.chat_conversations RENAME COLUMN session_id TO acp_session_id;
ALTER TABLE kb.agent_runs RENAME COLUMN session_id TO acp_session_id;

ALTER INDEX IF EXISTS kb.idx_run_events_run_id_created RENAME TO idx_acp_run_events_run_id_created;
ALTER INDEX IF EXISTS kb.idx_sessions_is_archived RENAME TO idx_acp_sessions_is_archived;
ALTER INDEX IF EXISTS kb.idx_sessions_project_id RENAME TO idx_acp_sessions_project_id;

ALTER TABLE kb.run_events RENAME TO acp_run_events;
ALTER TABLE kb.sessions RENAME TO acp_sessions;
