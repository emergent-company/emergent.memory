package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	sdkgraph "github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/graph"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newGraphBatchTestCmd returns a bare command whose output is discarded.
func newGraphBatchTestCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "test"}
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	return cmd
}

// graphBatchNoopAuth satisfies the SDK auth.Provider for a local test server.
type graphBatchNoopAuth struct{}

func (graphBatchNoopAuth) Authenticate(*http.Request) error { return nil }
func (graphBatchNoopAuth) Refresh(context.Context) error    { return nil }

func TestRunSubgraphOnBranch_UpsertsObjectsAndResolvesRefs(t *testing.T) {
	var branchesOnObjects, branchesOnRels []string
	var relSrc, relDst string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/graph/objects/bulk-upsert":
			var req sdkgraph.BulkUpsertObjectsRequest
			require.NoError(t, json.NewDecoder(r.Body).Decode(&req))
			resp := sdkgraph.BulkUpsertObjectsResponse{}
			for i, item := range req.Items {
				if item.BranchID != nil {
					branchesOnObjects = append(branchesOnObjects, *item.BranchID)
				}
				resp.Success++
				resp.Results = append(resp.Results, sdkgraph.BulkUpsertObjectResult{
					Index:   i,
					Success: true,
					Object: &sdkgraph.GraphObject{
						EntityID: "eid-" + *item.Key,
						Type:     item.Type,
						Key:      item.Key,
						BranchID: item.BranchID,
					},
				})
			}
			_ = json.NewEncoder(w).Encode(resp)

		case "/api/graph/objects/bulk":
			var req sdkgraph.BulkCreateObjectsRequest
			require.NoError(t, json.NewDecoder(r.Body).Decode(&req))
			resp := sdkgraph.BulkCreateObjectsResponse{}
			for i, item := range req.Items {
				if item.BranchID != nil {
					branchesOnObjects = append(branchesOnObjects, *item.BranchID)
				}
				resp.Success++
				resp.Results = append(resp.Results, sdkgraph.BulkCreateObjectResult{
					Index:   i,
					Success: true,
					Object: &sdkgraph.GraphObject{
						EntityID: "eid-keyless",
						Type:     item.Type,
						BranchID: item.BranchID,
					},
				})
			}
			_ = json.NewEncoder(w).Encode(resp)

		case "/api/graph/relationships/bulk-upsert":
			var req sdkgraph.BulkUpsertRelationshipsRequest
			require.NoError(t, json.NewDecoder(r.Body).Decode(&req))
			resp := sdkgraph.BulkUpsertRelationshipsResponse{}
			for i, item := range req.Items {
				if item.BranchID != nil {
					branchesOnRels = append(branchesOnRels, *item.BranchID)
				}
				relSrc, relDst = item.SrcID, item.DstID
				resp.Success++
				resp.Results = append(resp.Results, sdkgraph.BulkUpsertRelationshipResult{
					Index:   i,
					Success: true,
					Relationship: &sdkgraph.GraphRelationship{
						EntityID: "eid-rel", Type: item.Type, SrcID: item.SrcID, DstID: item.DstID,
						BranchID: item.BranchID,
					},
				})
			}
			_ = json.NewEncoder(w).Encode(resp)

		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	client := sdkgraph.NewClient(http.DefaultClient, srv.URL, graphBatchNoopAuth{}, "", "")

	key := "k-a"
	sg := subgraphInput{
		Objects: []subgraphObjectInput{
			{Ref: "a", Type: "Doc", Key: &key, Name: "A"},
			{Ref: "b", Type: "Doc", Name: "B"},
		},
		Relationships: []subgraphRelationshipInput{
			{Type: "links", SrcRef: "a", DstRef: "b"},
		},
	}

	oldOutput := graphOutputFlag
	graphOutputFlag = "table"
	defer func() { graphOutputFlag = oldOutput }()

	cmd := newGraphBatchTestCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)

	require.NoError(t, runSubgraphOnBranch(cmd, client, sg, "branch-1", &out))

	assert.Equal(t, []string{"branch-1", "branch-1"}, branchesOnObjects)
	assert.Equal(t, []string{"branch-1"}, branchesOnRels)
	assert.Equal(t, "eid-k-a", relSrc)
	assert.Equal(t, "eid-keyless", relDst)
	assert.Contains(t, out.String(), "Created 2 objects, 1 relationships on branch branch-1")
}

func TestRunSubgraphOnBranch_PerRowErrorDoesNotAbort(t *testing.T) {
	relCalls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/graph/objects/bulk-upsert":
			var req sdkgraph.BulkUpsertObjectsRequest
			require.NoError(t, json.NewDecoder(r.Body).Decode(&req))
			resp := sdkgraph.BulkUpsertObjectsResponse{}
			for i, item := range req.Items {
				resp.Success++
				resp.Results = append(resp.Results, sdkgraph.BulkUpsertObjectResult{
					Index:   i,
					Success: true,
					Object: &sdkgraph.GraphObject{
						EntityID: "eid-" + *item.Key, Type: item.Type, Key: item.Key,
						BranchID: item.BranchID,
					},
				})
			}
			_ = json.NewEncoder(w).Encode(resp)

		case "/api/graph/relationships/bulk-upsert":
			relCalls++
			_ = json.NewEncoder(w).Encode(sdkgraph.BulkUpsertRelationshipsResponse{})

		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	client := sdkgraph.NewClient(http.DefaultClient, srv.URL, graphBatchNoopAuth{}, "", "")

	key := "k-a"
	sg := subgraphInput{
		Objects: []subgraphObjectInput{{Ref: "a", Type: "Doc", Key: &key}},
		Relationships: []subgraphRelationshipInput{
			{Type: "links", SrcRef: "a", DstRef: "does-not-exist"},
		},
	}

	oldOutput := graphOutputFlag
	graphOutputFlag = "table"
	defer func() { graphOutputFlag = oldOutput }()

	cmd := newGraphBatchTestCmd()
	var out, errOut bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)

	err := runSubgraphOnBranch(cmd, client, sg, "branch-1", &out)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "1 relationship(s) failed to import")
	assert.Contains(t, errOut.String(), `dst_ref "does-not-exist" was not created`)
	assert.Equal(t, 0, relCalls, "no relationship batch should be sent for an unresolvable ref")
	assert.True(t, strings.Contains(out.String(), "Created 1 objects, 0 relationships"), out.String())
}
