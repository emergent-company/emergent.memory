package a2ui

import "strings"

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
//
// Scoping is idempotent: an id that already carries one or more scopes is
// reduced to its bare form before the new one is added. A card action quotes the
// surface id back to the model (see A2UIActionMessage), and a model that echoes
// it must not grow a new scope layer per turn — without the guard the id nests
// without bound (`runA.0:s1` → `runB.0:runA.0:s1` → …), the follow-up addresses
// nothing that is on screen, and the id leaks into the persisted user turn.

// NamespaceID scopes a surface identifier to ns. An empty namespace, or an empty
// id, returns the id unchanged so a caller that does not scope (or a test) sees
// exactly what the model wrote.
func NamespaceID(ns, id string) string {
	if ns == "" || id == "" {
		return id
	}
	return ns + ":" + BareSurfaceID(id)
}

// BareSurfaceID returns id with every surface scope this package applies
// stripped. An id that carries no scope is returned unchanged, so callers can
// apply it unconditionally — most importantly the card-action framing, which
// must show the model its own id rather than a run UUID.
func BareSurfaceID(id string) string {
	for {
		rest, ok := cutScopePrefix(id)
		if !ok {
			return id
		}
		id = rest
	}
}

// cutScopePrefix splits one "<scope>:<id>" pair, where a scope is the
// "<run>.<step>" form a2uiNamespace produces: a run token without '.', ':' or
// whitespace, a decimal step, then the separator. The strict shape keeps a
// model-authored id such as "card:1" or "my.card:v2" intact.
func cutScopePrefix(id string) (string, bool) {
	colon := strings.IndexByte(id, ':')
	if colon < 0 {
		return "", false
	}
	scope := id[:colon]
	dot := strings.LastIndexByte(scope, '.')
	if dot <= 0 || dot == len(scope)-1 {
		return "", false
	}
	for _, r := range scope[:dot] {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
		default:
			return "", false
		}
	}
	for _, r := range scope[dot+1:] {
		if r < '0' || r > '9' {
			return "", false
		}
	}
	rest := id[colon+1:]
	if rest == "" {
		return "", false
	}
	return rest, true
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
