## 1. Scope resolver foundation

- [x] 1.1 Add `scope.go` with `resolveScopeRefs` (UUID now, slug-ready) against the request-cached org access tree, and verify unit tests cover a valid ref, an unknown ref, and a project that belongs to a different org
- [x] 1.2 Add `scopeRewrite` (Pre middleware: strip `/o/{org}[/p/{project}]` before routing) and `applyScope` (mirror resolved ids into the request `sessionContext`, 404 on failure); verify unit tests assert the mirrored ids, the path rewrite, and the fail-closed 404
- [x] 1.3 Verify the resolver never writes the session cookie

## 2. Route dispatch

- [x] 2.1 Strip the scope prefix before routing so the flat route table is reused; org-only scopes map onto `/orgs/:id`, project scopes strip entirely
- [x] 2.2 Verify a scoped URL dispatches to the flat handler and reports its refs (`TestScopeRewriteStripsPrefix`), and that the resolver rejects invalid refs (`TestApplyScopeValidatesAgainstAccessTree`)
- [x] 2.3 Confirm `/o/` and `/p/` prefixes cannot collide with existing routes or the global pass-through list

## 3. Context and landing

- [x] 3.1 Point `/` (`uiRoot`) at the last-used project's scoped URL, else the org scope, else the wizard; verify handler tests cover each branch
- [x] 3.2 Resolve page context from the path (cookie demoted to the `/` default + recent list); verify the resolver overrides the cookie (`TestApplyScopeValidatesAgainstAccessTree`)
- [x] 3.3 Org-context handlers continue to work via the reused `/orgs/:id` routes; verify their tests pass

## 4. Links and actions

- [x] 4.1 Add `rewriteScopedHTML` + `scopedPathFor`/`linkURL`: rewrite `href`/`action`/`hx-*` absolute internal paths to the scope base at render time, mapping `/orgs/{id}` → `/o/{id}` and passing global paths through; verify `TestRewriteScopedHTML`
- [x] 4.2 Add `scopeLocation` to rewrite `Location`/`HX-Redirect` PRG targets on scoped requests; verify `TestScopeLocationRewritesRedirects`
- [x] 4.3 Convert the project switcher rows and the post-create/switch redirects to scoped URLs (`projectScopeURL`); verify switcher render tests
- [x] 4.4 Add `data-project-id`/`data-org-id` (resolved ids) to the shell `<body>`; scope `data-href`/`data-autosave-url` in the rewriter

## 5. Client-side scope propagation

- [x] 5.1 Add `window.MemoryScope` (base + `path()`) and wrap `fetch` to send `X-Org-ID`/`X-Project-ID` on same-origin `/api/*`; scope JS-built app URLs (board, object preview, chat partials, share-session, inbox refresh)
- [x] 5.2 Wrap `EventSource` to carry `org_id`/`project_id` query params (headers unsupported); inbox already sends `project_id`
- [x] 5.3 Add `resolveClientScope` middleware that accepts and access-tree-validates client `X-Org-ID`/`X-Project-ID` (headers or query) on unscoped requests; verify `TestResolveClientScope` and `TestResolveProjectRefAnyOrg`

## 6. Tests

- [x] 6.1 Update gateway unit/render tests for scoped paths, the switcher links, and `uiRoot`
- [x] 6.2 Add the `scopedPath`/`orgScopedPath` e2e helper and a scoped-URL spec; dev-harness fixtures keep unscoped navigation because the hard cut is session-mode only (D7)
- [x] 6.3 Add a two-tab project-isolation test at the resolver level (`TestApplyScopeIsPerRequestPath`: one session, two scoped paths → two scopes)

## 7. Verification

- [x] 7.1 `templ generate` produces no source diff
- [x] 7.2 `go build ./...` and `go test ./...` pass in `gateway/`
- [x] 7.3 `golangci-lint run ./...` passes for this change (0 issues)
- [ ] 7.4 Manual browser test: open two tabs on two different projects; copy a deep link and confirm it opens the same object/project
- [x] 7.5 Route every direct `render.RenderPartial` fragment through `renderPartial`; rewriter also runs on global pages; hard cut enabled in session mode (`TestScopeRewriteHardCut`)
- [x] 7.6 Add scoped-URL routing notes to `gateway/AGENTS.md`
