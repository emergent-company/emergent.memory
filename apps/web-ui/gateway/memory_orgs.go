package main

import (
	"context"
	"net/http"
	"net/url"
	"time"
)

// --- orgs & projects (user tenancy) ---

// Org is one org in a signed-in user's tenancy (GET /api/orgs).
type Org struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// ProjectRef is the minimal {id, name, orgId} project shape of the tenancy
// endpoints (GET/POST /api/projects). The project-record routes (settings.go)
// use the richer Project DTO.
type ProjectRef struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	OrgID string `json:"orgId"`
	// DeletionStatus/DeletionScheduledFor describe a soft-deleted project
	// awaiting purge; pending projects are only returned when the caller asks
	// for them via include_pending.
	DeletionStatus       string     `json:"deletionStatus,omitempty"`
	DeletionScheduledFor *time.Time `json:"deletionScheduledFor,omitempty"`
}

// PendingDeletion reports whether the project is soft-deleted and awaiting
// purge. Memory may signal this with a status, a scheduled purge time, or both.
func (p ProjectRef) PendingDeletion() bool {
	return p.DeletionStatus == "pending_deletion" || p.DeletionScheduledFor != nil
}

// ListOrgs lists the orgs visible to the caller (GET /api/orgs). Bare JSON.
func (m *MemoryClient) ListOrgs(ctx context.Context) ([]Org, error) {
	var out []Org
	if err := m.do(ctx, http.MethodGet, "/api/orgs", nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// ListProjects lists the projects visible to the caller (GET /api/projects).
// Bare JSON array.
func (m *MemoryClient) ListProjects(ctx context.Context) ([]ProjectRef, error) {
	var out []ProjectRef
	if err := m.do(ctx, http.MethodGet, "/api/projects", nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// ListProjectsIncludingPending lists the caller's projects including those
// soft-deleted and awaiting purge (GET /api/projects?include_pending=true).
// Bare JSON array, same shape as ListProjects.
func (m *MemoryClient) ListProjectsIncludingPending(ctx context.Context) ([]ProjectRef, error) {
	var out []ProjectRef
	if err := m.do(ctx, http.MethodGet, "/api/projects?include_pending=true", nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// CreateProject creates a project in the given org (POST /api/projects;
// 201 → bare created project).
func (m *MemoryClient) CreateProject(ctx context.Context, name, orgID string) (*ProjectRef, error) {
	body := map[string]string{"name": name, "orgId": orgID}
	var out ProjectRef
	if err := m.do(ctx, http.MethodPost, "/api/projects", body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// CreateOrg creates an org (POST /api/orgs; 201 → bare created org).
func (m *MemoryClient) CreateOrg(ctx context.Context, name string) (*Org, error) {
	body := map[string]string{"name": name}
	var out Org
	if err := m.do(ctx, http.MethodPost, "/api/orgs", body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ProjectAccessDto is one project in the user's org access tree, nested in
// OrgWithProjectsDto (GET /api/user/orgs-and-projects).
type ProjectAccessDto struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	OrgID string `json:"orgId"`
	Role  string `json:"role"` // project_admin | project_user
}

// OrgWithProjectsDto is one org in the signed-in user's access tree with its
// nested projects and the user's role at each level.
type OrgWithProjectsDto struct {
	ID       string             `json:"id"`
	Name     string             `json:"name"`
	Role     string             `json:"role"` // org_admin | org_member
	Projects []ProjectAccessDto `json:"projects"`
}

// GetOrgsAndProjects returns the org→project access tree with per-level roles
// (GET /api/user/orgs-and-projects).
func (m *MemoryClient) GetOrgsAndProjects(ctx context.Context) ([]OrgWithProjectsDto, error) {
	var out []OrgWithProjectsDto
	if err := m.do(ctx, http.MethodGet, "/api/user/orgs-and-projects", nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// GetOrg fetches one org by id (GET /api/orgs/{id}).
func (m *MemoryClient) GetOrg(ctx context.Context, orgID string) (*Org, error) {
	path := "/api/orgs/" + url.PathEscape(orgID)
	var out Org
	if err := m.do(ctx, http.MethodGet, path, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// UpdateOrg renames an org (PATCH /api/orgs/{id}; body {"name": ...};
// 200 → bare updated org). Name-only: description/logo deferred pending a
// memory DB column.
func (m *MemoryClient) UpdateOrg(ctx context.Context, orgID, name string) (*Org, error) {
	path := "/api/orgs/" + url.PathEscape(orgID)
	body := map[string]string{"name": name}
	var out Org
	if err := m.do(ctx, http.MethodPatch, path, body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// DeleteOrg deletes an org (DELETE /api/orgs/{id}).
func (m *MemoryClient) DeleteOrg(ctx context.Context, orgID string) error {
	path := "/api/orgs/" + url.PathEscape(orgID)
	return m.do(ctx, http.MethodDelete, path, nil, nil)
}

// DeleteProject deletes a project by id (DELETE /api/projects/{id}). Memory
// deletes asynchronously (202), so the response body is ignored.
func (m *MemoryClient) DeleteProject(ctx context.Context, projectID string) error {
	path := "/api/projects/" + url.PathEscape(projectID)
	return m.do(ctx, http.MethodDelete, path, nil, nil)
}

// RestoreProject cancels a project's pending deletion (POST
// /api/projects/{id}/restore). The response body is ignored; errors surface
// through the usual `do` error mapping.
func (m *MemoryClient) RestoreProject(ctx context.Context, projectID string) error {
	path := "/api/projects/" + url.PathEscape(projectID) + "/restore"
	return m.do(ctx, http.MethodPost, path, nil, nil)
}

// TransferProject reparents a project to another org the caller belongs to
// (POST /api/projects/{id}/transfer; JSON body {"orgId": destinationOrgID}).
// The memory service authorizes the reparent (requester org_admin of the
// project's current org and a member of the destination) and applies any
// membership consequences; its response body is ignored, errors surface
// verbatim through the usual `do` error mapping.
func (m *MemoryClient) TransferProject(ctx context.Context, projectID, destinationOrgID string) error {
	path := "/api/projects/" + url.PathEscape(projectID) + "/transfer"
	body := map[string]string{"orgId": destinationOrgID}
	return m.do(ctx, http.MethodPost, path, body, nil)
}

// OrgMemberDto is one member of an org
// (GET /api/orgs/{id}/members).
type OrgMemberDto struct {
	ID          string  `json:"id"`
	Email       string  `json:"email"`
	DisplayName *string `json:"displayName,omitempty"`
	FirstName   *string `json:"firstName,omitempty"`
	LastName    *string `json:"lastName,omitempty"`
	Role        string  `json:"role"` // org_admin | org_member
	JoinedAt    string  `json:"joinedAt"`
}

// ListOrgMembers lists members of an org (GET /api/orgs/{id}/members). Bare
// JSON array.
func (m *MemoryClient) ListOrgMembers(ctx context.Context, orgID string) ([]OrgMemberDto, error) {
	path := "/api/orgs/" + url.PathEscape(orgID) + "/members"
	var out []OrgMemberDto
	if err := m.do(ctx, http.MethodGet, path, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// OrgToolSettingDto is one per-org tool toggle/config (admin routes under
// /api/admin/orgs/{orgId}/tool-settings).
type OrgToolSettingDto struct {
	ID        string         `json:"id"`
	OrgID     string         `json:"orgId"`
	ToolName  string         `json:"toolName"`
	Enabled   bool           `json:"enabled"`
	Config    map[string]any `json:"config,omitempty"`
	CreatedAt string         `json:"createdAt"`
	UpdatedAt string         `json:"updatedAt"`
}

// UpsertOrgToolSettingInput is the request to create/update an org tool setting
// (PUT /api/admin/orgs/{orgId}/tool-settings/{toolName}).
type UpsertOrgToolSettingInput struct {
	Enabled bool           `json:"enabled"`
	Config  map[string]any `json:"config,omitempty"`
}

// ListOrgToolSettings lists an org's tool settings
// (GET /api/admin/orgs/{orgId}/tool-settings). Bare JSON array.
func (m *MemoryClient) ListOrgToolSettings(ctx context.Context, orgID string) ([]OrgToolSettingDto, error) {
	path := "/api/admin/orgs/" + url.PathEscape(orgID) + "/tool-settings"
	var out []OrgToolSettingDto
	if err := m.do(ctx, http.MethodGet, path, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// UpsertOrgToolSetting creates or updates one org's tool setting
// (PUT /api/admin/orgs/{orgId}/tool-settings/{toolName}; 200 → the setting).
func (m *MemoryClient) UpsertOrgToolSetting(ctx context.Context, orgID, toolName string, in UpsertOrgToolSettingInput) (*OrgToolSettingDto, error) {
	path := "/api/admin/orgs/" + url.PathEscape(orgID) + "/tool-settings/" + url.PathEscape(toolName)
	var out OrgToolSettingDto
	if err := m.do(ctx, http.MethodPut, path, in, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// DeleteOrgToolSetting deletes an org's tool setting
// (DELETE /api/admin/orgs/{orgId}/tool-settings/{toolName}).
func (m *MemoryClient) DeleteOrgToolSetting(ctx context.Context, orgID, toolName string) error {
	path := "/api/admin/orgs/" + url.PathEscape(orgID) + "/tool-settings/" + url.PathEscape(toolName)
	return m.do(ctx, http.MethodDelete, path, nil, nil)
}

// ProjectMemberDto is one member of the active project
// (GET /api/projects/{id}/members).
type ProjectMemberDto struct {
	ID          string `json:"id"`
	Email       string `json:"email"`
	DisplayName string `json:"displayName"`
	FirstName   string `json:"firstName"`
	LastName    string `json:"lastName"`
	AvatarURL   string `json:"avatarUrl"`
	Role        string `json:"role"` // project_admin | project_user
	JoinedAt    string `json:"joinedAt"`
}

// ListMembers lists members of the active project (GET /api/projects/{id}/members).
func (m *MemoryClient) ListMembers(ctx context.Context) ([]ProjectMemberDto, error) {
	path := "/api/projects/" + url.PathEscape(m.projectIDFor(ctx)) + "/members"
	var out []ProjectMemberDto
	if err := m.do(ctx, http.MethodGet, path, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// RemoveMember removes a member from the active project
// (DELETE /api/projects/{id}/members/{userId}; 403 last-admin when removing the
// sole admin — surfaces via the normal error path, do not special-case).
func (m *MemoryClient) RemoveMember(ctx context.Context, userID string) error {
	path := "/api/projects/" + url.PathEscape(m.projectIDFor(ctx)) + "/members/" + url.PathEscape(userID)
	return m.do(ctx, http.MethodDelete, path, nil, nil)
}

// Invite role enum (exact strings): org_admin, project_admin, project_user.

// CreateInviteDto is the request to invite an email to an org/project
// (POST /api/invites).
type CreateInviteDto struct {
	OrgID     string `json:"orgId"`
	ProjectID string `json:"projectId,omitempty"`
	Email     string `json:"email"`
	Role      string `json:"role"`
}

// Invite is the created invitation returned by POST /api/invites (201).
type Invite struct {
	ID             string `json:"id"`
	OrganizationID string `json:"organizationId"`
	ProjectID      string `json:"projectId,omitempty"`
	Email          string `json:"email"`
	Role           string `json:"role"`
	Token          string `json:"token"`
	Status         string `json:"status"`
	CreatedAt      string `json:"createdAt"`
	ExpiresAt      string `json:"expiresAt,omitempty"`
}

// SentInviteDto is one invitation sent for the active project
// (GET /api/projects/{id}/invites).
type SentInviteDto struct {
	ID        string `json:"id"`
	Email     string `json:"email"`
	Role      string `json:"role"`
	Status    string `json:"status"` // pending | accepted | declined | revoked | expired
	CreatedAt string `json:"createdAt"`
	ExpiresAt string `json:"expiresAt,omitempty"`
}

// ListInvites lists invitations sent for the active project
// (GET /api/projects/{id}/invites).
func (m *MemoryClient) ListInvites(ctx context.Context) ([]SentInviteDto, error) {
	path := "/api/projects/" + url.PathEscape(m.projectIDFor(ctx)) + "/invites"
	var out []SentInviteDto
	if err := m.do(ctx, http.MethodGet, path, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// CreateInvite invites an email to an org/project with a role
// (POST /api/invites; 201 → Invite with token).
func (m *MemoryClient) CreateInvite(ctx context.Context, in CreateInviteDto) (*Invite, error) {
	var out Invite
	if err := m.do(ctx, http.MethodPost, "/api/invites", in, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// AcceptInvite accepts a pending invite by token (POST /api/invites/accept;
// 200 {"status":"accepted"}).
func (m *MemoryClient) AcceptInvite(ctx context.Context, token string) error {
	body := map[string]string{"token": token}
	return m.do(ctx, http.MethodPost, "/api/invites/accept", body, nil)
}

// PendingInviteDto is one pending invite addressed to the signed-in user
// (GET /api/invites/pending).
type PendingInviteDto struct {
	ID               string `json:"id"`
	ProjectID        string `json:"projectId,omitempty"`
	ProjectName      string `json:"projectName,omitempty"`
	OrganizationID   string `json:"organizationId"`
	OrganizationName string `json:"organizationName,omitempty"`
	Role             string `json:"role"`
	Token            string `json:"token"`
	CreatedAt        string `json:"createdAt"`
	ExpiresAt        string `json:"expiresAt,omitempty"`
}

// ListPendingInvites lists pending invites for the signed-in user
// (GET /api/invites/pending).
func (m *MemoryClient) ListPendingInvites(ctx context.Context) ([]PendingInviteDto, error) {
	var out []PendingInviteDto
	if err := m.do(ctx, http.MethodGet, "/api/invites/pending", nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// DeclineInvite declines a pending invite (POST /api/invites/{id}/decline).
func (m *MemoryClient) DeclineInvite(ctx context.Context, inviteID string) error {
	path := "/api/invites/" + url.PathEscape(inviteID) + "/decline"
	return m.do(ctx, http.MethodPost, path, nil, nil)
}

// CancelInvite revokes a pending invite (DELETE /api/invites/{id}).
func (m *MemoryClient) CancelInvite(ctx context.Context, inviteID string) error {
	path := "/api/invites/" + url.PathEscape(inviteID)
	return m.do(ctx, http.MethodDelete, path, nil, nil)
}

// UserSearchResultDto is one match in a user search (GET /api/users/search?email=).
type UserSearchResultDto struct {
	ID          string `json:"id"`
	Email       string `json:"email"`
	DisplayName string `json:"displayName"`
	FirstName   string `json:"firstName"`
	LastName    string `json:"lastName"`
	AvatarURL   string `json:"avatarUrl"`
}

// UserSearchResponseDto wraps user-search results ({users: [...]}).
type UserSearchResponseDto struct {
	Users []UserSearchResultDto `json:"users"`
}

// SearchUsers finds registered users by email prefix (GET /api/users/search?email=;
// partial match, min 2 chars, up to 10 results).
func (m *MemoryClient) SearchUsers(ctx context.Context, email string) ([]UserSearchResultDto, error) {
	path := "/api/users/search?email=" + url.QueryEscape(email)
	var out UserSearchResponseDto
	if err := m.do(ctx, http.MethodGet, path, nil, &out); err != nil {
		return nil, err
	}
	return out.Users, nil
}
