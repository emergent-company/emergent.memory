package blueprints

import (
	"encoding/json"
	"testing"
)

// TestAgentFileUIConfigToRequests verifies a blueprint agent's `ui` block is
// carried into the SDK create/update requests as the opaque uiConfig JSON, and
// that an absent or empty block is omitted so an update preserves an existing
// appearance instead of clearing it.
func TestAgentFileUIConfigToRequests(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		ui       *AgentUI
		wantJSON string
		wantNil  bool
	}{
		{name: "no ui", ui: nil, wantNil: true},
		{name: "empty ui", ui: &AgentUI{}, wantNil: true},
		{name: "icon only", ui: &AgentUI{Icon: "bot"}, wantJSON: `{"icon":"bot"}`},
		{name: "color only", ui: &AgentUI{Color: "#4F46E5"}, wantJSON: `{"color":"#4F46E5"}`},
		{name: "icon and color", ui: &AgentUI{Icon: "database", Color: "#2563EB"}, wantJSON: `{"color":"#2563EB","icon":"database"}`},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			create := agentFileToCreateRequest(AgentFile{Name: "a", UI: tc.ui})
			update := agentFileToUpdateRequest(AgentFile{Name: "a", UI: tc.ui})

			for label, raw := range map[string]json.RawMessage{"create": create.UIConfig, "update": update.UIConfig} {
				if tc.wantNil {
					if raw != nil {
						t.Fatalf("%s UIConfig = %s, want nil", label, raw)
					}
					continue
				}
				var got map[string]string
				if err := json.Unmarshal(raw, &got); err != nil {
					t.Fatalf("%s UIConfig is not a JSON object: %v (%s)", label, err, raw)
				}
				want := map[string]string{}
				if err := json.Unmarshal([]byte(tc.wantJSON), &want); err != nil {
					t.Fatalf("bad test fixture: %v", err)
				}
				if len(got) != len(want) {
					t.Fatalf("%s UIConfig = %v, want %v", label, got, want)
				}
				for k, v := range want {
					if got[k] != v {
						t.Errorf("%s UIConfig[%q] = %q, want %q", label, k, got[k], v)
					}
				}
			}
		})
	}
}
