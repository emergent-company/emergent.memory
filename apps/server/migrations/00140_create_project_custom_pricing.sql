-- +goose Up
-- +goose StatementBegin
CREATE TABLE kb.project_custom_pricing (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    project_id UUID NOT NULL REFERENCES kb.projects(id) ON DELETE CASCADE,
    provider VARCHAR(50) NOT NULL,
    model VARCHAR(255) NOT NULL,
    text_input_price NUMERIC(12,8) NOT NULL DEFAULT 0,
    image_input_price NUMERIC(12,8) NOT NULL DEFAULT 0,
    video_input_price NUMERIC(12,8) NOT NULL DEFAULT 0,
    audio_input_price NUMERIC(12,8) NOT NULL DEFAULT 0,
    output_price NUMERIC(12,8) NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_project_custom_pricing UNIQUE (project_id, provider, model)
);

CREATE INDEX idx_project_custom_pricing_project_id ON kb.project_custom_pricing(project_id);
-- +goose StatementEnd

-- +goose Down
DROP TABLE IF EXISTS kb.project_custom_pricing;
