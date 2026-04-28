-- +goose Up
-- Create session_todo_status enum
CREATE TYPE kb.session_todo_status AS ENUM ('draft', 'pending', 'in_progress', 'completed', 'cancelled');

-- Create session_todos table
CREATE TABLE kb.session_todos (
    id               uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    session_id       uuid        NOT NULL REFERENCES kb.acp_sessions(id) ON DELETE CASCADE,
    content          text        NOT NULL,
    status           kb.session_todo_status NOT NULL DEFAULT 'draft',
    author           text,
    "order"          integer     NOT NULL DEFAULT 0,
    context_snapshot text,
    created_at       timestamptz NOT NULL DEFAULT current_timestamp,
    updated_at       timestamptz NOT NULL DEFAULT current_timestamp
);

CREATE INDEX idx_session_todos_session_id ON kb.session_todos(session_id);
CREATE INDEX idx_session_todos_status ON kb.session_todos(status);

-- +goose Down
DROP TABLE IF EXISTS kb.session_todos;
DROP TYPE IF EXISTS kb.session_todo_status;
