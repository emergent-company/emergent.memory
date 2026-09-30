-- +goose Up
-- +goose StatementBegin

-- Add explicit scope, requires_action, and event_key to kb.notifications.
-- The DEFAULT 'account' backfills existing rows (the only producers today are
-- agent questions and budget alerts, both account-appropriate).
ALTER TABLE kb.notifications
    ADD COLUMN IF NOT EXISTS scope TEXT NOT NULL DEFAULT 'account',
    ADD COLUMN IF NOT EXISTS requires_action BOOLEAN NOT NULL DEFAULT false,
    ADD COLUMN IF NOT EXISTS event_key TEXT;

CREATE INDEX IF NOT EXISTS idx_notifications_user_scope_project_read
    ON kb.notifications (user_id, scope, project_id, read);

CREATE INDEX IF NOT EXISTS idx_notifications_user_requires_action_read
    ON kb.notifications (user_id, requires_action, read);

-- Per-user, per-project, per-event-key, per-channel notification preferences.
-- project_id NULL is the account-scope default (informational; account
-- delivery is mandatory and not user-suppressible).
CREATE TABLE IF NOT EXISTS kb.notification_preferences (
    id uuid DEFAULT public.uuid_generate_v4() NOT NULL,
    user_id uuid NOT NULL,
    project_id uuid,
    event_key text NOT NULL,
    channel text NOT NULL DEFAULT 'in_app',
    enabled boolean NOT NULL DEFAULT false,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT notification_preferences_pkey PRIMARY KEY (id),
    CONSTRAINT ux_notification_preferences UNIQUE (user_id, project_id, event_key, channel),
    CONSTRAINT fk_notification_preferences_user
        FOREIGN KEY (user_id) REFERENCES core.user_profiles(id),
    CONSTRAINT fk_notification_preferences_project
        FOREIGN KEY (project_id) REFERENCES kb.projects(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_notification_preferences_user_id
    ON kb.notification_preferences (user_id);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

DROP TABLE IF EXISTS kb.notification_preferences;

DROP INDEX IF EXISTS kb.idx_notifications_user_scope_project_read;
DROP INDEX IF EXISTS kb.idx_notifications_user_requires_action_read;

ALTER TABLE kb.notifications
    DROP COLUMN IF EXISTS scope,
    DROP COLUMN IF EXISTS requires_action,
    DROP COLUMN IF EXISTS event_key;

-- +goose StatementEnd
