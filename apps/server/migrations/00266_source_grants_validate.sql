-- +goose Up
-- +goose StatementBegin
-- Validate the relaxed owner-source constraint added NOT VALID in 00265.
--
-- Splitting this out matches 00257/00258: VALIDATE takes only SHARE UPDATE
-- EXCLUSIVE and scans while the table stays fully readable and writable, rather
-- than holding the ACCESS EXCLUSIVE lock the ADD CONSTRAINT took for the whole
-- scan.
ALTER TABLE kb.graph_objects VALIDATE CONSTRAINT graph_objects_owner_source_requires_source_class;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
-- Validation is not reversible (and does not need to be): 00265 drops the
-- constraint itself.
SELECT 1;
-- +goose StatementEnd
