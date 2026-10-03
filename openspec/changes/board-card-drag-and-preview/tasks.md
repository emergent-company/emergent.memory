## 1. Drag between columns

- [x] 1.1 Make every board card draggable and map a `(fromStatus → target lane)` drop to the supported board action; reject unsupported pairs in the UI with no request (unit + js-dom tests)
- [x] 1.2 Route `review → revision` to the existing item action dialog (feedback required) instead of a blind POST
- [x] 1.3 Add the lane drop / card dragging affordance in `app.css`, reduced-motion safe

## 2. Shared object preview on card activation

- [x] 2.1 Card click / Enter / Space opens `window.MemoryObjectPreview` on the canonical id instead of loading the board dialog
- [x] 2.2 Add the `?actions=board` selector to the preview partial and render the status-gated work-item actions into `#object-preview-actions-slot`; chat previews stay read-only
- [x] 2.3 Close the preview after a successful action from its slot; keep `/board/items/:id` + `BoardDrawer` reachable for feedback

## 3. Verify

- [x] 3.1 `templ generate`, `go build ./...`, `go test ./...` from `apps/web-ui/gateway`
- [x] 3.2 `golangci-lint run ./...` and `node --check` on changed JS
- [x] 3.3 Hermetic js-dom gate (`npx playwright test --config=js-dom.config.ts`)
- [ ] 3.4 Browser smoke test against a live gateway (drag blocked→ready, review→done, any→blocked; click card → shared preview)

<!-- openspec:archive-ready -->
