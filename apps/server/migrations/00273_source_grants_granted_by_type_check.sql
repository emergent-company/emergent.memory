-- +goose Up
-- Enforce the granting-actor vocabulary on kb.source_grants (issue #277).
--
-- granted_by_type is text NOT NULL DEFAULT 'system' with no CHECK (00266), but
-- the code has exactly three values — graph.ActorUser = "user",
-- graph.ActorAgent = "agent", graph.ActorSystem = "system" — and the audit read
-- relies on them being the only values: a creator grant is ('user',
-- <user_profiles.id>) and a 00266 backfill row is ('system', NULL). A fourth
-- spelling ('User', 'service', '') would be silently missed or mis-bucketed by
-- any query that groups or filters on granted_by_type, which is precisely the
-- property this table exists to provide ("auditing is a query, with the
-- granting actor and time").
--
-- 'agent' stays in the domain: a grant minted on an agent's behalf would record
-- it, and the enum's full domain is user|agent|system — restricting the CHECK to
-- the two values reachable today would make the constraint understate the
-- model's vocabulary. This is defense-in-depth, not an exploitable gap: every
-- writer today sets one of the constants.
--
-- ADD NOT VALID then VALIDATE, matching the 00266/00267 (and 00268) lock
-- pattern: the ACCESS EXCLUSIVE lock from ADD CONSTRAINT is not held for the
-- scan. The table is small and newly added, so a single migration is enough.

ALTER TABLE kb.source_grants
    ADD CONSTRAINT source_grants_granted_by_type_check
    CHECK (granted_by_type IN ('user', 'agent', 'system')) NOT VALID;

ALTER TABLE kb.source_grants VALIDATE CONSTRAINT source_grants_granted_by_type_check;

-- +goose Down
ALTER TABLE kb.source_grants DROP CONSTRAINT source_grants_granted_by_type_check;
