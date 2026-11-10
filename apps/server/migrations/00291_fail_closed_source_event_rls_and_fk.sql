-- +goose Up
-- +goose StatementBegin
-- Make the three source-ingress tables fail closed under RLS and stop an org
-- App delete from cascading into project sources (issue #565).
--
-- Version 00291: 00290 is taken by 00290_unbind_non_oauth_sources.sql (#569),
-- which is ahead of this change in review.
--
-- 00289 created kb.source_triggers, kb.source_events and kb.source_event_claims
-- with the baseline fail-open policy 00281 replaced for the graph tables:
--
--   COALESCE(current_setting('app.current_project_id', true), '') = ''
--   OR project_id::text = current_setting('app.current_project_id', true)
--
-- so an unset context matched every row. The policies below match only the
-- project named by the context; an unset or empty context makes the cast NULL
-- and therefore matches nothing (and a GUC-less insert is refused by WITH
-- CHECK).
--
-- The session-less webhook ingress is the writer that must supply the
-- context: after it resolves the source it sets app.current_project_id from
-- kb.sources.project_id inside the transaction that touches these tables
-- (domain/sources store_source_apps.go, set_config(..., is_local = true)).
-- kb.sources itself stays on its baseline fail-open policy because the ingress
-- must read it before a project is known.
--
-- Idempotent: DROP POLICY IF EXISTS before CREATE POLICY, matching 00281.

DROP POLICY IF EXISTS source_triggers_all ON kb.source_triggers;
CREATE POLICY source_triggers_all ON kb.source_triggers FOR ALL USING (
    project_id = NULLIF(current_setting('app.current_project_id', true), '')::uuid
) WITH CHECK (
    project_id = NULLIF(current_setting('app.current_project_id', true), '')::uuid
);

DROP POLICY IF EXISTS source_events_all ON kb.source_events;
CREATE POLICY source_events_all ON kb.source_events FOR ALL USING (
    project_id = NULLIF(current_setting('app.current_project_id', true), '')::uuid
) WITH CHECK (
    project_id = NULLIF(current_setting('app.current_project_id', true), '')::uuid
);

DROP POLICY IF EXISTS source_event_claims_all ON kb.source_event_claims;
CREATE POLICY source_event_claims_all ON kb.source_event_claims FOR ALL USING (
    project_id = NULLIF(current_setting('app.current_project_id', true), '')::uuid
) WITH CHECK (
    project_id = NULLIF(current_setting('app.current_project_id', true), '')::uuid
);

-- kb.source_apps is org-scoped; kb.sources is project-scoped. 00289 created
-- kb.sources.app_id with ON DELETE CASCADE, so deleting one App credential
-- destroyed every project source bound to it (and, by cascade, its triggers,
-- events, claims and graph provenance). Deleting an org App must not silently
-- delete project data.
--
-- The constraint name below is the one PostgreSQL generated for 00289's inline
-- column reference; it was read back from pg_constraint rather than guessed.
-- The service refuses first with an error that names the bound sources
-- (Repository.DeleteSourceApp); RESTRICT is the database-level backstop for a
-- concurrent bind and for any direct SQL delete.
ALTER TABLE kb.sources DROP CONSTRAINT IF EXISTS sources_app_id_fkey;
ALTER TABLE kb.sources ADD CONSTRAINT sources_app_id_fkey
    FOREIGN KEY (app_id) REFERENCES kb.source_apps (id) ON DELETE RESTRICT;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
-- Restore the 00289 fail-open policies verbatim and the original cascade.
DROP POLICY IF EXISTS source_triggers_all ON kb.source_triggers;
CREATE POLICY source_triggers_all ON kb.source_triggers FOR ALL USING (
    (COALESCE(current_setting('app.current_project_id'::text, true), ''::text) = ''::text)
    OR ((project_id)::text = current_setting('app.current_project_id'::text, true))
) WITH CHECK (
    (COALESCE(current_setting('app.current_project_id'::text, true), ''::text) = ''::text)
    OR ((project_id)::text = current_setting('app.current_project_id'::text, true))
);

DROP POLICY IF EXISTS source_events_all ON kb.source_events;
CREATE POLICY source_events_all ON kb.source_events FOR ALL USING (
    (COALESCE(current_setting('app.current_project_id'::text, true), ''::text) = ''::text)
    OR ((project_id)::text = current_setting('app.current_project_id'::text, true))
) WITH CHECK (
    (COALESCE(current_setting('app.current_project_id'::text, true), ''::text) = ''::text)
    OR ((project_id)::text = current_setting('app.current_project_id'::text, true))
);

DROP POLICY IF EXISTS source_event_claims_all ON kb.source_event_claims;
CREATE POLICY source_event_claims_all ON kb.source_event_claims FOR ALL USING (
    (COALESCE(current_setting('app.current_project_id'::text, true), ''::text) = ''::text)
    OR ((project_id)::text = current_setting('app.current_project_id'::text, true))
) WITH CHECK (
    (COALESCE(current_setting('app.current_project_id'::text, true), ''::text) = ''::text)
    OR ((project_id)::text = current_setting('app.current_project_id'::text, true))
);

ALTER TABLE kb.sources DROP CONSTRAINT IF EXISTS sources_app_id_fkey;
ALTER TABLE kb.sources ADD CONSTRAINT sources_app_id_fkey
    FOREIGN KEY (app_id) REFERENCES kb.source_apps (id) ON DELETE CASCADE;
-- +goose StatementEnd
