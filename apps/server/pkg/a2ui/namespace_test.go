package a2ui

import "testing"

func TestNamespaceMessages_ScopesEverySurfaceID(t *testing.T) {
	msgs := []Message{
		{CreateSurface: &CreateSurface{SurfaceID: "s1", CatalogID: CatalogID}},
		{UpdateComponents: &UpdateComponents{SurfaceID: "s1"}},
		{UpdateDataModel: &UpdateDataModel{SurfaceID: "s1", Path: "/x"}},
		{DeleteSurface: &DeleteSurface{SurfaceID: "s1"}},
	}

	got := NamespaceMessages("run-1.2", msgs)
	for i, m := range got {
		if id := surfaceIDIn(m); id != "run-1.2:s1" {
			t.Fatalf("message %d surfaceId = %q, want run-1.2:s1", i, id)
		}
	}
	// The input is untouched: scoping is a rewrite of copies.
	for i, m := range msgs {
		if id := surfaceIDIn(m); id != "s1" {
			t.Fatalf("input message %d was rewritten to %q", i, id)
		}
	}
}

func TestNamespaceMessages_EmptyNamespaceIsIdentity(t *testing.T) {
	msgs := []Message{{CreateSurface: &CreateSurface{SurfaceID: "s1"}}}
	got := NamespaceMessages("", msgs)
	if len(got) != 1 || got[0].CreateSurface == nil || got[0].CreateSurface.SurfaceID != "s1" {
		t.Fatalf("NamespaceMessages(\"\") = %+v, want the input unchanged", got)
	}
}

func TestNamespaceID_DistinctNamespacesNeverCollide(t *testing.T) {
	if NamespaceID("run-1.0", "s1") == NamespaceID("run-2.0", "s1") {
		t.Fatal("two namespaces produced the same surface id")
	}
}

// surfaceIDIn returns whichever surfaceId the message carries.
func surfaceIDIn(m Message) string {
	switch {
	case m.CreateSurface != nil:
		return m.CreateSurface.SurfaceID
	case m.UpdateComponents != nil:
		return m.UpdateComponents.SurfaceID
	case m.UpdateDataModel != nil:
		return m.UpdateDataModel.SurfaceID
	case m.DeleteSurface != nil:
		return m.DeleteSurface.SurfaceID
	}
	return ""
}
