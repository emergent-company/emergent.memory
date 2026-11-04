package graph_test

import (
	"context"
	"sort"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/domain/graph"
	"github.com/emergent-company/emergent.memory/pkg/auth"
)

// TestWriteStampFiltersQueryRows proves the read half of #2376 (#2468): the
// stamp columns are queryable, so "what did this run write" and "what did this
// key write" are answerable from a listing.
//
// The fixture writes four objects under two credentials and three runs, one of
// them a delegated sub-run whose root is the first run. Each filter must return
// exactly its own rows; the root-run filter must also return the sub-run's row;
// and the reported total must agree with the page.
func TestWriteStampFiltersQueryRows(t *testing.T) {
	ctx, _, _, svc, projectID := setupProvenanceTest(t)

	credA, credB := uuid.New(), uuid.New()
	rootRun, subRun, otherRun := uuid.New(), uuid.New(), uuid.New()

	createObject := func(key string, credential *uuid.UUID, runID, rootID *uuid.UUID) *graph.GraphObjectResponse {
		sctx := ctx
		if credential != nil {
			sctx = auth.ContextWithCredential(sctx, auth.CredentialAPIKey, credential)
		}
		if runID != nil || rootID != nil {
			sctx = auth.ContextWithRunIDs(sctx, runID, rootID)
		}
		resp, err := svc.Create(sctx, projectID, &graph.CreateGraphObjectRequest{
			Type:       "StampFilterObj",
			Key:        strPtr(key),
			Properties: map[string]any{"title": key},
		}, nil)
		require.NoError(t, err)
		return resp
	}

	// Two writes by credential A inside one run chain, one by credential B, and
	// one unattributed background-style write.
	createObject("stampfilter/a", &credA, &rootRun, &rootRun)
	createObject("stampfilter/sub", &credA, &subRun, &rootRun)
	createObject("stampfilter/b", &credB, &otherRun, &otherRun)
	createObject("stampfilter/none", nil, nil, nil)

	listKeys := func(stamp graph.WriteStampFilters) []string {
		resp, err := svc.List(ctx, graph.ListParams{ProjectID: projectID, Stamp: stamp, Limit: 100})
		require.NoError(t, err)
		keys := make([]string, 0, len(resp.Items))
		for _, item := range resp.Items {
			if item.Key != nil {
				keys = append(keys, *item.Key)
			}
		}
		sort.Strings(keys)
		return keys
	}

	t.Run("root run returns its delegation subtree", func(t *testing.T) {
		require.Equal(t,
			[]string{"stampfilter/a", "stampfilter/sub"},
			listKeys(graph.WriteStampFilters{RunID: &rootRun}),
		)
	})

	t.Run("a sub-run returns only its own write", func(t *testing.T) {
		require.Equal(t, []string{"stampfilter/sub"}, listKeys(graph.WriteStampFilters{RunID: &subRun}))
	})

	t.Run("credential separates two writers", func(t *testing.T) {
		require.Equal(t,
			[]string{"stampfilter/a", "stampfilter/sub"},
			listKeys(graph.WriteStampFilters{CredentialID: &credA}),
		)
		require.Equal(t, []string{"stampfilter/b"}, listKeys(graph.WriteStampFilters{CredentialID: &credB}))
	})

	t.Run("credential kind separates key writes from unattributed ones", func(t *testing.T) {
		apiKey := auth.CredentialAPIKey
		require.Equal(t,
			[]string{"stampfilter/a", "stampfilter/b", "stampfilter/sub"},
			listKeys(graph.WriteStampFilters{CredentialKind: &apiKey}),
		)

		none := auth.CredentialNone
		require.Equal(t, []string{"stampfilter/none"}, listKeys(graph.WriteStampFilters{CredentialKind: &none}))
	})

	t.Run("data class filters", func(t *testing.T) {
		synthesized := graph.DataClassSynthesized
		require.Equal(t,
			[]string{"stampfilter/a", "stampfilter/b", "stampfilter/none", "stampfilter/sub"},
			listKeys(graph.WriteStampFilters{DataClass: &synthesized}),
		)

		source := graph.DataClassSource
		require.Empty(t, listKeys(graph.WriteStampFilters{DataClass: &source}))
	})

	t.Run("filters compose as AND", func(t *testing.T) {
		require.Equal(t,
			[]string{"stampfilter/sub"},
			listKeys(graph.WriteStampFilters{CredentialID: &credA, RunID: &subRun}),
		)
	})

	t.Run("an unmatched filter is an empty page with a zero total", func(t *testing.T) {
		unmatched := uuid.New()
		resp, err := svc.List(ctx, graph.ListParams{
			ProjectID: projectID,
			Stamp:     graph.WriteStampFilters{RunID: &unmatched},
			Limit:     100,
		})
		require.NoError(t, err)
		require.Empty(t, resp.Items)
		require.NotNil(t, resp.Total)
		require.Zero(t, *resp.Total)
	})

	t.Run("the total agrees with the filtered page", func(t *testing.T) {
		resp, err := svc.List(ctx, graph.ListParams{
			ProjectID: projectID,
			Stamp:     graph.WriteStampFilters{RunID: &rootRun},
			Limit:     100,
		})
		require.NoError(t, err)
		require.Len(t, resp.Items, 2)
		require.NotNil(t, resp.Total)
		require.Equal(t, 2, *resp.Total)

		count, err := svc.CountObjects(ctx, graph.ListParams{
			ProjectID: projectID,
			Stamp:     graph.WriteStampFilters{RunID: &rootRun},
		})
		require.NoError(t, err)
		require.Equal(t, 2, count)
	})

	t.Run("pagination composes with the filter", func(t *testing.T) {
		first, err := svc.List(ctx, graph.ListParams{
			ProjectID: projectID,
			Stamp:     graph.WriteStampFilters{CredentialID: &credA},
			Limit:     1,
		})
		require.NoError(t, err)
		require.Len(t, first.Items, 1)
		require.NotNil(t, first.NextCursor)

		second, err := svc.List(ctx, graph.ListParams{
			ProjectID: projectID,
			Stamp:     graph.WriteStampFilters{CredentialID: &credA},
			Limit:     1,
			Cursor:    first.NextCursor,
		})
		require.NoError(t, err)
		require.Len(t, second.Items, 1)
		require.NotEqual(t, first.Items[0].CanonicalID, second.Items[0].CanonicalID)
	})

	t.Run("relationships accept the same credential and run filters", func(t *testing.T) {
		src := createObject("stampfilter/rel-src", &credA, &rootRun, &rootRun)
		dst := createObject("stampfilter/rel-dst", &credB, &otherRun, &otherRun)

		createRelationship := func(relType string, credential, runID uuid.UUID) {
			rctx := auth.ContextWithCredential(ctx, auth.CredentialAPIKey, &credential)
			rctx = auth.ContextWithRunIDs(rctx, &runID, &runID)
			_, err := svc.CreateRelationship(rctx, projectID, &graph.CreateGraphRelationshipRequest{
				Type:  relType,
				SrcID: src.CanonicalID,
				DstID: dst.CanonicalID,
			}, nil)
			require.NoError(t, err)
		}
		createRelationship("StampFilterRelA", credA, rootRun)
		createRelationship("StampFilterRelB", credB, otherRun)

		listRelationships := func(stamp graph.WriteStampFilters) []string {
			resp, err := svc.ListRelationships(ctx, graph.RelationshipListParams{
				ProjectID: projectID,
				Stamp:     stamp,
				Limit:     100,
			})
			require.NoError(t, err)
			ids := make([]string, 0, len(resp.Items))
			for _, item := range resp.Items {
				ids = append(ids, item.Type)
			}
			return ids
		}

		require.Len(t, listRelationships(graph.WriteStampFilters{CredentialID: &credA}), 1)
		require.Len(t, listRelationships(graph.WriteStampFilters{RunID: &rootRun}), 1)

		apiKey := auth.CredentialAPIKey
		require.Len(t, listRelationships(graph.WriteStampFilters{CredentialKind: &apiKey}), 2)

		unmatched := uuid.New()
		require.Empty(t, listRelationships(graph.WriteStampFilters{RunID: &unmatched}))

		// CountRelationships must apply the same predicates, or a relationship
		// total would contradict its page.
		count, err := svc.CountRelationships(context.Background(), graph.RelationshipListParams{
			ProjectID: projectID,
			Stamp:     graph.WriteStampFilters{CredentialID: &credA},
		})
		require.NoError(t, err)
		require.Equal(t, 1, count)
	})
}
