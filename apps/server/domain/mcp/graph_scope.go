package mcp

import (
	"github.com/google/uuid"

	"github.com/emergent-company/emergent.memory/domain/graph"
)

// filterTraverseResponseByScope drops traverse nodes whose object type is
// excluded by the object axis and edges whose relationship type is excluded by
// the relationship axis. When the object axis is restricted it also drops edges
// whose src/dst endpoint resolves (by physical or canonical id) to a node whose
// type is excluded, so an allowed relationship cannot dangle onto an excluded
// object. An endpoint absent from the (possibly paginated) node page has an
// unknown type and is NOT dropped — mirroring the unscoped traverse service,
// which keeps an edge when either endpoint is on the page — so legitimate
// cross-page edges survive. A nil or unrestricted scope returns res unchanged.
// It mutates res in place (reusing the slices' backing arrays) and returns it.
func filterTraverseResponseByScope(scope *TypeScope, res *graph.TraverseGraphResponse) *graph.TraverseGraphResponse {
	if res == nil {
		return res
	}
	// nodeTypeByID resolves an endpoint id to the original node's type. Built only
	// when the object axis is restricted, so unrestricted behavior stays
	// byte-identical.
	var nodeTypeByID map[uuid.UUID]string
	if scope.HasObjectRestriction() {
		nodeTypeByID = make(map[uuid.UUID]string, len(res.Nodes)*2)
		for _, n := range res.Nodes {
			if n == nil {
				continue
			}
			nodeTypeByID[n.ID] = n.Type
			nodeTypeByID[n.CanonicalID] = n.Type
		}

		nodes := make([]*graph.TraverseNode, 0, len(res.Nodes))
		for _, n := range res.Nodes {
			if n != nil && scope.AllowsObject(n.Type) {
				nodes = append(nodes, n)
			}
		}
		res.Nodes = nodes
	}
	// Edge filtering runs when EITHER axis restricts: the relationship axis drops
	// excluded relationship types, and the object axis (via nodeTypeByID) drops
	// edges whose endpoints are not permitted nodes, so an object-only scope still
	// removes dangling edges.
	if scope.HasRelationshipRestriction() || nodeTypeByID != nil {
		edges := make([]*graph.TraverseEdge, 0, len(res.Edges))
		for _, e := range res.Edges {
			if e == nil || !scope.AllowsRelationship(e.Type) {
				continue
			}
			if nodeTypeByID != nil {
				// Drop only when an endpoint's type is KNOWN to be excluded. An
				// endpoint absent from the (possibly paginated) node page has an
				// unknown type and is kept, so a legitimate cross-page edge is not
				// silently dropped for scoped agents.
				if t, ok := nodeTypeByID[e.SrcID]; ok && !scope.AllowsObject(t) {
					continue
				}
				if t, ok := nodeTypeByID[e.DstID]; ok && !scope.AllowsObject(t) {
					continue
				}
			}
			edges = append(edges, e)
		}
		res.Edges = edges
	}
	return res
}
