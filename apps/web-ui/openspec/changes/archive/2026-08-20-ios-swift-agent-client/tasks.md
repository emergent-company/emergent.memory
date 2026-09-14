## 1. Token server (Alfred backend)

- [x] 1.1 Add `POST /api/token` route to `agent/admin.py` that validates an `X-API-Key` header against a new `TOKEN_API_KEY` env var
- [x] 1.2 Mint a room-join JWT from `LIVEKIT_API_KEY`/`LIVEKIT_API_SECRET` with `room_join=True` grant, requested `room`, `identity`, and `room_config.agents` for the requested agent name
- [x] 1.3 Return `{ server_url, participant_token }` matching the Swift `TokenSourceResponse` shape; reject malformed/unauthorized/disallowed-room requests with non-success status
- [x] 1.4 Add `TOKEN_API_KEY` to `.env.example`; document the endpoint in `INFRASTRUCTURE.md`
- [x] 1.5 Verify endpoint with `curl` (valid + invalid key + bad room) against a locally running admin server

## 2. iOS project scaffold

- [x] 2.1 Clone `livekit-examples/agent-starter-swift` into a new `client/ios/` directory and pin `Package.resolved`
- [x] 2.2 Rename project/bundle (`VoiceAgent` → Alfred-appropriate), set `PRODUCT_BUNDLE_IDENTIFIER` + `DEVELOPMENT_TEAM` placeholders in `VoiceAgent.xcconfig`
- [x] 2.3 Remove `BroadcastExtension` (screen share) and camera/video/chat features not needed for the voice client
- [x] 2.4 Add `NSLocalNetworkUsageDescription` and `NSAppTransportSecurity` → `NSAllowsLocalNetworking = true` to Info.plist
- [x] 2.5 Add mic usage description (`NSMicrophoneUsageDescription`) if not already present

## 3. Connection + dispatch

- [x] 3.1 Add an `AgentToConnect` case implementing `EndpointTokenSource` pointing at the token endpoint from §1, carrying server URL + agent name
- [x] 3.2 Wire a settings screen (UserDefaults-backed) for server URL, agent name (`alfred-google-rt` default), and token endpoint, consumed by the token source
- [x] 3.3 Confirm `Session`/`LocalMedia` connects, publishes mic with `echoCancellationMode` enabled, and agent dispatch occurs on connect
- [x] 3.4 Implement connect-failure and dispatch-failure states with retry (token error, unreachable server)

## 4. Session lifecycle + Alfred signals

- [x] 4.1 Play a cue-to-speak chime on `lk.agent.ready` text stream
- [x] 4.2 End session on `lk.agent.events` `user_state_changed` → `away`
- [x] 4.3 Scan `lk.transcription` user transcripts for a configured exit keyword and end session on match
- [x] 4.4 End session when the agent participant disconnects; return UI to idle

## 5. Build, sign, and run

- [ ] 5.1 Build + run on iOS simulator against the tailnet IP (`ws://100.69.175.118:7880`), verify full talk loop with the running `alfred-google-rt` worker
- [ ] 5.2 Set `DEVELOPMENT_TEAM` + unique bundle id; build and run on a physical iPhone over Tailscale; verify audio + barge-in
- [x] 5.3 Verify a clean `go build ./...` equivalent (backend) / Xcode build with no new warnings beyond the known dSYM warning
- [ ] 5.4 Confirm the macOS Python client still works unchanged (regression check)

## 6. Verify against spec

- [x] 6.1 Walk every `#### Scenario` in `specs/ios-agent-client/spec.md` and confirm the app + token server satisfy it (or record a deliberate deviation)
- [x] 6.2 Run `openspec validate --change ios-swift-agent-client` (or `--strict`) and fix any issues

## 7. QR onboarding (enhancement, user-requested)

- [x] 7.1 Add `GET /api/qr-config` to `agent/admin.py` returning `{serverURL, tokenEndpoint, apiKey, agentName, roomName}`; render it as a QR code (client-side JS) on the existing admin page
- [x] 7.2 Add a QR scanner to the iOS app (camera permission, AVFoundation scan, decode JSON, populate `AlfredConfig`, save) with a "Scan QR" entry point on the Connect screen
- [ ] 7.3 Deploy updated `admin.py` to the LXC + verify `/api/qr-config`; rebuild/reinstall the app and verify an end-to-end QR scan configures + connects
