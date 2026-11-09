package graph

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
)

func TestObjectRef(t *testing.T) {
	id := uuid.MustParse("9f1c0a2e-3b4d-7c8e-1f2a-9d0b6c4e8a71")
	assert.Equal(t, "#9d0b6c4e8a71", ObjectRef(id))
}

func TestObjectRefNil(t *testing.T) {
	assert.Equal(t, "", ObjectRef(uuid.Nil))
}

func TestObjectRefStableAcrossVersions(t *testing.T) {
	canonical := uuid.MustParse("9f1c0a2e-3b4d-7c8e-1f2a-9d0b6c4e8a71")
	version := uuid.New()
	assert.Equal(t, ObjectRef(canonical), ObjectRef(canonical))
	assert.NotEqual(t, ObjectRef(canonical), ObjectRef(version))
}

func TestToResponseCarriesNameAndRef(t *testing.T) {
	name := "Fix login redirect loop"
	obj := &GraphObject{
		ID:          uuid.New(),
		CanonicalID: uuid.MustParse("9f1c0a2e-3b4d-7c8e-1f2a-9d0b6c4e8a71"),
		Type:        "Task",
		Name:        &name,
	}
	resp := obj.ToResponse()
	assert.Equal(t, name, *resp.Name)
	assert.Equal(t, "#9d0b6c4e8a71", resp.Ref)
}
