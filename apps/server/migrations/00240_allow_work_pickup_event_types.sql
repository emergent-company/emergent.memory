-- +goose Up
-- +goose StatementBegin
-- Work-pickup signals (work-pickup-signals) emit reaction events whose type is
-- status_changed / assigned. The baseline CHECK on kb.agent_processing_log only
-- allowed created / updated / deleted, so any object-driven dispatch with the
-- new signal types failed the constraint and enqueued zero runs. Relax the
-- constraint to admit the two work-pickup signal types.
ALTER TABLE kb.agent_processing_log
    DROP CONSTRAINT IF EXISTS chk_agent_processing_log_event_type;

ALTER TABLE kb.agent_processing_log
    ADD CONSTRAINT chk_agent_processing_log_event_type
    CHECK (event_type = ANY (ARRAY[
        'created'::text,
        'updated'::text,
        'deleted'::text,
        'status_changed'::text,
        'assigned'::text
    ]));
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
-- Restore the baseline constraint. Rows already logged with status_changed /
-- assigned would violate it; remove them first so the tighten cannot fail.
DELETE FROM kb.agent_processing_log
    WHERE event_type IN ('status_changed', 'assigned');

ALTER TABLE kb.agent_processing_log
    DROP CONSTRAINT IF EXISTS chk_agent_processing_log_event_type;

ALTER TABLE kb.agent_processing_log
    ADD CONSTRAINT chk_agent_processing_log_event_type
    CHECK (event_type = ANY (ARRAY['created'::text, 'updated'::text, 'deleted'::text]));
-- +goose StatementEnd
