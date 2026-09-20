package cmd

import (
	"testing"

	"github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/a2a"
	"github.com/stretchr/testify/assert"
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
