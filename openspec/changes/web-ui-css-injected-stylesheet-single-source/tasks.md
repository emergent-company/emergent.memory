## 1. Remove the injected duplicate stylesheet

- [x] 1.1 Delete `ensureChatControlStyle()` from `webui/static/js/chat-components.js` (the injected `<style>` that mirrored `app.css`'s chat run-control surface + rail badge). The measurement in #890 §5.1 found 47 of the injected sheet's 84 selectors duplicated in `app.css`; all belong to this sheet, so it is a pure duplicate.
- [x] 1.2 Drop the only call site: remove `MemoryChatComponents.ensureChatControlStyle()` from the `chat.js` boot sequence (`webui/static/js/chat.js`), and remove the `ensureChatControlStyle` export from `window.MemoryChatComponents`.
- [x] 1.3 Keep `ensureBadgeStyle()` — it is the genuinely runtime-only subset (`app.css` defines no `.memory-badge*` rules) plus two documented `.memory-tool-chip[data-status]` border overrides. Update its comment to state it is the only remaining injected sheet and that nothing it injects re-declares an `app.css` rule.
- [x] 1.4 Update `webui/css/app.css` comments that described the injected copy: the "Radius exception list" note (now only `.memory-badge .625rem`, a JS-only exception with no theme token), the run-control surface header ("chat-components.js injects the same rules"), and the rail-badge `.hidden` note (no longer "keep identical to the injected copy").
- [x] 1.5 Add a guard test (`injected_stylesheet_test.go`) that extracts the injected CSS from `chat-components.js` and asserts none of the app.css-owned chat-control / rail-badge selectors reappear, while the runtime-only badge shell remains. This is the automated check that prevents the two sources drifting apart again.

## 2. OpenSpec delta

- [x] 2.1 Replace the `web-ui-css` "Badge stylesheet copies stay in sync" requirement (which mandated the two-copies convention) with "Runtime-injected styles are not a second stylesheet" (REMOVED + ADDED delta).

## 3. Deferred (not in this change)

- [ ] 3.1 Muted text/icon scale (§5.2, issue #1026's second requirement): a ~500-site migration whose "no opacity outside the scale" rule makes partial work unsatisfying. Tracked separately.

## 4. Verification

- [x] 4.1 `templ generate` produces no diff; `go build ./...` and `go test ./...` pass from `apps/web-ui/gateway`.
- [x] 4.2 `task css` runs and the new guard test passes against the real compiled output.
- [x] 4.3 `openspec validate --all --strict` passes.
