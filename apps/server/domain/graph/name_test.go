package graph

import (
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

func TestAutoWorkKey(t *testing.T) {
	name := "Fix login redirect loop"
	key := autoWorkKey("Task", &name)
	assert.True(t, strings.HasPrefix(key, "fix-login-redirect-loop-"), key)
	parts := strings.Split(key, "-")
	assert.Len(t, parts[len(parts)-1], 8)

	key2 := autoWorkKey("Task", &name)
	assert.NotEqual(t, key, key2, "keys must not collide")
}

func TestAutoWorkKeyFallbacks(t *testing.T) {
	assert.True(t, strings.HasPrefix(autoWorkKey("Work Item", nil), "work-item-"))
	assert.True(t, strings.HasPrefix(autoWorkKey("", nil), "item-"))
}
