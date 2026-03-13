package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	sdkdocs "github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/documents"
	"github.com/olekukonko/tablewriter"
	"github.com/spf13/cobra"
)

// ─────────────────────────────────────────────
// Top-level command
// ─────────────────────────────────────────────

var documentsCmd = &cobra.Command{
	Use:     "documents",
	Short:   "Manage project documents",
	Long:    "Commands for managing documents in the Memory platform",
	GroupID: "knowledge",
}

// ─────────────────────────────────────────────
// Flag variables
// ─────────────────────────────────────────────

var (
	docsProjectFlag     string
	docsOutputFlag      string
	docsLimitFlag       int
	docsAutoExtractFlag bool
)

// ─────────────────────────────────────────────
// Helper: resolve project + set context on client
// ─────────────────────────────────────────────

func getDocsClient(cmd *cobra.Command) (*sdkdocs.Client, error) {
	c, err := getClient(cmd)
	if err != nil {
		return nil, err
	}

	projectID, err := resolveProjectContext(cmd, docsProjectFlag)
	if err != nil {
		return nil, err
	}

	c.SetContext("", projectID)
	return c.SDK.Documents, nil
}

// ─────────────────────────────────────────────
// documents list
// ─────────────────────────────────────────────

var documentsListCmd = &cobra.Command{
	Use:   "list",
	Short: "List documents",
	Long: `List documents in the current project.

Output is a table with columns: ID, Filename, MIME Type, Size (bytes), and
Created date. Use --limit to control how many records are returned. Use
--output json to receive the full document list as JSON.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		d, err := getDocsClient(cmd)
		if err != nil {
			return err
		}

		opts := &sdkdocs.ListOptions{}
		if docsLimitFlag > 0 {
			opts.Limit = docsLimitFlag
		}

		result, err := d.List(context.Background(), opts)
		if err != nil {
			return fmt.Errorf("failed to list documents: %w", err)
		}

		out := cmd.OutOrStdout()

		if docsOutputFlag == "json" {
			return json.NewEncoder(out).Encode(result.Documents)
		}

		if len(result.Documents) == 0 {
			fmt.Fprintln(out, "No documents found.")
			return nil
		}

		table := tablewriter.NewWriter(out)
		table.Header("ID", "Filename", "MIME Type", "Size (bytes)", "Created")
		for _, doc := range result.Documents {
			filename := ""
			if doc.Filename != nil {
				filename = *doc.Filename
			}
			mime := ""
			if doc.MimeType != nil {
				mime = *doc.MimeType
			}
			size := ""
			if doc.FileSizeBytes != nil {
				size = fmt.Sprintf("%d", *doc.FileSizeBytes)
			}
			_ = table.Append(
				doc.ID,
				filename,
				mime,
				size,
				doc.CreatedAt.Format("2006-01-02"),
			)
		}
		return table.Render()
	},
}

// ─────────────────────────────────────────────
// documents get
// ─────────────────────────────────────────────

var documentsGetCmd = &cobra.Command{
	Use:   "get <id>",
	Short: "Get a document by ID",
	Long: `Get details for a specific document by its ID.

Prints ID, Filename, MIME Type, Size (bytes), Conversion Status, total Chunks,
Embedded Chunks, and Created/Updated timestamps. Use --output json to receive
the full document record as JSON instead.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		d, err := getDocsClient(cmd)
		if err != nil {
			return err
		}

		doc, err := d.Get(context.Background(), args[0])
		if err != nil {
			return fmt.Errorf("failed to get document: %w", err)
		}

		out := cmd.OutOrStdout()

		if docsOutputFlag == "json" {
			return json.NewEncoder(out).Encode(doc)
		}

		filename := ""
		if doc.Filename != nil {
			filename = *doc.Filename
		}
		mime := ""
		if doc.MimeType != nil {
			mime = *doc.MimeType
		}
		convStatus := ""
		if doc.ConversionStatus != nil {
			convStatus = *doc.ConversionStatus
		}

		fmt.Fprintf(out, "ID:                 %s\n", doc.ID)
		fmt.Fprintf(out, "Filename:           %s\n", filename)
		fmt.Fprintf(out, "MIME Type:          %s\n", mime)
		if doc.FileSizeBytes != nil {
			fmt.Fprintf(out, "Size:               %d bytes\n", *doc.FileSizeBytes)
		}
		fmt.Fprintf(out, "Conversion Status:  %s\n", convStatus)
		fmt.Fprintf(out, "Chunks:             %d\n", doc.Chunks)
		fmt.Fprintf(out, "Embedded Chunks:    %d\n", doc.EmbeddedChunks)
		fmt.Fprintf(out, "Created:            %s\n", doc.CreatedAt.Format("2006-01-02 15:04:05"))
		fmt.Fprintf(out, "Updated:            %s\n", doc.UpdatedAt.Format("2006-01-02 15:04:05"))

		return nil
	},
}

// ─────────────────────────────────────────────
// documents upload
// ─────────────────────────────────────────────

var documentsUploadCmd = &cobra.Command{
	Use:   "upload <file>",
	Short: "Upload a file as a document",
	Long:  "Upload a local file and create a document record. Use --auto-extract to trigger extraction after upload.",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		filePath := args[0]

		f, err := os.Open(filePath)
		if err != nil {
			return fmt.Errorf("failed to open file %s: %w", filePath, err)
		}
		defer f.Close()

		// Extract just the filename from path
		filename := filePath
		for i := len(filePath) - 1; i >= 0; i-- {
			if filePath[i] == '/' || filePath[i] == '\\' {
				filename = filePath[i+1:]
				break
			}
		}

		d, err := getDocsClient(cmd)
		if err != nil {
			return err
		}

		input := &sdkdocs.UploadFileInput{
			Filename: filename,
			Reader:   f,
		}

		result, err := d.UploadWithOptions(context.Background(), input, docsAutoExtractFlag)
		if err != nil {
			return fmt.Errorf("failed to upload document: %w", err)
		}

		out := cmd.OutOrStdout()

		if docsOutputFlag == "json" {
			// Output the document directly (flat), consistent with other creation commands.
			// Fall back to the full response if the document is nil (e.g. duplicate).
			if result.Document != nil {
				return json.NewEncoder(out).Encode(result.Document)
			}
			return json.NewEncoder(out).Encode(result)
		}

		if result.IsDuplicate {
			fmt.Fprintf(out, "Document already exists (duplicate).\n")
			if result.ExistingDocumentID != nil {
				fmt.Fprintf(out, "  Existing ID: %s\n", *result.ExistingDocumentID)
			}
			return nil
		}

		fmt.Fprintf(out, "Document uploaded successfully!\n")
		if result.Document != nil {
			fmt.Fprintf(out, "  ID:       %s\n", result.Document.ID)
			fmt.Fprintf(out, "  Name:     %s\n", result.Document.Name)
			fmt.Fprintf(out, "  Status:   %s\n", result.Document.ConversionStatus)
		}
		return nil
	},
}

// ─────────────────────────────────────────────
// documents delete
// ─────────────────────────────────────────────

var documentsDeleteCmd = &cobra.Command{
	Use:   "delete <id>",
	Short: "Delete a document",
	Long: `Delete a document and all related entities.

Prints the deletion status and a summary of removed entities: Chunks,
Extraction jobs, Graph objects, and Graph relationships. Use --output json
for a machine-readable response.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		d, err := getDocsClient(cmd)
		if err != nil {
			return err
		}

		result, err := d.Delete(context.Background(), args[0])
		if err != nil {
			return fmt.Errorf("failed to delete document: %w", err)
		}

		out := cmd.OutOrStdout()

		if docsOutputFlag == "json" {
			return json.NewEncoder(out).Encode(result)
		}

		fmt.Fprintf(out, "Document deleted (status: %s).\n", result.Status)
		if result.Summary != nil {
			fmt.Fprintf(out, "  Chunks removed:             %d\n", result.Summary.Chunks)
			fmt.Fprintf(out, "  Extraction jobs removed:    %d\n", result.Summary.ExtractionJobs)
			fmt.Fprintf(out, "  Graph objects removed:      %d\n", result.Summary.GraphObjects)
			fmt.Fprintf(out, "  Graph relationships removed:%d\n", result.Summary.GraphRelationships)
		}
		return nil
	},
}

// ─────────────────────────────────────────────
// init — wire up the command tree
// ─────────────────────────────────────────────

func init() {
	// Persistent flags on the parent command
	documentsCmd.PersistentFlags().StringVar(&docsProjectFlag, "project", "", "Project ID (overrides config/env)")
	documentsCmd.PersistentFlags().StringVar(&docsOutputFlag, "output", "table", "Output format: table or json")

	// Per-subcommand flags
	documentsListCmd.Flags().IntVar(&docsLimitFlag, "limit", 50, "Maximum number of results")

	documentsUploadCmd.Flags().BoolVar(&docsAutoExtractFlag, "auto-extract", false, "Trigger extraction after upload")

	// Assemble
	documentsCmd.AddCommand(documentsListCmd)
	documentsCmd.AddCommand(documentsGetCmd)
	documentsCmd.AddCommand(documentsUploadCmd)
	documentsCmd.AddCommand(documentsDeleteCmd)

	rootCmd.AddCommand(documentsCmd)
}
