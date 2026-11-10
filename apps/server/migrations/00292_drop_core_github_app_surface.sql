-- +goose Up
-- +goose StatementBegin
-- Drop the old core.github_* App surface (#574, spec task 1.5).
--
-- 00282 introduced these tables for the review producer. #571 replaced that
-- path with kb.source_apps / kb.source_triggers / kb.source_events behind
-- POST /api/webhooks/source-apps/:webhookKey, so the App row model, its
-- installation bindings, ingress dedupe and per-head review claims have no
-- reader or writer. No data migration: the tracking issue records no
-- production rows.
--
-- Drop order mirrors the Down section of 00282_github_app_integration.sql
-- (children before parents). Indexes and constraints go with their tables.
DROP TABLE IF EXISTS core.github_pr_review_claims;
DROP TABLE IF EXISTS core.github_app_deliveries;
DROP TABLE IF EXISTS core.github_app_installations;
DROP TABLE IF EXISTS core.github_apps;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
-- Down migration not provided: this surface is permanently removed (#574),
-- matching 00089_remove_integrations_datasource_githubapp.sql. Recreating the
-- tables empty would restore a schema no code reads; 00282's Up holds the
-- definition if it is ever needed again.
-- +goose StatementEnd
