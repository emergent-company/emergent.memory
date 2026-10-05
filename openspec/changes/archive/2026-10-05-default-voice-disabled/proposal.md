## Why

Voice currently resolves as enabled when a project has no stored `enabled`
setting, so a freshly created project starts with voice on and the chat page
renders voice controls the user never opted into. Feedback #1497 asks for voice
to be disabled by default for a fresh project.

The `project-voice-settings` capability spec documents the master toggle and the
chat page's hidden-controls behavior, but is silent on the fresh-project default,
so this behavior change is not represented in any requirement.

## What Changes

- `resolveVoiceSettings` defaults `Enabled` to `false` when the project has no
  stored voice setting. A stored `enabled=true` still resolves voice as enabled;
  stored `false` stays disabled.
- We deliberately do NOT migrate existing projects: the default applies to any
  project without an explicit `enabled` value.

## Capabilities

### Modified Capabilities

- `project-voice-settings`: adds the fresh-project default — voice resolves as
  disabled when no `enabled` setting is stored, while a stored `enabled=true`
  still resolves voice as enabled.

## Impact

- `apps/web-ui/gateway/settings_handlers.go` — `resolveVoiceSettings` default.
- `apps/web-ui/gateway/voice_settings_test.go` — default + override unit tests.
