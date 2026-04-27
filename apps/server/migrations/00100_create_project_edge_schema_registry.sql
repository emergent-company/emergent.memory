-- +goose Up
-- Create the project_edge_schema_registry table for relationship (edge) type registrations.
-- This was added to the Go code but the migration was never written.
-- See: schema_tools.go executeCreateSchema (INSERT), executeGetSchema (SELECT), executeSchemaCompiledTypes (SELECT)

CREATE TABLE kb.project_edge_schema_registry (
    id uuid DEFAULT public.uuid_generate_v4() NOT NULL,
    project_id uuid NOT NULL,
    schema_id uuid,
    type_name text NOT NULL,
    type_schema jsonb,
    created_at timestamp with time zone DEFAULT now() NOT NULL
);

-- Primary key
ALTER TABLE ONLY kb.project_edge_schema_registry
    ADD CONSTRAINT project_edge_schema_registry_pkey PRIMARY KEY (id);

-- Foreign key to projects
ALTER TABLE ONLY kb.project_edge_schema_registry
    ADD CONSTRAINT project_edge_schema_registry_project_id_fkey
    FOREIGN KEY (project_id) REFERENCES kb.projects(id) ON DELETE CASCADE;

-- Foreign key to graph_schemas
ALTER TABLE ONLY kb.project_edge_schema_registry
    ADD CONSTRAINT project_edge_schema_registry_schema_id_fkey
    FOREIGN KEY (schema_id) REFERENCES kb.graph_schemas(id) ON DELETE SET NULL;

-- Index for project-scoped lookups
CREATE INDEX idx_project_edge_schema_registry_project_id
    ON kb.project_edge_schema_registry(project_id);

-- Index for schema-scoped lookups
CREATE INDEX idx_project_edge_schema_registry_schema_id
    ON kb.project_edge_schema_registry(schema_id);

-- Unique constraint: one type_name per project per schema
CREATE UNIQUE INDEX idx_project_edge_schema_registry_unique
    ON kb.project_edge_schema_registry(project_id, schema_id, type_name);

-- Row-Level Security
ALTER TABLE kb.project_edge_schema_registry ENABLE ROW LEVEL SECURITY;

CREATE POLICY project_edge_schema_registry_select ON kb.project_edge_schema_registry
    FOR SELECT USING (
        (COALESCE(current_setting('app.current_project_id'::text, true), ''::text) = ''::text)
        OR ((project_id)::text = current_setting('app.current_project_id'::text, true))
    );

CREATE POLICY project_edge_schema_registry_insert ON kb.project_edge_schema_registry
    FOR INSERT WITH CHECK (
        (COALESCE(current_setting('app.current_project_id'::text, true), ''::text) = ''::text)
        OR ((project_id)::text = current_setting('app.current_project_id'::text, true))
    );

CREATE POLICY project_edge_schema_registry_update ON kb.project_edge_schema_registry
    FOR UPDATE USING (
        (COALESCE(current_setting('app.current_project_id'::text, true), ''::text) = ''::text)
        OR ((project_id)::text = current_setting('app.current_project_id'::text, true))
    ) WITH CHECK (
        (COALESCE(current_setting('app.current_project_id'::text, true), ''::text) = ''::text)
        OR ((project_id)::text = current_setting('app.current_project_id'::text, true))
    );

CREATE POLICY project_edge_schema_registry_delete ON kb.project_edge_schema_registry
    FOR DELETE USING (
        (COALESCE(current_setting('app.current_project_id'::text, true), ''::text) = ''::text)
        OR ((project_id)::text = current_setting('app.current_project_id'::text, true))
    );

-- +goose Down
DROP TABLE IF EXISTS kb.project_edge_schema_registry CASCADE;
