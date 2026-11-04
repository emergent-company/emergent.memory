package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/labstack/echo/v4"
)

// Bulk actions on a selection of graph objects. The table keeps its selection in
// Alpine and every bulk dialog posts the picked ids as one CSV field to this one
// endpoint, discriminating on `action`, so the browser needs a single route
// rather than one per action.
const (
	// objectsBulkMaxIDs bounds a single request. A selection can only hold rows
	// the client has loaded (25 per page), so the cap guards a hand-made POST
	// rather than a real limit.
	objectsBulkMaxIDs = 500
	// objectsBulkStatusChunk matches the server's per-call cap on
	// BulkUpdateStatusRequest (validate:"max=100").
	objectsBulkStatusChunk = 100
)

// uiObjectsBulk runs one bulk action over the posted ids and redirects back to
// the page the selection was made on (carrying its filters), so the browser
// reloads the same view with the change applied. Partial failures are reported
// in the flash rather than as an error: an action that changed 3 of 5 objects
// did change 3 of 5, and hiding that behind an error page would lose the work.
func (s *Server) uiObjectsBulk(c echo.Context) error {
	ctx := c.Request().Context()
	if err := c.Request().ParseForm(); err != nil {
		return redirectWithError(c, "/objects", err)
	}
	back := objectsBulkReturnPath(c.FormValue("back"))
	ids := parseBulkObjectIDs(c.FormValue("ids"))
	if len(ids) == 0 {
		return redirectWithError(c, back, errors.New("select at least one object"))
	}

	action := strings.TrimSpace(c.FormValue("action"))
	// branch is the branch the page was filtered to, when it was. Delete needs it:
	// without a branch the endpoint tombstones the MAIN head, so deleting from a
	// branch-scoped view would remove the object from main and leave it on the
	// branch the user is looking at. Labels and status key on the object's
	// physical id and land on the right version either way.
	branch := strings.TrimSpace(c.FormValue("branch"))
	var (
		applied, failed int
		err             error
	)
	switch action {
	case "label_add", "label_remove":
		label := strings.TrimSpace(c.FormValue("label"))
		if label == "" {
			return redirectWithError(c, back, errors.New("label is required"))
		}
		applied, failed, err = s.bulkObjectLabels(ctx, ids, label, action == "label_add")
	case "status":
		status := strings.TrimSpace(c.FormValue("status"))
		if status == "" {
			return redirectWithError(c, back, errors.New("status is required"))
		}
		applied, failed, err = s.bulkObjectStatus(ctx, ids, status)
	case "delete":
		applied, failed, err = s.bulkObjectDelete(ctx, ids, branch)
	default:
		return redirectWithError(c, back, fmt.Errorf("unknown bulk action %q", action))
	}
	if err != nil {
		// Nothing changed and the cause is known (upstream outage, rejected
		// status): say why instead of "0 of N".
		return redirectWithError(c, back, err)
	}
	if (action == "label_add" || action == "label_remove") && applied > 0 {
		// The create form's label suggestions are cached for labelSuggestionsTTL;
		// a bulk label write must show up there immediately (#1504), exactly as a
		// single-object label edit does.
		s.invalidateObjectLabelSuggestions(ctx)
	}

	q := url.Values{}
	q.Set("bulk", action)
	q.Set("bulk_n", strconv.Itoa(applied))
	if failed > 0 {
		q.Set("bulk_f", strconv.Itoa(failed))
	}
	return c.Redirect(http.StatusSeeOther, objectsAppendQuery(back, q))
}

// bulkObjectLabels adds or removes one label across the selection. It reads each
// object first because the update is a whole-label-set replacement: sending only
// the new label would drop every other label the object already carries.
// Objects already in the requested state are skipped (neither applied nor
// failed), so the flash reports work actually done.
func (s *Server) bulkObjectLabels(ctx context.Context, ids []string, label string, add bool) (applied, failed int, err error) {
	var firstErr error
	for _, id := range ids {
		obj, getErr := s.memory.GetGraphObject(ctx, id)
		if getErr != nil {
			failed++
			if firstErr == nil {
				firstErr = getErr
			}
			continue
		}
		labels, changed := toggleObjectLabel(obj.Labels, label, add)
		if !changed {
			continue
		}
		if _, updErr := s.memory.UpdateObject(ctx, id, &UpdateObjectRequest{
			Labels:        labels,
			ReplaceLabels: true,
		}); updErr != nil {
			failed++
			if firstErr == nil {
				firstErr = updErr
			}
			continue
		}
		applied++
	}
	if applied == 0 && firstErr != nil {
		return 0, failed, firstErr
	}
	return applied, failed, nil
}

// toggleObjectLabel returns labels with label added or removed, plus whether
// anything changed. It never mutates the slice it is given: the caller's label
// list belongs to the fetched object.
func toggleObjectLabel(labels []string, label string, add bool) ([]string, bool) {
	for _, l := range labels {
		if l != label {
			continue
		}
		if add {
			return labels, false
		}
		out := make([]string, 0, len(labels))
		for _, keep := range labels {
			if keep != label {
				out = append(out, keep)
			}
		}
		return out, true
	}
	if !add {
		return labels, false
	}
	return append(append([]string{}, labels...), label), true
}

// bulkObjectStatus sets one status across the selection through the server's
// bulk endpoint, chunked to its per-request cap.
func (s *Server) bulkObjectStatus(ctx context.Context, ids []string, status string) (applied, failed int, err error) {
	var firstErr error
	for start := 0; start < len(ids); start += objectsBulkStatusChunk {
		end := min(start+objectsBulkStatusChunk, len(ids))
		resp, bulkErr := s.memory.BulkUpdateStatus(ctx, ids[start:end], status)
		if bulkErr != nil {
			failed += end - start
			if firstErr == nil {
				firstErr = bulkErr
			}
			continue
		}
		applied += resp.Success
		failed += resp.Failed
		if resp.Failed > 0 && firstErr == nil {
			for _, r := range resp.Results {
				if !r.Success && r.Error != nil {
					firstErr = errors.New(*r.Error)
					break
				}
			}
		}
	}
	if applied == 0 && firstErr != nil {
		return 0, failed, firstErr
	}
	return applied, failed, nil
}

// bulkObjectDelete soft-deletes the selection one object at a time: the graph
// delete endpoint takes a path id plus the branch to resolve it on, and branch
// history keeps the versions, so a mistaken delete is restorable from its branch.
//
// branchID is the page's branch filter and is forwarded for exactly that reason:
// the endpoint deletes the MAIN head when it is omitted, which is the wrong
// branch whenever the user is looking at a branch (and would leave the object in
// the list they just acted on).
func (s *Server) bulkObjectDelete(ctx context.Context, ids []string, branchID string) (applied, failed int, err error) {
	var firstErr error
	for _, id := range ids {
		if delErr := s.memory.DeleteGraphObject(ctx, id, branchID); delErr != nil {
			failed++
			if firstErr == nil {
				firstErr = delErr
			}
			continue
		}
		applied++
	}
	if applied == 0 && firstErr != nil {
		return 0, failed, firstErr
	}
	return applied, failed, nil
}

// parseBulkObjectIDs splits the CSV `ids` field the selection posts, trims,
// de-duplicates (a row can be selected twice across appended pages), and caps
// the request size.
func parseBulkObjectIDs(raw string) []string {
	var out []string
	seen := map[string]bool{}
	for _, part := range strings.Split(raw, ",") {
		id := strings.TrimSpace(part)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
		if len(out) == objectsBulkMaxIDs {
			break
		}
	}
	return out
}

// objectsBulkReturnPath validates the `back` field the dialogs carry: the page
// the selection was made on, so the redirect lands on the same filtered view.
// Anything that is not this app's objects page (a host, a scheme, another path)
// falls back to /objects instead of becoming an open redirect.
func objectsBulkReturnPath(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.IsAbs() || u.Host != "" || strings.HasPrefix(u.Path, "//") {
		return "/objects"
	}
	switch {
	case u.Path == "/objects":
	case strings.HasPrefix(u.Path, "/agents/") && strings.HasSuffix(u.Path, "/objects"):
	default:
		return "/objects"
	}
	// Drop the params this handler writes, so a page reloaded from a flash URL
	// does not re-show a stale toast.
	q := u.Query()
	for _, key := range []string{"bulk", "bulk_n", "bulk_f", "err"} {
		q.Del(key)
	}
	if enc := q.Encode(); enc != "" {
		return u.Path + "?" + enc
	}
	return u.Path
}

// objectsAppendQuery adds q to path, keeping any query it already carries.
func objectsAppendQuery(path string, q url.Values) string {
	enc := q.Encode()
	if enc == "" {
		return path
	}
	if strings.Contains(path, "?") {
		return path + "&" + enc
	}
	return path + "?" + enc
}

// objectsBulkFlash turns the PRG query params uiObjectsBulk writes into the
// success toast the redirected page shows.
func objectsBulkFlash(c echo.Context) string {
	action := c.QueryParam("bulk")
	if action == "" {
		return ""
	}
	n, _ := strconv.Atoi(c.QueryParam("bulk_n"))
	failed, _ := strconv.Atoi(c.QueryParam("bulk_f"))
	if n == 0 && failed == 0 {
		switch action {
		case "label_add":
			return "Every selected object already had that label."
		case "label_remove":
			return "No selected object had that label."
		default:
			return "Nothing changed."
		}
	}
	var verb string
	switch action {
	case "label_add":
		verb = "Labelled"
	case "label_remove":
		verb = "Removed the label from"
	case "status":
		verb = "Set the status on"
	case "delete":
		verb = "Deleted"
	default:
		return ""
	}
	msg := verb + " " + countLabel(n, "object", "objects") + "."
	if failed > 0 {
		msg += " " + countLabel(failed, "object failed", "objects failed") + "."
	}
	return msg
}
