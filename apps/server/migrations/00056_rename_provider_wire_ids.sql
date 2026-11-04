-- +goose Up
-- Rename provider wire IDs to match models.dev canonical provider slugs.
-- "google-ai"  → "google"         (Google AI / Gemini API)
-- "vertex-ai"  → "google-vertex"  (Google Cloud Vertex AI)

UPDATE kb.org_provider_configs
    SET provider = 'google'        WHERE provider = 'google-ai';
UPDATE kb.org_provider_configs
    SET provider = 'google-vertex' WHERE provider = 'vertex-ai';

UPDATE kb.project_provider_configs
    SET provider = 'google'        WHERE provider = 'google-ai';
UPDATE kb.project_provider_configs
    SET provider = 'google-vertex' WHERE provider = 'vertex-ai';

UPDATE kb.provider_supported_models
    SET provider = 'google'        WHERE provider = 'google-ai';
UPDATE kb.provider_supported_models
    SET provider = 'google-vertex' WHERE provider = 'vertex-ai';

UPDATE kb.llm_usage_events
    SET provider = 'google'        WHERE provider = 'google-ai';
UPDATE kb.llm_usage_events
    SET provider = 'google-vertex' WHERE provider = 'vertex-ai';

UPDATE kb.provider_pricing
    SET provider = 'google'        WHERE provider = 'google-ai';
UPDATE kb.provider_pricing
    SET provider = 'google-vertex' WHERE provider = 'vertex-ai';

UPDATE kb.organization_custom_pricing
    SET provider = 'google'        WHERE provider = 'google-ai';
UPDATE kb.organization_custom_pricing
    SET provider = 'google-vertex' WHERE provider = 'vertex-ai';

-- +goose Down
UPDATE kb.org_provider_configs
    SET provider = 'google-ai'    WHERE provider = 'google';
UPDATE kb.org_provider_configs
    SET provider = 'vertex-ai'    WHERE provider = 'google-vertex';

UPDATE kb.project_provider_configs
    SET provider = 'google-ai'    WHERE provider = 'google';
UPDATE kb.project_provider_configs
    SET provider = 'vertex-ai'    WHERE provider = 'google-vertex';

UPDATE kb.provider_supported_models
    SET provider = 'google-ai'    WHERE provider = 'google';
UPDATE kb.provider_supported_models
    SET provider = 'vertex-ai'    WHERE provider = 'google-vertex';

UPDATE kb.llm_usage_events
    SET provider = 'google-ai'    WHERE provider = 'google';
UPDATE kb.llm_usage_events
    SET provider = 'vertex-ai'    WHERE provider = 'google-vertex';

UPDATE kb.provider_pricing
    SET provider = 'google-ai'    WHERE provider = 'google';
UPDATE kb.provider_pricing
    SET provider = 'vertex-ai'    WHERE provider = 'google-vertex';

UPDATE kb.organization_custom_pricing
    SET provider = 'google-ai'    WHERE provider = 'google';
UPDATE kb.organization_custom_pricing
    SET provider = 'vertex-ai'    WHERE provider = 'google-vertex';
