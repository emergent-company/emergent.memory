-- +goose Up
-- +goose StatementBegin
-- Flip the kb.graph_objects and kb.graph_relationships RLS policies from
-- fail-open to fail-closed (issue #32, step 1).
--
-- The baseline policy was:
--
--   COALESCE(current_setting('app.current_project_id', true), '') = ''
--   OR project_id::text = current_setting('app.current_project_id', true)
--
-- so an unset context matched every row. The policies below match only the
-- project named by the context; an unset or empty context makes the cast NULL
-- and therefore matches nothing.
--
-- This is inert while the connecting role is a superuser (superusers bypass
-- row security even on FORCE tables). It becomes effective once the server
-- connects as a non-owner, non-BYPASSRLS role.
--
-- Idempotent: DROP POLICY IF EXISTS before CREATE POLICY, matching the style
-- of the baseline and later RLS migrations.

DROP POLICY IF EXISTS graph_objects_select ON kb.graph_objects;
CREATE POLICY graph_objects_select ON kb.graph_objects FOR SELECT USING (
    project_id = NULLIF(current_setting('app.current_project_id', true), '')::uuid
);

DROP POLICY IF EXISTS graph_objects_insert ON kb.graph_objects;
CREATE POLICY graph_objects_insert ON kb.graph_objects FOR INSERT WITH CHECK (
    project_id = NULLIF(current_setting('app.current_project_id', true), '')::uuid
);

DROP POLICY IF EXISTS graph_objects_update ON kb.graph_objects;
CREATE POLICY graph_objects_update ON kb.graph_objects FOR UPDATE USING (
    project_id = NULLIF(current_setting('app.current_project_id', true), '')::uuid
) WITH CHECK (
    project_id = NULLIF(current_setting('app.current_project_id', true), '')::uuid
);

DROP POLICY IF EXISTS graph_objects_delete ON kb.graph_objects;
CREATE POLICY graph_objects_delete ON kb.graph_objects FOR DELETE USING (
    project_id = NULLIF(current_setting('app.current_project_id', true), '')::uuid
);

DROP POLICY IF EXISTS graph_relationships_select ON kb.graph_relationships;
CREATE POLICY graph_relationships_select ON kb.graph_relationships FOR SELECT USING (
    project_id = NULLIF(current_setting('app.current_project_id', true), '')::uuid
);

DROP POLICY IF EXISTS graph_relationships_insert ON kb.graph_relationships;
CREATE POLICY graph_relationships_insert ON kb.graph_relationships FOR INSERT WITH CHECK (
    project_id = NULLIF(current_setting('app.current_project_id', true), '')::uuid
);

DROP POLICY IF EXISTS graph_relationships_update ON kb.graph_relationships;
CREATE POLICY graph_relationships_update ON kb.graph_relationships FOR UPDATE USING (
    project_id = NULLIF(current_setting('app.current_project_id', true), '')::uuid
) WITH CHECK (
    project_id = NULLIF(current_setting('app.current_project_id', true), '')::uuid
);

DROP POLICY IF EXISTS graph_relationships_delete ON kb.graph_relationships;
CREATE POLICY graph_relationships_delete ON kb.graph_relationships FOR DELETE USING (
    project_id = NULLIF(current_setting('app.current_project_id', true), '')::uuid
);
-- +goose StatementEnd

-- +goose Down
-- Restore the baseline 00001 fail-open policies verbatim.
-- +goose StatementBegin
DROP POLICY IF EXISTS graph_objects_select ON kb.graph_objects;
CREATE POLICY graph_objects_select ON kb.graph_objects FOR SELECT USING (((COALESCE(current_setting('app.current_project_id'::text, true), ''::text) = ''::text) OR ((project_id)::text = current_setting('app.current_project_id'::text, true))));

DROP POLICY IF EXISTS graph_objects_insert ON kb.graph_objects;
CREATE POLICY graph_objects_insert ON kb.graph_objects FOR INSERT WITH CHECK (((COALESCE(current_setting('app.current_project_id'::text, true), ''::text) = ''::text) OR ((project_id)::text = current_setting('app.current_project_id'::text, true))));

DROP POLICY IF EXISTS graph_objects_update ON kb.graph_objects;
CREATE POLICY graph_objects_update ON kb.graph_objects FOR UPDATE USING (((COALESCE(current_setting('app.current_project_id'::text, true), ''::text) = ''::text) OR ((project_id)::text = current_setting('app.current_project_id'::text, true)))) WITH CHECK (((COALESCE(current_setting('app.current_project_id'::text, true), ''::text) = ''::text) OR ((project_id)::text = current_setting('app.current_project_id'::text, true))));

DROP POLICY IF EXISTS graph_objects_delete ON kb.graph_objects;
CREATE POLICY graph_objects_delete ON kb.graph_objects FOR DELETE USING (((COALESCE(current_setting('app.current_project_id'::text, true), ''::text) = ''::text) OR ((project_id)::text = current_setting('app.current_project_id'::text, true))));

DROP POLICY IF EXISTS graph_relationships_select ON kb.graph_relationships;
CREATE POLICY graph_relationships_select ON kb.graph_relationships FOR SELECT USING (((COALESCE(current_setting('app.current_project_id'::text, true), ''::text) = ''::text) OR ((project_id)::text = current_setting('app.current_project_id'::text, true))));

DROP POLICY IF EXISTS graph_relationships_insert ON kb.graph_relationships;
CREATE POLICY graph_relationships_insert ON kb.graph_relationships FOR INSERT WITH CHECK (((COALESCE(current_setting('app.current_project_id'::text, true), ''::text) = ''::text) OR ((project_id)::text = current_setting('app.current_project_id'::text, true))));

DROP POLICY IF EXISTS graph_relationships_update ON kb.graph_relationships;
CREATE POLICY graph_relationships_update ON kb.graph_relationships FOR UPDATE USING (((COALESCE(current_setting('app.current_project_id'::text, true), ''::text) = ''::text) OR ((project_id)::text = current_setting('app.current_project_id'::text, true)))) WITH CHECK (((COALESCE(current_setting('app.current_project_id'::text, true), ''::text) = ''::text) OR ((project_id)::text = current_setting('app.current_project_id'::text, true))));

DROP POLICY IF EXISTS graph_relationships_delete ON kb.graph_relationships;
CREATE POLICY graph_relationships_delete ON kb.graph_relationships FOR DELETE USING (((COALESCE(current_setting('app.current_project_id'::text, true), ''::text) = ''::text) OR ((project_id)::text = current_setting('app.current_project_id'::text, true))));
-- +goose StatementEnd
