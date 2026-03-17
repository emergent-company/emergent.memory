package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/orgs"
	"github.com/spf13/cobra"
)

var orgsCmd = &cobra.Command{
	Use:     "orgs",
	Short:   "Manage organizations",
	Long:    "Commands for managing organizations in the Memory platform",
	GroupID: "account",
}

var listOrgsCmd = &cobra.Command{
	Use:   "list",
	Short: "List all organizations",
	Long: `List all organizations you are a member of.

Output prints a numbered list with each organization's Name and ID.`,
	RunE: runListOrgs,
}

var getOrgCmd = &cobra.Command{
	Use:   "get <id>",
	Short: "Get organization details",
	Long:  "Get details for a specific organization by ID.",
	Args:  cobra.ExactArgs(1),
	RunE:  runGetOrg,
}

var createOrgCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a new organization",
	Long: `Create a new organization in the Memory platform.

Prints the new organization's Name and ID on success.`,
	RunE: runCreateOrg,
}

var deleteOrgCmd = &cobra.Command{
	Use:   "delete <id>",
	Short: "Delete an organization",
	Long:  "Permanently delete an organization by ID.",
	Args:  cobra.ExactArgs(1),
	RunE:  runDeleteOrg,
}

var orgName string

func runListOrgs(cmd *cobra.Command, args []string) error {
	c, err := getAccountClient(cmd)
	if err != nil {
		return err
	}

	orgList, err := c.SDK.Orgs.List(context.Background())
	if err != nil {
		return fmt.Errorf("failed to list organizations: %w", err)
	}

	if len(orgList) == 0 {
		if output == "json" {
			fmt.Println("[]")
			return nil
		}
		fmt.Println("No organizations found.")
		return nil
	}

	if output == "json" {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(orgList)
	}

	fmt.Printf("Found %d organization(s):\n\n", len(orgList))
	for i, o := range orgList {
		fmt.Printf("%d. %s (%s)\n", i+1, o.Name, o.ID)
	}

	return nil
}

func runGetOrg(cmd *cobra.Command, args []string) error {
	c, err := getAccountClient(cmd)
	if err != nil {
		return err
	}

	org, err := c.SDK.Orgs.Get(context.Background(), args[0])
	if err != nil {
		return fmt.Errorf("failed to get organization: %w", err)
	}

	if output == "json" {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(org)
	}

	fmt.Printf("Organization: %s (%s)\n", org.Name, org.ID)

	return nil
}

func runCreateOrg(cmd *cobra.Command, args []string) error {
	if orgName == "" {
		return fmt.Errorf("organization name is required. Use --name flag")
	}

	c, err := getAccountClient(cmd)
	if err != nil {
		return err
	}

	org, err := c.SDK.Orgs.Create(context.Background(), &orgs.CreateOrganizationRequest{
		Name: orgName,
	})
	if err != nil {
		return fmt.Errorf("failed to create organization: %w", err)
	}

	if output == "json" {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(org)
	}

	fmt.Println("Organization created successfully!")
	fmt.Printf("  Name: %s (%s)\n", org.Name, org.ID)

	return nil
}

func runDeleteOrg(cmd *cobra.Command, args []string) error {
	c, err := getAccountClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := c.SDK.Orgs.Delete(ctx, args[0]); err != nil {
		return fmt.Errorf("failed to delete organization: %w", err)
	}

	fmt.Printf("Organization %s deleted.\n", args[0])
	return nil
}

func init() {
	createOrgCmd.Flags().StringVar(&orgName, "name", "", "Organization name (required)")
	_ = createOrgCmd.MarkFlagRequired("name")

	orgsCmd.AddCommand(listOrgsCmd)
	orgsCmd.AddCommand(getOrgCmd)
	orgsCmd.AddCommand(createOrgCmd)
	orgsCmd.AddCommand(deleteOrgCmd)
	rootCmd.AddCommand(orgsCmd)
}
