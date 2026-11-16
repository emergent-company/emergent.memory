package graph

import (
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestResolveObjectNameIsExplicitOnly(t *testing.T) {
	explicit := "  Explicit title  "
	got := resolveObjectName(&explicit)
	assert.Equal(t, "Explicit title", *got)

	// properties.name and properties.title are ordinary properties. They are
	// never a title, so an omitted explicit name stays nameless (#824).
	assert.Nil(t, resolveObjectName(nil))
}

func TestResolveObjectNameEmptyExplicitClears(t *testing.T) {
	empty := "   "
	assert.Nil(t, resolveObjectName(&empty))
}

// machineKeyRegexp is every machine-key shape the display layer and migration
// 00311 treat as an identity, never a title: the current generated key
// (`<type>-<12 hex>`), the legacy generated key (`<type>-<8 hex>`), and the
// source-scoped dedup key (`<provider>:<uuid>:<external id>`). Keep the three
// alternatives in step with apps/web-ui/gateway/documents.go and
// apps/server/migrations/00311_object_name_title_only.sql.
var machineKeyRegexp = regexp.MustCompile(`^[a-z0-9_-]+-[0-9a-f]{12}$|^[a-z0-9_-]+-[0-9a-f]{8}$|^[a-z][a-z0-9_-]*:[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}:.+$`)

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

func TestMachineKeyRegexpShapes(t *testing.T) {
	for _, key := range []string{
		"task-0f9a8b7c6d5e",  // current 12-hex generated key
		"work-item-0f9a8b7c", // legacy 8-hex generated key
		"github:357590e8-6cbf-46bb-8c29-d8f839dd4ba8:emergent-company/emergent.memory#772",
		// A human key that ends in 8 hex characters (a date suffix is the real
		// case) is indistinguishable from a legacy minted key. It is treated as
		// machine on purpose: the object falls back to `type · #ref` rather than
		// risk printing an identity string as a title.
		"report-20251116",
	} {
		assert.Regexp(t, machineKeyRegexp, key, "must be recognised as a machine key")
	}
	for _, key := range []string{
		"Fix login redirect loop",                       // a human title, not a key
		"forskrift/1997-06-13-44#kapittel-1-paragraf-2", // a source identifier, not machine-shaped
		"task-0f9a8b7c6d5",                              // 11 hex: not a generated key
	} {
		assert.NotRegexp(t, machineKeyRegexp, key, "must not be treated as a machine key")
	}
}
