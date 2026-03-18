package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/olekukonko/tablewriter"
	"github.com/spf13/cobra"
)

// ─────────────────────────────────────────────
// Top-level and sub-group commands
// ─────────────────────────────────────────────

var extractionCmd = &cobra.Command{
	Use:     "extraction",
	Short:   "Manage extraction operations",
	Long:    "Commands for managing extraction jobs and related operations in the Memory platform",
	GroupID: "knowledge",
}

var extractionJobsCmd = &cobra.Command{
	Use:   "jobs",
	Short: "Manage extraction jobs",
	Long:  "Create, monitor, and manage extraction jobs that extract structured entities from documents",
}

// ─────────────────────────────────────────────
// Flag variables
// ─────────────────────────────────────────────

var (
	extractionProjectFlag  string
	extractionOutputFlag   string
	extractionDocumentFlag string
	extractionStatusFlag   string
	extractionLimitFlag    int
)

// ─────────────────────────────────────────────
// Helper: get HTTP client with auth
// ─────────────────────────────────────────────

// getExtractionHTTPClient returns (baseURL, apiKey, *http.Client) for making
// raw HTTP calls to the admin extraction API. It uses the existing getClient
// helper to retrieve authentication credentials.
func getExtractionHTTPClient(cmd *cobra.Command) (string, string, *http.Client, error) {
	c, err := getClient(cmd)
	if err != nil {
		return "", "", nil, err
	}

	baseURL := c.BaseURL()
	apiKey := c.APIKey()
	httpClient := &http.Client{Timeout: 30 * time.Second}

	return baseURL, apiKey, httpClient, nil
}

// ─────────────────────────────────────────────
// extraction jobs create
// ─────────────────────────────────────────────

var extractionJobsCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a new extraction job",
	Long: `Create a new extraction job for a document.

Requires --project and --document flags. The extraction job will process the
document and extract structured entities based on any installed schemas.

Requires an API key with admin:write scope.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if extractionDocumentFlag == "" {
			return fmt.Errorf("--document is required")
		}

		baseURL, apiKey, httpClient, err := getExtractionHTTPClient(cmd)
		if err != nil {
			return err
		}

		projectID, err := resolveProjectContext(cmd, extractionProjectFlag)
		if err != nil {
			return err
		}

		// Build request body
		reqBody := map[string]string{
			"project_id":  projectID,
			"source_id":   extractionDocumentFlag,
			"source_type": "document",
		}
		bodyJSON, _ := json.Marshal(reqBody)

		url := baseURL + "/api/admin/extraction-jobs"
		req, err := http.NewRequest(http.MethodPost, url, strings.NewReader(string(bodyJSON)))
		if err != nil {
			return fmt.Errorf("failed to create request: %w", err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-API-Key", apiKey)

		resp, err := httpClient.Do(req)
		if err != nil {
			return fmt.Errorf("failed to create extraction job: %w", err)
		}
		defer resp.Body.Close()

		body, _ := io.ReadAll(resp.Body)
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return fmt.Errorf("server returned status %d: %s", resp.StatusCode, string(body))
		}

		out := cmd.OutOrStdout()

		if extractionOutputFlag == "json" {
			fmt.Fprintln(out, string(body))
			return nil
		}

		// Parse response for table output
		var result struct {
			Success bool `json:"success"`
			Data    struct {
				ID     string `json:"id"`
				Status string `json:"status"`
			} `json:"data"`
		}
		if err := json.Unmarshal(body, &result); err == nil && result.Success {
			fmt.Fprintf(out, "Extraction job created successfully\n")
			fmt.Fprintf(out, "  Job ID: %s\n", result.Data.ID)
			fmt.Fprintf(out, "  Status: %s\n", result.Data.Status)
		} else {
			fmt.Fprintln(out, string(body))
		}

		return nil
	},
}

// ─────────────────────────────────────────────
// extraction jobs get
// ─────────────────────────────────────────────

var extractionJobsGetCmd = &cobra.Command{
	Use:   "get <job-id>",
	Short: "Get extraction job details",
	Long: `Get detailed information about an extraction job by its ID.

Shows the job status, progress, created/updated timestamps, and any error messages.

Requires an API key with admin:read scope.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		jobID := args[0]

		baseURL, apiKey, httpClient, err := getExtractionHTTPClient(cmd)
		if err != nil {
			return err
		}

		url := baseURL + "/api/admin/extraction-jobs/" + jobID
		req, err := http.NewRequest(http.MethodGet, url, nil)
		if err != nil {
			return fmt.Errorf("failed to create request: %w", err)
		}
		req.Header.Set("X-API-Key", apiKey)

		resp, err := httpClient.Do(req)
		if err != nil {
			return fmt.Errorf("failed to get extraction job: %w", err)
		}
		defer resp.Body.Close()

		body, _ := io.ReadAll(resp.Body)
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return fmt.Errorf("server returned status %d: %s", resp.StatusCode, string(body))
		}

		out := cmd.OutOrStdout()

		if extractionOutputFlag == "json" {
			fmt.Fprintln(out, string(body))
			return nil
		}

		// Parse response for table output
		var result struct {
			Success bool `json:"success"`
			Data    struct {
				ID              string    `json:"id"`
				ProjectID       string    `json:"project_id"`
				SourceID        string    `json:"source_id"`
				SourceType      string    `json:"source_type"`
				Status          string    `json:"status"`
				ObjectsCreated  int       `json:"objects_created"`
				Error           string    `json:"error,omitempty"`
				CreatedAt       time.Time `json:"created_at"`
				UpdatedAt       time.Time `json:"updated_at"`
				CompletedAt     time.Time `json:"completed_at,omitempty"`
				DiscoveredTypes []string  `json:"discovered_types,omitempty"`
			} `json:"data"`
		}
		if err := json.Unmarshal(body, &result); err == nil && result.Success {
			data := result.Data
			fmt.Fprintf(out, "Job ID:          %s\n", data.ID)
			fmt.Fprintf(out, "Project ID:      %s\n", data.ProjectID)
			fmt.Fprintf(out, "Source ID:       %s\n", data.SourceID)
			fmt.Fprintf(out, "Source Type:     %s\n", data.SourceType)
			fmt.Fprintf(out, "Status:          %s\n", data.Status)
			fmt.Fprintf(out, "Objects Created: %d\n", data.ObjectsCreated)
			if len(data.DiscoveredTypes) > 0 {
				fmt.Fprintf(out, "Discovered Types: %s\n", strings.Join(data.DiscoveredTypes, ", "))
			}
			fmt.Fprintf(out, "Created At:      %s\n", data.CreatedAt.Format(time.RFC3339))
			fmt.Fprintf(out, "Updated At:      %s\n", data.UpdatedAt.Format(time.RFC3339))
			if !data.CompletedAt.IsZero() {
				fmt.Fprintf(out, "Completed At:    %s\n", data.CompletedAt.Format(time.RFC3339))
			}
			if data.Error != "" {
				fmt.Fprintf(out, "Error:           %s\n", data.Error)
			}
		} else {
			fmt.Fprintln(out, string(body))
		}

		return nil
	},
}

// ─────────────────────────────────────────────
// extraction jobs list
// ─────────────────────────────────────────────

var extractionJobsListCmd = &cobra.Command{
	Use:   "list",
	Short: "List extraction jobs",
	Long: `List extraction jobs for a project.

Use --status to filter by job status (queued, running, completed, failed, cancelled).
Use --document to filter by source document ID.
Use --limit to control the number of results (default 50).

Requires an API key with admin:read scope.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		baseURL, apiKey, httpClient, err := getExtractionHTTPClient(cmd)
		if err != nil {
			return err
		}

		projectID, err := resolveProjectContext(cmd, extractionProjectFlag)
		if err != nil {
			return err
		}

		// Build query parameters
		url := fmt.Sprintf("%s/api/admin/extraction-jobs/projects/%s", baseURL, projectID)
		params := []string{}
		if extractionStatusFlag != "" {
			params = append(params, fmt.Sprintf("status=%s", extractionStatusFlag))
		}
		if extractionDocumentFlag != "" {
			params = append(params, fmt.Sprintf("source_id=%s", extractionDocumentFlag))
		}
		if extractionLimitFlag > 0 {
			params = append(params, fmt.Sprintf("limit=%d", extractionLimitFlag))
		}
		if len(params) > 0 {
			url += "?" + strings.Join(params, "&")
		}

		req, err := http.NewRequest(http.MethodGet, url, nil)
		if err != nil {
			return fmt.Errorf("failed to create request: %w", err)
		}
		req.Header.Set("X-API-Key", apiKey)

		resp, err := httpClient.Do(req)
		if err != nil {
			return fmt.Errorf("failed to list extraction jobs: %w", err)
		}
		defer resp.Body.Close()

		body, _ := io.ReadAll(resp.Body)
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return fmt.Errorf("server returned status %d: %s", resp.StatusCode, string(body))
		}

		out := cmd.OutOrStdout()

		if extractionOutputFlag == "json" {
			fmt.Fprintln(out, string(body))
			return nil
		}

		// Parse response for table output
		var result struct {
			Success bool `json:"success"`
			Data    struct {
				Jobs []struct {
					ID          string    `json:"id"`
					SourceID    string    `json:"source_id"`
					Status      string    `json:"status"`
					CreatedAt   time.Time `json:"created_at"`
					CompletedAt time.Time `json:"completed_at,omitempty"`
				} `json:"jobs"`
			} `json:"data"`
		}

		if err := json.Unmarshal(body, &result); err == nil && result.Success {
			if len(result.Data.Jobs) == 0 {
				fmt.Fprintln(out, "No extraction jobs found.")
				return nil
			}

			table := tablewriter.NewWriter(out)
			table.Header("Job ID", "Source ID", "Status", "Created At")
			for _, job := range result.Data.Jobs {
				_ = table.Append(
					job.ID,
					job.SourceID,
					job.Status,
					job.CreatedAt.Format("2006-01-02 15:04:05"),
				)
			}
			return table.Render()
		}

		fmt.Fprintln(out, string(body))
		return nil
	},
}

// ─────────────────────────────────────────────
// extraction jobs cancel
// ─────────────────────────────────────────────

var extractionJobsCancelCmd = &cobra.Command{
	Use:   "cancel <job-id>",
	Short: "Cancel an extraction job",
	Long: `Cancel a running or queued extraction job.

Completed or failed jobs cannot be cancelled.

Requires an API key with admin:write scope.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		jobID := args[0]

		baseURL, apiKey, httpClient, err := getExtractionHTTPClient(cmd)
		if err != nil {
			return err
		}

		url := fmt.Sprintf("%s/api/admin/extraction-jobs/%s/cancel", baseURL, jobID)
		req, err := http.NewRequest(http.MethodPost, url, nil)
		if err != nil {
			return fmt.Errorf("failed to create request: %w", err)
		}
		req.Header.Set("X-API-Key", apiKey)

		resp, err := httpClient.Do(req)
		if err != nil {
			return fmt.Errorf("failed to cancel extraction job: %w", err)
		}
		defer resp.Body.Close()

		body, _ := io.ReadAll(resp.Body)
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return fmt.Errorf("server returned status %d: %s", resp.StatusCode, string(body))
		}

		fmt.Fprintf(cmd.OutOrStdout(), "Extraction job %s cancelled successfully\n", jobID)
		return nil
	},
}

// ─────────────────────────────────────────────
// extraction jobs retry
// ─────────────────────────────────────────────

var extractionJobsRetryCmd = &cobra.Command{
	Use:   "retry <job-id>",
	Short: "Retry a failed extraction job",
	Long: `Retry a failed extraction job.

The job will be re-queued for processing with the same configuration.

Requires an API key with admin:write scope.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		jobID := args[0]

		baseURL, apiKey, httpClient, err := getExtractionHTTPClient(cmd)
		if err != nil {
			return err
		}

		url := fmt.Sprintf("%s/api/admin/extraction-jobs/%s/retry", baseURL, jobID)
		req, err := http.NewRequest(http.MethodPost, url, nil)
		if err != nil {
			return fmt.Errorf("failed to create request: %w", err)
		}
		req.Header.Set("X-API-Key", apiKey)

		resp, err := httpClient.Do(req)
		if err != nil {
			return fmt.Errorf("failed to retry extraction job: %w", err)
		}
		defer resp.Body.Close()

		body, _ := io.ReadAll(resp.Body)
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return fmt.Errorf("server returned status %d: %s", resp.StatusCode, string(body))
		}

		fmt.Fprintf(cmd.OutOrStdout(), "Extraction job %s retry initiated successfully\n", jobID)
		return nil
	},
}

// ─────────────────────────────────────────────
// extraction jobs logs
// ─────────────────────────────────────────────

var extractionJobsLogsCmd = &cobra.Command{
	Use:   "logs <job-id>",
	Short: "Get logs for an extraction job",
	Long: `Get execution logs for an extraction job.

Shows processing steps, entity discoveries, and any errors encountered during extraction.

Requires an API key with admin:read scope.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		jobID := args[0]

		baseURL, apiKey, httpClient, err := getExtractionHTTPClient(cmd)
		if err != nil {
			return err
		}

		url := fmt.Sprintf("%s/api/admin/extraction-jobs/%s/logs", baseURL, jobID)
		req, err := http.NewRequest(http.MethodGet, url, nil)
		if err != nil {
			return fmt.Errorf("failed to create request: %w", err)
		}
		req.Header.Set("X-API-Key", apiKey)

		resp, err := httpClient.Do(req)
		if err != nil {
			return fmt.Errorf("failed to get extraction job logs: %w", err)
		}
		defer resp.Body.Close()

		body, _ := io.ReadAll(resp.Body)
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return fmt.Errorf("server returned status %d: %s", resp.StatusCode, string(body))
		}

		out := cmd.OutOrStdout()

		if extractionOutputFlag == "json" {
			fmt.Fprintln(out, string(body))
			return nil
		}

		// Parse response for table output
		var result struct {
			Success bool `json:"success"`
			Data    struct {
				Logs []struct {
					Timestamp string `json:"timestamp"`
					Level     string `json:"level"`
					Message   string `json:"message"`
				} `json:"logs"`
			} `json:"data"`
		}

		if err := json.Unmarshal(body, &result); err == nil && result.Success {
			if len(result.Data.Logs) == 0 {
				fmt.Fprintln(out, "No logs available for this job.")
				return nil
			}

			for _, log := range result.Data.Logs {
				fmt.Fprintf(out, "[%s] %s: %s\n", log.Timestamp, log.Level, log.Message)
			}
			return nil
		}

		fmt.Fprintln(out, string(body))
		return nil
	},
}

// ─────────────────────────────────────────────
// extraction jobs stats
// ─────────────────────────────────────────────

var extractionJobsStatsCmd = &cobra.Command{
	Use:   "stats",
	Short: "Get extraction job statistics for a project",
	Long: `Get aggregated statistics for extraction jobs in a project.

Shows counts by status (queued, running, completed, failed, cancelled) and other metrics.

Requires an API key with admin:read scope.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		baseURL, apiKey, httpClient, err := getExtractionHTTPClient(cmd)
		if err != nil {
			return err
		}

		projectID, err := resolveProjectContext(cmd, extractionProjectFlag)
		if err != nil {
			return err
		}

		url := fmt.Sprintf("%s/api/admin/extraction-jobs/projects/%s/statistics", baseURL, projectID)
		req, err := http.NewRequest(http.MethodGet, url, nil)
		if err != nil {
			return fmt.Errorf("failed to create request: %w", err)
		}
		req.Header.Set("X-API-Key", apiKey)

		resp, err := httpClient.Do(req)
		if err != nil {
			return fmt.Errorf("failed to get extraction job stats: %w", err)
		}
		defer resp.Body.Close()

		body, _ := io.ReadAll(resp.Body)
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return fmt.Errorf("server returned status %d: %s", resp.StatusCode, string(body))
		}

		out := cmd.OutOrStdout()

		if extractionOutputFlag == "json" {
			fmt.Fprintln(out, string(body))
			return nil
		}

		// Parse response for table output
		var result struct {
			Success bool `json:"success"`
			Data    struct {
				Total     int `json:"total"`
				Queued    int `json:"queued"`
				Running   int `json:"running"`
				Completed int `json:"completed"`
				Failed    int `json:"failed"`
				Cancelled int `json:"cancelled"`
			} `json:"data"`
		}

		if err := json.Unmarshal(body, &result); err == nil && result.Success {
			stats := result.Data
			fmt.Fprintf(out, "Extraction Job Statistics\n\n")
			fmt.Fprintf(out, "Total:     %d\n", stats.Total)
			fmt.Fprintf(out, "Queued:    %d\n", stats.Queued)
			fmt.Fprintf(out, "Running:   %d\n", stats.Running)
			fmt.Fprintf(out, "Completed: %d\n", stats.Completed)
			fmt.Fprintf(out, "Failed:    %d\n", stats.Failed)
			fmt.Fprintf(out, "Cancelled: %d\n", stats.Cancelled)
			return nil
		}

		fmt.Fprintln(out, string(body))
		return nil
	},
}

// ─────────────────────────────────────────────
// init — wire up the command tree
// ─────────────────────────────────────────────

func init() {
	// Persistent flags for extraction commands
	extractionCmd.PersistentFlags().StringVar(&extractionProjectFlag, "project", "", "Project ID (overrides config/env)")
	extractionCmd.PersistentFlags().StringVar(&extractionOutputFlag, "output", "table", "Output format: table or json")

	// jobs create flags
	extractionJobsCreateCmd.Flags().StringVar(&extractionDocumentFlag, "document", "", "Document ID to extract from (required)")
	_ = extractionJobsCreateCmd.MarkFlagRequired("document")

	// jobs list flags
	extractionJobsListCmd.Flags().StringVar(&extractionStatusFlag, "status", "", "Filter by status (queued, running, completed, failed, cancelled)")
	extractionJobsListCmd.Flags().StringVar(&extractionDocumentFlag, "document", "", "Filter by document ID")
	extractionJobsListCmd.Flags().IntVar(&extractionLimitFlag, "limit", 50, "Maximum number of results")

	// Assemble jobs subcommands
	extractionJobsCmd.AddCommand(extractionJobsCreateCmd)
	extractionJobsCmd.AddCommand(extractionJobsGetCmd)
	extractionJobsCmd.AddCommand(extractionJobsListCmd)
	extractionJobsCmd.AddCommand(extractionJobsCancelCmd)
	extractionJobsCmd.AddCommand(extractionJobsRetryCmd)
	extractionJobsCmd.AddCommand(extractionJobsLogsCmd)
	extractionJobsCmd.AddCommand(extractionJobsStatsCmd)

	// Assemble top-level extraction command
	extractionCmd.AddCommand(extractionJobsCmd)

	// Register with root command
	rootCmd.AddCommand(extractionCmd)
}
