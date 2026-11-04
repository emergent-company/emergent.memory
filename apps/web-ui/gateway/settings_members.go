package main

import (
	"github.com/labstack/echo/v4"
)

// Project Settings members management (/settings/members*) — the same member
// list/invite/detail/role/remove UI as the legacy top-level /members* pages,
// rendered inside the settings rail (ProjectMembersPage) and redirecting back
// to /settings/members. The handlers are thin wrappers over the shared
// membersSurface logic in org_members_ui.go; the surface only changes the base
// path (links + redirect targets) and the page shell.

func (s *Server) uiSettingsMembers(c echo.Context) error {
	return s.uiMembersFor(membersSettingsSurface, c)
}

func (s *Server) uiSettingsMemberInvitePage(c echo.Context) error {
	return s.uiMemberInvitePageFor(membersSettingsSurface, c)
}

func (s *Server) uiSettingsMemberInviteCreate(c echo.Context) error {
	return s.uiMemberInviteCreateFor(membersSettingsSurface, c)
}

func (s *Server) uiSettingsMemberDetails(c echo.Context) error {
	return s.uiMemberDetailsFor(membersSettingsSurface, c)
}

func (s *Server) uiSettingsRemoveMember(c echo.Context) error {
	return s.uiRemoveMemberFor(membersSettingsSurface, c)
}

func (s *Server) uiSettingsChangeMemberRole(c echo.Context) error {
	return s.uiChangeMemberRoleFor(membersSettingsSurface, c)
}
