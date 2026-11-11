-- +goose Up
-- Per-project flag: when true, a data-source sync runs object extraction after
-- each ingested item and each new version. It is separate from
-- `auto_extract_objects`, which governs uploaded/created documents: a project
-- may want extraction on uploads without paying for extraction on every synced
-- item (Gmail, Drive, web crawls).
--
-- Defaults to false on purpose. Before this flag every synced item was
-- extracted unconditionally; defaulting to the old behaviour would silently
-- keep spending tokens on sources, so existing projects opt in explicitly and
-- syncs keep chunking/embedding when it is off.
ALTER TABLE kb.projects ADD COLUMN auto_extract_source_syncs boolean NOT NULL DEFAULT false;

-- +goose Down
ALTER TABLE kb.projects DROP COLUMN auto_extract_source_syncs;
