-- +goose Up
-- +goose StatementBegin
-- Rename tables
ALTER TABLE kb.graph_template_packs RENAME TO graph_schemas;
ALTER TABLE kb.project_template_packs RENAME TO project_schemas;
ALTER TABLE kb.template_pack_studio_sessions RENAME TO schema_studio_sessions;
ALTER TABLE kb.template_pack_studio_messages RENAME TO schema_studio_messages;
ALTER TABLE kb.project_object_type_registry RENAME TO project_object_schema_registry;
-- +goose StatementEnd

-- +goose StatementBegin
-- Rename columns
ALTER TABLE kb.project_schemas RENAME COLUMN template_pack_id TO schema_id;
ALTER TABLE kb.project_object_schema_registry RENAME COLUMN template_pack_id TO schema_id;
-- +goose StatementEnd

-- +goose StatementBegin
SET search_path TO kb;
-- Rename primary key indexes
ALTER INDEX "PK_5bdff6c04be4775e82f1cef130b" RENAME TO graph_schemas_pkey;
ALTER INDEX "PK_c3edf237839b7a0dd374437a670" RENAME TO project_schemas_pkey;
ALTER INDEX "PK_734eabf182ef87e9b747c864d71" RENAME TO project_object_schema_registry_pkey;
RESET search_path;
-- +goose StatementEnd

-- +goose StatementBegin
-- Rename FK constraints
ALTER TABLE kb.graph_schemas RENAME CONSTRAINT graph_template_packs_parent_version_id_fkey TO graph_schemas_parent_version_id_fkey;
ALTER TABLE kb.project_schemas RENAME CONSTRAINT "FK_359c704937c9f1857fd80898ef2" TO project_schemas_project_id_fkey;
ALTER TABLE kb.project_schemas RENAME CONSTRAINT "FK_440cc8aae6f630830193b703f54" TO project_schemas_schema_id_fkey;
ALTER TABLE kb.project_object_schema_registry RENAME CONSTRAINT "FK_b8a4633d03d7ce7bc67701f8efb" TO project_object_schema_registry_project_id_fkey;
ALTER TABLE kb.schema_studio_sessions RENAME CONSTRAINT template_pack_studio_sessions_pack_id_fkey TO schema_studio_sessions_schema_id_fkey;
ALTER TABLE kb.schema_studio_sessions RENAME CONSTRAINT template_pack_studio_sessions_project_id_fkey TO schema_studio_sessions_project_id_fkey;
ALTER TABLE kb.schema_studio_messages RENAME CONSTRAINT template_pack_studio_messages_session_id_fkey TO schema_studio_messages_session_id_fkey;
-- +goose StatementEnd

-- +goose StatementBegin
SET search_path TO kb;
-- Rename pkey indexes for studio tables
ALTER INDEX template_pack_studio_sessions_pkey RENAME TO schema_studio_sessions_pkey;
ALTER INDEX template_pack_studio_messages_pkey RENAME TO schema_studio_messages_pkey;
-- Rename other indexes
ALTER INDEX "IDX_graph_template_packs_draft" RENAME TO "IDX_graph_schemas_draft";
ALTER INDEX "IDX_graph_template_packs_parent_version_id" RENAME TO "IDX_graph_schemas_parent_version_id";
ALTER INDEX "IDX_template_pack_studio_sessions_pack_id" RENAME TO "IDX_schema_studio_sessions_schema_id";
ALTER INDEX "IDX_template_pack_studio_sessions_status" RENAME TO "IDX_schema_studio_sessions_status";
ALTER INDEX "IDX_template_pack_studio_sessions_user_id" RENAME TO "IDX_schema_studio_sessions_user_id";
ALTER INDEX "IDX_template_pack_studio_messages_session_id" RENAME TO "IDX_schema_studio_messages_session_id";
RESET search_path;
-- +goose StatementEnd

-- +goose StatementBegin
-- Rename RLS policies on project_schemas
ALTER POLICY project_template_packs_delete ON kb.project_schemas RENAME TO project_schemas_delete;
ALTER POLICY project_template_packs_insert ON kb.project_schemas RENAME TO project_schemas_insert;
ALTER POLICY project_template_packs_select ON kb.project_schemas RENAME TO project_schemas_select;
ALTER POLICY project_template_packs_update ON kb.project_schemas RENAME TO project_schemas_update;
-- Rename RLS policies on project_object_schema_registry
ALTER POLICY project_object_type_registry_delete ON kb.project_object_schema_registry RENAME TO project_object_schema_registry_delete;
ALTER POLICY project_object_type_registry_insert ON kb.project_object_schema_registry RENAME TO project_object_schema_registry_insert;
ALTER POLICY project_object_type_registry_select ON kb.project_object_schema_registry RENAME TO project_object_schema_registry_select;
ALTER POLICY project_object_type_registry_update ON kb.project_object_schema_registry RENAME TO project_object_schema_registry_update;
-- Rename RLS policies on schema_studio_sessions
ALTER POLICY template_pack_studio_sessions_delete ON kb.schema_studio_sessions RENAME TO schema_studio_sessions_delete;
ALTER POLICY template_pack_studio_sessions_insert ON kb.schema_studio_sessions RENAME TO schema_studio_sessions_insert;
ALTER POLICY template_pack_studio_sessions_select ON kb.schema_studio_sessions RENAME TO schema_studio_sessions_select;
ALTER POLICY template_pack_studio_sessions_update ON kb.schema_studio_sessions RENAME TO schema_studio_sessions_update;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
-- Reverse: rename RLS policies back
ALTER POLICY project_schemas_delete ON kb.project_schemas RENAME TO project_template_packs_delete;
ALTER POLICY project_schemas_insert ON kb.project_schemas RENAME TO project_template_packs_insert;
ALTER POLICY project_schemas_select ON kb.project_schemas RENAME TO project_template_packs_select;
ALTER POLICY project_schemas_update ON kb.project_schemas RENAME TO project_template_packs_update;
ALTER POLICY project_object_schema_registry_delete ON kb.project_object_schema_registry RENAME TO project_object_type_registry_delete;
ALTER POLICY project_object_schema_registry_insert ON kb.project_object_schema_registry RENAME TO project_object_type_registry_insert;
ALTER POLICY project_object_schema_registry_select ON kb.project_object_schema_registry RENAME TO project_object_type_registry_select;
ALTER POLICY project_object_schema_registry_update ON kb.project_object_schema_registry RENAME TO project_object_type_registry_update;
ALTER POLICY schema_studio_sessions_delete ON kb.schema_studio_sessions RENAME TO template_pack_studio_sessions_delete;
ALTER POLICY schema_studio_sessions_insert ON kb.schema_studio_sessions RENAME TO template_pack_studio_sessions_insert;
ALTER POLICY schema_studio_sessions_select ON kb.schema_studio_sessions RENAME TO template_pack_studio_sessions_select;
ALTER POLICY schema_studio_sessions_update ON kb.schema_studio_sessions RENAME TO template_pack_studio_sessions_update;
-- +goose StatementEnd

-- +goose StatementBegin
SET search_path TO kb;
ALTER INDEX "IDX_schema_studio_messages_session_id" RENAME TO "IDX_template_pack_studio_messages_session_id";
ALTER INDEX "IDX_schema_studio_sessions_schema_id" RENAME TO "IDX_template_pack_studio_sessions_pack_id";
ALTER INDEX "IDX_schema_studio_sessions_status" RENAME TO "IDX_template_pack_studio_sessions_status";
ALTER INDEX "IDX_schema_studio_sessions_user_id" RENAME TO "IDX_template_pack_studio_sessions_user_id";
ALTER INDEX "IDX_graph_schemas_draft" RENAME TO "IDX_graph_template_packs_draft";
ALTER INDEX "IDX_graph_schemas_parent_version_id" RENAME TO "IDX_graph_template_packs_parent_version_id";
ALTER INDEX schema_studio_sessions_pkey RENAME TO template_pack_studio_sessions_pkey;
ALTER INDEX schema_studio_messages_pkey RENAME TO template_pack_studio_messages_pkey;
ALTER INDEX graph_schemas_pkey RENAME TO "PK_5bdff6c04be4775e82f1cef130b";
ALTER INDEX project_schemas_pkey RENAME TO "PK_c3edf237839b7a0dd374437a670";
ALTER INDEX project_object_schema_registry_pkey RENAME TO "PK_734eabf182ef87e9b747c864d71";
RESET search_path;
-- +goose StatementEnd

-- +goose StatementBegin
-- Reverse: rename FK constraints back
ALTER TABLE kb.graph_schemas RENAME CONSTRAINT graph_schemas_parent_version_id_fkey TO graph_template_packs_parent_version_id_fkey;
ALTER TABLE kb.project_schemas RENAME CONSTRAINT project_schemas_project_id_fkey TO "FK_359c704937c9f1857fd80898ef2";
ALTER TABLE kb.project_schemas RENAME CONSTRAINT project_schemas_schema_id_fkey TO "FK_440cc8aae6f630830193b703f54";
ALTER TABLE kb.project_object_schema_registry RENAME CONSTRAINT project_object_schema_registry_project_id_fkey TO "FK_b8a4633d03d7ce7bc67701f8efb";
ALTER TABLE kb.schema_studio_sessions RENAME CONSTRAINT schema_studio_sessions_schema_id_fkey TO template_pack_studio_sessions_pack_id_fkey;
ALTER TABLE kb.schema_studio_sessions RENAME CONSTRAINT schema_studio_sessions_project_id_fkey TO template_pack_studio_sessions_project_id_fkey;
ALTER TABLE kb.schema_studio_messages RENAME CONSTRAINT schema_studio_messages_session_id_fkey TO template_pack_studio_messages_session_id_fkey;
-- +goose StatementEnd

-- +goose StatementBegin
-- Reverse: rename columns back
ALTER TABLE kb.project_schemas RENAME COLUMN schema_id TO template_pack_id;
ALTER TABLE kb.project_object_schema_registry RENAME COLUMN schema_id TO template_pack_id;
-- +goose StatementEnd

-- +goose StatementBegin
-- Reverse: rename tables back
ALTER TABLE kb.project_object_schema_registry RENAME TO project_object_type_registry;
ALTER TABLE kb.schema_studio_messages RENAME TO template_pack_studio_messages;
ALTER TABLE kb.schema_studio_sessions RENAME TO template_pack_studio_sessions;
ALTER TABLE kb.project_schemas RENAME TO project_template_packs;
ALTER TABLE kb.graph_schemas RENAME TO graph_template_packs;
-- +goose StatementEnd
