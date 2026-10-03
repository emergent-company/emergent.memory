## 1. Derive the work-path status map

- [x] 1.1 Model the agent `workConfig.status` on the gateway definitions summary (`memory.go`)
- [x] 1.2 Derive a project-wide `boardStatusMap` from the agent definitions, defaulting to the built-in statuses and keeping a phase's default on ambiguous declarations (`board.go`)
- [x] 1.3 Emit the map on `#board[data-board-status-map]` and thread it through the board render

## 2. Use it for every board action

- [x] 2.1 Gate the drawer/preview actions on the mapped phases (`board.templ`, `object_preview.go`)
- [x] 2.2 Derive app.js drag transitions from `#board[data-board-status-map]` instead of the literal statuses

## 3. Verify

- [x] 3.1 Gateway unit tests: mapping derivation, `#board` attribute, custom-mapped drawer actions
- [x] 3.2 js-dom specs: custom-mapped drag transitions + default board unchanged
- [x] 3.3 `templ generate`, `go build ./...`, `go test ./...`, `node --check`, js-dom gate, `lint-ratchet.sh`
