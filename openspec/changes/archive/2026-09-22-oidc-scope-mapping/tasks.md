## 1. Configuration

- [x] 1.1 Add `OIDCDefaultScopes []string` (`env:"ZITADEL_OIDC_DEFAULT_SCOPES"`, default empty) and `UserinfoGrantAllScopes bool` (`env:"ZITADEL_USERINFO_GRANT_ALL_SCOPES"`, default `true`) to `ZitadelConfig` in `apps/server/internal/config/config.go`
- [x] 1.2 Add config unit tests asserting the defaults and comma-separated parsing of `ZITADEL_OIDC_DEFAULT_SCOPES`

## 2. Scope resolver

- [x] 2.1 Add `apps/server/pkg/auth/scope_mapping.go` with canonical role constants, the local `viewerReadOnlyScopes` mirror, `filterMemoryScopes`, `roleToScopes`, and the fail-closed `resolveOIDCScopes` (order: explicit Memory scopes → mapped role → default set → empty; role-lookup error → empty)
- [x] 2.2 Add unit tests: introspection path with no Memory scopes yields no scopes; `project_viewer` yields the read-only set; unmapped role yields empty when no default is configured; configured default set is applied when no explicit grant; explicit Memory scopes are passed through verbatim without a union; role-lookup error yields empty

## 3. Auth pipeline wiring

- [x] 3.1 Extend the introspector seam (`Introspect`, `GetUserInfo` on an interface) so both token-validation paths can be faked in tests, and add a `roleLookup` seam defaulting to the `kb.project_memberships` query
- [x] 3.2 Pass the request's `X-Project-ID` from `authenticate` into `validateToken`; resolve scopes for OIDC sessions after the user profile is ensured; never cache derived scopes
- [x] 3.3 Gate the userinfo `GetAllScopes()` grant on `UserinfoGrantAllScopes && !introspectionConfigured`
- [x] 3.4 Add unit tests covering: userinfo grant-all preserved when introspection is unconfigured; userinfo grant-all suppressed once introspection is configured (the `GetAllScopes()` value is never substituted); unknown issuer/userinfo failure yields no scopes

## 4. Role-string consistency

- [x] 4.1 Change `apps/server/domain/standalone/bootstrap.go` to write `project_admin` for the bootstrapped project membership (leave the org membership `owner`)
- [x] 4.2 Add migration `apps/server/migrations/00165_normalize_project_membership_owner_role.sql` normalising `role='owner'` rows in `kb.project_memberships` to `project_admin`
- [x] 4.3 Add/extend a unit test asserting the bootstrapped project membership role is a canonical project role

## 5. Verification

- [x] 5.1 `cd apps/server && PATH="/root/go/bin:$PATH" go build ./...`
- [x] 5.2 `cd apps/server && PATH="/root/go/bin:$PATH" go test ./pkg/auth/... ./domain/projects/... ./domain/standalone/...`
- [x] 5.3 `cd apps/server && PATH="/root/go/bin:$PATH" golangci-lint run ./...`
- [x] 5.4 `openspec validate oidc-scope-mapping`
- [x] 5.5 Update `openspec/specs/project-viewer-role/spec.md` main spec on archive (delta included in this change)
