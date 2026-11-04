package cmd

import (
	"bytes"
	"encoding/json"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/agentdefinitions"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The `agent-definitions get/list --json` contracts (#2094). The bug was that
// the key set changed with the definition's state, so every test here compares
// a sparse object's keys with a fully populated object's keys against an
// explicitly written list — a full-object snapshot would not catch it.

// getJSONKeys decodes --json output into its top-level key set, sorted.
func getJSONKeys(t *testing.T, b []byte) []string {
	t.Helper()
	var obj map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(b, &obj), "output must be a JSON object: %s", b)
	keys := make([]string, 0, len(obj))
	for k := range obj {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// jsonFieldKeys returns the wire names of t's exported fields, taken from their
// `json` tags (omitempty stripped).
func jsonFieldKeys(t reflect.Type) []string {
	keys := make([]string, 0, t.NumField())
	for i := 0; i < t.NumField(); i++ {
		name, _, _ := strings.Cut(t.Field(i).Tag.Get("json"), ",")
		if name == "" || name == "-" {
			continue
		}
		keys = append(keys, name)
	}
	sort.Strings(keys)
	return keys
}

var wantAgentDefinitionJSONKeys = []string{
	"acpConfig", "autoLoadSkills", "config", "createdAt", "defaultTimeout",
	"description", "dispatchMode", "id", "isDefault", "maxSteps", "model",
	"name", "productId", "projectId", "skills", "systemPrompt", "tools",
	"uiConfig", "updatedAt", "visibility",
}

var wantAgentDefinitionSummaryJSONKeys = []string{
	"createdAt", "description", "id", "isDefault", "name", "projectId",
	"toolCount", "uiConfig", "updatedAt", "visibility",
}

// encodeAgentDefinition runs the real `get --json` encoder and returns the
// decoded object, so the assertions below test the command's output path and not
// a hand-built struct.
func encodeAgentDefinition(t *testing.T, d agentdefinitions.AgentDefinition) map[string]json.RawMessage {
	t.Helper()
	var buf bytes.Buffer
	require.NoError(t, printAgentDefinitionJSON(&buf, d))
	var obj map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(buf.Bytes(), &obj))
	return obj
}

func TestAgentDefinitionGetJSONKeySetIsStateIndependent(t *testing.T) {
	zero := agentdefinitions.AgentDefinition{}
	sparse := agentdefinitions.AgentDefinition{
		ID:         "11111111-1111-1111-1111-111111111111",
		ProjectID:  "22222222-2222-2222-2222-222222222222",
		Name:       "sparse-def",
		Visibility: "project",
	}

	desc := "does a thing"
	prompt := "you are a test fixture"
	product := "33333333-3333-3333-3333-333333333333"
	maxSteps := 7
	timeout := 30
	temp := float32(0.2)
	maxTokens := 1024
	thinking := true
	full := agentdefinitions.AgentDefinition{
		ID:             "44444444-4444-4444-4444-444444444444",
		ProductID:      &product,
		ProjectID:      "22222222-2222-2222-2222-222222222222",
		Name:           "full-def",
		Description:    &desc,
		SystemPrompt:   &prompt,
		Model:          &agentdefinitions.ModelConfig{Name: "fake-model-1", Temperature: &temp, MaxTokens: &maxTokens, EnableThinking: &thinking},
		Tools:          []string{"fake-tool-a", "fake-tool-b"},
		Skills:         []string{"fake-skill"},
		AutoLoadSkills: true,
		IsDefault:      true,
		MaxSteps:       &maxSteps,
		DefaultTimeout: &timeout,
		Visibility:     "external",
		DispatchMode:   "async",
		ACPConfig: &agentdefinitions.ACPConfig{
			DisplayName:  "Fake Display",
			Description:  "fake acp description",
			Capabilities: []string{"fake-cap"},
			InputModes:   []string{"text/plain"},
			OutputModes:  []string{"text/plain"},
		},
		Config:    map[string]any{"fakeKey": "fakeValue"},
		UIConfig:  json.RawMessage(`{"fakeUi":true}`),
		CreatedAt: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC),
		UpdatedAt: time.Date(2026, 1, 2, 3, 4, 6, 0, time.UTC),
	}

	zeroObj := encodeAgentDefinition(t, zero)
	sparseObj := encodeAgentDefinition(t, sparse)
	fullObj := encodeAgentDefinition(t, full)

	assert.Equal(t, wantAgentDefinitionJSONKeys, getJSONKeys(t, mustJSON(t, zeroObj)), "zero value")
	assert.Equal(t, wantAgentDefinitionJSONKeys, getJSONKeys(t, mustJSON(t, sparseObj)), "sparse definition")
	assert.Equal(t, wantAgentDefinitionJSONKeys, getJSONKeys(t, mustJSON(t, fullObj)), "fully populated definition")

	// Zero-value conventions, which consumers are told to rely on.
	for _, key := range []string{
		"productId", "description", "systemPrompt", "model", "maxSteps",
		"defaultTimeout", "acpConfig", "config", "uiConfig",
	} {
		assert.JSONEq(t, "null", string(sparseObj[key]), "%q must be null when unset, not absent", key)
	}
	assert.JSONEq(t, "[]", string(sparseObj["tools"]), "tools must be [] when unset, not null")
	assert.JSONEq(t, "[]", string(sparseObj["skills"]), "skills must be [] when unset, not null")
	assert.JSONEq(t, `""`, string(sparseObj["dispatchMode"]), "dispatchMode keeps its zero value")

	// Set values survive the projection unchanged.
	assert.JSONEq(t, `"does a thing"`, string(fullObj["description"]))
	assert.Contains(t, string(fullObj["model"]), `"fake-model-1"`)
	assert.JSONEq(t, `["fake-tool-a","fake-tool-b"]`, string(fullObj["tools"]))
	assert.JSONEq(t, `{"fakeKey":"fakeValue"}`, string(fullObj["config"]))
	assert.JSONEq(t, `{"fakeUi":true}`, string(fullObj["uiConfig"]))
	assert.Contains(t, string(fullObj["acpConfig"]), `"displayName":"Fake Display"`)

	// Every field, not just the spot checks above: the completeness test
	// reflects over the struct, so a field kept in the struct but dropped from
	// newAgentDefinitionJSON (e.g. `ProductID: d.ProductID`) would otherwise
	// ship as a permanent zero. Every field of `full` is non-zero, so each one
	// must survive the constructor unchanged.
	rt := reflect.TypeOf(full)
	for i := 0; i < rt.NumField(); i++ {
		wire, _, _ := strings.Cut(rt.Field(i).Tag.Get("json"), ",")
		if wire == "" || wire == "-" {
			continue
		}
		want, err := json.Marshal(reflect.ValueOf(full).Field(i).Interface())
		require.NoError(t, err)
		assert.JSONEq(t, string(want), string(fullObj[wire]),
			"newAgentDefinitionJSON must copy %s (%s)", rt.Field(i).Name, wire)
	}
}

// TestAgentDefinitionGetJSONRegressionReproduced pins *why* the projection
// exists: encoding the SDK type directly drops keys as soon as optional fields
// are unset. If someone replaces printAgentDefinitionJSON with enc.Encode(d),
// the key-set assertions above fail; this test documents the shape that was
// there before.
func TestAgentDefinitionGetJSONRegressionReproduced(t *testing.T) {
	b, err := json.Marshal(agentdefinitions.AgentDefinition{ID: "1", Name: "sparse"})
	require.NoError(t, err)
	var raw map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(b, &raw))

	for _, key := range []string{"description", "skills", "productId", "model", "config"} {
		_, present := raw[key]
		assert.False(t, present, "the SDK type drops %q when unset — that is the defect", key)
	}
}

func TestAgentDefinitionListJSONKeySetIsStateIndependent(t *testing.T) {
	desc := "listed"
	sparse := []agentdefinitions.AgentDefinitionSummary{
		{ID: "11111111-1111-1111-1111-111111111111", ProjectID: "p", Name: "sparse", Visibility: "project"},
	}
	full := []agentdefinitions.AgentDefinitionSummary{
		{
			ID:          "44444444-4444-4444-4444-444444444444",
			ProjectID:   "p",
			Name:        "full",
			Description: &desc,
			Visibility:  "external",
			IsDefault:   true,
			ToolCount:   3,
			UIConfig:    json.RawMessage(`{"fakeUi":true}`),
			CreatedAt:   time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC),
			UpdatedAt:   time.Date(2026, 1, 2, 3, 4, 6, 0, time.UTC),
		},
	}

	var sparseBuf, fullBuf bytes.Buffer
	require.NoError(t, printAgentDefinitionListJSON(&sparseBuf, sparse))
	require.NoError(t, printAgentDefinitionListJSON(&fullBuf, full))

	var sparseList, fullList []map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(sparseBuf.Bytes(), &sparseList))
	require.NoError(t, json.Unmarshal(fullBuf.Bytes(), &fullList))
	require.Len(t, sparseList, 1)
	require.Len(t, fullList, 1)

	assert.Equal(t, wantAgentDefinitionSummaryJSONKeys, getJSONKeys(t, mustJSON(t, sparseList[0])))
	assert.Equal(t, wantAgentDefinitionSummaryJSONKeys, getJSONKeys(t, mustJSON(t, fullList[0])))

	assert.JSONEq(t, "null", string(sparseList[0]["description"]))
	assert.JSONEq(t, "null", string(sparseList[0]["uiConfig"]))
	assert.JSONEq(t, `"listed"`, string(fullList[0]["description"]))
	assert.JSONEq(t, "3", string(fullList[0]["toolCount"]))

	// An empty list stays [] rather than null.
	var emptyBuf bytes.Buffer
	require.NoError(t, printAgentDefinitionListJSON(&emptyBuf, nil))
	assert.JSONEq(t, "[]", emptyBuf.String())
}

func TestAgentDefinitionJSONNestedShapesAreStable(t *testing.T) {
	zero := agentdefinitions.AgentDefinition{
		Model:     &agentdefinitions.ModelConfig{},
		ACPConfig: &agentdefinitions.ACPConfig{},
	}
	obj := encodeAgentDefinition(t, zero)

	var model map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(obj["model"], &model))
	assert.Equal(t,
		[]string{"enableThinking", "maxTokens", "name", "temperature"},
		getJSONKeys(t, mustJSON(t, model)),
		"model has a fixed key set")
	for _, key := range []string{"temperature", "maxTokens", "enableThinking"} {
		assert.JSONEq(t, "null", string(model[key]), "%q must be null when unset", key)
	}

	var acp map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(obj["acpConfig"], &acp))
	assert.Equal(t,
		[]string{"capabilities", "description", "displayName", "inputModes", "outputModes"},
		getJSONKeys(t, mustJSON(t, acp)),
		"acpConfig has a fixed key set")
	for _, key := range []string{"capabilities", "inputModes", "outputModes"} {
		assert.JSONEq(t, "[]", string(acp[key]), "%q must be [] when unset, not null", key)
	}
}

// TestAgentDefinitionJSONCoversEverySDKField makes a new SDK field a test
// failure instead of a silent omission from --json. The projection must name
// exactly the fields the SDK type exposes.
func TestAgentDefinitionJSONCoversEverySDKField(t *testing.T) {
	assert.Equal(t,
		jsonFieldKeys(reflect.TypeOf(agentdefinitions.AgentDefinition{})),
		jsonFieldKeys(reflect.TypeOf(agentDefinitionJSON{})),
		"agentDefinitionJSON must cover every agentdefinitions.AgentDefinition field")
	assert.Equal(t,
		jsonFieldKeys(reflect.TypeOf(agentdefinitions.AgentDefinitionSummary{})),
		jsonFieldKeys(reflect.TypeOf(agentDefinitionSummaryJSON{})),
		"agentDefinitionSummaryJSON must cover every agentdefinitions.AgentDefinitionSummary field")
	assert.Equal(t,
		jsonFieldKeys(reflect.TypeOf(agentdefinitions.ModelConfig{})),
		jsonFieldKeys(reflect.TypeOf(agentDefinitionModelJSON{})),
		"agentDefinitionModelJSON must cover every agentdefinitions.ModelConfig field")
	assert.Equal(t,
		jsonFieldKeys(reflect.TypeOf(agentdefinitions.ACPConfig{})),
		jsonFieldKeys(reflect.TypeOf(agentDefinitionACPJSON{})),
		"agentDefinitionACPJSON must cover every agentdefinitions.ACPConfig field")
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	require.NoError(t, err)
	return b
}
