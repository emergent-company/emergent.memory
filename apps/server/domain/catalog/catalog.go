// Package catalog owns the built-in reference vocabulary (object types and the
// candidate relationships between them). It is a shipped, read-only dataset:
// never compiled into a project wholesale, never a blueprint. A project schema
// pack is produced from a projection only when types are installed.
package catalog

import (
	"embed"
	"encoding/json"
	"errors"
	"io/fs"
	"log/slog"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

//go:embed data/*.yaml
var dataFS embed.FS

// Type is one catalog object type. It mirrors the subset of a schema pack's
// object type the catalog needs; unknown pack fields are ignored.
type Type struct {
	Name        string         `json:"name"`
	Label       string         `json:"label,omitempty"`
	Description string         `json:"description,omitempty"`
	Category    string         `json:"category,omitempty"`
	Parent      string         `json:"parent,omitempty"`
	Labels      []string       `json:"labels,omitempty"`
	UI          map[string]any `json:"ui,omitempty"`
	Properties  map[string]any `json:"properties,omitempty"`
	Required    []string       `json:"required,omitempty"`
}

// Relationship is one candidate relationship between two catalog types. Plural
// sourceTypes/targetTypes are expanded to singular pairs at load time.
type Relationship struct {
	Name        string         `json:"name"`
	Label       string         `json:"label,omitempty"`
	Description string         `json:"description,omitempty"`
	SourceType  string         `json:"sourceType,omitempty"`
	TargetType  string         `json:"targetType,omitempty"`
	SourceTypes []string       `json:"sourceTypes,omitempty"`
	TargetTypes []string       `json:"targetTypes,omitempty"`
	Properties  map[string]any `json:"properties,omitempty"`
	Required    []string       `json:"required,omitempty"`
}

// Catalog is the loaded reference vocabulary. Types are unique by name
// (first file in lexical order wins); relationships are unique by
// (name, source, target) after plural expansion.
type Catalog struct {
	Version       string
	Types         []Type
	Relationships []Relationship
}

// fileDoc is the on-disk shape of one catalog data file. It deliberately reuses
// the pack spelling (objectTypeSchemas/relationshipTypeSchemas) so the data can
// be validated by the same shape.
type fileDoc struct {
	Name                    string         `json:"name"`
	Version                 string         `json:"version"`
	ObjectTypeSchemas       []Type         `json:"objectTypeSchemas"`
	ObjectTypes             []Type         `json:"objectTypes"`
	RelationshipTypeSchemas []Relationship `json:"relationshipTypeSchemas"`
	RelationshipTypes       []Relationship `json:"relationshipTypes"`
}

// maxExpandedPairs bounds a plural relationship's cross-product.
const maxExpandedPairs = 10_000

// Load reads the embedded catalog dataset.
func Load() (*Catalog, error) { return load(dataFS, "data") }

func load(fsys fs.FS, dir string) (*Catalog, error) {
	entries, err := fs.ReadDir(fsys, dir)
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(strings.ToLower(e.Name()), ".yaml") {
			continue
		}
		names = append(names, e.Name())
	}
	sort.Strings(names)

	cat := &Catalog{}
	seenType := make(map[string]string)
	seenRel := make(map[string]bool)
	for _, name := range names {
		data, err := fs.ReadFile(fsys, dir+"/"+name)
		if err != nil {
			return nil, err
		}
		doc, err := decode(data)
		if err != nil {
			return nil, errors.New(name + ": " + err.Error())
		}
		if cat.Version == "" {
			cat.Version = doc.Version
		}
		types := doc.ObjectTypeSchemas
		if len(types) == 0 {
			types = doc.ObjectTypes
		}
		for _, t := range types {
			if t.Name == "" {
				continue
			}
			if prev, ok := seenType[t.Name]; ok {
				slog.Debug("catalog: skipping duplicate type",
					slog.String("type", t.Name),
					slog.String("kept", prev),
					slog.String("skipped", name))
				continue
			}
			seenType[t.Name] = name
			cat.Types = append(cat.Types, t)
		}
		rels := doc.RelationshipTypeSchemas
		if len(rels) == 0 {
			rels = doc.RelationshipTypes
		}
		for _, r := range rels {
			pairs, err := expandPairs(r)
			if err != nil {
				return nil, errors.New(name + ": " + err.Error())
			}
			for _, pair := range pairs {
				key := relationshipKey(pair)
				if seenRel[key] {
					continue
				}
				seenRel[key] = true
				cat.Relationships = append(cat.Relationships, pair)
			}
		}
	}
	return cat, nil
}

func decode(data []byte) (*fileDoc, error) {
	var raw any
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return nil, err
	}
	b, err := json.Marshal(raw)
	if err != nil {
		return nil, err
	}
	var doc fileDoc
	if err := json.Unmarshal(b, &doc); err != nil {
		return nil, err
	}
	return &doc, nil
}

// expandPairs flattens plural sourceTypes/targetTypes into singular pairs.
func expandPairs(r Relationship) ([]Relationship, error) {
	sources := r.SourceTypes
	if len(sources) == 0 && r.SourceType != "" {
		sources = []string{r.SourceType}
	}
	targets := r.TargetTypes
	if len(targets) == 0 && r.TargetType != "" {
		targets = []string{r.TargetType}
	}
	if len(sources) == 0 || len(targets) == 0 {
		return nil, nil
	}
	if int64(len(sources))*int64(len(targets)) > maxExpandedPairs {
		return nil, errors.New("relationship " + r.Name + " expands past the safe pair limit")
	}
	out := make([]Relationship, 0, len(sources)*len(targets))
	for _, s := range sources {
		for _, t := range targets {
			pair := r
			pair.SourceType = s
			pair.TargetType = t
			pair.SourceTypes = nil
			pair.TargetTypes = nil
			out = append(out, pair)
		}
	}
	return out, nil
}

func relationshipKey(r Relationship) string {
	return r.Name + "\x00" + r.SourceType + "\x00" + r.TargetType
}
