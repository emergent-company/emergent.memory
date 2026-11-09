-- +goose Up
-- +goose StatementBegin
-- Atomic duplicate suppression for feedback intake (issue #472).
--
-- The previous in-memory check was a read-then-write: two concurrent identical
-- submissions could both observe "no duplicate" before either recorded its
-- result, so both filed an issue and both spent a rate-limit slot. The arbiter
-- is now the primary key on (owner_identity, dedupe_key): the claim is a single
-- INSERT ... ON CONFLICT DO UPDATE ... WHERE ... RETURNING, so exactly one
-- caller can hold the pair.
--
-- owner_identity, not identity: IDENTITY is a SQL keyword and the column also
-- makes the ownership explicit in a table another domain may read.
--
-- state:
--   pending — a caller is filing the issue right now. A row older than the
--             claim TTL (2 minutes) is treated as abandoned and may be taken
--             over, so a crashed process cannot block a retry forever.
--   filed   — the issue exists. issue_number/issue_url are the answer a repeat
--             submission within the dedupe window (10 minutes) receives. A row
--             older than the window may be taken over and re-filed.
--
-- A takeover rotates this row in place; claims do not accumulate. The claim
-- token lets a slow caller release or complete only the claim it actually owns,
-- so a takeover cannot be clobbered by the stale owner.
--
-- Schema: kb. The repo keeps durable product state in kb (projects, backup
-- schedules, email logs, MCP server config); core is reserved for identity and
-- authentication records (user_profiles, api_tokens, superadmins). A feedback
-- claim is product intake anchored to an identity and a project, and the
-- service already reads kb.projects, so kb is the right home.
CREATE TABLE kb.feedback_dedupe_claims (
    owner_identity text        NOT NULL,
    dedupe_key     text        NOT NULL,
    state          text        NOT NULL DEFAULT 'pending',
    claim_token    uuid        NOT NULL,
    issue_number   integer,
    issue_url      text,
    claimed_at     timestamptz NOT NULL,
    updated_at     timestamptz NOT NULL,
    CONSTRAINT feedback_dedupe_claims_pkey PRIMARY KEY (owner_identity, dedupe_key),
    CONSTRAINT feedback_dedupe_claims_state_check
        CHECK (state IN ('pending', 'filed')),
    CONSTRAINT feedback_dedupe_claims_filed_check
        CHECK (
            (state = 'pending' AND issue_number IS NULL AND issue_url IS NULL)
            OR
            (state = 'filed' AND issue_number IS NOT NULL AND issue_url IS NOT NULL)
        )
);

-- Supports the maintenance sweep (and any future janitor) that expires rows
-- older than the dedupe window.
CREATE INDEX feedback_dedupe_claims_claimed_at_idx
    ON kb.feedback_dedupe_claims (claimed_at);

COMMENT ON TABLE kb.feedback_dedupe_claims IS
    'One atomic claim per (submitter identity, submission fingerprint) for feedback duplicate suppression; see issue #472.';
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS kb.feedback_dedupe_claims;
-- +goose StatementEnd
