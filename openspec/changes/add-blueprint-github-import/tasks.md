## 1. Server fetch + extract + manifest loader

- [ ] 1.1 Add a GitHub archive fetch helper: resolve the accepted repo URL to a `codeload.github.com` tarball/zipball URL for the given ref, stream it down with a bounded timeout and a 50 MiB cap (return a typed "too large" error at 413). Verify: unit tests assert the cap is enforced at the exact limit and the timeout bounds the request.
- [ ] 1.2 Add a safe archive extractor: detect zip vs tar, reject path traversal (`../`), absolute paths, and symlinks/hardlinks escaping the archive root, and cap file count (20000) and total extracted bytes (200 MiB). Verify: unit tests cover each rejection and each cap with a crafted archive.
- [ ] 1.3 Add a manifest loader that reads the blueprint manifest (pack metadata + object/relationship types) from the extracted tree and validates it. Verify: unit tests assert a valid manifest decodes and an invalid/missing manifest surfaces a manifest error.
- [ ] 1.4 Wire fetch + extract + manifest loader into the existing create/publish service path so a valid archive creates and publishes a blueprint. Verify: unit test asserts the create and publish service calls are made with the loaded manifest.

## 2. SSRF / allowlist guard

- [ ] 2.1 Add a URL acceptance parser: accept only `https://github.com/<org>/<repo>` with optional `#<ref>` and/or `/tree/<ref>`; reject every other host and all non-https schemes. Verify: unit tests assert the accepted shapes parse and other hosts / non-https schemes are rejected.
- [ ] 2.2 Add a fetch allowlist guard: only `https://codeload.github.com/...` is fetched; the final URL and every redirect hop's host must be allowlisted; redirects to a non-allowlisted host are refused. Verify: unit tests assert a non-allowlisted final URL and a redirect hop to a non-allowlisted host are both refused.
- [ ] 2.3 Add host-resolution guards: reject IP-literal hosts, localhost, and private/link-local/loopback/CGNAT/metadata ranges before any connection. Verify: unit tests cover each rejected address class.

## 3. Endpoint, authz, and error mapping

- [ ] 3.1 Add `POST /api/blueprints/import` with body `{url, ref?, token?}` scoped to the caller's project (global only for an authorized superadmin), matching the existing blueprint-create authorization guards. Verify: handler test asserts project scoping and superadmin-only global behavior.
- [ ] 3.2 Use the optional token only as an outbound Authorization header; never persist, log, or echo it in responses or errors. Verify: test asserts the token is not present in any response/error body or log output.
- [ ] 3.3 Map errors: 400 invalid URL/manifest, 413 archive too large, 502 fetch failure, 401/403 authorization. Verify: handler tests cover each mapping.
- [ ] 3.4 Gate the endpoint with existing authz and return 401 for unauthenticated and 403 for insufficient role. Verify: handler tests cover both.

## 4. Web UI form + gateway wiring

- [ ] 4.1 Add a gallery import surface with a URL field, an optional ref field, and an optional token field, posting to the import flow. Verify: render test shows the three fields.
- [ ] 4.2 On success install (apply) the imported blueprint and show success; on failure show the mapped error. Verify: handler/render tests cover success and error paths.
- [ ] 4.3 Preserve existing registry-pack and bundled-pack install paths and their ids/`data-testid`/htmx hooks. Verify: render test asserts the existing install actions are unchanged.

## 5. Tests for every guard case

- [ ] 5.1 Unit tests for: allowlisted URL accepted; non-github host rejected; non-https rejected; redirect to non-allowlisted host rejected; oversize archive rejected; traversal/symlink archive rejected; valid manifest creates a published blueprint in the caller's project; private token not leaked; unauthenticated rejected; insufficient role rejected. Verify: all scenarios have a corresponding test.

## 6. Verification

- [ ] 6.1 Run `go build ./...` and `go test ./...` from `apps/server`; verify both succeed.
- [ ] 6.2 Run `PATH="/root/go/bin:$PATH" task lint`; verify no new issues.
- [ ] 6.3 Run `templ generate` and `go build ./...` and `go test ./...` from `apps/web-ui/gateway`; verify all succeed.
- [ ] 6.4 Run `PATH="/root/go/bin:$PATH" openspec validate add-blueprint-github-import --strict`; verify it passes.
