package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"sync"

	"github.com/emergent-company/emergent.memory/apps/server/pkg/sdk"
	sdkgraph "github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/graph"
)

type Config struct {
	Format        string
	Domain        string
	Schema        string
	Out           string
	NoFields      bool
	RelationsOnly bool
}

func main() {
	cfg := Config{}
	flag.StringVar(&cfg.Format, "format", "plantuml", "Output format (plantuml|mermaid)")
	flag.StringVar(&cfg.Domain, "domain", "", "Filter to entities from one domain")
	flag.StringVar(&cfg.Schema, "schema", "", "Filter to one DB schema")
	flag.StringVar(&cfg.Out, "out", "", "Write output to file (default: stdout)")
	flag.BoolVar(&cfg.NoFields, "no-fields", false, "Omit field details")
	flag.BoolVar(&cfg.RelationsOnly, "relations-only", false, "Only show entities with references")
	flag.Parse()

	client, err := sdk.NewFromEnv()
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to create sdk client: %v\n", err)
		os.Exit(1)
	}

	ctx := context.Background()
	data, err := fetchData(ctx, client)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to fetch data: %v\n", err)
		os.Exit(1)
	}

	var out io.Writer = os.Stdout
	if cfg.Out != "" {
		f, err := os.Create(cfg.Out)
		if err != nil {
			fmt.Fprintf(os.Stderr, "failed to create output file: %v\n", err)
			os.Exit(1)
		}
		defer f.Close()
		out = f
	}

	if cfg.Format == "mermaid" {
		err = renderMermaid(out, data, cfg)
	} else {
		err = renderPlantUML(out, data, cfg)
	}

	if err != nil {
		fmt.Fprintf(os.Stderr, "render failed: %v\n", err)
		os.Exit(1)
	}
}

type GraphData struct {
	Entities      []*sdkgraph.GraphObject
	Fields        []*sdkgraph.GraphObject
	HasField      []*sdkgraph.GraphRelationship
	References    []*sdkgraph.GraphRelationship
}

func fetchData(ctx context.Context, client *sdk.Client) (*GraphData, error) {
	data := &GraphData{}
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

	fetch(func() error {
		items, err := listAllObjects(ctx, client, "Entity")
		data.Entities = items
		return err
	})
	fetch(func() error {
		items, err := listAllObjects(ctx, client, "Field")
		data.Fields = items
		return err
	})
	fetch(func() error {
		items, err := listAllRelationships(ctx, client, "has_field")
		data.HasField = items
		return err
	})
	fetch(func() error {
		items, err := listAllRelationships(ctx, client, "references")
		data.References = items
		return err
	})

	wg.Wait()
	if len(errs) > 0 {
		return nil, errs[0]
	}
	return data, nil
}

func listAllObjects(ctx context.Context, client *sdk.Client, typ string) ([]*sdkgraph.GraphObject, error) {
	var all []*sdkgraph.GraphObject
	var cursor string
	for {
		resp, err := client.Graph.ListObjects(ctx, &sdkgraph.ListObjectsOptions{
			Type: typ, Limit: 500, Cursor: cursor,
		})
		if err != nil {
			return nil, err
		}
		all = append(all, resp.Items...)
		if resp.NextCursor == nil || *resp.NextCursor == "" {
			break
		}
		cursor = *resp.NextCursor
	}
	return all, nil
}

func listAllRelationships(ctx context.Context, client *sdk.Client, typ string) ([]*sdkgraph.GraphRelationship, error) {
	var all []*sdkgraph.GraphRelationship
	var cursor string
	for {
		resp, err := client.Graph.ListRelationships(ctx, &sdkgraph.ListRelationshipsOptions{
			Type: typ, Limit: 500, Cursor: cursor,
		})
		if err != nil {
			return nil, err
		}
		all = append(all, resp.Items...)
		if resp.NextCursor == nil || *resp.NextCursor == "" {
			break
		}
		cursor = *resp.NextCursor
	}
	return all, nil
}

func inferDBType(goType string) string {
	goType = strings.TrimPrefix(goType, "*")
	switch {
	case goType == "uuid.UUID" || goType == "uuid.NullUUID":
		return "uuid"
	case goType == "string":
		return "text"
	case goType == "bool":
		return "boolean"
	case goType == "int" || goType == "int64" || goType == "int32":
		return "integer"
	case goType == "float32" || goType == "float64":
		return "float"
	case goType == "time.Time":
		return "timestamptz"
	case goType == "[]byte":
		return "bytes"
	case strings.HasPrefix(goType, "map[") || strings.HasSuffix(goType, "any"):
		return "jsonb"
	case strings.HasPrefix(goType, "[]"):
		return "array"
	default:
		// Custom Go types (enums, type aliases) without package qualifier → text in Postgres
		if !strings.ContainsAny(goType, "[]*. ") {
			return "text"
		}
		return goType
	}
}

func getPropString(obj *sdkgraph.GraphObject, key string) string {
	if v, ok := obj.Properties[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
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

func renderPlantUML(w io.Writer, data *GraphData, cfg Config) error {
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

	// Filter and group entities
	schemas := make(map[string][]*sdkgraph.GraphObject)
	referencedEntities := make(map[string]bool)
	for _, rel := range data.References {
		referencedEntities[rel.SrcID] = true
		referencedEntities[rel.DstID] = true
	}

	for _, e := range data.Entities {
		domain := getPropString(e, "domain")
		schema := getPropString(e, "db_schema")
		if cfg.Domain != "" && domain != cfg.Domain {
			continue
		}
		if cfg.Schema != "" && schema != cfg.Schema {
			continue
		}
		if cfg.RelationsOnly && !referencedEntities[e.EntityID] {
			continue
		}
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
		sort.Slice(ents, func(i, j int) bool {
			return getPropString(ents[i], "name") < getPropString(ents[j], "name")
		})

		for _, e := range ents {
			keyWithoutPrefix := strings.TrimPrefix(*e.Key, "entity-")
			alias := strings.ReplaceAll(keyWithoutPrefix, "-", "_")
			fmt.Fprintf(w, "  class \"%s\" as entity_%s {\n", getPropString(e, "name"), alias)
			if !cfg.NoFields {
				fields := entityFields[e.EntityID]
				sort.Slice(fields, func(i, j int) bool {
					return getPropInt(fields[i], "ordinal") < getPropInt(fields[j], "ordinal")
				})

				var pks, others []*sdkgraph.GraphObject
				for _, f := range fields {
					if getPropBool(f, "is_fk") && getPropString(f, "column") == "" {
						continue
					}
					if getPropBool(f, "is_pk") {
						pks = append(pks, f)
					} else {
						others = append(others, f)
					}
				}

				for _, f := range pks {
					renderPlantUMLField(w, f)
				}
				if len(pks) > 0 && len(others) > 0 {
					fmt.Fprintln(w, "    --")
				}
				for _, f := range others {
					renderPlantUMLField(w, f)
				}
			}
			fmt.Fprintln(w, "  }")
		}
		fmt.Fprintln(w, "}")
	}

	fmt.Fprintln(w, "\n' References")
	for _, rel := range data.References {
		src, ok1 := entityMap[rel.SrcID]
		dst, ok2 := entityMap[rel.DstID]
		if !ok1 || !ok2 {
			continue
		}
		// Check if both are in our filtered set
		if !isEntityIncluded(src, cfg, referencedEntities) || !isEntityIncluded(dst, cfg, referencedEntities) {
			continue
		}

		srcKeyWithoutPrefix := strings.TrimPrefix(*src.Key, "entity-")
		dstKeyWithoutPrefix := strings.TrimPrefix(*dst.Key, "entity-")
		srcAlias := strings.ReplaceAll(srcKeyWithoutPrefix, "-", "_")
		dstAlias := strings.ReplaceAll(dstKeyWithoutPrefix, "-", "_")
		label := ""
		if v, ok := rel.Properties["via_field"]; ok {
			label = fmt.Sprintf(" : %v", v)
		}
		if v, ok := rel.Properties["relation"]; ok {
			if label == "" {
				label = fmt.Sprintf(" : (%v)", v)
			} else {
				label = fmt.Sprintf("%s (%v)", label, v)
			}
		}
		fmt.Fprintf(w, "entity_%s --> entity_%s%s\n", srcAlias, dstAlias, label)
	}

	fmt.Fprintln(w, "@enduml")
	return nil
}

func renderPlantUMLField(w io.Writer, f *sdkgraph.GraphObject) {
	name := getPropString(f, "name")
	typ := getPropString(f, "db_type")
	if typ == "" {
		typ = inferDBType(getPropString(f, "go_type"))
	}
	// Replace commas inside type strings (e.g. numeric(10,6)) to avoid PlantUML parse issues
	typ = strings.ReplaceAll(typ, ",", ";")
	suffix := ""
	if getPropBool(f, "is_pk") {
		suffix = " <<PK>>"
	} else if getPropBool(f, "is_nullable") {
		suffix = " [0..1]"
	}
	fmt.Fprintf(w, "    + %s : %s%s\n", name, typ, suffix)
}

func getRelPropString(rel *sdkgraph.GraphRelationship, key string) string {
	if v, ok := rel.Properties[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

func renderMermaid(w io.Writer, data *GraphData, cfg Config) error {
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

	referencedEntities := make(map[string]bool)
	for _, rel := range data.References {
		referencedEntities[rel.SrcID] = true
		referencedEntities[rel.DstID] = true
	}

	var included []*sdkgraph.GraphObject
	for _, e := range data.Entities {
		if isEntityIncluded(e, cfg, referencedEntities) {
			included = append(included, e)
		}
	}

	sort.Slice(included, func(i, j int) bool {
		return getPropString(included[i], "table") < getPropString(included[j], "table")
	})

	for _, e := range included {
		tableName := getPropString(e, "table")
		if tableName == "" {
			tableName = strings.ReplaceAll(*e.Key, "-", "_")
		}
		fmt.Fprintf(w, "    %s {\n", tableName)
		if !cfg.NoFields {
			fields := entityFields[e.EntityID]
			sort.Slice(fields, func(i, j int) bool {
				return getPropInt(fields[i], "ordinal") < getPropInt(fields[j], "ordinal")
			})
			for _, f := range fields {
				if getPropBool(f, "is_fk") && getPropString(f, "column") == "" {
					continue
				}
				name := getPropString(f, "name")
				typ := getPropString(f, "db_type")
				if typ == "" {
					typ = inferDBType(getPropString(f, "go_type"))
				}
				suffix := ""
				if getPropBool(f, "is_pk") {
					suffix = " PK"
				} else if getPropBool(f, "is_fk") {
					suffix = " FK"
				}
				fmt.Fprintf(w, "        %s %s%s\n", typ, name, suffix)
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
		if !isEntityIncluded(src, cfg, referencedEntities) || !isEntityIncluded(dst, cfg, referencedEntities) {
			continue
		}

		srcTable := getPropString(src, "table")
		if srcTable == "" {
			srcTable = strings.ReplaceAll(*src.Key, "-", "_")
		}
		dstTable := getPropString(dst, "table")
		if dstTable == "" {
			dstTable = strings.ReplaceAll(*dst.Key, "-", "_")
		}

		relation := getRelPropString(rel, "relation")
		label := getRelPropString(rel, "via_field")
		if label == "" {
			label = relation
		}

		arrow := "}o--||" // default belongs-to
		switch relation {
		case "has-many":
			arrow = "||--o{"
		case "belongs-to":
			arrow = "}o--||"
		case "has-one":
			arrow = "||--||"
		case "m2m":
			arrow = "}o--o{"
		}

		fmt.Fprintf(w, "    %s %s %s : \"%s\"\n", srcTable, arrow, dstTable, label)
	}

	return nil
}

func isEntityIncluded(e *sdkgraph.GraphObject, cfg Config, referenced map[string]bool) bool {
	domain := getPropString(e, "domain")
	schema := getPropString(e, "db_schema")
	if cfg.Domain != "" && domain != cfg.Domain {
		return false
	}
	if cfg.Schema != "" && schema != cfg.Schema {
		return false
	}
	if cfg.RelationsOnly && !referenced[e.EntityID] {
		return false
	}
	return true
}
