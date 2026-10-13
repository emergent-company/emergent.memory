## 1. Specs & tests first (TDD)

- [x] 1.1 Land the delta spec: `specs/web-agent-public-share/spec.md` (ADDED owner copy-link reveal + encryption-key prerequisite requirements)
- [x] 1.2 `scope_test.go`: fail-first assertion that `data-reveal-url` is scope-rewritten under a base and passes through unchanged globally

## 2. Gateway copy-link hardening

- [x] 2.1 `share_manage.templ`: `copyText` textarea + `execCommand` fallback with success/failure callbacks
- [x] 2.2 `share_manage.templ`: honest labels — "Copied" on success, "Copy failed" when both paths fail (both `data-copy-link` and `data-reveal-url` paths)
- [x] 2.3 `scope.go`: add `data-reveal-url` to `scopeAttrRe`

## 3. Verify

- [x] 3.1 `go build ./...` clean
- [x] 3.2 `go test ./...` passes
- [x] 3.3 `templ generate` clean
- [x] 3.4 `task lint` clean
- [x] 3.5 `openspec validate fix-agent-share-link-reveal` passes
