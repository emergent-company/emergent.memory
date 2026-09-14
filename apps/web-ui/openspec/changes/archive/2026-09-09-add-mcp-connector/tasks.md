# Tasks — add-mcp-connector

TDD throughout: every unit ships with deterministic tests (fake hub via
httptest + gorilla/websocket; script-runner seam for Apple tools; shortened
injectable timers). Worktree lane required (main tree shared with parallel
sessions); commit per finished unit on the lane branch.

## 1. Module scaffold

- [x] 1.1 Create `connector/` Go module (own `go.mod`, module path
      `github.com/emergent-company/memory.web-ui/connector`, toolchain matching
      `gateway/go.mod`) with `cmd/memory-connector` printing a version stub.
      Verify: `go build ./...` + `go vet ./...` pass in `connector/`.
- [x] 1.2 Add deps `github.com/gorilla/websocket` + `gopkg.in/yaml.v3` and pin
      them. Verify: `go mod tidy` leaves a clean `go.sum`; build passes.

## 2. Config (`internal/config`)

- [x] 2.1 Implement YAML config struct + `Load(path)` with validation
      (server_url non-empty, token non-empty) and default `instance_id` of
      `<hostname>-connector`. Verify: unit tests for load, defaults,
      validation errors, and `--config` override.
- [x] 2.2 Implement `init` command: capture server URL/token/project/instance
      via flags (non-interactive) or prompts, write file mode 0600, then probe
      `GET /api/mcp-relay/sessions` with the token (project context from
      token). 2xx → success message; 401/403 → auth failure, NO file written;
      other errors reported. Verify: tests with httptest server (200/401/500);
      test that failed auth leaves no file behind and mode is 0600 on success.

## 3. Apple tools + registry (`internal/appletools`, `internal/toolreg`)

- [x] 3.1 Define registry: `Register(name, description, inputSchema, handler)`,
      `List()` returning the nested `{tools:[{name,description,inputSchema}]}`
      payload shape the hub re-serves, `Lookup(name)`. Verify: unit tests
      (list shape, unknown-tool lookup).
- [x] 3.2 Add osascript runner as an interface (`Run(script string, args ...)
      (stdout string, err error)`) with a real `exec` implementation and
      bounded timeout (~15s default). Verify: fake-runner unit tests; real
      runner not invoked in CI.
- [x] 3.3 Implement Notes tools (search notes: query + folder? + limit?; create
      note: title + body?) emitting JSON from AppleScript, mapped to runner
      errors (not-authorized -1743 → guidance message). Verify: unit tests
      against fake runner (parse output, arg validation, error mapping).
- [x] 3.4 Implement Reminders tools (list reminders: list? +
      include_completed? default false; add reminder: title + due_date? +
      notes?) likewise. Verify: unit tests against fake runner.
- [x] 3.5 Platform gate: Apple provider registers tools only when the runner is
      available on this platform (runtime detection, no build tags); registry
      exposes a platform note for `status`. Verify: tests run on any OS —
      Apple tools listed when runner available, empty + note otherwise.

## 4. Relay client (`internal/relay`)

- [x] 4.1 Frame codec: register/response/ping/pong/error frames matching the
      mcprelay handler in the .slim clone (field names + nested tools shape);
      response frames echo the call id. Verify: codec unit tests incl. malformed
      input.
- [x] 4.2 Client connect + register: dial
      `wss?ws://host/api/mcp-relay/connect` (project context from token; URL
      derived from server_url), send register with instance id/version/tools,
      re-register after reconnect. Verify: fake-hub integration test —
      register received, instance appears in fake sessions with tool count.
- [x] 4.3 Dispatch: hub call frame → registry lookup → handler → response
      frame with same id; unknown tool and handler errors returned as error
      responses. Verify: fake-hub tests for success, unknown tool, handler
      failure, and response-id pairing.
- [x] 4.4 Resilience: WS control ping ~25s; drop detection; reconnect with
      capped exponential backoff (30s → 5min cap, shortened + injectable in
      tests); clean SIGTERM/SIGINT shutdown. Verify: fake-hub tests — drop →
      reconnect → re-register observed; shutdown closes cleanly; timers
      injected so tests run fast.

## 5. Status + docs

- [x] 5.1 `status` command: local connection state + registered tools (+
      platform note when Apple tools absent) and best-effort parity check via
      `GET /api/mcp-relay/sessions`. Verify: unit tests with fake hub/state
      injection.
- [x] 5.2 README for `connector/`: install/build, `init`, run, Automation
      permission note, `wss` guidance, MIT attribution NOTICE (Diane-derived
      behavior + Apple tool concepts, © Emergent Company / diane). Verify:
      reviewer reads; NOTICE file present.

## 6. Verification gate

- [x] 6.1 `go build ./...`, `go vet ./...`, `go test ./... -count=1` all pass
      in `connector/`.
- [x] 6.2 Linter clean: `golangci-lint run` in `connector/`.
- [x] 6.3 Manual macOS smoke on mcj-mini: build binary, `init` against dev
      Memory, run relay, confirm node appears on `/settings/mcp-nodes` and a
      Notes/Reminders call round-trips (execute `tools/ios-build-mac.sh`-style
      rsync or documented manual copy). Record result in a session doc.
