package blueprints

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"

	sdkgraph "github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/graph"
)

const (
	dumpPageSize  = 250
	dumpSplitSize = 50 * 1024 * 1024 // 50 MB
)

// DumpResult summarises the outcome of a dump run.
type DumpResult struct {
	ObjectsDumped       int
	RelationshipsDumped int
}

// Dumper exports graph objects and relationships as JSONL seed files.
type Dumper struct {
	graph *sdkgraph.Client
	types []string // optional type filter (empty = all types)
	out   io.Writer
}

// NewDumper creates a Dumper. types is an optional list of object/relationship
// types to export; if empty all types are exported. out receives progress lines;
// if nil, os.Stdout is used.
func NewDumper(graph *sdkgraph.Client, types []string, out io.Writer) *Dumper {
	if out == nil {
		out = os.Stdout
	}
	return &Dumper{
		graph: graph,
		types: types,
		out:   out,
	}
}

// Run exports all objects and relationships to outputDir/seed/objects/ and
// outputDir/seed/relationships/. It returns a DumpResult summary.
func (d *Dumper) Run(ctx context.Context, outputDir string) (DumpResult, error) {
	var result DumpResult

	objectsDir := filepath.Join(outputDir, "seed", "objects")
	relsDir := filepath.Join(outputDir, "seed", "relationships")

	if err := os.MkdirAll(objectsDir, 0o755); err != nil {
		return result, fmt.Errorf("create objects dir: %w", err)
	}
	if err := os.MkdirAll(relsDir, 0o755); err != nil {
		return result, fmt.Errorf("create relationships dir: %w", err)
	}

	entityKeyMap, objectCount, err := d.dumpObjects(ctx, objectsDir)
	if err != nil {
		return result, err
	}
	result.ObjectsDumped = objectCount

	relCount, err := d.dumpRelationships(ctx, relsDir, entityKeyMap)
	if err != nil {
		return result, err
	}
	result.RelationshipsDumped = relCount

	fmt.Fprintf(d.out, "Dumped %d objects, %d relationships → %s\n",
		result.ObjectsDumped, result.RelationshipsDumped, outputDir)

	return result, nil
}

// dumpObjects paginates ListObjects, writes per-type JSONL files with 50 MB splits,
// and returns an entityID→key map (for relationship endpoint resolution).
func (d *Dumper) dumpObjects(ctx context.Context, outputDir string) (map[string]string, int, error) {
	entityKeyMap := make(map[string]string)
	writers := make(map[string]*splitWriter)

	defer func() {
		for _, sw := range writers {
			sw.close()
		}
	}()

	opts := &sdkgraph.ListObjectsOptions{
		Limit: dumpPageSize,
	}
	if len(d.types) > 0 {
		opts.Types = d.types
	}

	total := 0
	cursor := ""

	for {
		if cursor != "" {
			opts.Cursor = cursor
		}

		resp, err := d.graph.ListObjects(ctx, opts)
		if err != nil {
			return nil, total, fmt.Errorf("list objects: %w", err)
		}

		for _, obj := range resp.Items {
			sw, err := getSplitWriter(writers, outputDir, obj.Type)
			if err != nil {
				return nil, total, err
			}

			rec := SeedObjectRecord{
				Type:       obj.Type,
				Properties: obj.Properties,
				Labels:     obj.Labels,
			}
			if obj.Key != nil {
				rec.Key = *obj.Key
				entityKeyMap[obj.EntityID] = *obj.Key
			}
			if obj.Status != nil {
				rec.Status = *obj.Status
			}

			line, err := json.Marshal(rec)
			if err != nil {
				return nil, total, fmt.Errorf("marshal object: %w", err)
			}
			if err := sw.writeLine(line); err != nil {
				return nil, total, fmt.Errorf("write object: %w", err)
			}

			total++
		}

		fmt.Fprintf(d.out, "  objects: %d fetched...\n", total)

		if resp.NextCursor == nil || *resp.NextCursor == "" {
			break
		}
		cursor = *resp.NextCursor
	}

	return entityKeyMap, total, nil
}

// dumpRelationships paginates ListRelationships, writes per-type JSONL files
// with 50 MB splits, and returns the total relationship count.
func (d *Dumper) dumpRelationships(ctx context.Context, outputDir string, entityKeyMap map[string]string) (int, error) {
	writers := make(map[string]*splitWriter)

	defer func() {
		for _, sw := range writers {
			sw.close()
		}
	}()

	// Build a set of exported entity IDs for filtering when --types is active.
	exportedEntityIDs := make(map[string]bool)
	if len(d.types) > 0 {
		for entityID := range entityKeyMap {
			exportedEntityIDs[entityID] = true
		}
	}

	opts := &sdkgraph.ListRelationshipsOptions{
		Limit: dumpPageSize,
	}

	total := 0
	cursor := ""

	for {
		if cursor != "" {
			opts.Cursor = cursor
		}

		resp, err := d.graph.ListRelationships(ctx, opts)
		if err != nil {
			return total, fmt.Errorf("list relationships: %w", err)
		}

		for _, rel := range resp.Items {
			// When types filter is active, skip relationships where either
			// endpoint falls outside the exported object set.
			if len(d.types) > 0 {
				if !exportedEntityIDs[rel.SrcID] || !exportedEntityIDs[rel.DstID] {
					continue
				}
			}

			sw, err := getSplitWriter(writers, outputDir, rel.Type)
			if err != nil {
				return total, err
			}

			rec := SeedRelationshipRecord{
				Type:       rel.Type,
				Properties: rel.Properties,
				Weight:     rel.Weight,
			}

			// Prefer key references over raw entity IDs.
			srcKey, srcHasKey := entityKeyMap[rel.SrcID]
			dstKey, dstHasKey := entityKeyMap[rel.DstID]
			if srcHasKey && dstHasKey {
				rec.SrcKey = srcKey
				rec.DstKey = dstKey
			} else {
				rec.SrcID = rel.SrcID
				rec.DstID = rel.DstID
			}

			line, err := json.Marshal(rec)
			if err != nil {
				return total, fmt.Errorf("marshal relationship: %w", err)
			}
			if err := sw.writeLine(line); err != nil {
				return total, fmt.Errorf("write relationship: %w", err)
			}

			total++
		}

		fmt.Fprintf(d.out, "  relationships: %d fetched...\n", total)

		if resp.NextCursor == nil || *resp.NextCursor == "" {
			break
		}
		cursor = *resp.NextCursor
	}

	return total, nil
}

// ─────────────────────────────────────────────────────────────────────────────
// splitWriter — writes JSONL lines to a sequence of files, splitting at
// dumpSplitSize bytes. Files are named <typeName>.jsonl for the first file,
// then <typeName>.001.jsonl, <typeName>.002.jsonl, etc. once splitting occurs.
// ─────────────────────────────────────────────────────────────────────────────

type splitWriter struct {
	dir      string
	typeName string
	file     *os.File
	written  int64
	part     int // 0 = unsplit; 1+ = split sequence
}

func getSplitWriter(writers map[string]*splitWriter, dir, typeName string) (*splitWriter, error) {
	if sw, ok := writers[typeName]; ok {
		return sw, nil
	}
	sw := &splitWriter{dir: dir, typeName: typeName}
	if err := sw.openNext(); err != nil {
		return nil, err
	}
	writers[typeName] = sw
	return sw, nil
}

func (sw *splitWriter) fileName() string {
	if sw.part == 0 {
		return filepath.Join(sw.dir, sw.typeName+".jsonl")
	}
	return filepath.Join(sw.dir, fmt.Sprintf("%s.%03d.jsonl", sw.typeName, sw.part))
}

func (sw *splitWriter) openNext() error {
	if sw.part == 0 && sw.file == nil {
		// First file: use the plain name.
	} else {
		// Rename the current unsplit file to .001.jsonl when first split occurs,
		// or just open the next numbered file for subsequent splits.
		if sw.part == 0 {
			// We were on the unsplit file; close it and rename to .001.jsonl.
			if err := sw.file.Close(); err != nil {
				return err
			}
			oldName := filepath.Join(sw.dir, sw.typeName+".jsonl")
			sw.part = 1
			newName := sw.fileName()
			if err := os.Rename(oldName, newName); err != nil {
				return fmt.Errorf("rename split file: %w", err)
			}
			sw.part++ // next file will be .002
		} else {
			if err := sw.file.Close(); err != nil {
				return err
			}
			sw.part++
		}
		sw.written = 0
	}

	f, err := os.Create(sw.fileName())
	if err != nil {
		return fmt.Errorf("create file %s: %w", sw.fileName(), err)
	}
	sw.file = f
	return nil
}

func (sw *splitWriter) writeLine(line []byte) error {
	// Check if we need to split before writing.
	needed := int64(len(line)) + 1 // +1 for newline
	if sw.written > 0 && sw.written+needed > dumpSplitSize {
		if err := sw.openNext(); err != nil {
			return err
		}
	}

	if _, err := sw.file.Write(line); err != nil {
		return err
	}
	if _, err := sw.file.Write([]byte("\n")); err != nil {
		return err
	}
	sw.written += needed
	return nil
}

func (sw *splitWriter) close() {
	if sw.file != nil {
		sw.file.Close()
		sw.file = nil
	}
}
