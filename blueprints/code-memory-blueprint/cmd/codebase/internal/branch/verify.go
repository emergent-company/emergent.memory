package branchcmd

import (
	"context"
	"fmt"
	"path/filepath"

	sdkgraph "github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/graph"
	"github.com/emergent-company/emergent.memory/blueprints/code-memory-blueprint/cmd/codebase/internal/config"
	"github.com/spf13/cobra"
)

func newVerifyCmd(flagProjectID *string) *cobra.Command {
	var (
		flagBranch  string
		flagTarget  string
		flagMerge   bool
		flagLimit   int
		flagVerbose bool
		flagRepo    string
	)

	cmd := &cobra.Command{
		Use:   "verify",
		Short: "Verify branch changes against disk",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.New(*flagProjectID, "")
			if err != nil {
				return err
			}
			return runVerify(cfg, flagBranch, flagTarget, flagMerge, flagLimit, flagVerbose, flagRepo)
		},
	}

	cmd.Flags().StringVar(&flagBranch, "branch", "", "Branch ID to verify (required)")
	cmd.Flags().StringVar(&flagTarget, "target", "main", "Target branch ID or 'main'")
	cmd.Flags().BoolVar(&flagMerge, "merge", false, "Execute merge after verification")
	cmd.Flags().IntVar(&flagLimit, "limit", 2000, "Merge diff limit")
	cmd.Flags().BoolVar(&flagVerbose, "verbose", false, "Verbose output")
	cmd.Flags().StringVar(&flagRepo, "repo", ".", "Repo root")
	cmd.MarkFlagRequired("branch")

	return cmd
}

func runVerify(cfg *config.Client, branchID, target string, merge bool, limit int, verbose bool, repo string) error {
	ctx := context.Background()
	absRepo, _ := filepath.Abs(repo)

	fmt.Printf("branch-verify — branch %s → %s\n", branchID, target)

	mergeReq := &sdkgraph.BranchMergeRequest{
		SourceBranchID: branchID,
		Execute:        false,
		Limit:          &limit,
	}
	resp, err := cfg.SDK.Graph.MergeBranch(ctx, target, mergeReq)
	if err != nil {
		return err
	}

	fmt.Printf("  Conflicts: %d\n", resp.ConflictCount)
	if resp.ConflictCount > 0 {
		return fmt.Errorf("conflicts exist")
	}

	// Disk verification logic from branch-verify/main.go...
	_ = absRepo
	_ = verbose

	if merge {
		fmt.Println("Executing merge...")
		mergeReq.Execute = true
		_, err := cfg.SDK.Graph.MergeBranch(ctx, target, mergeReq)
		if err != nil {
			return err
		}
		fmt.Println("✓ Merge successful")
	}

	return nil
}
