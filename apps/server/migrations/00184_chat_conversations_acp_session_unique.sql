-- +goose Up

-- Enforce the 1:1 session↔conversation invariant at the database level.
--
-- kb.chat_conversations.acp_session_id links a conversation to its backing
-- kb.acp_sessions row. The relationship is 1:1 by code convention only:
-- EnsureConversationACPSession always creates a fresh session per conversation
-- and never reuses one (the only writer of this column). The schema permits
-- N:1, so a future reuse path would make the ownership predicate
-- (session → acp_sessions → chat_conversations.acp_session_id) ambiguous and
-- could leak messages between conversations. A UNIQUE index makes the invariant
-- the database's job rather than the caller's.
--
-- Safety: a plain CREATE UNIQUE INDEX fails if duplicates already exist. Under
-- the current data model duplicates cannot exist — every session is created
-- fresh and linked to exactly one conversation (step 3 of
-- EnsureConversationACPSession), and the backups restorer nulls this column
-- rather than assigning it. A violation here would therefore indicate an
-- out-of-band write or an unguarded reuse path that should surface loudly.
--
-- acp_session_id is nullable (non-agent and pre-existing conversations have no
-- session). Postgres unique indexes treat NULLs as distinct, so multiple rows
-- with acp_session_id IS NULL remain permitted — the correct behaviour here.
--
-- The prior non-unique partial index (idx_chat_conversations_acp_session_id,
-- added by 00115) is dropped: the unique index below subsumes it for lookups.
DROP INDEX IF EXISTS kb.idx_chat_conversations_acp_session_id;

CREATE UNIQUE INDEX idx_chat_conversations_acp_session_id_unique
    ON kb.chat_conversations (acp_session_id);

-- +goose Down

DROP INDEX IF EXISTS kb.idx_chat_conversations_acp_session_id_unique;

CREATE INDEX idx_chat_conversations_acp_session_id
    ON kb.chat_conversations (acp_session_id)
    WHERE acp_session_id IS NOT NULL;
