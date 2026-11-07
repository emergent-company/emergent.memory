-- +goose Up
-- +goose StatementBegin
-- Persist the required-property list a discovery run inferred for a type (#234).
--
-- The list was parsed and then dropped: parseTypeDiscoveryResponse hardcoded it
-- empty, and the merge that feeds the auto-created pack, the job's
-- discovered_types and finalize's buildSchema had nothing to read it from. It
-- now travels on the candidate like inferred_schema does.
--
-- Default '[]' rather than NULL: "no required properties" is a real answer, and
-- an absent list and an empty one mean the same thing to every consumer.
ALTER TABLE kb.discovery_type_candidates
    ADD COLUMN IF NOT EXISTS required_properties jsonb NOT NULL DEFAULT '[]'::jsonb;

COMMENT ON COLUMN kb.discovery_type_candidates.required_properties IS
    'Property names the model marked as required for this type; unioned (and filtered to surviving properties) when candidates merge.';
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE kb.discovery_type_candidates DROP COLUMN IF EXISTS required_properties;
-- +goose StatementEnd
