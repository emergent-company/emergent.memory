package agents

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	require.NoError(t, err)
	return string(b)
}

// mustJSONT is mustJSON for a require.TestingT, so helpers like
// assertSingleMember can accept a recording stub in addition to *testing.T.
func mustJSONT(t require.TestingT, v any) string {
	if h, ok := t.(interface{ Helper() }); ok {
		h.Helper()
	}
	b, err := json.Marshal(v)
	require.NoError(t, err)
	return string(b)
}

func assertNotContains(t *testing.T, s string, substrs ...string) {
	t.Helper()
	for _, sub := range substrs {
		assert.NotContains(t, s, sub)
	}
}

func assertContains(t *testing.T, s string, substrs ...string) {
	t.Helper()
	for _, sub := range substrs {
		assert.Contains(t, s, sub)
	}
}

func TestA2ADTO_AgentCard_CamelCaseKeys(t *testing.T) {
	card := AgentCard{
		Name:        "Memory",
		Description: "desc",
		Version:     "1.0.0",
		Provider: &AgentProvider{
			Organization: "Emergent",
			URL:          "https://example.com",
		},
		SupportedInterfaces: []AgentInterface{
			{URL: "u", ProtocolBinding: "HTTP+JSON", ProtocolVersion: "1.0"},
		},
		Capabilities: A2AAgentCapabilities{
			Streaming:         true,
			ExtendedAgentCard: true,
		},
		SecuritySchemes: map[string]SecurityScheme{
			"memoryApiToken": {HTTPAuth: &HTTPAuthSecurityScheme{Scheme: "Bearer", BearerFormat: "emt"}},
		},
		SecurityRequirements: []SecurityRequirement{
			{Schemes: map[string]SecuritySchemeReference{"memoryApiToken": {List: []string{}}}},
		},
		DefaultInputModes:  []string{"text/plain"},
		DefaultOutputModes: []string{"text/plain"},
		Skills:             []AgentSkill{},
	}

	j := mustJSON(t, card)
	assertContains(t, j,
		`"name"`, `"description"`, `"version"`, `"provider"`,
		`"supportedInterfaces"`, `"protocolBinding"`, `"protocolVersion"`,
		`"capabilities"`, `"extendedAgentCard"`, `"streaming"`,
		`"securitySchemes"`, `"httpAuthSecurityScheme"`, `"bearerFormat"`,
		`"securityRequirements"`, `"schemes"`, `"list"`,
		`"defaultInputModes"`, `"defaultOutputModes"`, `"skills"`,
	)
	assertNotContains(t, j, `"protocol_binding"`, `"supported_interfaces"`, `"default_input_modes"`)
}

func TestA2ADTO_TaskState_EnumSpelling(t *testing.T) {
	cases := map[TaskState]string{
		TaskStateUnspecified:   `"TASK_STATE_UNSPECIFIED"`,
		TaskStateSubmitted:     `"TASK_STATE_SUBMITTED"`,
		TaskStateWorking:       `"TASK_STATE_WORKING"`,
		TaskStateCompleted:     `"TASK_STATE_COMPLETED"`,
		TaskStateFailed:        `"TASK_STATE_FAILED"`,
		TaskStateCanceled:      `"TASK_STATE_CANCELED"`,
		TaskStateInputRequired: `"TASK_STATE_INPUT_REQUIRED"`,
		TaskStateRejected:      `"TASK_STATE_REJECTED"`,
		TaskStateAuthRequired:  `"TASK_STATE_AUTH_REQUIRED"`,
	}
	for state, want := range cases {
		ts := TaskStatus{State: state}
		j := mustJSON(t, ts)
		assert.Contains(t, j, want)
	}
}

func TestA2ADTO_Role_EnumSpelling(t *testing.T) {
	cases := map[Role]string{
		RoleUnspecified: `"ROLE_UNSPECIFIED"`,
		RoleUser:        `"ROLE_USER"`,
		RoleAgent:       `"ROLE_AGENT"`,
	}
	for role, want := range cases {
		m := Message{Role: role}
		j := mustJSON(t, m)
		assert.Contains(t, j, want)
	}
}

func TestA2ADTO_Part_SerializesExactlyOneVariant_NoKind(t *testing.T) {
	// Text variant: only "text" is emitted; no "kind", "raw", "url", "data".
	p := Part{Text: strPtr("hello")}
	j := mustJSON(t, p)
	var m map[string]any
	require.NoError(t, json.Unmarshal([]byte(j), &m))
	assert.Len(t, m, 1, "text part must serialize exactly one member, got: %s", j)
	assert.Equal(t, "hello", m["text"])
	assertNotContains(t, j, `"kind"`, `"raw"`, `"url"`, `"data"`)

	// Data variant: only "data" is emitted.
	d := Part{Data: map[string]any{"toolName": "search", "input": map[string]any{"q": "x"}}}
	jd := mustJSON(t, d)
	var dm map[string]any
	require.NoError(t, json.Unmarshal([]byte(jd), &dm))
	assert.Len(t, dm, 1, "data part must serialize exactly one member, got: %s", jd)
	_, ok := dm["data"]
	assert.True(t, ok)
	assertNotContains(t, jd, `"kind"`, `"text"`, `"raw"`, `"url"`)
}

func TestA2ADTO_StreamResponse_NeverEmitsKind(t *testing.T) {
	// Each variant must serialize its member only; "kind" must never appear.
	task := StreamResponse{Task: &Task{ID: "t1"}}
	j := mustJSON(t, task)
	assertContains(t, j, `"task"`)
	assertNotContains(t, j, `"kind"`, `"message"`, `"statusUpdate"`, `"artifactUpdate"`)

	msg := StreamResponse{Message: &Message{MessageID: "m1"}}
	j = mustJSON(t, msg)
	assertContains(t, j, `"message"`)
	assertNotContains(t, j, `"kind"`, `"task"`, `"statusUpdate"`, `"artifactUpdate"`)

	status := StreamResponse{StatusUpdate: &TaskStatusUpdateEvent{TaskID: "t1"}}
	j = mustJSON(t, status)
	assertContains(t, j, `"statusUpdate"`)
	assertNotContains(t, j, `"kind"`, `"task"`, `"message"`, `"artifactUpdate"`)

	artifact := StreamResponse{ArtifactUpdate: &TaskArtifactUpdateEvent{TaskID: "t1"}}
	j = mustJSON(t, artifact)
	assertContains(t, j, `"artifactUpdate"`)
	assertNotContains(t, j, `"kind"`, `"task"`, `"message"`, `"statusUpdate"`)
}

func TestA2ADTO_MessageAndTask_CamelCaseKeys(t *testing.T) {
	msg := Message{
		MessageID:        "m1",
		ContextID:        "c1",
		TaskID:           "t1",
		Role:             RoleUser,
		Parts:            []Part{TextPart("hi")},
		Metadata:         map[string]any{"k": "v"},
		Extensions:       []string{"x"},
		ReferenceTaskIDs: []string{"r1"},
	}
	j := mustJSON(t, msg)
	assertContains(t, j, `"messageId"`, `"contextId"`, `"taskId"`, `"role"`, `"parts"`, `"referenceTaskIds"`)
	assertNotContains(t, j, `"message_id"`, `"context_id"`, `"task_id"`)

	task := Task{
		ID:        "t1",
		ContextID: "c1",
		Status:    TaskStatus{State: TaskStateWorking},
		Artifacts: []Artifact{{ArtifactID: "a1", Parts: []Part{TextPart("x")}}},
	}
	jt := mustJSON(t, task)
	assertContains(t, jt, `"contextId"`, `"artifactId"`, `"state"`)
	assertNotContains(t, jt, `"context_id"`, `"artifact_id"`)
}

func TestA2ADTO_SendMessageResponse_Union(t *testing.T) {
	r := SendMessageResponse{Task: &Task{ID: "t1"}}
	j := mustJSON(t, r)
	assertContains(t, j, `"task"`)
	assertNotContains(t, j, `"message"`)
}

func TestA2ADTO_ListTasksRequest_QueryTags(t *testing.T) {
	// Query-bound fields carry `query` tags, not JSON tags.
	j := mustJSON(t, ListTasksRequest{ContextID: "c", Status: "working", PageSize: 10})
	assertNotContains(t, j, `"contextId"`)
	assert.False(t, strings.Contains(j, "contextId"))
}
