package blueprints

import (
	"context"
	"fmt"
	"io"
	"os"

	sdkgraph "github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/graph"
)

const seedBatchSize = 100

// Seeder applies seed objects and relationships to a project.
type Seeder struct {
	graph   *sdkgraph.Client
	dryRun  bool
	upgrade bool
	out     io.Writer
}

// NewSeeder creates a Seeder. out receives progress lines; if nil, os.Stdout is used.
func NewSeeder(graph *sdkgraph.Client, dryRun, upgrade bool, out io.Writer) *Seeder {
	if out == nil {
		out = os.Stdout
	}
	return &Seeder{
		graph:   graph,
		dryRun:  dryRun,
		upgrade: upgrade,
		out:     out,
	}
}

// Run applies objects then relationships. It returns a SeedResult summary.
// Errors for individual records are accumulated (not returned) so the run
// continues; only a fatal setup error causes a non-nil return error.
func (s *Seeder) Run(ctx context.Context, objects []SeedObjectRecord, rels []SeedRelationshipRecord) (SeedResult, error) {
	var result SeedResult

	// Phase 1: objects — build key→entityID map for relationship resolution.
	keyMap, err := s.applyObjects(ctx, objects, &result)
	if err != nil {
		return result, err
	}

	// Phase 2: relationships.
	if err := s.applyRelationships(ctx, rels, keyMap, &result); err != nil {
		return result, err
	}

	return result, nil
}

// applyObjects processes object records in batches of seedBatchSize.
// It returns a key→entityID map built from all successfully created/upserted objects.
func (s *Seeder) applyObjects(ctx context.Context, objects []SeedObjectRecord, result *SeedResult) (map[string]string, error) {
	keyMap := make(map[string]string)
	if len(objects) == 0 {
		return keyMap, nil
	}

	for start := 0; start < len(objects); start += seedBatchSize {
		end := start + seedBatchSize
		if end > len(objects) {
			end = len(objects)
		}
		batch := objects[start:end]

		batchKeyMap, err := s.applyObjectBatch(ctx, batch, result)
		if err != nil {
			return keyMap, err
		}
		for k, v := range batchKeyMap {
			keyMap[k] = v
		}
	}

	return keyMap, nil
}

// applyObjectBatch processes one batch of objects.
// It looks up which keys already exist, then bulk-creates new ones and
// (if upgrade is set) upserts existing ones.
func (s *Seeder) applyObjectBatch(ctx context.Context, batch []SeedObjectRecord, result *SeedResult) (map[string]string, error) {
	batchKeyMap := make(map[string]string)

	// Collect all non-empty keys in this batch for the pre-check.
	var keys []string
	for _, rec := range batch {
		if rec.Key != "" {
			keys = append(keys, rec.Key)
		}
	}

	// Pre-check: find which keys already exist in the project.
	existingByKey := make(map[string]string) // key → entityID
	for _, key := range keys {
		if s.dryRun {
			// In dry-run we skip the API call.
			continue
		}
		resp, err := s.graph.ListObjects(ctx, &sdkgraph.ListObjectsOptions{
			Key:   key,
			Limit: 1,
		})
		if err != nil {
			// Non-fatal: treat as not-found, attempt create later.
			fmt.Fprintf(s.out, "  warn: key lookup failed for %q: %v\n", key, err)
			continue
		}
		if len(resp.Items) > 0 {
			existingByKey[key] = resp.Items[0].EntityID
		}
	}

	// Separate records into: new (to bulk-create) vs existing (to upsert or skip).
	var toCreate []SeedObjectRecord
	var toUpsert []SeedObjectRecord
	var toSkip []SeedObjectRecord

	for _, rec := range batch {
		if rec.Key == "" {
			// Keyless objects are always new.
			toCreate = append(toCreate, rec)
			continue
		}
		if _, exists := existingByKey[rec.Key]; exists {
			if s.upgrade {
				toUpsert = append(toUpsert, rec)
			} else {
				toSkip = append(toSkip, rec)
			}
		} else {
			toCreate = append(toCreate, rec)
		}
	}

	// Dry-run: print intentions and return.
	if s.dryRun {
		for _, rec := range toCreate {
			fmt.Fprintf(s.out, "  [dry-run] would create object type=%s key=%q\n", rec.Type, rec.Key)
			// Populate a placeholder entry so relationships can resolve keys.
			if rec.Key != "" {
				batchKeyMap[rec.Key] = "[dry-run:" + rec.Key + "]"
			}
		}
		for _, rec := range toUpsert {
			fmt.Fprintf(s.out, "  [dry-run] would upsert object type=%s key=%q\n", rec.Type, rec.Key)
			batchKeyMap[rec.Key] = "[dry-run:" + rec.Key + "]"
		}
		for _, rec := range toSkip {
			fmt.Fprintf(s.out, "  [dry-run] would skip object type=%s key=%q (already exists)\n", rec.Type, rec.Key)
			batchKeyMap[rec.Key] = "[dry-run:" + rec.Key + "]"
		}
		result.ObjectsCreated += len(toCreate)
		result.ObjectsUpdated += len(toUpsert)
		result.ObjectsSkipped += len(toSkip)
		return batchKeyMap, nil
	}

	// Bulk-create new objects.
	if len(toCreate) > 0 {
		items := make([]sdkgraph.CreateObjectRequest, len(toCreate))
		for i, rec := range toCreate {
			req := sdkgraph.CreateObjectRequest{
				Type:       rec.Type,
				Properties: rec.Properties,
				Labels:     rec.Labels,
			}
			if rec.Key != "" {
				req.Key = &rec.Key
			}
			if rec.Status != "" {
				req.Status = &rec.Status
			}
			items[i] = req
		}

		resp, err := s.graph.BulkCreateObjects(ctx, &sdkgraph.BulkCreateObjectsRequest{Items: items})
		if err != nil {
			// If the whole bulk call fails, count all as failed.
			fmt.Fprintf(s.out, "  error: bulk create failed: %v\n", err)
			result.ObjectsFailed += len(toCreate)
		} else {
			for i, r := range resp.Results {
				if r.Success && r.Object != nil {
					result.ObjectsCreated++
					// Register the entityID in the key map.
					if toCreate[i].Key != "" {
						batchKeyMap[toCreate[i].Key] = r.Object.EntityID
					}
				} else {
					result.ObjectsFailed++
					errMsg := "<unknown>"
					if r.Error != nil {
						errMsg = *r.Error
					}
					fmt.Fprintf(s.out, "  error: create object[%d] type=%s key=%q: %s\n",
						i, toCreate[i].Type, toCreate[i].Key, errMsg)
				}
			}
		}
	}

	// Upsert existing objects individually.
	for _, rec := range toUpsert {
		keyVal := rec.Key // guaranteed non-empty for upsert candidates
		req := &sdkgraph.CreateObjectRequest{
			Type:       rec.Type,
			Key:        &keyVal,
			Properties: rec.Properties,
			Labels:     rec.Labels,
		}
		if rec.Status != "" {
			req.Status = &rec.Status
		}
		obj, err := s.graph.UpsertObject(ctx, req)
		if err != nil {
			result.ObjectsFailed++
			fmt.Fprintf(s.out, "  error: upsert object type=%s key=%q: %v\n", rec.Type, rec.Key, err)
			continue
		}
		result.ObjectsUpdated++
		batchKeyMap[rec.Key] = obj.EntityID
	}

	// Record skipped objects.
	for _, rec := range toSkip {
		result.ObjectsSkipped++
		// Still register the existing entityID so relationships can resolve.
		if id, ok := existingByKey[rec.Key]; ok {
			batchKeyMap[rec.Key] = id
		}
		fmt.Fprintf(s.out, "  skip: object type=%s key=%q (already exists; use --upgrade to update)\n", rec.Type, rec.Key)
	}

	return batchKeyMap, nil
}

// applyRelationships processes relationship records in batches of seedBatchSize.
func (s *Seeder) applyRelationships(ctx context.Context, rels []SeedRelationshipRecord, keyMap map[string]string, result *SeedResult) error {
	if len(rels) == 0 {
		return nil
	}

	for start := 0; start < len(rels); start += seedBatchSize {
		end := start + seedBatchSize
		if end > len(rels) {
			end = len(rels)
		}
		if err := s.applyRelationshipBatch(ctx, rels[start:end], keyMap, result); err != nil {
			return err
		}
	}
	return nil
}

// applyRelationshipBatch processes one batch of relationships.
func (s *Seeder) applyRelationshipBatch(ctx context.Context, batch []SeedRelationshipRecord, keyMap map[string]string, result *SeedResult) error {
	type indexed struct {
		index int
		rec   SeedRelationshipRecord
		req   sdkgraph.CreateRelationshipRequest
	}

	var toCreate []indexed

	for i, rec := range batch {
		srcID, dstID, err := resolveRelationshipEndpoints(rec, keyMap)
		if err != nil {
			result.RelsFailed++
			fmt.Fprintf(s.out, "  error: relationship type=%s: %v\n", rec.Type, err)
			continue
		}

		if s.dryRun {
			fmt.Fprintf(s.out, "  [dry-run] would create relationship type=%s src=%s dst=%s\n",
				rec.Type, srcID, dstID)
			result.RelsCreated++
			continue
		}

		toCreate = append(toCreate, indexed{
			index: i,
			rec:   rec,
			req: sdkgraph.CreateRelationshipRequest{
				Type:       rec.Type,
				SrcID:      srcID,
				DstID:      dstID,
				Properties: rec.Properties,
				Weight:     rec.Weight,
			},
		})
	}

	if len(toCreate) == 0 {
		return nil
	}

	items := make([]sdkgraph.CreateRelationshipRequest, len(toCreate))
	for i, entry := range toCreate {
		items[i] = entry.req
	}

	resp, err := s.graph.BulkCreateRelationships(ctx, &sdkgraph.BulkCreateRelationshipsRequest{Items: items})
	if err != nil {
		fmt.Fprintf(s.out, "  error: bulk create relationships failed: %v\n", err)
		result.RelsFailed += len(toCreate)
		return nil
	}

	for i, r := range resp.Results {
		if r.Success {
			result.RelsCreated++
		} else {
			result.RelsFailed++
			errMsg := "<unknown>"
			if r.Error != nil {
				errMsg = *r.Error
			}
			fmt.Fprintf(s.out, "  error: relationship[%d] type=%s: %s\n",
				toCreate[i].index, toCreate[i].rec.Type, errMsg)
		}
	}

	return nil
}

// resolveRelationshipEndpoints returns the srcID and dstID to use for a
// relationship, resolving keys via keyMap where available.
func resolveRelationshipEndpoints(rec SeedRelationshipRecord, keyMap map[string]string) (srcID, dstID string, err error) {
	if rec.SrcKey != "" {
		id, ok := keyMap[rec.SrcKey]
		if !ok {
			return "", "", fmt.Errorf("src key %q not found in key map", rec.SrcKey)
		}
		srcID = id
	} else {
		srcID = rec.SrcID
	}

	if rec.DstKey != "" {
		id, ok := keyMap[rec.DstKey]
		if !ok {
			return "", "", fmt.Errorf("dst key %q not found in key map", rec.DstKey)
		}
		dstID = id
	} else {
		dstID = rec.DstID
	}

	if srcID == "" || dstID == "" {
		return "", "", fmt.Errorf("could not resolve src/dst endpoints")
	}
	return srcID, dstID, nil
}
