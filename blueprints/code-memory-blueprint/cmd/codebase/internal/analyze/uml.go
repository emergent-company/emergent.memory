package analyzecmd

import (
	"context"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"sync"

	"github.com/emergent-company/emergent.memory/apps/server/pkg/sdk"
	sdkgraph "github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/graph"
	"github.com/emergent-company/emergent.memory/blueprints/code-memory-blueprint/cmd/codebase/internal/config"
	"github.com/spf13/cobra"
)

func newUMLCmd(flagProjectID *string, flagBranch *string, flagFormat *string) *cobra.Command {
	var (
		flagDomain   string
		flagSchema   string
		flagNoFields bool
	)

	cmd := &cobra.Command{
		Use:   "uml",
		Short: "Generate UML diagrams (PlantUML or Mermaid)",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.New(*flagProjectID, *flagBranch)
			if err != nil {
				return err
			}
			return runUML(cfg.SDK, flagDomain, flagSchema, flagNoFields, *flagFormat)
		},
	}

	cmd.Flags().StringVar(&flagDomain, "domain", "", "Filter to entities from one domain")
	cmd.Flags().StringVar(&flagSchema, "schema", "plantuml", "Diagram schema (plantuml|mermaid)")
	cmd.Flags().BoolVar(&flagNoFields, "no-fields", false, "Omit field details")

	return cmd
}

func runUML(client *sdk.Client, domain, schemaType string, noFields bool, format string) error {
	ctx := context.Background()
	data, err := fetchUMLData(ctx, client)
	if err != nil {
		return err
	}

	var out io.Writer = os.Stdout
	if schemaType == "mermaid" {
		return renderMermaid(out, data, domain, noFields)
	}
	return renderPlantUML(out, data, domain, noFields)
}

type UMLData struct {
	Entities   []*sdkgraph.GraphObject
	Fields     []*sdkgraph.GraphObject
	HasField   []*sdkgraph.GraphRelationship
	References []*sdkgraph.GraphRelationship
}

func fetchUMLData(ctx context.Context, client *sdk.Client) (*UMLData, error) {
	data := &UMLData{}
	var wg sync.WaitGroup
	var errs []error
	var mu sync.Mutex

	fetch := func(fn func() error) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := fn(); err != nil {
				mu.Lock()
				errs = append(errs, err)
				mu.Unlock()
			}
		}()
	}

	fetch(func() error { items, err := listAll(ctx, client.Graph, "Entity"); data.Entities = items; return err })
	fetch(func() error { items, err := listAll(ctx, client.Graph, "Field"); data.Fields = items; return err })
	fetch(func() error {
		items, err := listAllRels(ctx, client.Graph, "has_field")
		data.HasField = items
		return err
	})
	fetch(func() error {
		items, err := listAllRels(ctx, client.Graph, "references")
		data.References = items
		return err
	})

	wg.Wait()
	if len(errs) > 0 {
		return nil, errs[0]
	}
	return data, nil
}

func renderPlantUML(w io.Writer, data *UMLData, domainFilter string, noFields bool) error {
	fmt.Fprintln(w, "@startuml")
	fmt.Fprintln(w, "!theme plain")
	fmt.Fprintln(w, "skinparam classAttributeIconSize 0")
	fmt.Fprintln(w, "skinparam classFontSize 12")
	fmt.Fprintln(w, "skinparam packageStyle rectangle")

	entityMap := make(map[string]*sdkgraph.GraphObject)
	for _, e := range data.Entities {
		entityMap[e.EntityID] = e
	}

	fieldMap := make(map[string]*sdkgraph.GraphObject)
	for _, f := range data.Fields {
		fieldMap[f.EntityID] = f
	}

	entityFields := make(map[string][]*sdkgraph.GraphObject)
	for _, rel := range data.HasField {
		if f, ok := fieldMap[rel.DstID]; ok {
			entityFields[rel.SrcID] = append(entityFields[rel.SrcID], f)
		}
	}

	schemas := make(map[string][]*sdkgraph.GraphObject)
	for _, e := range data.Entities {
		domain := sp(e, "domain")
		if domainFilter != "" && domain != domainFilter {
			continue
		}
		schema := sp(e, "db_schema")
		schemas[schema] = append(schemas[schema], e)
	}

	schemaNames := make([]string, 0, len(schemas))
	for s := range schemas {
		schemaNames = append(schemaNames, s)
	}
	sort.Strings(schemaNames)

	for _, s := range schemaNames {
		fmt.Fprintf(w, "\npackage \"%s\" {\n", s)
		ents := schemas[s]
		sort.Slice(ents, func(i, j int) bool { return sp(ents[i], "name") < sp(ents[j], "name") })
		for _, e := range ents {
			alias := strings.ReplaceAll(strings.TrimPrefix(derefStr(e.Key), "entity-"), "-", "_")
			fmt.Fprintf(w, "  class \"%s\" as entity_%s {\n", sp(e, "name"), alias)
			if !noFields {
				fields := entityFields[e.EntityID]
				sort.Slice(fields, func(i, j int) bool { return getPropInt(fields[i], "ordinal") < getPropInt(fields[j], "ordinal") })
				for _, f := range fields {
					renderPlantUMLField(w, f)
				}
			}
			fmt.Fprintln(w, "  }")
		}
		fmt.Fprintln(w, "}")
	}

	for _, rel := range data.References {
		src, ok1 := entityMap[rel.SrcID]
		dst, ok2 := entityMap[rel.DstID]
		if !ok1 || !ok2 {
			continue
		}
		if (domainFilter != "" && sp(src, "domain") != domainFilter) || (domainFilter != "" && sp(dst, "domain") != domainFilter) {
			continue
		}
		srcAlias := strings.ReplaceAll(strings.TrimPrefix(derefStr(src.Key), "entity-"), "-", "_")
		dstAlias := strings.ReplaceAll(strings.TrimPrefix(derefStr(dst.Key), "entity-"), "-", "_")
		label := ""
		if v, ok := rel.Properties["via_field"]; ok {
			label = fmt.Sprintf(" : %v", v)
		}
		fmt.Fprintf(w, "entity_%s --> entity_%s%s\n", srcAlias, dstAlias, label)
	}

	fmt.Fprintln(w, "@enduml")
	return nil
}

func renderPlantUMLField(w io.Writer, f *sdkgraph.GraphObject) {
	name := sp(f, "name")
	typ := sp(f, "db_type")
	if typ == "" {
		typ = inferDBType(sp(f, "go_type"))
	}
	typ = strings.ReplaceAll(typ, ",", ";")
	suffix := ""
	if getPropBool(f, "is_pk") {
		suffix = " <<PK>>"
	}
	fmt.Fprintf(w, "    + %s : %s%s\n", name, typ, suffix)
}

func renderMermaid(w io.Writer, data *UMLData, domainFilter string, noFields bool) error {
	fmt.Fprintln(w, "erDiagram")
	entityMap := make(map[string]*sdkgraph.GraphObject)
	for _, e := range data.Entities {
		entityMap[e.EntityID] = e
	}
	fieldMap := make(map[string]*sdkgraph.GraphObject)
	for _, f := range data.Fields {
		fieldMap[f.EntityID] = f
	}
	entityFields := make(map[string][]*sdkgraph.GraphObject)
	for _, rel := range data.HasField {
		if f, ok := fieldMap[rel.DstID]; ok {
			entityFields[rel.SrcID] = append(entityFields[rel.SrcID], f)
		}
	}

	for _, e := range data.Entities {
		if domainFilter != "" && sp(e, "domain") != domainFilter {
			continue
		}
		tableName := sp(e, "table")
		if tableName == "" {
			tableName = strings.ReplaceAll(derefStr(e.Key), "-", "_")
		}
		fmt.Fprintf(w, "    %s {\n", tableName)
		if !noFields {
			fields := entityFields[e.EntityID]
			sort.Slice(fields, func(i, j int) bool { return getPropInt(fields[i], "ordinal") < getPropInt(fields[j], "ordinal") })
			for _, f := range fields {
				typ := sp(f, "db_type")
				if typ == "" {
					typ = inferDBType(sp(f, "go_type"))
				}
				fmt.Fprintf(w, "        %s %s\n", typ, sp(f, "name"))
			}
		}
		fmt.Fprintln(w, "    }")
	}

	for _, rel := range data.References {
		src, ok1 := entityMap[rel.SrcID]
		dst, ok2 := entityMap[rel.DstID]
		if !ok1 || !ok2 {
			continue
		}
		if (domainFilter != "" && sp(src, "domain") != domainFilter) || (domainFilter != "" && sp(dst, "domain") != domainFilter) {
			continue
		}
		srcTable := sp(src, "table")
		if srcTable == "" {
			srcTable = strings.ReplaceAll(derefStr(src.Key), "-", "_")
		}
		dstTable := sp(dst, "table")
		if dstTable == "" {
			dstTable = strings.ReplaceAll(derefStr(dst.Key), "-", "_")
		}
		label := spRel(rel, "via_field")
		fmt.Fprintf(w, "    %s }o--|| %s : \"%s\"\n", srcTable, dstTable, label)
	}
	return nil
}

func inferDBType(goType string) string {
	goType = strings.TrimPrefix(goType, "*")
	switch goType {
	case "uuid.UUID", "uuid.NullUUID":
		return "uuid"
	case "string":
		return "text"
	case "bool":
		return "boolean"
	case "int", "int64", "int32":
		return "integer"
	case "time.Time":
		return "timestamptz"
	default:
		return "text"
	}
}

func getPropBool(obj *sdkgraph.GraphObject, key string) bool {
	if v, ok := obj.Properties[key]; ok {
		if b, ok := v.(bool); ok {
			return b
		}
	}
	return false
}

func getPropInt(obj *sdkgraph.GraphObject, key string) int {
	if v, ok := obj.Properties[key]; ok {
		if f, ok := v.(float64); ok {
			return int(f)
		}
	}
	return 0
}

func spRel(r *sdkgraph.GraphRelationship, key string) string {
	if v, ok := r.Properties[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}
