-- +goose Up
-- Org-level provider config and model config are deprecated.
-- All configuration is now project-level only.
-- Tables are retained for data preservation but are no longer used by the server.

COMMENT ON TABLE kb.org_provider_configs IS 'DEPRECATED: org-level provider credentials. All provider config is now per-project via kb.project_provider_configs.';
COMMENT ON TABLE kb.org_model_config IS 'DEPRECATED: org-level model config. All model config is now per-project via kb.project_model_config.';

-- +goose Down
COMMENT ON TABLE kb.org_provider_configs IS NULL;
COMMENT ON TABLE kb.org_model_config IS NULL;
