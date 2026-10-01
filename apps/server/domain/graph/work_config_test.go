package graph

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBoardEnabledTypeNamesFromRaw(t *testing.T) {
	// Array format.
	arr := `[{"name":"ResearchRequest","boardEnabled":true},{"name":"Note","boardEnabled":false}]`
	require.ElementsMatch(t, []string{"ResearchRequest"}, boardEnabledTypeNamesFromRaw(json.RawMessage(arr)))

	// Map format.
	m := `{"ResearchRequest":{"boardEnabled":true},"Note":{"boardEnabled":false}}`
	require.ElementsMatch(t, []string{"ResearchRequest"}, boardEnabledTypeNamesFromRaw(json.RawMessage(m)))

	// No board-enabled types.
	none := `[{"name":"Note","boardEnabled":false}]`
	require.Empty(t, boardEnabledTypeNamesFromRaw(json.RawMessage(none)))
}
