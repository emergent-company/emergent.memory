## 1. Server — second standalone identity

- [ ] 1.1 Add `APIKey2` + `UserEmail2` to `StandaloneConfig` (`internal/config/config.go`)
- [ ] 1.2 Create the second user (`zitadel_user_id='standalone-2'` + `core.user_emails` row) in `domain/standalone/bootstrap.go`
- [ ] 1.3 Map `STANDALONE_API_KEY_2` → second user in `pkg/auth/middleware.go` `checkStandaloneAPIKey` (and the Bearer-token fallback)

## 2. e2e — compose + tests

- [ ] 2.1 Add `STANDALONE_API_KEY_2` / `STANDALONE_USER_EMAIL_2` to the compose server + client env
- [ ] 2.2 Add `invite_lifecycle_test.go`: create validation, list-by-project, revoke, accept (happy/wrong-token/wrong-email/twice), decline (happy/wrong-user)

## 3. Verification

- [ ] 3.1 `go build ./...` + `go test ./...` in `apps/server/`
- [ ] 3.2 `go build ./...` + `go vet ./...` in `e2e/`
- [ ] 3.3 `openspec validate add-invite-lifecycle-e2e`
