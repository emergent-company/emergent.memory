-- +goose Up
-- +goose StatementBegin
-- Validate the write-attribution constraints added NOT VALID in 00257.
--
-- Splitting them out is deliberate: ADD CONSTRAINT ... CHECK NOT VALID takes an
-- ACCESS EXCLUSIVE lock but does not scan, so it commits immediately; VALIDATE
-- then takes only SHARE UPDATE EXCLUSIVE and scans while the table stays fully
-- readable and writable. Doing both in one transaction would hold the exclusive
-- lock for the whole scan of the busiest table in the schema.

ALTER TABLE kb.graph_objects VALIDATE CONSTRAINT graph_objects_credential_kind_check;
ALTER TABLE kb.graph_objects VALIDATE CONSTRAINT graph_objects_data_class_check;
ALTER TABLE kb.graph_objects VALIDATE CONSTRAINT graph_objects_owner_source_requires_source_class;
ALTER TABLE kb.graph_objects VALIDATE CONSTRAINT graph_objects_credential_id_pairs_with_kind;
ALTER TABLE kb.graph_objects VALIDATE CONSTRAINT graph_objects_owner_source_id_fkey;
ALTER TABLE kb.graph_relationships VALIDATE CONSTRAINT graph_relationships_credential_kind_check;
ALTER TABLE kb.graph_relationships VALIDATE CONSTRAINT graph_relationships_credential_id_pairs_with_kind;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
-- Validation is not reversible (and does not need to be): 00257 drops the
-- constraints themselves.
SELECT 1;
-- +goose StatementEnd
