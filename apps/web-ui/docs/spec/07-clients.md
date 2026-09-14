# 07 — Clients

Three clients. One is the main UI (web); two are voice (iOS, Mac).

## Web (go-daisy) — main UI

- **Role:** manage everything + text chat. No voice (D5).
- **Surface:** agents, MCP servers, chat, sessions, settings (see 04-go-application.md).
- **Auth:** `X-API-Key` to the Go gateway (same key as iOS).
- **Chat:** streams memory's chat SSE relayed by the Go gateway.
- **Styles:** one gateway-compiled Tailwind + daisyUI sheet; go-daisy's components are
  `@source`d (vendored) and its custom CSS `@import`ed — go-daisy's monolithic `app.css`
  is not loaded (D22). In-content links + sidebar nav both partial-swap `#main-content`
  (hx-boost / hx-get); no full reloads on navigation (D23).
- **PWA:** "Add to Home Screen" opens standalone (no Safari chrome) via
  `apple-mobile-web-app-capable`; branded splash screens (`apple-touch-startup-image`),
  a maskable icon, and safe-area padding for the notch/home-indicator are in place (D32).

## iOS — voice client

- **Role:** voice conversation with a selected agent.
- **Shell lifted from Diane** (see 11-reuse-from-diane.md): shared Codable models, protocol-DI
  HTTP client, adaptive nav (`IOSContentView`), list views, `Components/`. Retargeted to the
  Go gateway:
  - token via `POST /api/token` + QR onboarding (now served by the Go app); optionally the
    Diane pairing-code flow (`POST /pair`) as a nicer onboarding path.
  - agent picker reads `GET /api/agents` (gateway) instead of a hardcoded list.
  - conversation/sessions views read gateway session endpoints (memory-backed).
- **Build from scratch (Diane has none):** streaming chat UI, voice/LiveKit audio handling,
  session list/resume, write paths (Diane's iOS client is read-only-stubbed).
- **Audio:** publish mic with AEC (`enable_aec`), subscribe to agent audio; honor
  `lk.agent.ready` (chime) and away/exit signals from the bridge.

### iOS development

- **Dev host is `mcj@mcj-mini-2-1`** (Mac Mini, Tailscale MagicDNS). Do all iOS work there —
  this Linux server has no Xcode, so Swift cannot be built or type-checked here.
- **Sync:** rsync `client/ios/` → `/Users/mcj/alfred/client/ios` (the `deploy.sh` step).
- **Build:** `xcodebuild -project VoiceAgent.xcodeproj -scheme VoiceAgent -destination 'generic/platform=iOS Simulator' CODE_SIGNING_ALLOWED=NO build` (run on the Mac).
- Never claim an iOS change compiles until the Mac build actually passes.

## Mac — wake-word console

- **Role:** always-listening voice client on the owner's Mac.
- **Existing** `client/wakeword_client.py` (Porcupine "hey alfred") reused, retargeted to the
  new bridge. It is a console client, not iOS (D11).
- Wake word → join LiveKit room → agent dispatched → converse → auto-timeout.

## macOS connector app (`Memory.app`)

- **Role:** run the local MCP connector on a Mac and expose its tools to Memory
  over the relay (Apple Notes/Reminders via AppleScript), while also being the
  normal desktop UI (dashboard, project switcher, agent details).
- **Shape:** SwiftUI app (Dock + menu-bar status item) embedding the Go connector
  engine as a bundle resource; the app spawns the engine as a **direct child
  process** so macOS Automation (TCC) grants and identity attach to the app.
- **Sign-in:** Zitadel OIDC, **native app + PKCE** in the system browser;
  accounts are multi-session and environment-scoped (Prod / Dev). Secrets are
  0600 files under `~/.config/memory-connector/accounts/<accountId>/` (no
  Keychain prompts across ad-hoc rebuilds).
- **Connection:** explicit per-project **Connect** toggle; the engine runs only
  while a project is connected (per-account profiles hold enabled tools +
  instance id). Tools default OFF per project; a master switch toggles all.
- **App identity:** product/display name `Memory`; bundle id stays
  `com.emergent.memory.connector` (stable TCC/keychain identity). Build/install:
  `tools/mac-build.sh --install` (product `Memory.app`); optional stable signing
  via `tools/mac-sign.sh`.
- See [INFRASTRUCTURE.md](../../INFRASTRUCTURE.md) for environment values and
  [2026-09-13-memory-mac-connector](../sessions/2026-09-13-memory-mac-connector.md)
  for the delivery log.

## Shared client behaviors

- All voice clients are **dumb terminals**: capture audio, play audio, show state. No agent
  config, no model, no memory access directly — everything via the Go gateway.
- Voice **language** is a per-agent setting (D13), set on the agent definition; the bridge
  applies it to STT/TTS. Clients are language-agnostic.
- Exit keywords (`stop`/`goodbye`) end the voice session; handled by the bridge.

## Interface summary

| Client | Transport | Talks to |
|---|---|---|
| Web | HTTPS REST + SSE | Go gateway |
| iOS | HTTPS (token/agents) + WebRTC (voice) | Go gateway + LiveKit |
| Mac | WebRTC (voice) | LiveKit |
