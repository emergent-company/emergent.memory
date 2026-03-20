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
	branchNameFlag        string
	branchDescriptionFlag string
	branchParentFlag      string
	branchSourceFlag      string
	branchExecuteFlag     bool
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
	Long: `Manage isolated workspaces (branches) for the knowledge graph.

A branch is a copy of the graph where you can create, update, and delete
objects and relationships without affecting the main graph. Changes stay
isolated until you explicitly merge them.

The main graph has no branch ID. All graph write commands (objects create,
relationships create, etc.) accept --branch <id> to target a branch instead
of the main graph. Without --branch, writes go to the main graph.

Typical workflow:
  1. Create a branch
  2. Write objects/relationships with --branch <id>
  3. Preview the merge (dry run)
  4. Execute the merge into a target branch
  5. Delete the branch`,
}

// ─────────────────────────────────────────────
// graph branches list
// ─────────────────────────────────────────────

var graphBranchesListCmd = &cobra.Command{
	Use:   "list",
	Short: "List graph branches",
	Long: `List all branches for the current project.

The main graph is not a branch and does not appear in this list. Every
branch shown here has an ID you can pass to --branch on graph write commands,
or as the target/source of a merge.

Use --output json to get full branch details including parent_branch_id and
created_at, which is useful for scripting.

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
		table.Header("ID", "Name", "Description", "Parent Branch ID", "Created")
		for _, br := range branches {
			desc := ""
			if br.Description != nil {
				desc = *br.Description
			}
			parent := ""
			if br.ParentBranchID != nil {
				parent = *br.ParentBranchID
			}
			_ = table.Append(br.ID, br.Name, desc, parent, br.CreatedAt)
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
		if branch.Description != nil {
			fmt.Fprintf(out, "Desc:     %s\n", *branch.Description)
		}
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
	Long: `Create a new branch — an isolated workspace for the knowledge graph.

Objects and relationships written with --branch <id> are visible only on
that branch until merged. The main graph is unaffected until you run
"memory graph branches merge".

--parent is optional metadata recording which branch this was forked from.
It does not affect merge behavior — it is lineage information only.

After creating a branch, capture its ID immediately:

  BRANCH_ID=$(memory graph branches create --name "my-branch" --output json \
    | python3 -c "import json,sys; print(json.load(sys.stdin)['id'])")

Then use it on all graph writes:

  memory graph objects create --type Service --key "svc-x" --branch "$BRANCH_ID"
  memory graph relationships create --type depends_on --from <id> --to <id> --branch "$BRANCH_ID"

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
		if branchDescriptionFlag != "" {
			req.Description = &branchDescriptionFlag
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
		if branch.Description != nil {
			fmt.Fprintf(out, "Desc:     %s\n", *branch.Description)
		}
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
	Long: `Update a branch's name or description.

Use --name to rename the branch. Use --description to set or update the
description. At least one flag is required.

Examples:
  memory graph branches update <branch-id> --name "new-name"
  memory graph branches update <branch-id> --description "staging area for Q4 planning"
  memory graph branches update <branch-id> --name "new-name" --description "updated purpose"`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if branchNameFlag == "" && branchDescriptionFlag == "" {
			return fmt.Errorf("at least one of --name or --description is required")
		}

		b, _, err := getBranchesClient(cmd)
		if err != nil {
			return err
		}

		updateReq := &sdkbranches.UpdateBranchRequest{}
		if branchNameFlag != "" {
			updateReq.Name = &branchNameFlag
		}
		if branchDescriptionFlag != "" {
			updateReq.Description = &branchDescriptionFlag
		}

		branch, err := b.Update(context.Background(), args[0], updateReq)
		if err != nil {
			return fmt.Errorf("failed to update branch: %w", err)
		}

		out := cmd.OutOrStdout()

		if graphOutputFlag == "json" {
			return json.NewEncoder(out).Encode(branch)
		}

		fmt.Fprintf(out, "ID:       %s\n", branch.ID)
		fmt.Fprintf(out, "Name:     %s\n", branch.Name)
		if branch.Description != nil {
			fmt.Fprintf(out, "Desc:     %s\n", *branch.Description)
		}
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
	Long: `Delete a branch and all objects that exist only on that branch.

Objects that have already been merged into another branch are unaffected.
This operation is irreversible.

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
	Use:   "merge <target-branch-id|main>",
	Short: "Merge a source branch into a target branch (or main)",
	Long: `Merge changes from a source branch into a target branch.

DIRECTION: source → target. The source branch is read; the target branch
receives the changes.

TARGET: use a branch UUID from "memory graph branches list", or the special
keyword "main" to merge into the main graph (branch_id IS NULL).

By default this is a DRY RUN — no changes are made. Pass --execute only
when you are ready to apply.

MERGE CLASSIFICATIONS:
  added        — object exists on source only; will be created on target
  fast_forward — object changed on source only; target will be updated
  conflict     — object changed on BOTH branches; merge is BLOCKED
  unchanged    — identical on both branches; nothing to do

If any conflicts exist, --execute is blocked. Resolve conflicts manually
(update the source or target so they agree) then re-run.

The merge runs in a single database transaction — all-or-nothing.

WORKFLOW:
  # 1. Dry run first — always
  memory graph branches merge main --source <source-id>

  # 2. Inspect conflicts in detail
  memory graph branches merge main --source <source-id> --output json

  # 3. Execute when clean
  memory graph branches merge main --source <source-id> --execute

Examples:
  memory graph branches merge main --source <source-id>
  memory graph branches merge main --source <source-id> --execute
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

		targetDisplay := "main"
		if result.TargetBranchID != nil {
			targetDisplay = *result.TargetBranchID
		}
		fmt.Fprintf(out, "Merge %s\n", mode)
		fmt.Fprintf(out, "Source:  %s\n", result.SourceBranchID)
		fmt.Fprintf(out, "Target:  %s\n", targetDisplay)
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
	graphBranchesCreateCmd.Flags().StringVar(&branchDescriptionFlag, "description", "", "Branch description (optional)")
	graphBranchesCreateCmd.Flags().StringVar(&branchParentFlag, "parent", "", "Parent branch ID")

	// Flags for branches update
	graphBranchesUpdateCmd.Flags().StringVar(&branchNameFlag, "name", "", "New branch name")
	graphBranchesUpdateCmd.Flags().StringVar(&branchDescriptionFlag, "description", "", "New branch description")

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
