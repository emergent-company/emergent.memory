-- +goose Up
-- +goose StatementBegin
-- Instance-wide outbound email settings, managed by a platform superadmin.
-- One row only: the singleton id is always true. The SMTP password lives in an
-- encrypted JSON blob so it is never returned by the read API.
CREATE TABLE IF NOT EXISTS core.instance_email_settings (
    id boolean PRIMARY KEY DEFAULT true CHECK (id),
    enabled boolean NOT NULL DEFAULT false,
    provider text NOT NULL DEFAULT '',
    from_email text NOT NULL DEFAULT '',
    from_name text NOT NULL DEFAULT '',
    smtp_host text NOT NULL DEFAULT '',
    smtp_port integer NOT NULL DEFAULT 587,
    smtp_username text NOT NULL DEFAULT '',
    smtp_tls text NOT NULL DEFAULT 'starttls',
    settings_encrypted text NOT NULL DEFAULT '',
    updated_at timestamptz NOT NULL DEFAULT now(),
    updated_by uuid REFERENCES core.user_profiles (id) ON DELETE SET NULL
);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS core.instance_email_settings;
-- +goose StatementEnd
