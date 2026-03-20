package cmd

import (
	"context"
	"encoding/json"
	"fmt"

	sdkbranches "github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/branches"
	"github.com/olekukonko/tablewriter"
	"github.com/spf13/cobra"
)

// ─────────────────────────────────────────────
// Branch-specific flag variables
// ─────────────────────────────────────────────

var (
	branchNameFlag    string
	branchParentFlag  string
	branchSourceFlag  string
	branchExecuteFlag bool
)

// ─────────────────────────────────────────────
// Helper
// ─────────────────────────────────────────────

// getBranchesClient resolves the project context and returns the branches SDK
// client together with the resolved project ID (empty string if none set).
func getBranchesClient(cmd *cobra.Command) (*sdkbranches.Client, string, error) {
	c, err := getClient(cmd)
	if err != nil {
		return nil, "", err
	}

	projectID, err := resolveProjectContext(cmd, graphProjectFlag)
	if err != nil {
		return nil, "", err
	}

	c.SetContext("", projectID)
	return c.SDK.Branches, projectID, nil
}

// ─────────────────────────────────────────────
// graph branches (sub-group)
// ─────────────────────────────────────────────

var graphBranchesCmd = &cobra.Command{
	Use:   "branches",
	Short: "Manage graph branches",
	Long:  "Commands for creating, listing, updating, deleting, and merging graph branches",
}

// ─────────────────────────────────────────────
// graph branches list
// ─────────────────────────────────────────────

var graphBranchesListCmd = &cobra.Command{
	Use:   "list",
	Short: "List graph branches",
	Long: `List graph branches, optionally filtered by project.

Use --project to filter branches belonging to a specific project.
Use --output json to receive the full list as JSON.

Examples:
  memory graph branches list
  memory graph branches list --project <project-id>
  memory graph branches list --output json`,
	RunE: func(cmd *cobra.Command, args []string) error {
		b, projectID, err := getBranchesClient(cmd)
		if err != nil {
			return err
		}

		opts := &sdkbranches.ListOptions{}
		if projectID != "" {
			opts.ProjectID = projectID
		}

		branches, err := b.List(context.Background(), opts)
		if err != nil {
			return fmt.Errorf("failed to list branches: %w", err)
		}

		out := cmd.OutOrStdout()

		if graphOutputFlag == "json" {
			return json.NewEncoder(out).Encode(branches)
		}

		if len(branches) == 0 {
			fmt.Fprintln(out, "No branches found.")
			return nil
		}

		table := tablewriter.NewWriter(out)
		table.Header("ID", "Name", "Parent Branch ID", "Created")
		for _, br := range branches {
			parent := ""
			if br.ParentBranchID != nil {
				parent = *br.ParentBranchID
			}
			_ = table.Append(br.ID, br.Name, parent, br.CreatedAt)
		}
		return table.Render()
	},
}

// ─────────────────────────────────────────────
// graph branches get
// ─────────────────────────────────────────────

var graphBranchesGetCmd = &cobra.Command{
	Use:   "get <id>",
	Short: "Get a branch by ID",
	Long: `Get details for a branch by its ID.

Use --output json to receive the full object as JSON.

Examples:
  memory graph branches get <branch-id>
  memory graph branches get <branch-id> --output json`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		b, _, err := getBranchesClient(cmd)
		if err != nil {
			return err
		}

		branch, err := b.Get(context.Background(), args[0])
		if err != nil {
			return fmt.Errorf("failed to get branch: %w", err)
		}

		out := cmd.OutOrStdout()

		if graphOutputFlag == "json" {
			return json.NewEncoder(out).Encode(branch)
		}

		fmt.Fprintf(out, "ID:       %s\n", branch.ID)
		fmt.Fprintf(out, "Name:     %s\n", branch.Name)
		if branch.ProjectID != nil {
			fmt.Fprintf(out, "Project:  %s\n", *branch.ProjectID)
		}
		if branch.ParentBranchID != nil {
			fmt.Fprintf(out, "Parent:   %s\n", *branch.ParentBranchID)
		}
		fmt.Fprintf(out, "Created:  %s\n", branch.CreatedAt)

		return nil
	},
}

// ─────────────────────────────────────────────
// graph branches create
// ─────────────────────────────────────────────

var graphBranchesCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a new branch",
	Long: `Create a new graph branch.

Use --name to set the branch name (required).
Use --project to associate the branch with a project.
Use --parent to create the branch as a child of an existing branch.

Examples:
  memory graph branches create --name "scenario/what-if"
  memory graph branches create --name "feature-x" --project <project-id>
  memory graph branches create --name "child" --parent <parent-branch-id>`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if branchNameFlag == "" {
			return fmt.Errorf("--name is required")
		}

		b, projectID, err := getBranchesClient(cmd)
		if err != nil {
			return err
		}

		req := &sdkbranches.CreateBranchRequest{
			Name: branchNameFlag,
		}
		if projectID != "" {
			req.ProjectID = &projectID
		}
		if branchParentFlag != "" {
			req.ParentBranchID = &branchParentFlag
		}

		branch, err := b.Create(context.Background(), req)
		if err != nil {
			return fmt.Errorf("failed to create branch: %w", err)
		}

		out := cmd.OutOrStdout()

		if graphOutputFlag == "json" {
			return json.NewEncoder(out).Encode(branch)
		}

		fmt.Fprintf(out, "ID:       %s\n", branch.ID)
		fmt.Fprintf(out, "Name:     %s\n", branch.Name)
		if branch.ProjectID != nil {
			fmt.Fprintf(out, "Project:  %s\n", *branch.ProjectID)
		}
		if branch.ParentBranchID != nil {
			fmt.Fprintf(out, "Parent:   %s\n", *branch.ParentBranchID)
		}
		fmt.Fprintf(out, "Created:  %s\n", branch.CreatedAt)

		return nil
	},
}

// ─────────────────────────────────────────────
// graph branches update
// ─────────────────────────────────────────────

var graphBranchesUpdateCmd = &cobra.Command{
	Use:   "update <id>",
	Short: "Update a branch",
	Long: `Update a branch's name.

Use --name to set the new branch name (required).

Examples:
  memory graph branches update <branch-id> --name "new-name"`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if branchNameFlag == "" {
			return fmt.Errorf("--name is required")
		}

		b, _, err := getBranchesClient(cmd)
		if err != nil {
			return err
		}

		name := branchNameFlag
		branch, err := b.Update(context.Background(), args[0], &sdkbranches.UpdateBranchRequest{
			Name: &name,
		})
		if err != nil {
			return fmt.Errorf("failed to update branch: %w", err)
		}

		out := cmd.OutOrStdout()

		if graphOutputFlag == "json" {
			return json.NewEncoder(out).Encode(branch)
		}

		fmt.Fprintf(out, "ID:       %s\n", branch.ID)
		fmt.Fprintf(out, "Name:     %s\n", branch.Name)
		if branch.ProjectID != nil {
			fmt.Fprintf(out, "Project:  %s\n", *branch.ProjectID)
		}
		fmt.Fprintf(out, "Created:  %s\n", branch.CreatedAt)

		return nil
	},
}

// ─────────────────────────────────────────────
// graph branches delete
// ─────────────────────────────────────────────

var graphBranchesDeleteCmd = &cobra.Command{
	Use:   "delete <id>",
	Short: "Delete a branch",
	Long: `Delete a branch by ID. This also removes all branch lineage records.

Examples:
  memory graph branches delete <branch-id>`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		b, _, err := getBranchesClient(cmd)
		if err != nil {
			return err
		}

		if err := b.Delete(context.Background(), args[0]); err != nil {
			return fmt.Errorf("failed to delete branch: %w", err)
		}

		fmt.Fprintf(cmd.OutOrStdout(), "Branch %s deleted.\n", args[0])
		return nil
	},
}

// ─────────────────────────────────────────────
// graph branches merge
// ─────────────────────────────────────────────

var graphBranchesMergeCmd = &cobra.Command{
	Use:   "merge <target-branch-id>",
	Short: "Merge a source branch into a target branch",
	Long: `Merge a source branch into a target branch.

By default this is a dry run that shows what would change without mutating
state. Pass --execute to apply the merge.

The merge classifies each diverged object as:
  added        — exists on source only, will be added to target
  fast_forward — changed on source only, will be updated on target
  conflict     — changed on both branches, requires manual resolution
  unchanged    — identical on both branches, no action taken

Examples:
  memory graph branches merge <target-id> --source <source-id>
  memory graph branches merge <target-id> --source <source-id> --execute
  memory graph branches merge <target-id> --source <source-id> --output json`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if branchSourceFlag == "" {
			return fmt.Errorf("--source is required")
		}

		b, _, err := getBranchesClient(cmd)
		if err != nil {
			return err
		}

		result, err := b.Merge(context.Background(), args[0], &sdkbranches.MergeRequest{
			SourceBranchID: branchSourceFlag,
			Execute:        branchExecuteFlag,
		})
		if err != nil {
			return fmt.Errorf("failed to merge branch: %w", err)
		}

		out := cmd.OutOrStdout()

		if graphOutputFlag == "json" {
			return json.NewEncoder(out).Encode(result)
		}

		mode := "DRY RUN"
		if result.Applied {
			mode = "APPLIED"
		}

		fmt.Fprintf(out, "Merge %s\n", mode)
		fmt.Fprintf(out, "Source:  %s\n", result.SourceBranchID)
		fmt.Fprintf(out, "Target:  %s\n", result.TargetBranchID)
		fmt.Fprintf(out, "\nObjects (%d total):\n", result.TotalObjects)
		fmt.Fprintf(out, "  added:        %d\n", result.AddedCount)
		fmt.Fprintf(out, "  fast_forward: %d\n", result.FastForwardCount)
		fmt.Fprintf(out, "  conflict:     %d\n", result.ConflictCount)
		fmt.Fprintf(out, "  unchanged:    %d\n", result.UnchangedCount)

		if result.RelationshipsTotal != nil {
			fmt.Fprintf(out, "\nRelationships (%d total):\n", *result.RelationshipsTotal)
			if result.RelationshipsAddedCount != nil {
				fmt.Fprintf(out, "  added:        %d\n", *result.RelationshipsAddedCount)
			}
			if result.RelationshipsFastForwardCount != nil {
				fmt.Fprintf(out, "  fast_forward: %d\n", *result.RelationshipsFastForwardCount)
			}
			if result.RelationshipsConflictCount != nil {
				fmt.Fprintf(out, "  conflict:     %d\n", *result.RelationshipsConflictCount)
			}
			if result.RelationshipsUnchangedCount != nil {
				fmt.Fprintf(out, "  unchanged:    %d\n", *result.RelationshipsUnchangedCount)
			}
		}

		if result.Truncated && result.HardLimit != nil {
			fmt.Fprintf(out, "\nWarning: result truncated at %d — use --output json and pass a higher limit to see all changes.\n", *result.HardLimit)
		}

		if result.ConflictCount > 0 {
			fmt.Fprintf(out, "\n%d conflict(s) detected — use --output json for details.\n", result.ConflictCount)
		}

		if result.Applied && result.AppliedObjects != nil {
			fmt.Fprintf(out, "\n%d object(s) and relationship(s) applied to target branch.\n", *result.AppliedObjects)
		}

		return nil
	},
}

// ─────────────────────────────────────────────
// init — wire up the branches sub-tree
// ─────────────────────────────────────────────

func init() {
	// Flags for branches create
	graphBranchesCreateCmd.Flags().StringVar(&branchNameFlag, "name", "", "Branch name (required)")
	graphBranchesCreateCmd.Flags().StringVar(&branchParentFlag, "parent", "", "Parent branch ID")

	// Flags for branches update
	graphBranchesUpdateCmd.Flags().StringVar(&branchNameFlag, "name", "", "New branch name (required)")

	// Flags for branches merge
	graphBranchesMergeCmd.Flags().StringVar(&branchSourceFlag, "source", "", "Source branch ID to merge from (required)")
	graphBranchesMergeCmd.Flags().BoolVar(&branchExecuteFlag, "execute", false, "Execute the merge (default is dry run)")

	// Assemble branches subcommands
	graphBranchesCmd.AddCommand(graphBranchesListCmd)
	graphBranchesCmd.AddCommand(graphBranchesGetCmd)
	graphBranchesCmd.AddCommand(graphBranchesCreateCmd)
	graphBranchesCmd.AddCommand(graphBranchesUpdateCmd)
	graphBranchesCmd.AddCommand(graphBranchesDeleteCmd)
	graphBranchesCmd.AddCommand(graphBranchesMergeCmd)
}
