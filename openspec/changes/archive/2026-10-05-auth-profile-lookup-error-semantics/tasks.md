## 1. Delta spec

- [x] 1.1 Extend `auth-error-semantics` "Authoritative token rejections return 401" with the missing-user-profile case and a scenario
- [x] 1.2 Extend `auth-error-semantics` "Infrastructure token-validation failures return 503" with the profile-lookup database/pool case and a scenario
- [x] 1.3 Keep the delta text aligned with the shipped `classifyProfileLookupError` mapping in `apps/server/pkg/auth/middleware.go` (`sql.ErrNoRows` → 401, otherwise → 503)

## 2. Verification

- [x] 2.1 `openspec validate auth-profile-lookup-error-semantics --strict` is green
- [x] 2.2 Confirm no code change is required (spec-only correction of #1508); no unit test is added because there is no behaviour change
