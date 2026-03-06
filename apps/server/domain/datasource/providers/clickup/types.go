// Package clickup provides a data source provider for ClickUp Docs.
package clickup

// Config represents the ClickUp provider configuration.
// This is stored encrypted in DataSourceIntegration.config_encrypted.
type Config struct {
	// APIToken is the ClickUp personal API token
	APIToken string `json:"apiToken"`

	// WorkspaceID is the ClickUp workspace (team) ID
	WorkspaceID string `json:"workspaceId,omitempty"`

	// WorkspaceName is the workspace name for display
	WorkspaceName string `json:"workspaceName,omitempty"`

	// SelectedSpaces are the spaces to sync (empty = all spaces)
	SelectedSpaces []SelectedSpace `json:"selectedSpaces,omitempty"`

	// IncludeArchived includes archived docs and spaces
	IncludeArchived bool `json:"includeArchived,omitempty"`

	// LastSyncedAt is the timestamp of the last sync (Unix ms)
	LastSyncedAt int64 `json:"lastSyncedAt,omitempty"`
}

// SelectedSpace represents a space selected for syncing
type SelectedSpace struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// ----------------------------------------------------------------------------
// ClickUp API v2 Types
// ----------------------------------------------------------------------------

// Workspace represents a ClickUp workspace (team)
type Workspace struct {
	ID      string            `json:"id"`
	Name    string            `json:"name"`
	Color   string            `json:"color,omitempty"`
	Avatar  string            `json:"avatar,omitempty"`
	Members []WorkspaceMember `json:"members,omitempty"`
}

// WorkspaceMember represents a member in a workspace
type WorkspaceMember struct {
	User User `json:"user"`
}

// User represents a ClickUp user
type User struct {
	ID             int    `json:"id"`
	Username       string `json:"username"`
	Email          string `json:"email,omitempty"`
	Color          string `json:"color,omitempty"`
	ProfilePicture string `json:"profilePicture,omitempty"`
}

// Space represents a ClickUp space
type Space struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Private  bool   `json:"private"`
	Archived bool   `json:"archived"`
}

// WorkspacesResponse is the response from GET /team
type WorkspacesResponse struct {
	Teams []Workspace `json:"teams"`
}

// SpacesResponse is the response from GET /team/{team_id}/space
type SpacesResponse struct {
	Spaces []Space `json:"spaces"`
}

// ----------------------------------------------------------------------------
// ClickUp API v3 Types (Docs)
// ----------------------------------------------------------------------------

// DocParent identifies the parent container of a doc
type DocParent struct {
	ID   string `json:"id"`
	Type int    `json:"type"` // 6 = space, 5 = folder, 4 = list
}

// DocAvatar represents a doc's icon (emoji or custom)
type DocAvatar struct {
	Value string `json:"value"` // Format: "emoji::icon_value"
}

// DocCover represents a doc's cover image
type DocCover struct {
	Type  string `json:"type"`  // e.g., "color"
	Value string `json:"value"` // e.g., "#FF6900"
}

// Doc represents a ClickUp Doc (from v3 API)
type Doc struct {
	ID          string     `json:"id"`
	Name        string     `json:"name"`
	Parent      DocParent  `json:"parent"`
	WorkspaceID string     `json:"workspace_id"`
	CreatorID   int        `json:"creator_id"`
	DateCreated string     `json:"date_created"` // Unix timestamp as string
	DateUpdated string     `json:"date_updated"` // Unix timestamp as string
	Avatar      *DocAvatar `json:"avatar,omitempty"`
	Archived    bool       `json:"archived"`
	Deleted     bool       `json:"deleted"`
	Protected   bool       `json:"protected"`
}

// Page represents a page within a ClickUp Doc (from v3 API)
// Pages can have nested child pages (hierarchical structure)
type Page struct {
	PageID       string     `json:"page_id"`
	Name         string     `json:"name"`
	Content      string     `json:"content"` // Markdown content
	ParentPageID string     `json:"parent_page_id,omitempty"`
	DateCreated  string     `json:"date_created"`
	DateUpdated  string     `json:"date_updated"`
	CreatorID    int        `json:"creator_id"`
	Avatar       *DocAvatar `json:"avatar,omitempty"`
	Cover        *DocCover  `json:"cover,omitempty"`
	Archived     bool       `json:"archived"`
	Protected    bool       `json:"protected"`
	Pages        []Page     `json:"pages,omitempty"` // Nested child pages
}

// DocsResponse is the response from GET /workspaces/{workspace_id}/docs
type DocsResponse struct {
	Docs       []Doc  `json:"docs"`
	NextCursor string `json:"next_cursor,omitempty"`
}

// ----------------------------------------------------------------------------
// Document Metadata Types
// ----------------------------------------------------------------------------

// DocumentMetadata is stored in document.metadata for ClickUp-sourced docs
type DocumentMetadata struct {
	ClickUpDocID        string `json:"clickupDocId"`
	ClickUpWorkspaceID  string `json:"clickupWorkspaceId"`
	ClickUpSpaceID      string `json:"clickupSpaceId,omitempty"`
	ClickUpSpaceName    string `json:"clickupSpaceName,omitempty"`
	CreatorID           int    `json:"creatorId,omitempty"`
	ClickUpCreatedAt    string `json:"clickupCreatedAt,omitempty"`
	ClickUpUpdatedAt    string `json:"clickupUpdatedAt,omitempty"`
	Avatar              string `json:"avatar,omitempty"`
	Archived            bool   `json:"archived,omitempty"`
	PageCount           int    `json:"pageCount,omitempty"`
	Provider            string `json:"provider"` // Always "clickup"
}

// ConfigSchema is the JSON schema for provider configuration (used by UI)
var ConfigSchema = map[string]interface{}{
	"type":     "object",
	"required": []string{"apiToken"},
	"properties": map[string]interface{}{
		"apiToken": map[string]interface{}{
			"type":           "string",
			"title":          "API Token",
			"description":    "Your ClickUp personal API token. Get it from Settings > Apps in ClickUp.",
			"format":         "password",
			"ui:placeholder": "pk_12345678_...",
		},
		"workspaceId": map[string]interface{}{
			"type":     "string",
			"title":    "Workspace",
			"readOnly": true,
		},
		"workspaceName": map[string]interface{}{
			"type":     "string",
			"title":    "Workspace Name",
			"readOnly": true,
		},
		"includeArchived": map[string]interface{}{
			"type":        "boolean",
			"title":       "Include Archived",
			"description": "Include archived docs and spaces in sync",
			"default":     false,
		},
	},
	"ui:authType":       "token",
	"ui:testConnection": true,
	"ui:browseEnabled":  true,
}
