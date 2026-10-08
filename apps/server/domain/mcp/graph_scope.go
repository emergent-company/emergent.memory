package mcp

import "github.com/emergent-company/emergent.memory/domain/graph"

// filterTraverseResponseByScope drops traverse nodes whose object type is
// excluded by the object axis and edges whose relationship type is excluded by
// the relationship axis. A nil or unrestricted scope returns res unchanged. It
// mutates res in place (reusing the slices' backing arrays) and returns it.
func filterTraverseResponseByScope(scope *TypeScope, res *graph.TraverseGraphResponse) *graph.TraverseGraphResponse {
	if res == nil {
		return res
	}
	if scope.HasObjectRestriction() {
		nodes := make([]*graph.TraverseNode, 0, len(res.Nodes))
		for _, n := range res.Nodes {
			if n != nil && scope.AllowsObject(n.Type) {
				nodes = append(nodes, n)
			}
		}
		res.Nodes = nodes
	}
	if scope.HasRelationshipRestriction() {
		edges := make([]*graph.TraverseEdge, 0, len(res.Edges))
		for _, e := range res.Edges {
			if e != nil && scope.AllowsRelationship(e.Type) {
				edges = append(edges, e)
			}
		}
		res.Edges = edges
	}
	return res
}
