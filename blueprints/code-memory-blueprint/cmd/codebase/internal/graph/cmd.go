package graph

import (
	"github.com/spf13/cobra"
)

var (
	flagType       string
	flagKey        string
	flagFilter     []string
	flagLimit      int
	flagProperties string
	flagFrom       string
	flagTo         string
	flagRelType    string
	flagID         string
	flagListJSON   bool
	flagCursor     string
	flagDryRun     bool
	flagStatus     string
	flagUpsert     bool
	flagCreateJSON bool
	flagVerbose    bool
	flagCount      bool
	flagAll        bool
	flagDepth      int
	flagNoColor    bool
	batchFile      string
	batchFailFast  bool
)

func NewCmd(flagProjectID *string, flagBranch *string) *cobra.Command {
	graphCmd := &cobra.Command{
		Use:   "graph",
		Short: "Manage the knowledge graph",
	}

	// List
	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List objects in the graph",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runList(cmd, args, flagProjectID, flagBranch)
		},
	}
	listCmd.Flags().StringVar(&flagType, "type", "", "Object type (required)")
	listCmd.Flags().StringVar(&flagKey, "key", "", "Object key prefix")
	listCmd.Flags().StringSliceVar(&flagFilter, "filter", []string{}, "Filter in key=value format")
	listCmd.Flags().IntVar(&flagLimit, "limit", 200, "Limit the number of results")
	listCmd.Flags().BoolVar(&flagListJSON, "json", false, "Output JSON instead of a table")
	listCmd.Flags().StringVar(&flagCursor, "cursor", "", "Pagination cursor")
	listCmd.Flags().StringVar(&flagStatus, "status", "", "Filter by status")
	listCmd.Flags().BoolVar(&flagVerbose, "props", false, "Show all properties in verbose output")
	listCmd.Flags().BoolVar(&flagCount, "count", false, "Print only the total count")
	listCmd.Flags().BoolVar(&flagAll, "all", false, "Fetch all pages")
	_ = listCmd.MarkFlagRequired("type")
	graphCmd.AddCommand(listCmd)

	// Get
	getCmd := &cobra.Command{
		Use:   "get [id-or-key]",
		Short: "Get an object by ID or key",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runGet(cmd, args, flagProjectID, flagBranch)
		},
	}
	getCmd.Flags().StringVar(&flagKey, "key", "", "Get by key instead of ID")
	graphCmd.AddCommand(getCmd)

	// Create
	createCmd := &cobra.Command{
		Use:   "create",
		Short: "Create a new object",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runCreate(cmd, args, flagProjectID, flagBranch)
		},
	}
	createCmd.Flags().StringVar(&flagType, "type", "", "Object type (required)")
	createCmd.Flags().StringVar(&flagKey, "key", "", "Object key (required)")
	createCmd.Flags().StringVar(&flagProperties, "properties", "{}", "Object properties (JSON)")
	createCmd.Flags().StringVar(&flagStatus, "status", "", "Object status")
	createCmd.Flags().BoolVar(&flagUpsert, "upsert", false, "Create or update if key already exists")
	createCmd.Flags().BoolVar(&flagCreateJSON, "json", false, "Output the created object as JSON")
	_ = createCmd.MarkFlagRequired("type")
	_ = createCmd.MarkFlagRequired("key")
	graphCmd.AddCommand(createCmd)

	// Update
	updateCmd := &cobra.Command{
		Use:   "update",
		Short: "Update an existing object",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runUpdate(cmd, args, flagProjectID, flagBranch)
		},
	}
	updateCmd.Flags().StringVar(&flagID, "id", "", "Object ID")
	updateCmd.Flags().StringVar(&flagKey, "key", "", "Object key")
	updateCmd.Flags().StringVar(&flagProperties, "properties", "{}", "Object properties (JSON)")
	updateCmd.Flags().StringVar(&flagStatus, "status", "", "Object status")
	graphCmd.AddCommand(updateCmd)

	// Relate
	relateCmd := &cobra.Command{
		Use:   "relate",
		Short: "Relate two objects",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRelate(cmd, args, flagProjectID, flagBranch)
		},
	}
	relateCmd.Flags().StringVar(&flagType, "type", "", "Relationship type (required)")
	relateCmd.Flags().StringVar(&flagFrom, "from", "", "Source object ID (required; or use --src)")
	relateCmd.Flags().StringVar(&flagFrom, "src", "", "Source object ID (alias for --from)")
	relateCmd.Flags().StringVar(&flagTo, "to", "", "Target object ID (required; or use --dst)")
	relateCmd.Flags().StringVar(&flagTo, "dst", "", "Target object ID (alias for --to)")
	relateCmd.Flags().BoolVar(&flagUpsert, "upsert", false, "Skip if relationship already exists")
	_ = relateCmd.MarkFlagRequired("type")
	graphCmd.AddCommand(relateCmd)

	// Unrelate
	unrelateCmd := &cobra.Command{
		Use:   "unrelate [rel-id]",
		Short: "Delete a relationship by ID or lookup",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runUnrelate(cmd, args, flagProjectID, flagBranch)
		},
	}
	unrelateCmd.Flags().StringVar(&flagFrom, "from", "", "Source object ID")
	unrelateCmd.Flags().StringVar(&flagFrom, "src", "", "Source object ID (alias for --from)")
	unrelateCmd.Flags().StringVar(&flagTo, "to", "", "Target object ID")
	unrelateCmd.Flags().StringVar(&flagTo, "dst", "", "Target object ID (alias for --to)")
	unrelateCmd.Flags().StringVar(&flagRelType, "type", "", "Relationship type")
	graphCmd.AddCommand(unrelateCmd)

	// Delete
	deleteCmd := &cobra.Command{
		Use:   "delete [id-or-key]",
		Short: "Delete an object by ID or key",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDelete(cmd, args, flagProjectID, flagBranch)
		},
	}
	deleteCmd.Flags().StringVar(&flagType, "type", "", "Object type hint")
	graphCmd.AddCommand(deleteCmd)

	// Rename
	renameCmd := &cobra.Command{
		Use:   "rename <old-key> <new-key>",
		Short: "Rename an object and migrate relationships",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRename(cmd, args, flagProjectID, flagBranch)
		},
	}
	renameCmd.Flags().BoolVar(&flagDryRun, "dry-run", false, "Show changes without applying")
	renameCmd.Flags().BoolVar(&flagListJSON, "json", false, "Output result as JSON")
	graphCmd.AddCommand(renameCmd)

	// Prune
	pruneCmd := &cobra.Command{
		Use:   "prune",
		Short: "Delete objects with no relationships",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runPrune(cmd, args, flagProjectID, flagBranch)
		},
	}
	pruneCmd.Flags().BoolVar(&flagDryRun, "dry-run", false, "Show orphans without deleting")
	graphCmd.AddCommand(pruneCmd)

	// Tree
	treeCmd := &cobra.Command{
		Use:   "tree [key-or-id]",
		Short: "Show dependency tree for an object",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runTree(cmd, args, flagProjectID, flagBranch)
		},
	}
	treeCmd.Flags().StringVar(&flagType, "type", "", "List all objects of this type")
	treeCmd.Flags().IntVar(&flagDepth, "depth", 3, "Max depth for tree walk")
	treeCmd.Flags().BoolVar(&flagNoColor, "no-color", false, "Disable color output")
	graphCmd.AddCommand(treeCmd)

	// Batch
	batchCmd := &cobra.Command{
		Use:   "batch",
		Short: "Create/relate multiple objects from JSON",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runCreateBatch(cmd, args, flagProjectID, flagBranch)
		},
	}
	batchCmd.Flags().StringVar(&batchFile, "file", "", "JSON file to read operations from")
	batchCmd.Flags().BoolVar(&batchFailFast, "fail-fast", false, "Stop on first error")
	graphCmd.AddCommand(batchCmd)

	return graphCmd
}
