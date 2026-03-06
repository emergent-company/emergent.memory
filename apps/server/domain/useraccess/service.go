// Package useraccess handles user access tree (orgs and projects with roles)
package useraccess

import (
	"context"

	"github.com/uptrace/bun"
)

// ProjectWithRole represents a project with the user's role
type ProjectWithRole struct {
	ID                string                 `json:"id"`
	Name              string                 `json:"name"`
	OrgID             string                 `json:"orgId"`
	Role              string                 `json:"role"`
	KBPurpose         *string                `json:"kb_purpose,omitempty"`
	AutoExtractConfig map[string]interface{} `json:"auto_extract_config,omitempty"`
}

// OrgWithProjects represents an organization with its projects
type OrgWithProjects struct {
	ID       string            `json:"id"`
	Name     string            `json:"name"`
	Role     string            `json:"role"`
	Projects []ProjectWithRole `json:"projects"`
}

// Service handles user access operations
type Service struct {
	db bun.IDB
}

// NewService creates a new user access service
func NewService(db bun.IDB) *Service {
	return &Service{db: db}
}

type orgRow struct {
	OrgID   string `bun:"org_id"`
	OrgName string `bun:"org_name"`
	Role    string `bun:"role"`
}

type projectRow struct {
	ProjectID         string                 `bun:"project_id"`
	ProjectName       string                 `bun:"project_name"`
	OrgID             string                 `bun:"org_id"`
	Role              string                 `bun:"role"`
	KBPurpose         *string                `bun:"kb_purpose"`
	AutoExtractConfig map[string]interface{} `bun:"auto_extract_config,type:jsonb"`
}

// GetAccessTree returns the complete access tree for a user
func (s *Service) GetAccessTree(ctx context.Context, userID string) ([]OrgWithProjects, error) {
	if userID == "" {
		return []OrgWithProjects{}, nil
	}

	// Query organizations with membership
	var orgRows []orgRow
	err := s.db.NewRaw(`
		SELECT o.id as org_id, o.name as org_name, om.role
		FROM kb.orgs o
		INNER JOIN kb.organization_memberships om ON o.id = om.organization_id
		WHERE om.user_id = ?
		ORDER BY o.created_at DESC
	`, userID).Scan(ctx, &orgRows)
	if err != nil {
		return nil, err
	}

	// Query projects with membership
	var projectRows []projectRow
	err = s.db.NewRaw(`
		SELECT p.id as project_id, p.name as project_name, p.organization_id as org_id,
		       pm.role, p.kb_purpose, p.auto_extract_config
		FROM kb.projects p
		INNER JOIN kb.project_memberships pm ON p.id = pm.project_id
		WHERE pm.user_id = ?
		ORDER BY p.created_at DESC
	`, userID).Scan(ctx, &projectRows)
	if err != nil {
		return nil, err
	}

	// Build hierarchical structure
	orgsMap := make(map[string]*OrgWithProjects)

	// Initialize all orgs
	for _, org := range orgRows {
		orgsMap[org.OrgID] = &OrgWithProjects{
			ID:       org.OrgID,
			Name:     org.OrgName,
			Role:     org.Role,
			Projects: []ProjectWithRole{},
		}
	}

	// Add projects to their parent organizations
	for _, proj := range projectRows {
		if org, ok := orgsMap[proj.OrgID]; ok {
			project := ProjectWithRole{
				ID:    proj.ProjectID,
				Name:  proj.ProjectName,
				OrgID: proj.OrgID,
				Role:  proj.Role,
			}
			if proj.KBPurpose != nil {
				project.KBPurpose = proj.KBPurpose
			}
			if proj.AutoExtractConfig != nil {
				project.AutoExtractConfig = proj.AutoExtractConfig
			}
			org.Projects = append(org.Projects, project)
		}
	}

	// Convert to slice maintaining order
	result := make([]OrgWithProjects, 0, len(orgRows))
	for _, org := range orgRows {
		result = append(result, *orgsMap[org.OrgID])
	}

	return result, nil
}
