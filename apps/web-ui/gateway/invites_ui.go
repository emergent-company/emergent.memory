package main

import (
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/labstack/echo/v4"
)

// inviteAcceptPageData is the payload for InviteAcceptPage, the browser landing
// for an email invite link (/invites/accept?token=…).
//
// The token is resolved against the caller's OWN pending invitations
// (GET /api/invites/pending), never looked up globally: that both proves the
// invitation is addressed to the signed-in account and keeps an invite sent to
// someone else from leaking its project, role or sender. When the token is not
// in that list the page degrades to an unavailable state (expired, already
// accepted, or another account's invite).
type inviteAcceptPageData struct {
	Invite        *PendingInviteDto // nil when the token is not a pending invite of this account
	Token         string            // raw token from the URL, echoed into the account-switch link
	SignedInEmail string            // session email, for the "signed in as" line and wrong-account copy
	Unavailable   bool              // token is not (or no longer) a pending invite of this account
	WrongAccount  bool              // accept was refused with 403: this invite belongs to another email
	LoadErr       error             // pending-invite fetch failed — renders the error state, not "unavailable"
}

// inviteAcceptPath returns where a successful accept sends the browser: the
// accepted project's agents page when the invite was project-scoped (so the
// invitee lands inside the thing they just joined, with its scope active),
// otherwise the organization list.
func inviteAcceptPath(orgID, projectID string) string {
	if p := projectScopeURL(orgID, projectID); p != "" {
		return p + "/agents"
	}
	if orgID != "" {
		return "/o/" + url.PathEscape(orgID)
	}
	return "/orgs"
}

// inviteAcceptLanding is inviteAcceptPath plus the accept flash, for the
// redirect after the landing page's own accept form.
func inviteAcceptLanding(orgID, projectID string) string {
	return acceptedFlashPath(inviteAcceptPath(orgID, projectID))
}

// acceptedFlashPath appends the ?accepted=1 PRG flash to a local path.
func acceptedFlashPath(p string) string {
	sep := "?"
	if strings.Contains(p, "?") {
		sep = "&"
	}
	return p + sep + "accepted=1"
}

// inviteCountLabel renders the pending-invitation count for the switcher badge.
func inviteCountLabel(n int) string {
	return strconv.Itoa(n)
}

// inviteTargetLabel is the invitation's subject: the project name for a
// project-scoped invite, "organization invitation" for an org-only one (which
// has no project name to show).
func inviteTargetLabel(inv *PendingInviteDto) string {
	if inv == nil {
		return "Invitation"
	}
	if strings.TrimSpace(inv.ProjectName) != "" {
		return inv.ProjectName
	}
	return "Organization invitation"
}

// inviteUnavailableTitle is the headline for the two non-happy states.
func inviteUnavailableTitle(data inviteAcceptPageData) string {
	if data.WrongAccount {
		return "This invitation is for another account"
	}
	return "Invitation unavailable"
}

// inviteUnavailableCopy explains the non-happy states without guessing which
// one applies (the backend answers 403 for "not your invite" and 404 for
// "unknown/expired/already accepted"; the last can only be inferred).
func inviteUnavailableCopy(data inviteAcceptPageData) string {
	who := "this account"
	if strings.TrimSpace(data.SignedInEmail) != "" {
		who = data.SignedInEmail
	}
	if data.WrongAccount {
		return "You're signed in as " + who + ". This invitation was sent to a different email address, so it can't be accepted in this session. Sign in with the account that received it."
	}
	return "This invitation isn't available for " + who + ". It may have expired, already have been accepted, or have been sent to a different email address."
}

// invitationReturnTo is the local path back to the accept page for a token,
// used as return_to when the invitee switches accounts.
func invitationReturnTo(token string) string {
	if strings.TrimSpace(token) == "" {
		return "/invites/accept"
	}
	return "/invites/accept?token=" + url.QueryEscape(token)
}

// pendingInvitePickerMeta is the compact second line under an invitation in the
// project switcher: the organization (or "Organization invitation") and the
// role offered. The verbose "invited 3 days ago · expires …" line belongs to
// the dedicated invitations page, not the switcher.
func pendingInvitePickerMeta(inv PendingInviteDto) string {
	org := strings.TrimSpace(inv.OrganizationName)
	if org == "" {
		org = "Organization invitation"
	}
	if role := roleLabel(inv.Role); role != "" {
		return org + " · " + role
	}
	return org
}

// pendingInviteSectionLabel is the switcher section header for pending
// invitations.
func pendingInviteSectionLabel(n int) string {
	if n == 1 {
		return "1 pending invitation"
	}
	return inviteCountLabel(n) + " pending invitations"
}

// pendingInviteBadgeTitle is the switcher badge's accessible label.
func pendingInviteBadgeTitle(n int) string {
	if n == 1 {
		return "1 pending invitation"
	}
	return inviteCountLabel(n) + " pending invitations"
}

// uiInviteAccept renders the email-invite landing (GET /invites/accept).
// Anonymous visitors never reach it: requireSession 302s to
// /auth/login?return_to=<this URL>, so the token survives the sign-in round
// trip.
func (s *Server) uiInviteAccept(c echo.Context) error {
	ctx := c.Request().Context()
	data := inviteAcceptPageData{Token: strings.TrimSpace(c.QueryParam("token"))}
	if sc, ok := sessionContextFrom(ctx); ok {
		data.SignedInEmail = sc.Email
	}
	// ?denied=1 is the accept handler's hand-off when the backend answered 403:
	// the invitation exists but belongs to a different email than the one
	// signed in here. The token is therefore absent from this account's pending
	// list, and the page explains which case it is.
	data.WrongAccount = c.QueryParam("denied") != ""
	if data.Token != "" {
		invites, err := s.memory.ListPendingInvites(ctx)
		if err != nil {
			data.LoadErr = userFacingError(err)
		} else {
			for i := range invites {
				if invites[i].Token == data.Token {
					data.Invite = &invites[i]
					break
				}
			}
			data.Unavailable = data.Invite == nil
		}
	} else {
		data.Unavailable = true
	}
	return s.page(c, pageTitle("Invitation"), InviteAcceptPage(data))
}

// uiInviteAcceptSubmit accepts an invitation from the landing page (POST
// /invites/accept, form-encoded). A 403 means the invitation belongs to a
// different email than the signed-in account, which is rendered as its own
// state (with an account-switch link) rather than a generic failure; a 404
// covers expired/already-accepted. Anything else is an upstream failure.
func (s *Server) uiInviteAcceptSubmit(c echo.Context) error {
	ctx := c.Request().Context()
	token := strings.TrimSpace(c.FormValue("token"))
	data := inviteAcceptPageData{Token: token}
	if sc, ok := sessionContextFrom(ctx); ok {
		data.SignedInEmail = sc.Email
	}
	if token == "" {
		return redirectWithError(c, "/orgs", errors.New("invitation link is missing its token"))
	}

	if err := s.memory.AcceptInvite(ctx, token); err != nil {
		switch {
		case isMemoryStatus(err, http.StatusForbidden):
			data.Unavailable = true
			data.WrongAccount = true
		case isMemoryStatus(err, http.StatusNotFound), isMemoryStatus(err, http.StatusGone):
			data.Unavailable = true
		default:
			data.LoadErr = userFacingError(err)
		}
		return s.page(c, pageTitle("Invitation"), InviteAcceptPage(data))
	}

	return c.Redirect(http.StatusSeeOther, inviteAcceptLanding(
		strings.TrimSpace(c.FormValue("orgId")),
		strings.TrimSpace(c.FormValue("projectId")),
	))
}
