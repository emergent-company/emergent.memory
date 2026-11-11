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

// TestNamespaceID_IsIdempotent is the #592 guard against unbounded nesting: a
// card action reports back the id the surface was rendered under, the model is
// told to "respond with updated surfaces for the same surfaceId" and echoes it,
// and the next turn scopes that echo. Without the guard each card click added a
// layer (runA.0:s1 → runB.0:runA.0:s1 → runC.0:runB.0:runA.0:s1), so the update
// addressed nothing on screen and appended a new card every turn.
func TestNamespaceID_IsIdempotent(t *testing.T) {
	const scoped = "run-a.0:s1"
	if got := NamespaceID("run-a.0", scoped); got != scoped {
		t.Fatalf("re-scoping %q to its own namespace = %q, want unchanged", scoped, got)
	}
	if got, want := NamespaceID("run-b.0", scoped), "run-b.0:s1"; got != want {
		t.Fatalf("NamespaceID(run-b.0, %q) = %q, want %q", scoped, got, want)
	}
	// Idempotence extends to nested input a client may still hold from before
	// the guard existed.
	if got, want := NamespaceID("run-c.0", "run-b.0:run-a.0:s1"), "run-c.0:s1"; got != want {
		t.Fatalf("nested input = %q, want %q", got, want)
	}
	// Namespacing twice in a row is a no-op the second time.
	once := NamespaceID("run-d.3", "s1")
	if twice := NamespaceID("run-d.3", once); twice != once {
		t.Fatalf("NamespaceID twice = %q, want %q", twice, once)
	}
}

// TestNamespaceID_KeepsModelAuthoredIDs guards the strict scope shape: an id the
// model invented that merely contains a ':' or a '.' is not a scope and must
// survive untouched.
func TestNamespaceID_KeepsModelAuthoredIDs(t *testing.T) {
	for _, id := range []string{"card:1", "my.card:v2", "s1", "a:b:c", "run.x:s1"} {
		if got, want := NamespaceID("run-1.0", id), "run-1.0:"+id; got != want {
			t.Fatalf("NamespaceID(%q) = %q, want %q", id, got, want)
		}
	}
}

func TestBareSurfaceID(t *testing.T) {
	tests := []struct{ in, want string }{
		{"s1", "s1"},
		{"run-1.0:s1", "s1"},
		{"run-b.0:run-a.0:s1", "s1"},
		{"card:1", "card:1"},
		{"my.card:v2", "my.card:v2"},
		{"", ""},
	}
	for _, tt := range tests {
		if got := BareSurfaceID(tt.in); got != tt.want {
			t.Fatalf("BareSurfaceID(%q) = %q, want %q", tt.in, got, tt.want)
		}
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
