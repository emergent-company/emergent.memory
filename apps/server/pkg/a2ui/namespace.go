package a2ui

// Surface ids are the model's, and the prompt teaches every turn the same one
// ("s1"). A2UI requires a surfaceId to be unique for the renderer's lifetime —
// a second createSurface for an id that is still registered is an integrity
// error — so the server scopes every emitted surface to the run and step that
// produced it instead of trusting the model to invent unique ids (issue #592).
//
// Scoping is a pure rewrite of the ids inside the message list, so the
// progressive frames, the authoritative final frame and the persisted payload
// (which a reload replays) all carry the same identifier and a card action
// reports back the id the surface was rendered under.

// NamespaceID scopes a surface identifier to ns. An empty namespace, or an empty
// id, returns the id unchanged so a caller that does not scope (or a test) sees
// exactly what the model wrote.
func NamespaceID(ns, id string) string {
	if ns == "" || id == "" {
		return id
	}
	return ns + ":" + id
}

// NamespaceMessage returns m with every surfaceId it carries scoped to ns. The
// result shares no pointer member with m, so scoping never rewrites a message
// the caller still owns.
func NamespaceMessage(ns string, m Message) Message {
	if ns == "" {
		return m
	}
	if m.CreateSurface != nil {
		c := *m.CreateSurface
		c.SurfaceID = NamespaceID(ns, c.SurfaceID)
		m.CreateSurface = &c
	}
	if m.UpdateComponents != nil {
		u := *m.UpdateComponents
		u.SurfaceID = NamespaceID(ns, u.SurfaceID)
		m.UpdateComponents = &u
	}
	if m.UpdateDataModel != nil {
		u := *m.UpdateDataModel
		u.SurfaceID = NamespaceID(ns, u.SurfaceID)
		m.UpdateDataModel = &u
	}
	if m.DeleteSurface != nil {
		d := *m.DeleteSurface
		d.SurfaceID = NamespaceID(ns, d.SurfaceID)
		m.DeleteSurface = &d
	}
	return m
}

// NamespaceMessages returns a copy of msgs with every surfaceId scoped to ns. It
// returns msgs unchanged when ns is empty, so a caller can apply it
// unconditionally.
func NamespaceMessages(ns string, msgs []Message) []Message {
	if ns == "" || len(msgs) == 0 {
		return msgs
	}
	out := make([]Message, len(msgs))
	for i, m := range msgs {
		out[i] = NamespaceMessage(ns, m)
	}
	return out
}
