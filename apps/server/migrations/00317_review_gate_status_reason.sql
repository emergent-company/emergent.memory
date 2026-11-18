-- +goose Up
-- +goose StatementBegin

-- Terminal review-gate reason. A gate can be unapprovable (for example a pull
-- request with no changed files at its head), in which case no verdict can ever
-- be recorded. Persist why the gate reached that state so the reviewed check
-- and operators can explain it instead of watching an in_progress check that
-- never resolves (#1022).
ALTER TABLE kb.review_gates
    ADD COLUMN IF NOT EXISTS status_reason TEXT NOT NULL DEFAULT '';

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

ALTER TABLE kb.review_gates DROP COLUMN IF EXISTS status_reason;

-- +goose StatementEnd
