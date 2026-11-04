-- +goose Up
-- +goose StatementBegin

-- Repair legacy agent-question notification deep links (issue #1362).
--
-- Pre-#1312 ask_user notifications stored the console response page
-- `/agents/questions/<id>` as their action_url (or nothing at all for
-- approval-option questions), but the console serves no such route, so clicking
-- those rows 404s. Point them at the same target the current producer emits: the
-- run's chat conversation when one exists, else the approvals page. `source_id`
-- holds the run id (text) and `cc.id` is a uuid, so the comparison/concatenation
-- is cast explicitly.
UPDATE kb.notifications n
SET action_url = '/chat?c=' || cc.id::text,
    action_label = COALESCE(n.action_label, 'Review'),
    updated_at = now()
FROM kb.agent_runs ar
JOIN kb.chat_conversations cc ON cc.session_id = ar.session_id
WHERE n.type = 'agent_question'
  AND n.source_id = ar.id::text
  AND (n.action_url IS NULL OR n.action_url LIKE '/agents/questions/%');

-- Runs without a chat conversation (scheduled/background) fall back to the
-- approvals surface. Rows already repointed at a conversation by the statement
-- above no longer match this predicate.
UPDATE kb.notifications
SET action_url = '/settings/approvals',
    action_label = COALESCE(action_label, 'Review'),
    updated_at = now()
WHERE type = 'agent_question'
  AND (action_url IS NULL OR action_url LIKE '/agents/questions/%');

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
-- No-op: the pre-fix targets are not routable, so there is nothing correct to
-- restore.
-- +goose StatementEnd
