## 1. go-daisy toast queue (upstream)

- [x] 1.1 Persistent `duration <= 0` support in `ToastQueueInitPausable`: no timer armed, `pause()` is a no-op, `resume()` does not re-arm. Verify: go-daisy `go test ./components/alpine/...`.
- [x] 1.2 Close button + runtime-gated countdown bar / pointer cursor / root click for persistent toasts in `components/ui/toast-queue.templ`. Verify: go-daisy `go test ./components/ui/...`.
- [x] 1.3 Open go-daisy PR `emergent-company/go-daisy#27`; bump the gateway `go.mod` pin to the branch commit. Verify: `go build ./...` in the gateway resolves the new pin. (PR merged.)

## 2. Gateway wiring

- [x] 2.1 `toastDurationMs` + `flashToast` send duration 0 for `error`, 4200 otherwise; inline queue push uses the payload duration. Verify: `go test ./...` (`refactor_exact` flashToasts cases).
- [x] 2.2 `app.js` `toastDuration()` + `toast()` apply the same rule client-side. Verify: `node --check webui/static/js/app.js`.
- [x] 2.3 `mcp_servers.templ`'s page-local toast applies the same rule (error => duration 0) and stashes under the `kind` key `flushStashedToast` reads — it was the remaining call site still hardcoding 4200 for every kind.

## 3. Docs

- [x] 3.1 `apps/web-ui/docs/ui-patterns.md` §2 + §4 document persistent error toasts and the close button.
- [x] 3.2 `apps/web-ui/docs/ui-recipes.md` adds a `feedback.toast` recipe + decision-table row + source index entry.

## 4. Verification

- [x] 4.1 `go build ./...`, `go test ./...`, and `task lint` in the gateway.
- [ ] 4.2 Manual browser test: trigger an error toast, confirm it persists and the close button dismisses it; confirm a success toast still auto-dismisses. (Deferred to the user's local browser.)
- [x] 4.3 go-daisy#27 merged (`fe7aaf4fbc0c`); re-pin the gateway `go.mod` to the merged master commit `v0.12.1-0.20261006113055-fe7aaf4fbc0c`.
