// Package constitutioncmd provides `codebase constitution` — commands for
// managing the coding constitution (Rules) in the knowledge graph.
// Designed to be used by AI agents, not humans directly.
package constitutioncmd

import "github.com/spf13/cobra"

func NewCmd(flagProjectID *string, flagBranch *string, flagFormat *string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "constitution",
		Short: "Manage the codebase constitution (rules and constraints)",
		Long: `Manage the codebase constitution — a versioned set of Rules that encode
non-negotiable constraints for this codebase.

Rules have:
  - name       : short human label
  - statement  : the constraint in plain English
  - category   : naming | api | service | db | scenario | security | performance
  - audit_type : security | performance (optional, for audit command filtering)
  - applies_to : object type(s) this rule applies to
  - auto_check : optional Go regex applied to object keys for automatic checking

Key convention: rule-<category>-<slug>

Designed for AI agents. The AI reads the codebase, infers appropriate rules,
and uses these commands to create and check them.
`,
	}

	cmd.AddCommand(newRulesCmd(flagProjectID, flagBranch, flagFormat))
	cmd.AddCommand(newAddRuleCmd(flagProjectID, flagBranch))
	cmd.AddCommand(newCheckCmd(flagProjectID, flagBranch, flagFormat))
	cmd.AddCommand(newCreateCmd(flagProjectID, flagBranch))
	cmd.AddCommand(newAuditCmd(flagProjectID, flagBranch, flagFormat))
	return cmd
}
