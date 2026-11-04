package cmd

import (
	"bytes"
	"encoding/json"
	"reflect"
	"sort"
	"testing"
	"time"

	"github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/a2a"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestArtifactTextTrackerSuffix(t *testing.T) {
	t.Run("first appearance", func(t *testing.T) {
		tracker := newArtifactTextTracker()
		assert.Equal(t, "Hel", tracker.suffix("a", "Hel"))
	})

	t.Run("growing text", func(t *testing.T) {
		tracker := newArtifactTextTracker()
		assert.Equal(t, "Hel", tracker.suffix("a", "Hel"))
		assert.Equal(t, "lo", tracker.suffix("a", "Hello"))
	})

	t.Run("repeated identical text", func(t *testing.T) {
		tracker := newArtifactTextTracker()
		assert.Equal(t, "Hello", tracker.suffix("a", "Hello"))
		assert.Equal(t, "", tracker.suffix("a", "Hello"))
	})

	t.Run("multiple artifacts are tracked independently", func(t *testing.T) {
		tracker := newArtifactTextTracker()

		assert.Equal(t, "Hel", tracker.suffix("a", "Hel"))
		assert.Equal(t, "Wor", tracker.suffix("b", "Wor"))

		// Interleaved growth must not mix lengths across artifacts.
		assert.Equal(t, "lo", tracker.suffix("a", "Hello"))
		assert.Equal(t, "ld", tracker.suffix("b", "World"))
	})

	t.Run("shrinking or empty text emits nothing", func(t *testing.T) {
		tracker := newArtifactTextTracker()
		assert.Equal(t, "Hello", tracker.suffix("a", "Hello"))
		assert.Equal(t, "", tracker.suffix("a", "Hel"))
		assert.Equal(t, "", tracker.suffix("a", ""))
	})
}

func TestA2AProjectSelector_FlagOverridesConfig(t *testing.T) {
	prev := viper.Get("project_id")
	defer viper.Set("project_id", prev)

	viper.Set("project_id", "flag-proj")
	assert.Equal(t, "flag-proj", a2aProjectSelector("config-proj"))

	viper.Set("project_id", "")
	assert.Equal(t, "config-proj", a2aProjectSelector("config-proj"))
}

func TestA2AArtifactText(t *testing.T) {
	t.Run("empty artifact", func(t *testing.T) {
		assert.Equal(t, "", a2aArtifactText(a2a.Artifact{}))
	})

	t.Run("single text part", func(t *testing.T) {
		text := "hello"
		assert.Equal(t, "hello", a2aArtifactText(a2a.Artifact{Parts: []a2a.Part{a2a.TextPart(text)}}))
	})

	t.Run("multiple parts including non-text", func(t *testing.T) {
		a := "hello "
		b := "world"
		raw := "ignore me"
		assert.Equal(t, "hello world", a2aArtifactText(a2a.Artifact{Parts: []a2a.Part{
			a2a.TextPart(a),
			{Raw: &raw},
			a2a.TextPart(b),
		}}))
	})
}

// ── --json contract (#2094) ──────────────────────────────────────────────────
//
// Everything `memory a2a ... --json` prints is an A2A v1.0 wire object, printed
// verbatim (see a2aPrintJSON). Its key set is therefore the protocol's, not a
// CLI schema: required members are always present, spec-optional members are
// absent until set, and the presence unions below must keep varying — presence
// is the discriminator. These tests pin that contract so nobody "stabilises" a
// protocol object without deliberately rewriting the documented behaviour.
//
// A new SDK field on one of these types fails completeness below until the
// required/optional lists are updated on purpose.

type a2aJSONShape struct {
	name      string
	typ       reflect.Type
	zero      any
	populated any
	required  []string
	optional  []string
	// populatedKeys are the optional members the populated fixture sets. It
	// defaults to `optional`; the union shapes set only one variant at a time.
	populatedKeys []string
}

func a2aJSONShapes() []a2aJSONShape {
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	text := "fake reply text"
	return []a2aJSONShape{
		{
			name: "AgentCard",
			typ:  reflect.TypeOf(a2a.AgentCard{}),
			zero: &a2a.AgentCard{},
			populated: &a2a.AgentCard{
				Name:                "fake-agent",
				Description:         "fake description",
				SupportedInterfaces: []a2a.AgentInterface{{URL: "https://fake.invalid/a2a", ProtocolBinding: "JSONRPC", ProtocolVersion: "1.0"}},
				Provider:            &a2a.AgentProvider{Organization: "fake-org", URL: "https://fake.invalid"},
				Version:             "1.0.0",
				DocumentationURL:    "https://fake.invalid/docs",
				Capabilities:        a2a.AgentCapabilities{Streaming: true, PushNotifications: true, ExtendedAgentCard: true},
				SecuritySchemes:     map[string]a2a.SecurityScheme{"fake": {HTTPAuth: &a2a.HTTPAuthSecurityScheme{Scheme: "bearer", BearerFormat: "jwt"}}},
				SecurityRequirements: []a2a.SecurityRequirement{
					{Schemes: map[string]a2a.SecuritySchemeReference{"fake": {List: []string{"fake-scope"}}}},
				},
				DefaultInputModes:  []string{"text/plain"},
				DefaultOutputModes: []string{"text/plain"},
				Skills:             []a2a.AgentSkill{{ID: "fake-skill", Name: "Fake Skill", Description: "fake", Tags: []string{"fake-tag"}}},
				IconURL:            "https://fake.invalid/icon.png",
				Signatures:         []any{map[string]any{"fake": true}},
			},
			required: []string{"name", "description", "supportedInterfaces", "version", "capabilities", "defaultInputModes", "defaultOutputModes", "skills"},
			optional: []string{"provider", "documentationUrl", "securitySchemes", "securityRequirements", "iconUrl", "signatures"},
		},
		{
			name: "Task",
			typ:  reflect.TypeOf(a2a.Task{}),
			zero: &a2a.Task{},
			populated: &a2a.Task{
				ID:        "fake-task-id",
				ContextID: "fake-context-id",
				Status:    a2a.TaskStatus{State: a2a.TaskStateCompleted, Message: &a2a.Message{MessageID: "fake-msg", Role: a2a.RoleAgent, Parts: []a2a.Part{a2a.TextPart(text)}}, Timestamp: &now},
				Artifacts: []a2a.Artifact{{ArtifactID: "fake-artifact", Name: "fake", Parts: []a2a.Part{a2a.TextPart(text)}}},
				History:   []a2a.Message{{MessageID: "fake-msg", Role: a2a.RoleUser, Parts: []a2a.Part{a2a.TextPart(text)}}},
				Metadata:  map[string]any{"fakeKey": "fakeValue"},
			},
			required: []string{"id", "contextId", "status"},
			optional: []string{"artifacts", "history", "metadata"},
		},
		{
			name:      "SendMessageResponse",
			typ:       reflect.TypeOf(a2a.SendMessageResponse{}),
			zero:      &a2a.SendMessageResponse{},
			populated: &a2a.SendMessageResponse{Task: &a2a.Task{ID: "fake-task-id", ContextID: "fake-context-id", Status: a2a.TaskStatus{State: a2a.TaskStateWorking}}},
			required:  nil,
			optional:  []string{"task", "message"},
			// Presence is the discriminator: the fixture carries the task
			// variant, so `message` must stay absent.
			populatedKeys: []string{"task"},
		},
		{
			name:      "ListTasksResponse",
			typ:       reflect.TypeOf(a2a.ListTasksResponse{}),
			zero:      &a2a.ListTasksResponse{},
			populated: &a2a.ListTasksResponse{Tasks: []a2a.Task{{ID: "fake-task-id", ContextID: "fake-context-id", Status: a2a.TaskStatus{State: a2a.TaskStateCompleted}}}, NextPageToken: "fake-token", PageSize: 10, TotalSize: 1},
			required:  []string{"tasks"},
			optional:  []string{"nextPageToken", "pageSize", "totalSize"},
		},
	}
}

// a2aJSONObject runs the real --json printer and decodes the result.
func a2aJSONObject(t *testing.T, v any) map[string]json.RawMessage {
	t.Helper()
	var buf bytes.Buffer
	require.NoError(t, a2aPrintJSON(&buf, v))

	var obj map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(buf.Bytes(), &obj))
	return obj
}

func TestA2AJSONKeySetsFollowTheProtocol(t *testing.T) {
	for _, sh := range a2aJSONShapes() {
		t.Run(sh.name+"/zero value", func(t *testing.T) {
			obj := a2aJSONObject(t, sh.zero)

			for _, key := range sh.required {
				_, ok := obj[key]
				assert.True(t, ok, "required member %q must always be present", key)
			}
			for _, key := range sh.optional {
				_, ok := obj[key]
				assert.False(t, ok, "optional member %q must be absent when unset (A2A v1.0)", key)
			}
		})

		t.Run(sh.name+"/populated", func(t *testing.T) {
			obj := a2aJSONObject(t, sh.populated)

			for _, key := range sh.required {
				_, ok := obj[key]
				assert.True(t, ok, "required member %q must always be present", key)
			}
			documented := append(append([]string{}, sh.required...), sh.optional...)
			for key := range obj {
				assert.Contains(t, documented, key, "unexpected member %q: add it to the documented shape deliberately", key)
			}
			populatedKeys := sh.populatedKeys
			if populatedKeys == nil {
				populatedKeys = sh.optional
			}
			for _, key := range populatedKeys {
				_, ok := obj[key]
				assert.True(t, ok, "optional member %q must be present once set", key)
			}
		})

		// The printer is a passthrough: it adds nothing and drops nothing.
		t.Run(sh.name+"/passthrough", func(t *testing.T) {
			var buf bytes.Buffer
			require.NoError(t, a2aPrintJSON(&buf, sh.populated))

			want, err := json.MarshalIndent(sh.populated, "", "  ")
			require.NoError(t, err)
			assert.Equal(t, string(want)+"\n", buf.String(),
				"a2a --json must print the protocol object verbatim, not a CLI projection")
		})
	}
}

// TestA2AJSONSendResponseIsPresenceUnion pins the reason SendMessageResponse
// must *not* get a no-omitempty projection: A2A has no `kind` discriminator
// here, so the presence of `task` vs `message` is the discriminator. Emitting
// both keys would describe two variants at once.
func TestA2AJSONSendResponseIsPresenceUnion(t *testing.T) {
	zero := a2aJSONObject(t, &a2a.SendMessageResponse{})
	assert.Empty(t, zero, "an empty SendMessageResponse has no variant members")

	taskOnly := a2aJSONObject(t, &a2a.SendMessageResponse{Task: &a2a.Task{ID: "fake-task-id"}})
	assert.Contains(t, taskOnly, "task")
	assert.NotContains(t, taskOnly, "message")

	messageOnly := a2aJSONObject(t, &a2a.SendMessageResponse{Message: &a2a.Message{MessageID: "fake-msg"}})
	assert.Contains(t, messageOnly, "message")
	assert.NotContains(t, messageOnly, "task")
}

// TestA2AJSONShapeCoversEverySDKField fails when the SDK type grows a field
// that the documented shape does not classify.
func TestA2AJSONShapeCoversEverySDKField(t *testing.T) {
	for _, sh := range a2aJSONShapes() {
		t.Run(sh.name, func(t *testing.T) {
			documented := append(append([]string{}, sh.required...), sh.optional...)
			sort.Strings(documented)
			assert.Equal(t, jsonFieldKeys(sh.typ), documented)
		})
	}
}
