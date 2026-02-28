package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var adkSessionsProjectID string

func newADKSessionsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "adk-sessions",
		Short:   "Manage and inspect ADK sessions",
		Aliases: []string{"sessions"},
	}

	cmd.PersistentFlags().StringVar(&adkSessionsProjectID, "project-id", "", "Project name or ID")

	cmd.AddCommand(
		newListADKSessionsCmd(),
		newGetADKSessionCmd(),
	)

	return cmd
}

func newListADKSessionsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List ADK sessions for the active project",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()
			client, err := getClient(cmd)
			if err != nil {
				return err
			}

			projectID, err := resolveProjectContext(cmd, adkSessionsProjectID)
			if err != nil {
				return err
			}

			sessions, err := client.SDK.Agents.ListADKSessions(ctx, projectID)
			if err != nil {
				return fmt.Errorf("failed to list adk sessions: %w", err)
			}

			if len(sessions) == 0 {
				fmt.Println("No ADK sessions found")
				return nil
			}

			// Format output
			for _, s := range sessions {
				fmt.Printf("ID: %s | App: %s | User: %s | Updated: %s\n", s.ID, s.AppName, s.UserID, s.UpdateTime.Format("2006-01-02 15:04:05"))
			}

			return nil
		},
	}
	return cmd
}

func newGetADKSessionCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "get [id]",
		Short: "Get details and event history for a specific ADK session",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()
			client, err := getClient(cmd)
			if err != nil {
				return err
			}

			projectID, err := resolveProjectContext(cmd, adkSessionsProjectID)
			if err != nil {
				return err
			}

			sessionID := args[0]
			session, err := client.SDK.Agents.GetADKSession(ctx, projectID, sessionID)
			if err != nil {
				return fmt.Errorf("failed to get adk session: %w", err)
			}

			// Dump as JSON for inspection
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			if err := enc.Encode(session); err != nil {
				return fmt.Errorf("failed to encode session json: %w", err)
			}

			return nil
		},
	}
	return cmd
}

func init() {
	rootCmd.AddCommand(newADKSessionsCmd())
}
