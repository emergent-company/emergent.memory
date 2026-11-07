-- +goose Up
-- The non-null-credential CHECK must be added here, not inside 00266's CREATE
-- TABLE IF NOT EXISTS: a database that already ran 00266 skips the CREATE TABLE
-- (IF NOT EXISTS is a no-op) and never acquires the constraint. Adding it
-- explicitly (NOT VALID, then VALIDATE — the 00266/00267 lock pattern) makes
-- those databases converge with a fresh install.
--
-- The constraint itself: only the synthetic 'project' wildcard may carry a NULL
-- credential_id. A ('session', NULL) or ('api_key', NULL) row would read back
-- as a hidden wildcard that authorises every session / every api_key — exactly
-- the impersonation window this table closes. Concrete kinds must name a
-- credential (pairing the id with the kind, like 00257's stamp check).

ALTER TABLE kb.source_grants
    ADD CONSTRAINT source_grants_credential_id_pairs_with_kind
    CHECK (credential_kind = 'project' OR credential_id IS NOT NULL) NOT VALID;

ALTER TABLE kb.source_grants VALIDATE CONSTRAINT source_grants_credential_id_pairs_with_kind;

-- +goose Down
ALTER TABLE kb.source_grants DROP CONSTRAINT source_grants_credential_id_pairs_with_kind;
