package main

import "net/url"

// objectWorkControlTarget describes where the object page's workflow controls
// post and which context they must carry back. Preview controls swap the drawer
// body in place, so they carry the surface selector and the drawer's own query
// context (?actions=board, ?rel=) through the POST. Page controls redirect and
// preserve the optional ?from= origin trail.
type objectWorkControlTarget struct {
	ObjectID string
	Preview  bool
	Actions  string
	Rel      string
	From     string
}

// objectStatusPostsPath is the POST destination for the status control.
func (t objectWorkControlTarget) objectStatusPostsPath() string {
	return "/objects/" + url.PathEscape(t.ObjectID) + "/status"
}

// objectAssigneePostsPath is the POST destination for the assignee control.
func (t objectWorkControlTarget) objectAssigneePostsPath() string {
	return "/objects/" + url.PathEscape(t.ObjectID) + "/assignee"
}

// objectWorkControlDisabled reports whether a status control should render on
// the object surfaces. Only a type that declares a workflow gets the controls;
// the degraded free-text control (schema unavailable) has no dropdown options,
// so it is rendered as the plain header status badge instead.
func objectWorkControlDisabled(ctrl *objectStatusControl) bool {
	return ctrl == nil || !ctrl.Show || ctrl.Free
}
