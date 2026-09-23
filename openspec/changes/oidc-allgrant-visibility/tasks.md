## 1. Predicate on config

- [x] 1.1 Add `IntrospectionConfigured()` and `UserinfoAllGrantActive()` to `*ZitadelConfig`; delegate the auth middleware's `introspectionConfigured`/`oidcAllGrantEnabled` to them
- [x] 1.2 Cover the predicate for all four combinations of (flag on/off) × (introspection configured/not) in `TestZitadelConfigUserinfoAllGrantActive`

## 2. Startup warning (visibility)

- [x] 2.1 Emit a `WARN` at startup when the all-grant is active, naming the effect and both remediations (`ZITADEL_CLIENT_JWT`/`ZITADEL_CLIENT_JWT_PATH`, or `ZITADEL_USERINFO_GRANT_ALL_SCOPES=false`)
- [x] 2.2 Capture the log line in `TestWarnIfOIDCAllGrantActive` for all four combinations (asserting nothing is logged when inactive)

## 3. Health visibility

- [x] 3.1 Add an `oidc_all_grant` check entry reporting `warning`/`healthy`; keep it out of the critical/optional component lists so it never changes the overall status
- [x] 3.2 Cover the check in `TestOIDCAllGrantCheck` for all four combinations

## 4. Spec + docs

- [x] 4.1 Add the `oidc-allgrant-visibility` OpenSpec change with a delta requirement for the observable posture
- [x] 4.2 Note the shipped default and the warning in `.env.example`
- [x] 4.3 `openspec validate --strict` passes for the change

## 5. Verification

- [x] 5.1 `go build ./...` in `apps/server` passes
- [x] 5.2 `go test -count=1 ./internal/config/... ./pkg/auth/... ./domain/health/...` passes
- [x] 5.3 `golangci-lint run --new-from-rev=origin/main` on the touched packages reports 0 issues and `gofmt -l` is clean

## 6. Out of scope

- [ ] 6.1 No authorisation behaviour change (no default flip, no scope semantics, no gating change)
- [ ] 6.2 Live-Zitadel e2e (issue #736 item 3) — requires real Zitadel credentials in CI; decided separately, so #736 is referenced with `Refs`, not `Closes`
