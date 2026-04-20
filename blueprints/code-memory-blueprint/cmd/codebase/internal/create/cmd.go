package create

import (
	"context"
	"fmt"

	sdkgraph "github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/graph"
	"github.com/emergent-company/emergent.memory/blueprints/code-memory-blueprint/cmd/codebase/internal/config"
	"github.com/spf13/cobra"
)

func NewCmd(flagProjectID *string, flagBranch *string, flagFormat *string) *cobra.Command {
	createCmd := &cobra.Command{
		Use:   "create",
		Short: "Create a new graph object",
	}

	keyCmd := &cobra.Command{
		Use:   "key",
		Short: "Generate and print a key for an object type",
	}

	types := []string{"context", "uicomponent", "helper", "action", "apiendpoint", "sourcefile", "domain", "scenario", "step"}

	for _, t := range types {
		createCmd.AddCommand(newCreateTypeCmd(t, flagProjectID, flagBranch))
		keyCmd.AddCommand(newKeyTypeCmd(t))
	}

	return &cobra.Command{
		Use: "root",
		Run: func(cmd *cobra.Command, args []string) {
			createCmd.Execute()
		},
	}
}

// We need a way to return multiple commands to main.go
func Register(rootCmd *cobra.Command, flagProjectID *string, flagBranch *string, flagFormat *string) {
	rootCmd.AddCommand(newCreateRootCmd(flagProjectID, flagBranch, flagFormat))
	rootCmd.AddCommand(newKeyRootCmd())
}

func newCreateRootCmd(flagProjectID *string, flagBranch *string, flagFormat *string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a new graph object",
	}
	types := []string{"context", "uicomponent", "helper", "action", "apiendpoint", "sourcefile", "domain", "scenario", "step"}
	for _, t := range types {
		cmd.AddCommand(newCreateTypeCmd(t, flagProjectID, flagBranch))
	}
	return cmd
}

func newKeyRootCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "key",
		Short: "Generate and print a key for an object type",
	}
	types := []string{"context", "uicomponent", "helper", "action", "apiendpoint", "sourcefile", "domain", "scenario", "step"}
	for _, t := range types {
		cmd.AddCommand(newKeyTypeCmd(t))
	}
	return cmd
}

type createOptions struct {
	upsert bool
	// Common props
	name        string
	description string
	// Context
	route       string
	contextType string
	// UIComponent / Helper
	compType string
	// Action
	displayLabel string
	actionType   string
	domain       string
	// APIEndpoint
	handler      string
	method       string
	path         string
	file         string
	authRequired bool
	// SourceFile
	language string
	// Domain
	slug string
	app  string
	// Scenario
	given string
	when  string
	then  string
	// Step
	order    int
	scenario string
}

func newCreateTypeCmd(objType string, flagProjectID *string, flagBranch *string) *cobra.Command {
	opts := &createOptions{}
	cmd := &cobra.Command{
		Use:   objType + " <name>",
		Short: "Create a " + objType + " object",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			key := generateKey(objType, name, opts)

			c, err := config.New(*flagProjectID, *flagBranch)
			if err != nil {
				return err
			}

			ctx := context.Background()

			// Check if exists
			objs, err := c.Graph.ListObjects(ctx, &sdkgraph.ListObjectsOptions{
				Type: mapType(objType),
			})
			var existing *sdkgraph.GraphObject
			if err == nil {
				for _, o := range objs.Items {
					if o.Key != nil && *o.Key == key {
						existing = o
						break
					}
				}
			}

			if existing != nil {
				if !opts.upsert {
					fmt.Printf("already exists: %s\n", key)
					return nil
				}
				// Update existing
				props := buildProps(objType, name, opts)
				_, err = c.Graph.UpdateObject(ctx, existing.EntityID, &sdkgraph.UpdateObjectRequest{
					Properties: props,
				})
				if err != nil {
					return err
				}
				fmt.Println(key)
				return nil
			}

			// Create new
			props := buildProps(objType, name, opts)
			realType := mapType(objType)
			_, err = c.Graph.CreateObject(ctx, &sdkgraph.CreateObjectRequest{
				Type:       realType,
				Key:        &key,
				Properties: props,
			})
			if err != nil {
				return err
			}

			fmt.Println(key)
			return nil
		},
	}

	addFlags(cmd, objType, opts)
	return cmd
}

func newKeyTypeCmd(objType string) *cobra.Command {
	opts := &createOptions{}
	cmd := &cobra.Command{
		Use:   objType + " <name>",
		Short: "Generate key for " + objType,
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Println(generateKey(objType, args[0], opts))
		},
	}
	addFlags(cmd, objType, opts)
	return cmd
}

func addFlags(cmd *cobra.Command, objType string, opts *createOptions) {
	cmd.Flags().BoolVar(&opts.upsert, "upsert", false, "Update if exists")
	cmd.Flags().StringVar(&opts.name, "name", "", "Display name")
	cmd.Flags().StringVar(&opts.description, "description", "", "Description")

	switch objType {
	case "context":
		cmd.Flags().StringVar(&opts.route, "route", "", "Route path")
		cmd.Flags().StringVar(&opts.contextType, "context-type", "screen", "Context type")
	case "uicomponent":
		cmd.Flags().StringVar(&opts.compType, "type", "composite", "Component type (primitive/composite/layout)")
	case "helper":
		cmd.Flags().StringVar(&opts.compType, "type", "hook", "Helper type")
	case "action":
		cmd.Flags().StringVar(&opts.displayLabel, "display-label", "", "Display label")
		cmd.Flags().StringVar(&opts.actionType, "type", "trigger", "Action type")
		cmd.Flags().StringVar(&opts.domain, "domain", "", "Domain (required for key)")
	case "apiendpoint":
		cmd.Flags().StringVar(&opts.handler, "handler", "", "Handler name")
		cmd.Flags().StringVar(&opts.method, "method", "GET", "HTTP method")
		cmd.Flags().StringVar(&opts.path, "path", "", "API path")
		cmd.Flags().StringVar(&opts.domain, "domain", "", "Domain")
		cmd.Flags().StringVar(&opts.file, "file", "", "Source file")
		cmd.Flags().BoolVar(&opts.authRequired, "auth-required", false, "Auth required")
	case "sourcefile":
		cmd.Flags().StringVar(&opts.path, "path", "", "File path")
		cmd.Flags().StringVar(&opts.language, "language", "typescript", "Language")
	case "domain":
		cmd.Flags().StringVar(&opts.slug, "slug", "", "Slug")
		cmd.Flags().StringVar(&opts.app, "app", "", "App name")
	case "scenario":
		cmd.Flags().StringVar(&opts.given, "given", "", "Given")
		cmd.Flags().StringVar(&opts.when, "when", "", "When")
		cmd.Flags().StringVar(&opts.then, "then", "", "Then")
	case "step":
		cmd.Flags().IntVar(&opts.order, "order", 0, "Order (required for key)")
		cmd.Flags().StringVar(&opts.scenario, "scenario", "", "Scenario key (required for key)")
	}
}

func generateKey(objType, name string, opts *createOptions) string {
	switch objType {
	case "context":
		return ContextKey(name)
	case "uicomponent":
		return UIComponentKey(name)
	case "helper":
		return HelperKey(name)
	case "action":
		return ActionKey(opts.domain, name)
	case "apiendpoint":
		h := opts.handler
		if h == "" {
			h = name
		}
		return APIEndpointKey(opts.domain, h)
	case "sourcefile":
		p := opts.path
		if p == "" {
			p = name
		}
		return SourceFileKey(p)
	case "domain":
		s := opts.slug
		if s == "" {
			s = name
		}
		return DomainKey(s)
	case "scenario":
		return ScenarioKey(name)
	case "step":
		return ScenarioStepKey(opts.scenario, opts.order)
	default:
		return slugify(name)
	}
}

func mapType(objType string) string {
	switch objType {
	case "context":
		return "Context"
	case "uicomponent":
		return "UIComponent"
	case "helper":
		return "Helper"
	case "action":
		return "Action"
	case "apiendpoint":
		return "APIEndpoint"
	case "sourcefile":
		return "SourceFile"
	case "domain":
		return "Domain"
	case "scenario":
		return "Scenario"
	case "step":
		return "ScenarioStep"
	default:
		return objType
	}
}

func buildProps(objType, name string, opts *createOptions) map[string]any {
	p := make(map[string]any)
	displayName := opts.name
	if displayName == "" {
		displayName = name
	}
	p["name"] = displayName
	if opts.description != "" {
		p["description"] = opts.description
	}

	switch objType {
	case "context":
		p["route"] = opts.route
		p["context_type"] = opts.contextType
		p["type"] = "screen"
	case "uicomponent", "helper":
		p["type"] = opts.compType
	case "action":
		dl := opts.displayLabel
		if dl == "" {
			dl = displayName
		}
		p["display_label"] = dl
		p["type"] = opts.actionType
	case "apiendpoint":
		p["handler"] = opts.handler
		p["method"] = opts.method
		p["path"] = opts.path
		p["domain"] = opts.domain
		p["file"] = opts.file
		p["auth_required"] = opts.authRequired
	case "sourcefile":
		path := opts.path
		if path == "" {
			path = name
		}
		p["path"] = path
		p["language"] = opts.language
	case "domain":
		p["slug"] = opts.slug
		p["app"] = opts.app
	case "scenario":
		p["given"] = opts.given
		p["when"] = opts.when
		p["then"] = opts.then
	case "step":
		p["order"] = opts.order
	}
	return p
}
