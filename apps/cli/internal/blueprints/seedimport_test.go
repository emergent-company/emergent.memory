package blueprints_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/emergent-company/emergent.memory/apps/cli/internal/blueprints"
	sdkgraph "github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/graph"
)

// ──────────────────────────────────────────────────────────────────────────────
// LoadSeedPath — dumper layout
// ──────────────────────────────────────────────────────────────────────────────

func TestLoadSeedPath_DumpLayout(t *testing.T) {
	dir := t.TempDir()
	objDir := filepath.Join(dir, "seed", "objects")
	relDir := filepath.Join(dir, "seed", "relationships")
	if err := os.MkdirAll(objDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(relDir, 0o755); err != nil {
		t.Fatal(err)
	}

	seedWriteFile(t, filepath.Join(objDir, "Document.jsonl"),
		`{"type":"Document","key":"doc-1","properties":{"title":"One"}}`+"\n"+
			`{"type":"Document","key":"doc-2"}`+"\n")
	// #323: untyped objects dump to _untyped.jsonl and load back as "Untyped".
	seedWriteFile(t, filepath.Join(objDir, "_untyped.jsonl"),
		`{"type":"","key":"legacy-1"}`+"\n")
	seedWriteFile(t, filepath.Join(relDir, "mentions.jsonl"),
		`{"type":"mentions","srcKey":"doc-1","dstKey":"doc-2"}`+"\n")

	imp, err := blueprints.LoadSeedPath(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := len(imp.Objects); got != 3 {
		t.Errorf("expected 3 objects, got %d", got)
	}
	if got := len(imp.Relationships); got != 1 {
		t.Errorf("expected 1 relationship, got %d", got)
	}
	if got := imp.FailedRows(); got != 0 {
		t.Errorf("expected 0 failed rows, got %d (%v)", got, imp.Issues)
	}

	var untyped *blueprints.SeedObjectRecord
	for i := range imp.Objects {
		if imp.Objects[i].Key == "legacy-1" {
			untyped = &imp.Objects[i]
		}
	}
	if untyped == nil {
		t.Fatal("legacy object was not loaded")
	}
	if untyped.Type != blueprints.UntypedTypeName {
		t.Errorf("expected untyped object to load as %q, got %q", blueprints.UntypedTypeName, untyped.Type)
	}
}

func TestLoadSeedPath_PerRowErrorsDoNotAbort(t *testing.T) {
	dir := t.TempDir()
	seedWriteFile(t, filepath.Join(dir, "Document.jsonl"),
		`{"type":"Document","key":"ok"}`+"\n"+
			`{"type":"Document",`+"\n"+ // malformed JSON
			`{"key":"no-type"}`+"\n") // validation failure
	seedWriteFile(t, filepath.Join(dir, "mentions.jsonl"),
		`{"type":"mentions","srcKey":"only-src"}`+"\n"+ // missing dst
			`{"type":"mentions","srcKey":"ok","dstKey":"ok"}`+"\n")

	imp, err := blueprints.LoadSeedPath(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := len(imp.Objects); got != 1 {
		t.Errorf("expected 1 readable object, got %d", got)
	}
	if got := len(imp.Relationships); got != 1 {
		t.Errorf("expected 1 readable relationship, got %d", got)
	}
	if got := imp.FailedRows(); got != 3 {
		t.Errorf("expected 3 failed rows, got %d (%v)", got, imp.Issues)
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// Dump → import into a branch
// ──────────────────────────────────────────────────────────────────────────────

// branchFakeGraph is an in-memory graph keyed by branch. The main graph is
// stored under "" and branch graphs under their branch ID.
type branchFakeGraph struct {
	mu     sync.Mutex
	next   int
	objs   map[string][]*sdkgraph.GraphObject
	rels   map[string][]*sdkgraph.GraphRelationship
	byID   map[string]*sdkgraph.GraphObject
	byKey  map[string]string // branch|type|key -> entity ID
	mainID map[string]string // key -> entity ID in main
}

func newBranchFakeGraph() *branchFakeGraph {
	return &branchFakeGraph{
		objs:   map[string][]*sdkgraph.GraphObject{},
		rels:   map[string][]*sdkgraph.GraphRelationship{},
		byID:   map[string]*sdkgraph.GraphObject{},
		byKey:  map[string]string{},
		mainID: map[string]string{},
	}
}

func (f *branchFakeGraph) branchOf(id *string) string {
	if id == nil {
		return ""
	}
	return *id
}

func (f *branchFakeGraph) addMainObject(entityID, typ, key string) {
	k := key
	obj := &sdkgraph.GraphObject{EntityID: entityID, Type: typ, Key: &k}
	f.objs[""] = append(f.objs[""], obj)
	f.byID[entityID] = obj
	f.byKey["|"+typ+"|"+key] = entityID
	f.mainID[key] = entityID
}

func (f *branchFakeGraph) addMainRelationship(entityID, typ, src, dst string) {
	f.rels[""] = append(f.rels[""], &sdkgraph.GraphRelationship{
		EntityID: entityID, Type: typ, SrcID: src, DstID: dst,
	})
}

func (f *branchFakeGraph) handler(t *testing.T) http.Handler {
	t.Helper()
	mux := http.NewServeMux()

	mux.HandleFunc("/api/graph/objects/search", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		branch := r.URL.Query().Get("branch_id")
		items := []*sdkgraph.GraphObject{}
		wanted := map[string]bool{}
		if keys := r.URL.Query().Get("keys"); keys != "" {
			for _, k := range strings.Split(keys, ",") {
				wanted[strings.TrimSpace(k)] = true
			}
		}
		for _, obj := range f.objs[branch] {
			if len(wanted) > 0 && !wanted[*obj.Key] {
				continue
			}
			items = append(items, obj)
		}
		seedWriteJSON(t, w, sdkgraph.SearchObjectsResponse{Items: items, Total: len(items)})
	})

	mux.HandleFunc("/api/graph/relationships/search", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		items := f.rels[""]
		seedWriteJSON(t, w, sdkgraph.SearchRelationshipsResponse{Items: items, Total: len(items)})
	})

	mux.HandleFunc("/api/graph/objects/bulk", func(w http.ResponseWriter, r *http.Request) {
		var req sdkgraph.BulkCreateObjectsRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		f.mu.Lock()
		defer f.mu.Unlock()
		resp := sdkgraph.BulkCreateObjectsResponse{}
		for i, item := range req.Items {
			f.next++
			entityID := "branch-obj-" + strconv.Itoa(f.next)
			obj := &sdkgraph.GraphObject{
				EntityID:   entityID,
				Type:       item.Type,
				Key:        item.Key,
				BranchID:   item.BranchID,
				Properties: item.Properties,
			}
			branch := f.branchOf(item.BranchID)
			f.objs[branch] = append(f.objs[branch], obj)
			f.byID[entityID] = obj
			if item.Key != nil {
				f.byKey[branch+"|"+item.Type+"|"+*item.Key] = entityID
			}
			resp.Success++
			resp.Results = append(resp.Results, sdkgraph.BulkCreateObjectResult{Index: i, Success: true, Object: obj})
		}
		seedWriteJSON(t, w, resp)
	})

	mux.HandleFunc("/api/graph/objects/bulk-upsert", func(w http.ResponseWriter, r *http.Request) {
		var req sdkgraph.BulkUpsertObjectsRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		f.mu.Lock()
		defer f.mu.Unlock()
		resp := sdkgraph.BulkUpsertObjectsResponse{}
		for i, item := range req.Items {
			branch := f.branchOf(item.BranchID)
			if item.Key == nil {
				resp.Failed++
				msg := "key required"
				resp.Results = append(resp.Results, sdkgraph.BulkUpsertObjectResult{Index: i, Error: &msg})
				continue
			}
			lookup := branch + "|" + item.Type + "|" + *item.Key
			created := false
			entityID := f.byKey[lookup]
			if entityID == "" {
				f.next++
				entityID = "branch-obj-" + strconv.Itoa(f.next)
				created = true
				f.byKey[lookup] = entityID
			}
			obj := &sdkgraph.GraphObject{
				EntityID: entityID, Type: item.Type, Key: item.Key,
				BranchID: item.BranchID, Properties: item.Properties,
			}
			if created {
				f.objs[branch] = append(f.objs[branch], obj)
			}
			f.byID[entityID] = obj
			resp.Success++
			resp.Results = append(resp.Results, sdkgraph.BulkUpsertObjectResult{
				Index: i, Success: true, Created: created, Object: obj,
			})
		}
		seedWriteJSON(t, w, resp)
	})

	mux.HandleFunc("/api/graph/relationships/bulk-upsert", func(w http.ResponseWriter, r *http.Request) {
		var req sdkgraph.BulkUpsertRelationshipsRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		f.mu.Lock()
		defer f.mu.Unlock()
		resp := sdkgraph.BulkUpsertRelationshipsResponse{}
		for i, item := range req.Items {
			f.next++
			rel := &sdkgraph.GraphRelationship{
				EntityID: "branch-rel-" + strconv.Itoa(f.next),
				Type:     item.Type,
				SrcID:    item.SrcID,
				DstID:    item.DstID,
				BranchID: item.BranchID,
			}
			branch := f.branchOf(item.BranchID)
			f.rels[branch] = append(f.rels[branch], rel)
			resp.Success++
			resp.Results = append(resp.Results, sdkgraph.BulkUpsertRelationshipResult{
				Index: i, Success: true, Created: true, Relationship: rel,
			})
		}
		seedWriteJSON(t, w, resp)
	})

	return mux
}

func TestDumpImportIntoBranch(t *testing.T) {
	fake := newBranchFakeGraph()
	fake.addMainObject("main-a", "Document", "doc-a")
	fake.addMainObject("main-b", "Document", "doc-b")
	fake.addMainRelationship("main-rel-1", "mentions", "main-a", "main-b")

	srv := httptest.NewServer(fake.handler(t))
	defer srv.Close()
	client := newTestGraphClient(srv.URL)

	// 1. Dump the main graph to the seed layout.
	dumpDir := t.TempDir()
	if _, err := blueprints.NewDumper(client, nil, nil).Run(context.Background(), dumpDir); err != nil {
		t.Fatalf("dump: %v", err)
	}

	// 2. Edit the dump: add a relationship whose srcKey does not exist. The
	// import must report it per-row and still apply the good edge.
	relFile := filepath.Join(dumpDir, "seed", "relationships", "mentions.jsonl")
	seedAppendFile(t, relFile, `{"type":"mentions","srcKey":"missing-key","dstKey":"doc-b"}`+"\n")

	imp, err := blueprints.LoadSeedPath(dumpDir)
	if err != nil {
		t.Fatalf("load seed path: %v", err)
	}
	if got := len(imp.Objects); got != 2 {
		t.Fatalf("expected 2 objects in dump, got %d", got)
	}
	if got := len(imp.Relationships); got != 2 {
		t.Fatalf("expected 2 relationships in edited dump, got %d", got)
	}

	// 3. Import into branch br-1.
	seeder := blueprints.NewSeeder(client, false, true, &strings.Builder{})
	seeder.SetBranchID("br-1")
	result, err := seeder.Run(context.Background(), imp.Objects, imp.Relationships)
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if result.ObjectsCreated != 2 || result.ObjectsFailed != 0 {
		t.Errorf("expected 2 objects created / 0 failed, got %d / %d", result.ObjectsCreated, result.ObjectsFailed)
	}
	if result.RelsCreated != 1 || result.RelsFailed != 1 {
		t.Errorf("expected 1 relationship created / 1 failed, got %d / %d", result.RelsCreated, result.RelsFailed)
	}

	// 4. The branch holds both objects and the good edge, scoped to br-1.
	branchObjs := fake.objs["br-1"]
	if len(branchObjs) != 2 {
		t.Fatalf("expected 2 objects on branch, got %d", len(branchObjs))
	}
	branchIDs := map[string]string{}
	for _, obj := range branchObjs {
		if obj.BranchID == nil || *obj.BranchID != "br-1" {
			t.Errorf("object %s not scoped to br-1: %v", obj.EntityID, obj.BranchID)
		}
		if obj.Key != nil {
			branchIDs[*obj.Key] = obj.EntityID
		}
	}
	branchRels := fake.rels["br-1"]
	if len(branchRels) != 1 {
		t.Fatalf("expected 1 relationship on branch, got %d", len(branchRels))
	}
	gotRel := branchRels[0]
	if gotRel.BranchID == nil || *gotRel.BranchID != "br-1" {
		t.Errorf("relationship not scoped to br-1: %v", gotRel.BranchID)
	}
	if gotRel.SrcID != branchIDs["doc-a"] || gotRel.DstID != branchIDs["doc-b"] {
		t.Errorf("relationship endpoints %s -> %s do not resolve to branch objects (%v)",
			gotRel.SrcID, gotRel.DstID, branchIDs)
	}

	// 5. Main is unchanged: same objects, same edge, original IDs.
	if len(fake.objs[""]) != 2 {
		t.Errorf("main graph object count changed: %d", len(fake.objs[""]))
	}
	if fake.mainID["doc-a"] != "main-a" || fake.mainID["doc-b"] != "main-b" {
		t.Errorf("main object IDs changed: %v", fake.mainID)
	}
	if len(fake.rels[""]) != 1 || fake.rels[""][0].EntityID != "main-rel-1" {
		t.Errorf("main graph relationships changed: %+v", fake.rels[""])
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// helpers
// ──────────────────────────────────────────────────────────────────────────────

func seedWriteFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func seedAppendFile(t *testing.T, path, content string) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := f.WriteString(content); err != nil {
		t.Fatal(err)
	}
}

func seedWriteJSON(t *testing.T, w http.ResponseWriter, v any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		t.Errorf("encode response: %v", err)
	}
}
