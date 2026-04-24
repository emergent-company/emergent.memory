## 1. Schema Registration

- [x] 1.1 Add `Session` built-in schema definition (properties: title, started_at, ended_at, message_count, summary, agent_version) to schema seeding logic
- [x] 1.2 Add `Message` built-in schema definition (properties: role, content, sequence_number, timestamp, token_count, tool_calls) with content marked as embedding target
- [x] 1.3 Add `has_message` built-in relationship type (Session → Message) to schema seeding
- [x] 1.4 Register system-level embedding policy for Message.content at startup (idempotent upsert, system=true, not user-deletable)
- [x] 1.5 Verify idempotency: server restart with existing schemas does not error or duplicate

## 2. Session API Handler & Service

- [x] 2.1 Create `session_handler.go` in `domain/graph/` with `CreateSession`, `ListSessions`, `GetSession` handlers
- [x] 2.2 Create `session_service.go` with `CreateSession` (wraps graph object create), `ListSessions`, `GetSession`
- [x] 2.3 Implement `AppendMessage` service method: atomic transaction creating Message object + sequence_number assignment (SELECT COUNT FOR UPDATE) + has_message relationship
- [x] 2.4 Add `AppendMessage` and `ListMessages` handlers
- [x] 2.5 Register routes in `routes.go`: POST /sessions, GET /sessions, GET /sessions/:id, POST /sessions/:id/messages, GET /sessions/:id/messages
- [x] 2.6 Add Swagger annotations to all 5 endpoints

## 3. CLI

- [x] 3.1 Add `sessions` subcommand group to CLI with `create`, `list`, `get` commands
- [x] 3.2 Add `sessions messages` subcommand with `add` and `list` commands
- [x] 3.3 Wire all CLI commands to new API endpoints

## 4. Tests

- [x] 4.1 Unit test: sequence_number is assigned correctly (1, 2, 3) on sequential appends
- [x] 4.2 Unit test: concurrent appends produce unique sequence numbers
- [x] 4.3 Integration test: POST /sessions creates Session object retrievable via GET /graph/objects/:id
- [x] 4.4 Integration test: POST /sessions/:id/messages creates Message with embedding triggered
