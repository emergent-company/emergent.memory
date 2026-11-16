package graph

import (
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestResolveObjectNamePrecedence(t *testing.T) {
	explicit := "  Explicit title  "
	got := resolveObjectName(&explicit, map[string]any{"name": "Prop name", "title": "Prop title"})
	assert.Equal(t, "Explicit title", *got)

	got = resolveObjectName(nil, map[string]any{"name": "Prop name", "title": "Prop title"})
	assert.Equal(t, "Prop name", *got)

	got = resolveObjectName(nil, map[string]any{"title": "Prop title"})
	assert.Equal(t, "Prop title", *got)

	assert.Nil(t, resolveObjectName(nil, map[string]any{}))
	assert.Nil(t, resolveObjectName(nil, nil))
}

func TestResolveObjectNameEmptyExplicitClears(t *testing.T) {
	empty := "   "
	assert.Nil(t, resolveObjectName(&empty, map[string]any{"name": "fallback"}))
}

// machineKeyRegexp is the shape the display layer treats as a machine key and
// never renders as a title (apps/web-ui/gateway/documents.go, migration 00280).
var machineKeyRegexp = regexp.MustCompile(`^[a-z0-9_-]+-[0-9a-f]{12}$`)

func TestAutoWorkKey(t *testing.T) {
	name := "Fix login redirect loop"
	key := autoWorkKey("Task", &name)
	assert.True(t, strings.HasPrefix(key, "fix-login-redirect-loop-"), key)
	parts := strings.Split(key, "-")
	assert.Len(t, parts[len(parts)-1], 12)
	assert.Regexp(t, machineKeyRegexp, key, "a minted key must be recognisable as a machine key")

	key2 := autoWorkKey("Task", &name)
	assert.NotEqual(t, key, key2, "keys must not collide")
}

func TestAutoWorkKeyFallbacks(t *testing.T) {
	for _, tc := range []struct {
		typeName string
		prefix   string
	}{
		{"Work Item", "work-item-"},
		{"", "item-"},
	} {
		key := autoWorkKey(tc.typeName, nil)
		assert.True(t, strings.HasPrefix(key, tc.prefix), key)
		assert.Regexp(t, machineKeyRegexp, key, "a minted key must be recognisable as a machine key")
	}
}
