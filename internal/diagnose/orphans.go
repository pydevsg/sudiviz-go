package diagnose

import "github.com/pydevsg/sudiviz-go/internal/graph"

// Orphan detection: a resource is "orphan" if no path through the topology
// reaches it from a client-facing entry point. Cheap local rules approximate
// this:
//   - target group with NO incoming forwards_to edge (no listener routes to it)
//   - instance with NO outgoing registered_in edge (in no target group)
//   - security group with NO incoming guarded_by edge (attached to nothing)

// FindOrphanTargetGroups returns target groups no listener forwards to.
func FindOrphanTargetGroups(g *graph.InfraGraph) []*graph.Resource {
	var out []*graph.Resource
	for _, n := range g.ResourcesByKind(graph.KindTargetGroup) {
		if len(g.InEdges(n.ID, graph.RelationForwardsTo)) == 0 {
			out = append(out, n)
		}
	}
	return out
}

// FindOrphanInstances returns instances registered in no target group.
func FindOrphanInstances(g *graph.InfraGraph) []*graph.Resource {
	var out []*graph.Resource
	for _, n := range g.ResourcesByKind(graph.KindInstance) {
		if len(g.OutEdges(n.ID, graph.RelationRegisteredIn)) == 0 {
			out = append(out, n)
		}
	}
	return out
}

// FindUnusedSecurityGroups returns security groups nothing is guarded by.
func FindUnusedSecurityGroups(g *graph.InfraGraph) []*graph.Resource {
	var out []*graph.Resource
	for _, n := range g.ResourcesByKind(graph.KindSecurityGroup) {
		if len(g.InEdges(n.ID, graph.RelationGuardedBy)) == 0 {
			out = append(out, n)
		}
	}
	return out
}

// MarkOrphanedEdges annotates orphan nodes and their incident edges in place
// (orphan flag, unhealthy health, dashed edge style) and returns the graph
// for chaining. Every renderer relies on these annotations.
func MarkOrphanedEdges(g *graph.InfraGraph) *graph.InfraGraph {
	orphanIDs := map[string]bool{}
	for _, n := range FindOrphanTargetGroups(g) {
		orphanIDs[n.ID] = true
	}
	for _, n := range FindOrphanInstances(g) {
		orphanIDs[n.ID] = true
	}
	for _, n := range FindUnusedSecurityGroups(g) {
		orphanIDs[n.ID] = true
	}

	for id := range orphanIDs {
		if n := g.Resource(id); n != nil {
			n.Orphan = true
			n.Health = graph.HealthUnhealthy
		}
	}
	for _, e := range g.Edges() {
		if orphanIDs[e.From] || orphanIDs[e.To] {
			e.Style = "dashed"
			e.Orphan = true
		}
	}
	return g
}
