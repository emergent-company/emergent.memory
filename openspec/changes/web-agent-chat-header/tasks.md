## 1. Gateway data plumbing

- [ ] 1.1 Add `Description string` to `AgentDefinitionSummary` in `memory.go` with the json tag `description,omitempty` — verify `go build ./...`
- [ ] 1.2 Map `description` in `applyAgentGeneralSection` (`agent.go`): trim the form value onto `def.Description` (empty clears) — verify a unit test covers set + clear
- [ ] 1.3 Extend the general-section tests to assert the description round-trip and that an untouched field does not clobber an existing value

## 2. Agent editor

- [ ] 2.1 Add a **Short description** text input (id `agent-settings-description`, name `description`) to the General settings form in `agent.templ`, with a hint that it appears under the agent's name in chat — verify a render test asserts the id/name/value
- [ ] 2.2 Keep the field's saved value round-tripping through the form (no clobber on unrelated section saves)

## 3. Chat pane header

- [ ] 3.1 Replace the hardcoded header in `chat.templ` with an agent-aware block: icon tile (agent `ui_config` icon/color, neutral `lucide--bot` when unset), name as title, description as second line — verify render tests assert name/description/icon for the active agent
- [ ] 3.2 Add stable hooks (`data-testid="chat-agent-header"`, `#chat-header-icon`, `#chat-header-title`, `#chat-header-desc`) and `data-description` on `#chat-agent` options
- [ ] 3.3 Resolve the active agent server-side (conversation's agent when `?c=` names one, else the default/preselected agent); keep the generic "Chat with Memory" fallback when no agent is active
- [ ] 3.4 Hide the description line when the description is empty
- [ ] 3.5 Update `chat.js` to mirror the selected option's icon/name/description into the header on picker change and on conversation resume; write the description with `textContent` (never `innerHTML`) — verify with a JS-presence/render test where feasible

## 4. Verification

- [ ] 4.1 `templ generate` produces no diff; `go build ./...`, `go vet ./...` pass in `apps/web-ui/gateway`
- [ ] 4.2 `go test ./...` passes in `apps/web-ui/gateway`
- [ ] 4.3 `task lint` passes for the web-ui tree
- [ ] 4.4 Manual browser check on `/chat`: header shows the selected agent's icon/name/description; changing the picker updates it; resuming a session shows that conversation's agent; an agent with no description shows no second line
