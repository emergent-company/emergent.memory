// Deprecated: use `codebase seed entities` instead. Run `codebase --help` for details.
package main

import (
	"context"
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/emergent-company/emergent.memory/apps/server/pkg/sdk"
	sdkgraph "github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/graph"
)

var (
	dryRun  = flag.Bool("dry-run", false, "print what would be created, don't write to graph")
	domain  = flag.String("domain", "", "only process entities from this domain")
	verbose = flag.Bool("verbose", false, "print each object as it's created")
)

var entityFiles = []string{
	"apps/server/domain/agents/entity.go",
	"apps/server/domain/graph/entity.go",
	"apps/server/domain/superadmin/entity.go",
	"apps/server/domain/users/entity.go",
	"apps/server/domain/projects/entity.go",
	"apps/server/domain/apitoken/entity.go",
	"apps/server/domain/extraction/entity.go",
	"apps/server/domain/provider/entity.go",
	"apps/server/domain/schemas/entity.go",
	"apps/server/domain/chat/entity.go",
	"apps/server/domain/datasource/entity.go",
	"apps/server/domain/mcpregistry/entity.go",
	"apps/server/domain/schemaregistry/entity.go",
	"apps/server/domain/monitoring/entity.go",
	"apps/server/domain/journal/entity.go",
	"apps/server/domain/discoveryjobs/entity.go",
	"apps/server/domain/sandbox/entity.go",
	"apps/server/domain/branches/entity.go",
	"apps/server/domain/invites/entity.go",
	"apps/server/domain/notifications/entity.go",
	"apps/server/domain/email/entity.go",
	"apps/server/domain/chunks/entity.go",
	"apps/server/domain/documents/entity.go",
	"apps/server/domain/skills/entity.go",
	"apps/server/domain/integrations/entity.go",
	"apps/server/domain/embeddingpolicies/entity.go",
	"apps/server/domain/tasks/entity.go",
	"apps/server/domain/userprofile/entity.go",
	"apps/server/domain/githubapp/entity.go",
	"apps/server/domain/useractivity/entity.go",
	"apps/server/pkg/auth/user_profile.go",
	"apps/server/pkg/adk/session/bunsession/models.go",
}

type Entity struct {
	Key      string
	Name     string
	Schema   string
	Table    string
	Domain   string
	Fields   []Field
	ID       string // Graph ID
	Relations []Relation
}

type Field struct {
	Key        string
	Name       string
	Column     string
	GoType     string
	DBType     string
	IsPK       bool
	IsFK       bool
	Nullable   bool
	DefaultVal string
	Ordinal    int
	ID         string // Graph ID
}

type Relation struct {
	Type     string
	Join     string
	ViaField string
	Target   string // Struct name
}

func main() {
	flag.Parse()

	client, err := sdk.NewFromEnv()
	if err != nil && !*dryRun {
		log.Fatalf("failed to create sdk client: %v", err)
	}

	entities := parseEntities()

	if *domain != "" {
		filtered := []Entity{}
		for _, e := range entities {
			if e.Domain == *domain {
				filtered = append(filtered, e)
			}
		}
		entities = filtered
	}

	ctx := context.Background()

	// 1. Create Entities
	fmt.Fprintf(os.Stderr, "→ Creating %d entities...\n", len(entities))
	limit := make(chan struct{}, 10)
	var wg sync.WaitGroup
	for i := range entities {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			limit <- struct{}{}
			defer func() { <-limit }()

			e := &entities[idx]
			if *dryRun {
				fmt.Printf("[DRY-RUN] Create Entity: %s (%s.%s)\n", e.Key, e.Schema, e.Table)
				e.ID = "dry-run-id-" + e.Key
				return
			}

		obj, err := client.Graph.UpsertObject(ctx, &sdkgraph.CreateObjectRequest{
			Type: "Entity",
			Key:  strPtr(e.Key),
			Properties: map[string]any{
				"name":      e.Name,
				"db_schema": e.Schema,
				"table":     e.Table,
				"domain":    e.Domain,
			},
		})
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error upserting entity %s: %v\n", e.Key, err)
			return
		}
		e.ID = obj.EntityID // stable canonical ID, not version ID
		if *verbose {
			fmt.Printf("Upserted Entity: %s id=%s\n", e.Key, e.ID)
		}
		}(i)
	}
	wg.Wait()

	// 2. Create Fields
	fieldCount := 0
	for _, e := range entities {
		fieldCount += len(e.Fields)
	}
	fmt.Fprintf(os.Stderr, "→ Creating %d fields...\n", fieldCount)
	for i := range entities {
		e := &entities[i]
		if e.ID == "" && !*dryRun {
			continue
		}
		for j := range e.Fields {
			wg.Add(1)
			go func(entIdx, fldIdx int) {
				defer wg.Done()
				limit <- struct{}{}
				defer func() { <-limit }()

				ent := &entities[entIdx]
				f := &ent.Fields[fldIdx]

				if *dryRun {
					fmt.Printf("[DRY-RUN] Create Field: %s for %s\n", f.Key, ent.Key)
					f.ID = "dry-run-id-" + f.Key
					return
				}

			obj, err := client.Graph.UpsertObject(ctx, &sdkgraph.CreateObjectRequest{
				Type: "Field",
				Key:  strPtr(f.Key),
				Properties: map[string]any{
					"name":        f.Name,
					"column":      f.Column,
					"go_type":     f.GoType,
					"db_type":     f.DBType,
					"is_pk":       f.IsPK,
					"is_fk":       f.IsFK,
					"nullable":    f.Nullable,
					"default_val": f.DefaultVal,
					"ordinal":     f.Ordinal,
				},
			})
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error upserting field %s: %v\n", f.Key, err)
				return
			}
				f.ID = obj.EntityID // stable canonical ID

			_, err = client.Graph.UpsertRelationship(ctx, &sdkgraph.CreateRelationshipRequest{
				Type:  "has_field",
				SrcID: ent.ID,
				DstID: f.ID,
			})
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error upserting has_field rel for %s: %v\n", f.Key, err)
			}

				if *verbose {
					fmt.Printf("Created Field: %s\n", f.Key)
				}
			}(i, j)
		}
	}
	wg.Wait()

	// 3. Create References
	fmt.Fprintf(os.Stderr, "→ Creating references relationships...\n")
	refCount := 0
	// Map struct name to Entity for relation resolution
	structToEntity := make(map[string]*Entity)
	for i := range entities {
		structToEntity[entities[i].Name] = &entities[i]
	}

	for i := range entities {
		e := &entities[i]
		for _, rel := range e.Relations {
			target, ok := structToEntity[rel.Target]
			if !ok {
				continue
			}

			wg.Add(1)
			go func(srcEnt, dstEnt *Entity, r Relation) {
				defer wg.Done()
				limit <- struct{}{}
				defer func() { <-limit }()

				if *dryRun {
					fmt.Printf("[DRY-RUN] Create reference: %s -> %s (%s)\n", srcEnt.Key, dstEnt.Key, r.Type)
					return
				}

			_, err := client.Graph.UpsertRelationship(ctx, &sdkgraph.CreateRelationshipRequest{
				Type:  "references",
				SrcID: srcEnt.ID,
				DstID: dstEnt.ID,
				Properties: map[string]any{
					"relation":  r.Type, // schema property name is "relation" not "type"
					"join":      r.Join,
					"via_field": r.ViaField,
				},
			})
				if err != nil {
					fmt.Fprintf(os.Stderr, "Error creating reference %s -> %s: %v\n", srcEnt.Key, dstEnt.Key, err)
				}
				if *verbose {
					fmt.Printf("Created reference: %s -> %s\n", srcEnt.Key, dstEnt.Key)
				}
			}(e, target, rel)
			refCount++
		}
	}
	wg.Wait()

	fmt.Fprintf(os.Stderr, "→ Summary: %d entities, %d fields, %d references created.\n", len(entities), fieldCount, refCount)

	// 4. Create owned_by rels: Entity → Domain
	fmt.Fprintf(os.Stderr, "→ Creating owned_by relationships (Entity → Domain)...\n")
	ownedByCount := 0
	// Collect unique domain names
	domainNames := make(map[string]bool)
	for _, e := range entities {
		if e.Domain != "" && e.Domain != "unknown" {
			domainNames[e.Domain] = true
		}
	}
	// Fetch Domain entity IDs by key
	domainEntityIDs := make(map[string]string) // domain name → entity ID
	for domName := range domainNames {
		domKey := "domain-" + domName
		resp, err := client.Graph.ListObjects(ctx, &sdkgraph.ListObjectsOptions{
			Type: "Domain", Key: domKey, Limit: 1,
		})
		if err != nil || len(resp.Items) == 0 {
			if *verbose {
				fmt.Fprintf(os.Stderr, "  Domain not found: %s\n", domKey)
			}
			continue
		}
		domainEntityIDs[domName] = resp.Items[0].EntityID
	}

	for i := range entities {
		e := &entities[i]
		if e.ID == "" || e.Domain == "" || e.Domain == "unknown" {
			continue
		}
		domID, ok := domainEntityIDs[e.Domain]
		if !ok {
			continue
		}
		wg.Add(1)
		go func(entID, dID string, entKey string) {
			defer wg.Done()
			limit <- struct{}{}
			defer func() { <-limit }()

			if *dryRun {
				fmt.Printf("[DRY-RUN] owned_by: %s → domain-%s\n", entKey, e.Domain)
				return
			}
			_, err := client.Graph.UpsertRelationship(ctx, &sdkgraph.CreateRelationshipRequest{
				Type:  "owned_by",
				SrcID: entID,
				DstID: dID,
			})
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error creating owned_by for %s: %v\n", entKey, err)
			} else if *verbose {
				fmt.Printf("owned_by: %s → domain\n", entKey)
			}
		}(e.ID, domID, e.Key)
		ownedByCount++
	}
	wg.Wait()
	fmt.Fprintf(os.Stderr, "→ owned_by: %d relationships created.\n", ownedByCount)
}

func parseEntities() []Entity {
	var entities []Entity
	fset := token.NewFileSet()

	for _, path := range entityFiles {
		fullPath := filepath.Join("/root/emergent.memory", path)
		f, err := parser.ParseFile(fset, fullPath, nil, 0)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to parse %s: %v\n", path, err)
			continue
		}

		domainVal := inferDomain(path)

		for _, decl := range f.Decls {
			gen, ok := decl.(*ast.GenDecl)
			if !ok || gen.Tok != token.TYPE {
				continue
			}

			for _, spec := range gen.Specs {
				tSpec, ok := spec.(*ast.TypeSpec)
				if !ok {
					continue
				}
				sTyp, ok := tSpec.Type.(*ast.StructType)
				if !ok {
					continue
				}

				// Find bun table tag
				var schema, table string
				for _, field := range sTyp.Fields.List {
					if field.Tag == nil {
						continue
					}
					tag := strings.Trim(field.Tag.Value, "`")
					if strings.Contains(tag, `bun:"table:`) {
						parts := strings.Split(tag, `bun:"table:`)
						if len(parts) > 1 {
							val := strings.Split(parts[1], `"`)[0]
							val = strings.Split(val, ",")[0]
							if strings.Contains(val, ".") {
								sp := strings.Split(val, ".")
								schema = sp[0]
								table = sp[1]
							} else {
								table = val
							}
						}
					}
				}

				if table == "" {
					continue
				}

				entKey := fmt.Sprintf("entity-%s-%s", schema, strings.ReplaceAll(table, "_", "-"))
				if schema == "" {
					entKey = fmt.Sprintf("entity-%s", strings.ReplaceAll(table, "_", "-"))
				}
				entKey = strings.ToLower(entKey)

				ent := Entity{
					Key:    entKey,
					Name:   tSpec.Name.Name,
					Schema: schema,
					Table:  table,
					Domain: domainVal,
				}

				ordinal := 1
				for _, field := range sTyp.Fields.List {
					if len(field.Names) == 0 {
						// Embedded field, check if it's BaseModel
						if id, ok := field.Type.(*ast.SelectorExpr); ok {
							if id.Sel.Name == "BaseModel" {
								continue
							}
						}
						if id, ok := field.Type.(*ast.Ident); ok {
							if id.Name == "BaseModel" {
								continue
							}
						}
					}

					fieldName := ""
					if len(field.Names) > 0 {
						fieldName = field.Names[0].Name
					}

					var tagVal string
					if field.Tag != nil {
						tagVal = strings.Trim(field.Tag.Value, "`")
					}

					bunTag := getTagValue(tagVal, "bun")
					if bunTag == "-" {
						continue
					}

					// Relation check
					if strings.Contains(bunTag, "rel:") {
						relType := ""
						join := ""
						parts := strings.Split(bunTag, ",")
						for _, p := range parts {
							if strings.HasPrefix(p, "rel:") {
								relType = strings.TrimPrefix(p, "rel:")
							}
							if strings.HasPrefix(p, "join:") {
								join = strings.TrimPrefix(p, "join:")
							}
						}
						via := ""
						if strings.Contains(join, "=") {
							via = strings.Split(join, "=")[0]
						}

						target := ""
						// Extract target type name
						typ := field.Type
					loop:
						for {
							switch t := typ.(type) {
							case *ast.StarExpr:
								typ = t.X
							case *ast.ArrayType:
								typ = t.Elt
							case *ast.Ident:
								target = t.Name
								break loop
							case *ast.SelectorExpr:
								target = t.Sel.Name
								break loop
							default:
								break loop
							}
						}

						if target != "" {
							ent.Relations = append(ent.Relations, Relation{
								Type:     relType,
								Join:     join,
								ViaField: via,
								Target:   target,
							})
						}
						continue
					}

					column := strings.Split(bunTag, ",")[0]
					if column == "" && fieldName != "" {
						column = toSnakeCase(fieldName)
					}
					if column == "" {
						continue
					}

					goType := exprToString(field.Type)
					isPK := strings.Contains(bunTag, ",pk")
					nullable := strings.HasPrefix(goType, "*") || !strings.Contains(bunTag, "notnull")
					
					isFK := strings.Contains(bunTag, "rel:") || (strings.HasSuffix(fieldName, "ID") && (strings.Contains(goType, "uuid") || strings.Contains(goType, "string")))

					dbType := ""
				defaultVal := ""
				// Parse type: and default: from bun tag, respecting parens (e.g. numeric(10,6))
				dbType = extractBunTagValue(bunTag, "type:")
				defaultVal = extractBunTagValue(bunTag, "default:")

					fKey := fmt.Sprintf("field-%s-%s", entKey, strings.ReplaceAll(column, "_", "-"))
					fKey = strings.ToLower(fKey)

					ent.Fields = append(ent.Fields, Field{
						Key:        fKey,
						Name:       fieldName,
						Column:     column,
						GoType:     goType,
						DBType:     dbType,
						IsPK:       isPK,
						IsFK:       isFK,
						Nullable:   nullable,
						DefaultVal: defaultVal,
						Ordinal:    ordinal,
					})

					// Infer FK reference from bare *_id fields
					if !isPK && strings.HasSuffix(fieldName, "ID") && fieldName != "ID" {
						target := strings.TrimSuffix(fieldName, "ID")
						if target != "" {
							ent.Relations = append(ent.Relations, Relation{
								Type:     "belongs-to",
								ViaField: column, // the snake_case column name
								Target:   target,
							})
						}
					}

					ordinal++
				}

				entities = append(entities, ent)
			}
		}
	}

	return entities
}

func inferDomain(path string) string {
	if strings.HasPrefix(path, "apps/server/domain/") {
		parts := strings.Split(path, "/")
		return parts[3]
	}
	if strings.HasPrefix(path, "apps/server/pkg/auth/") {
		return "authinfo"
	}
	if strings.HasPrefix(path, "apps/server/pkg/adk/") {
		return "agents"
	}
	return "unknown"
}

func getTagValue(tag, key string) string {
	parts := strings.Split(tag, " ")
	for _, p := range parts {
		if strings.HasPrefix(p, key+":\"") {
			return strings.TrimSuffix(strings.TrimPrefix(p, key+":\""), "\"")
		}
	}
	return ""
}

func exprToString(expr ast.Expr) string {
	switch t := expr.(type) {
	case *ast.Ident:
		return t.Name
	case *ast.StarExpr:
		return "*" + exprToString(t.X)
	case *ast.SelectorExpr:
		return exprToString(t.X) + "." + t.Sel.Name
	case *ast.ArrayType:
		return "[]" + exprToString(t.Elt)
	case *ast.MapType:
		return "map[" + exprToString(t.Key) + "]" + exprToString(t.Value)
	default:
		return fmt.Sprintf("%T", expr)
	}
}

func toSnakeCase(s string) string {
	var res strings.Builder
	for i, r := range s {
		if i > 0 && r >= 'A' && r <= 'Z' {
			res.WriteRune('_')
		}
		res.WriteRune(r)
	}
	return strings.ToLower(res.String())
}

func strPtr(s string) *string {
	return &s
}

// extractBunTagValue extracts the value of a key like "type:" or "default:" from a bun tag string.
// It respects parentheses so that "type:numeric(10,6)" is returned as "numeric(10,6)" not "numeric(10".
func extractBunTagValue(bunTag, prefix string) string {
	idx := strings.Index(bunTag, prefix)
	if idx < 0 {
		return ""
	}
	rest := bunTag[idx+len(prefix):]
	// Read until comma not inside parens, or end of string
	depth := 0
	for i, ch := range rest {
		switch ch {
		case '(':
			depth++
		case ')':
			depth--
		case ',':
			if depth == 0 {
				return rest[:i]
			}
		}
	}
	return rest
}
