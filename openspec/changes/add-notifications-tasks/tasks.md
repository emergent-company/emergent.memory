## 1. Memory client methods

- [ ] 1.1 Add notification client methods `ListNotifications` (tab/category/unread_only/search), `NotificationCounts`, `NotificationStats` and verify unit tests against a fake Memory backend assert URL, query params, bearer token, and response decode
- [ ] 1.2 Add notification mutation methods `MarkNotificationRead`, `MarkNotificationUnread`, `DismissNotification`, `ResolveNotification` and verify unit tests cover success and upstream-error paths
- [ ] 1.3 Add task client methods `ListTasks`, `TaskCounts`, `GetTask`, `ResolveTask`, `CancelTask` and verify unit tests assert `project_id` + `X-Project-ID` scoping and the resolve body/notes

## 2. Handlers & routes

- [ ] 2.1 Add `GET /api/notifications`, `GET /api/notifications/counts`, `GET /api/notifications/stats` handlers and verify handler tests cover empty list, tab filter, and counts
- [ ] 2.2 Add `POST /api/notifications/{id}/read`, `/unread`, `/dismiss`, `/resolve` routes and verify handler tests cover success, unknown-id, and upstream-error responses
- [ ] 2.3 Add `GET /api/tasks`, `GET /api/tasks/counts`, `GET /api/tasks/{id}`, `POST /api/tasks/{id}/resolve`, `POST /api/tasks/{id}/cancel` and verify handler tests cover list, detail, resolve-with-notes, and cancel

## 3. SSE real-time stream

- [ ] 3.1 Add gateway SSE endpoint proxying Memory `/api/events/stream?projectId=<active>` with the session token and verify a handler test asserts the streamed entity event passes through and the bearer/project are set
- [ ] 3.2 Add client-side reconnect + badge update on `notification` events and verify a unit test (or observable DevTools check) confirms reconnect and in-place list update

## 4. Web UI

- [ ] 4.1 Add the notifications inbox templ (tabs, list, badge, mark-read/dismiss/resolve actions) and verify a templ render test asserts it renders with and without notifications
- [ ] 4.2 Add the task monitor templ (list, counts badge, resolve/cancel actions) and verify a templ render test asserts it renders empty and populated states
- [ ] 4.3 Wire inbox + task monitor into the UI shell/nav and verify render tests cover the new nav entries

## 5. Verification

- [ ] 5.1 `go build ./...` + `go vet ./...` + `go test ./...` in `gateway/` pass
- [ ] 5.2 `templ generate` produces no diff and `task lint` passes
- [ ] 5.3 Manual browser test: inbox lists notifications, badge updates on a new notification event, mark-read/dismiss and task resolve/cancel each observable in the DevTools browser
