## 1. Default voice to disabled for a fresh project (#1497)

- [x] 1.1 Default `resolveVoiceSettings` `Enabled` to `false` when the project has no stored `enabled` setting; a stored `enabled=true` still resolves enabled. Verify `go test ./... -run TestResolveVoiceSettings` passes.
- [x] 1.2 Unit-test the fresh-project default and the stored true/false override (`TestVoiceEnabled`, `TestResolveVoiceSettingsEnabledDefault`). Verify `go test ./... -run 'TestVoiceEnabled|TestResolveVoiceSettingsEnabledDefault'` passes.
