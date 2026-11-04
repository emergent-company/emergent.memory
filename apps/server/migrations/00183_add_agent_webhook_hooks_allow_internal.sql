-- +goose Up
-- Persist the allow_internal opt-in on webhook hooks (issue #1051(c)).
-- Hook creation refuses binding a webhook hook to an internal-visibility agent
-- unless the caller opts in (#1004). That opt-in must be persisted so the
-- invocation-time visibility check in ReceiveWebhook agrees with the create-time
-- guard: a hook bound to an internal-visibility agent is refused at invocation
-- unless allow_internal is true. The column defaults to false, so a hook bound
-- before the #1004 guard shipped (no opt-in recorded) stays refused at
-- invocation, and an opted-in hook proceeds by the operator's explicit choice.
ALTER TABLE kb.agent_webhook_hooks
  ADD COLUMN IF NOT EXISTS allow_internal boolean NOT NULL DEFAULT false;

-- +goose Down
ALTER TABLE kb.agent_webhook_hooks
  DROP COLUMN IF EXISTS allow_internal;
