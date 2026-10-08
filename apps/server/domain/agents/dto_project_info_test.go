package agents

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestIncludeProjectInfoFlag covers the storage reader/mutator that backs the
// per-agent opt-in flag (#1694).
func TestIncludeProjectInfoFlag(t *testing.T) {
	t.Run("nil and missing config are off", func(t *testing.T) {
		if (*AgentDefinition)(nil).IncludeProjectInfo() {
			t.Fatal("nil definition should report false")
		}
		if (&AgentDefinition{}).IncludeProjectInfo() {
			t.Fatal("nil config should report false")
		}
	})
	t.Run("non-bool value is off", func(t *testing.T) {
		def := &AgentDefinition{Config: map[string]any{includeProjectInfoConfigKey: "true"}}
		if def.IncludeProjectInfo() {
			t.Fatal("non-bool config value should report false")
		}
	})
	t.Run("true round-trips through the config blob", func(t *testing.T) {
		def := &AgentDefinition{Config: map[string]any{includeProjectInfoConfigKey: true}}
		if !def.IncludeProjectInfo() {
			t.Fatal("expected IncludeProjectInfo() == true")
		}
	})
	t.Run("setting false removes the key", func(t *testing.T) {
		def := &AgentDefinition{Config: map[string]any{"language": "Spanish"}}
		def.SetIncludeProjectInfo(true)
		if def.Config[includeProjectInfoConfigKey] != true {
			t.Fatalf("want config key true, got %#v", def.Config)
		}
		def.SetIncludeProjectInfo(false)
		if _, ok := def.Config[includeProjectInfoConfigKey]; ok {
			t.Fatalf("setting false must remove the key, got %#v", def.Config)
		}
		if def.Config["language"] != "Spanish" {
			t.Fatalf("setting the flag must not disturb other config keys, got %#v", def.Config)
		}
	})
	t.Run("setting true initialises a nil config", func(t *testing.T) {
		def := &AgentDefinition{}
		def.SetIncludeProjectInfo(true)
		if !def.IncludeProjectInfo() {
			t.Fatal("expected flag true after setting on nil config")
		}
	})
}

// TestIncludeProjectInfoDTORoundTrip locks the read/write contract: the flag is
// derived on the read DTO and accepted on create/update.
func TestIncludeProjectInfoDTORoundTrip(t *testing.T) {
	t.Run("read DTO exposes the flag", func(t *testing.T) {
		on := (&AgentDefinition{ID: "ad-1", Config: map[string]any{includeProjectInfoConfigKey: true}}).ToDTO()
		if !on.IncludeProjectInfo {
			t.Fatal("ToDTO().IncludeProjectInfo = false, want true")
		}
		off := (&AgentDefinition{ID: "ad-2"}).ToDTO()
		if off.IncludeProjectInfo {
			t.Fatal("ToDTO().IncludeProjectInfo = true, want false")
		}
		b, err := json.Marshal(on)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		if !strings.Contains(string(b), `"includeProjectInfo":true`) {
			t.Fatalf("read DTO JSON missing flag: %s", b)
		}
	})
	t.Run("create DTO accepts the flag", func(t *testing.T) {
		var dto CreateAgentDefinitionDTO
		if err := json.Unmarshal([]byte(`{"name":"a","includeProjectInfo":true}`), &dto); err != nil {
			t.Fatalf("unmarshal create: %v", err)
		}
		if dto.IncludeProjectInfo == nil || !*dto.IncludeProjectInfo {
			t.Fatalf("create DTO flag = %#v, want true", dto.IncludeProjectInfo)
		}
		// Absent flag is nil, so create defaults to off.
		var bare CreateAgentDefinitionDTO
		if err := json.Unmarshal([]byte(`{"name":"a"}`), &bare); err != nil {
			t.Fatalf("unmarshal bare create: %v", err)
		}
		if bare.IncludeProjectInfo != nil {
			t.Fatalf("absent create flag = %#v, want nil", bare.IncludeProjectInfo)
		}
	})
	t.Run("update DTO accepts the flag", func(t *testing.T) {
		var dto UpdateAgentDefinitionDTO
		if err := json.Unmarshal([]byte(`{"includeProjectInfo":false}`), &dto); err != nil {
			t.Fatalf("unmarshal update: %v", err)
		}
		if dto.IncludeProjectInfo == nil || *dto.IncludeProjectInfo {
			t.Fatalf("update DTO flag = %#v, want explicit false", dto.IncludeProjectInfo)
		}
	})
}
