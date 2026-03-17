package cmd

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/invitations"
	"github.com/emergent-company/emergent.memory/tools/cli/internal/completion"
	"github.com/spf13/cobra"
)

var teamCmd = &cobra.Command{
	Use:   "team",
	Short: "Manage project team members",
	Long:  "Commands for listing, inviting, and removing members of a project",
}

// ─── team list ───────────────────────────────────────────────────────────────

var teamListCmd = &cobra.Command{
	Use:               "list [project-name-or-id]",
	Short:             "List project team members",
	Long:              "List all members of a project with their roles and join dates. Use --stats for last-active info.",
	Args:              cobra.MaximumNArgs(1),
	ValidArgsFunction: completion.ProjectNamesCompletionFunc(),
	RunE:              runTeamList,
}

var (
	teamListStats bool
	teamListJSON  bool
)

func runTeamList(cmd *cobra.Command, args []string) error {
	c, err := getClient(cmd)
	if err != nil {
		return err
	}

	projectID, err := resolveProjectArgOrPick(cmd, c, args)
	if err != nil {
		return err
	}

	members, err := c.SDK.Projects.ListMembers(context.Background(), projectID)
	if err != nil {
		return fmt.Errorf("failed to list members: %w", err)
	}

	if teamListJSON {
		return json.NewEncoder(os.Stdout).Encode(members)
	}

	if len(members) == 0 {
		fmt.Println("No members found.")
		return nil
	}

	for i, m := range members {
		displayName := m.Email
		if m.DisplayName != nil && *m.DisplayName != "" {
			displayName = *m.DisplayName
		}
		joinedAt := ""
		if t, parseErr := time.Parse(time.RFC3339, m.JoinedAt); parseErr == nil {
			joinedAt = t.Format("2006-01-02")
		} else {
			joinedAt = m.JoinedAt
		}
		fmt.Printf("%d. %-30s %-30s %-20s joined %s\n",
			i+1, displayName, m.Email, m.Role, joinedAt)
	}

	return nil
}

// ─── team invite ─────────────────────────────────────────────────────────────

var teamInviteCmd = &cobra.Command{
	Use:   "invite <email> [project-name-or-id]",
	Short: "Invite someone to the project",
	Long: `Send an email invitation to join the project.

The invited user will receive an email with CLI install instructions and an
accept link. Use --role to control the access level (default: project_viewer).

Roles:
  project_viewer  Read-only access (cannot modify data)
  project_user    Read-write access
  project_admin   Full admin access`,
	Args:              cobra.RangeArgs(1, 2),
	ValidArgsFunction: completion.ProjectNamesCompletionFunc(),
	RunE:              runTeamInvite,
}

var teamInviteRole string

func runTeamInvite(cmd *cobra.Command, args []string) error {
	email := args[0]
	if !strings.Contains(email, "@") {
		return fmt.Errorf("invalid email address: %s", email)
	}

	projectArgs := args[1:]
	c, err := getClient(cmd)
	if err != nil {
		return err
	}

	projectID, err := resolveProjectArgOrPick(cmd, c, projectArgs)
	if err != nil {
		return err
	}

	// Look up project name for the invitation email
	project, err := c.SDK.Projects.Get(context.Background(), projectID, nil)
	projectName := projectID
	if err == nil && project != nil {
		projectName = project.Name
	}

	// Look up org ID (required by the invite API)
	orgID := ""
	if project != nil {
		orgID = project.OrgID
	}

	role := teamInviteRole
	validRoles := map[string]bool{
		"project_viewer": true,
		"project_user":   true,
		"project_admin":  true,
	}
	if !validRoles[role] {
		return fmt.Errorf("invalid role %q — must be one of: project_viewer, project_user, project_admin", role)
	}

	_, err = c.SDK.Invitations.Create(context.Background(), &invitations.CreateRequest{
		OrgID:       orgID,
		ProjectID:   projectID,
		Email:       email,
		Role:        role,
		ProjectName: projectName,
	})
	if err != nil {
		return fmt.Errorf("failed to send invitation: %w", err)
	}

	roleLabel := roleLabelForCLI(role)
	fmt.Printf("Invitation sent to %s (%s) for project %q\n", email, roleLabel, projectName)
	return nil
}

func roleLabelForCLI(role string) string {
	switch role {
	case "project_admin":
		return "admin"
	case "project_user":
		return "member"
	case "project_viewer":
		return "viewer (read-only)"
	default:
		return role
	}
}

// ─── team remove ─────────────────────────────────────────────────────────────

var teamRemoveCmd = &cobra.Command{
	Use:               "remove <email> [project-name-or-id]",
	Short:             "Remove a member from the project",
	Long:              "Remove a user from the project by email. Use --yes to skip confirmation.",
	Args:              cobra.RangeArgs(1, 2),
	ValidArgsFunction: completion.ProjectNamesCompletionFunc(),
	RunE:              runTeamRemove,
}

var teamRemoveYes bool

func runTeamRemove(cmd *cobra.Command, args []string) error {
	targetEmail := args[0]
	projectArgs := args[1:]

	c, err := getClient(cmd)
	if err != nil {
		return err
	}

	projectID, err := resolveProjectArgOrPick(cmd, c, projectArgs)
	if err != nil {
		return err
	}

	// Resolve email → user ID via member list
	members, err := c.SDK.Projects.ListMembers(context.Background(), projectID)
	if err != nil {
		return fmt.Errorf("failed to fetch member list: %w", err)
	}

	targetUserID := ""
	for _, m := range members {
		if strings.EqualFold(m.Email, targetEmail) {
			targetUserID = m.ID
			break
		}
	}
	if targetUserID == "" {
		return fmt.Errorf("%s is not a member of this project", targetEmail)
	}

	// Confirm unless --yes
	if !teamRemoveYes {
		fmt.Printf("Remove %s from project? [y/N] ", targetEmail)
		scanner := bufio.NewScanner(os.Stdin)
		scanner.Scan()
		answer := strings.TrimSpace(strings.ToLower(scanner.Text()))
		if answer != "y" && answer != "yes" {
			fmt.Println("Cancelled.")
			return nil
		}
	}

	if err := c.SDK.Projects.RemoveMember(context.Background(), projectID, targetUserID); err != nil {
		return fmt.Errorf("failed to remove member: %w", err)
	}

	fmt.Printf("Removed %s from project.\n", targetEmail)
	return nil
}

// ─── init ────────────────────────────────────────────────────────────────────

func initTeamCmd() {
	// team list flags
	teamListCmd.Flags().BoolVar(&teamListStats, "stats", false, "Show last-active stats per member")
	teamListCmd.Flags().BoolVar(&teamListJSON, "json", false, "Output as JSON")

	// team invite flags
	teamInviteCmd.Flags().StringVar(&teamInviteRole, "role", "project_viewer",
		"Role to assign (project_viewer, project_user, project_admin)")

	// team remove flags
	teamRemoveCmd.Flags().BoolVarP(&teamRemoveYes, "yes", "y", false, "Skip confirmation prompt")

	// Assemble subcommand tree
	teamCmd.AddCommand(teamListCmd)
	teamCmd.AddCommand(teamInviteCmd)
	teamCmd.AddCommand(teamRemoveCmd)
}
