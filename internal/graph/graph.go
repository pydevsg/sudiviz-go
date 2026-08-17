package graph

import (
	"time"

	gograph "gonum.org/v1/gonum/graph"
	"gonum.org/v1/gonum/graph/simple"
)

type edgeKey struct{ from, to string }

// InfraGraph is a directed graph of infrastructure resources. It wraps a
// gonum simple.DirectedGraph for structure/algorithms and keeps rich node and
// edge payloads addressable by string ID.
//
// Semantics mirror a networkx DiGraph: adding an edge auto-creates missing
// endpoint nodes (as Kind "unknown" placeholders), and re-adding an existing
// (from, to) pair replaces the previous edge payload.
type InfraGraph struct {
	AccountID    string
	Region       string
	VPCID        string
	DiscoveredAt time.Time

	g       *simple.DirectedGraph
	ids     map[string]int64     // resource ID -> gonum node ID
	nodes   map[int64]*Resource  // gonum node ID -> resource
	order   []string             // insertion order for deterministic iteration
	edges   map[edgeKey]*Edge    // (from, to) -> edge payload
	edgeSeq []edgeKey            // insertion order of edges
	out     map[string][]edgeKey // adjacency: outgoing edge keys per node
	in      map[string][]edgeKey // adjacency: incoming edge keys per node
}

// New returns an empty InfraGraph.
func New() *InfraGraph {
	return &InfraGraph{
		DiscoveredAt: time.Now().UTC(),
		g:            simple.NewDirectedGraph(),
		ids:          map[string]int64{},
		nodes:        map[int64]*Resource{},
		edges:        map[edgeKey]*Edge{},
		out:          map[string][]edgeKey{},
		in:           map[string][]edgeKey{},
	}
}

// ensure returns the resource node for id, creating an unknown-kind
// placeholder if it does not exist yet.
func (ig *InfraGraph) ensure(id string) *Resource {
	if gid, ok := ig.ids[id]; ok {
		return ig.nodes[gid]
	}
	n := ig.g.NewNode()
	ig.g.AddNode(n)
	r := &Resource{ID: id, Kind: KindUnknown, Label: id, Health: HealthUnknown}
	ig.ids[id] = n.ID()
	ig.nodes[n.ID()] = r
	ig.order = append(ig.order, id)
	return r
}

// AddResource inserts a resource node. If a placeholder (or previously added
// node) with the same ID exists, its kind, label, attrs, tags and cost are
// updated in place while the existing health is preserved when the incoming
// health is unknown — this mirrors how the Python builder merges target-group
// placeholders with full EC2 instance data.
func (ig *InfraGraph) AddResource(r Resource) *Resource {
	existing, ok := ig.node(r.ID)
	if !ok {
		node := ig.ensure(r.ID)
		*node = r
		return node
	}
	keepHealth := existing.Health
	*existing = r
	if r.Health == "" || r.Health == HealthUnknown {
		if keepHealth != "" {
			existing.Health = keepHealth
		}
	}
	return existing
}

// MergeMetadata updates only label/attrs/cost of an existing node, preserving
// kind and health (used when richer data arrives for an existing node).
func (ig *InfraGraph) MergeMetadata(id, label string, attrs map[string]any, cost float64) {
	if n, ok := ig.node(id); ok {
		if label != "" {
			n.Label = label
		}
		if attrs != nil {
			n.Attrs = attrs
		}
		if cost > 0 {
			n.MonthlyCost = cost
		}
	}
}

func (ig *InfraGraph) node(id string) (*Resource, bool) {
	gid, ok := ig.ids[id]
	if !ok {
		return nil, false
	}
	return ig.nodes[gid], true
}

// Resource returns the node with the given ID, or nil.
func (ig *InfraGraph) Resource(id string) *Resource {
	n, _ := ig.node(id)
	return n
}

// HasNode reports whether a node with the given ID exists.
func (ig *InfraGraph) HasNode(id string) bool {
	_, ok := ig.ids[id]
	return ok
}

// AddEdge inserts a directed edge, auto-creating placeholder endpoints. A
// second edge with the same (from, to) replaces the first.
func (ig *InfraGraph) AddEdge(e Edge) {
	if e.From == e.To {
		return // gonum forbids self-loops; networkx topology never needs them
	}
	if e.Style == "" {
		e.Style = "solid"
	}
	from := ig.ensure(e.From)
	to := ig.ensure(e.To)
	key := edgeKey{e.From, e.To}
	if _, exists := ig.edges[key]; !exists {
		ig.g.SetEdge(ig.g.NewEdge(gonumNode{ig.ids[from.ID]}, gonumNode{ig.ids[to.ID]}))
		ig.edgeSeq = append(ig.edgeSeq, key)
		ig.out[e.From] = append(ig.out[e.From], key)
		ig.in[e.To] = append(ig.in[e.To], key)
	}
	stored := e
	ig.edges[key] = &stored
}

// Resources returns all nodes in insertion order.
func (ig *InfraGraph) Resources() []*Resource {
	out := make([]*Resource, 0, len(ig.order))
	for _, id := range ig.order {
		n, _ := ig.node(id)
		out = append(out, n)
	}
	return out
}

// ResourcesByKind returns all nodes of the given kind in insertion order.
func (ig *InfraGraph) ResourcesByKind(kind Kind) []*Resource {
	var out []*Resource
	for _, id := range ig.order {
		if n, _ := ig.node(id); n.Kind == kind {
			out = append(out, n)
		}
	}
	return out
}

// Edges returns all edges in insertion order.
func (ig *InfraGraph) Edges() []*Edge {
	out := make([]*Edge, 0, len(ig.edgeSeq))
	for _, k := range ig.edgeSeq {
		out = append(out, ig.edges[k])
	}
	return out
}

// OutEdges returns edges leaving the node, optionally filtered by relation.
func (ig *InfraGraph) OutEdges(id string, relations ...Relation) []*Edge {
	return ig.filterEdges(ig.out[id], relations)
}

// InEdges returns edges entering the node, optionally filtered by relation.
func (ig *InfraGraph) InEdges(id string, relations ...Relation) []*Edge {
	return ig.filterEdges(ig.in[id], relations)
}

func (ig *InfraGraph) filterEdges(keys []edgeKey, relations []Relation) []*Edge {
	var out []*Edge
	for _, k := range keys {
		e := ig.edges[k]
		if len(relations) == 0 {
			out = append(out, e)
			continue
		}
		for _, rel := range relations {
			if e.Relation == rel {
				out = append(out, e)
				break
			}
		}
	}
	return out
}

// NodeCount and EdgeCount expose graph size.
func (ig *InfraGraph) NodeCount() int { return len(ig.order) }
func (ig *InfraGraph) EdgeCount() int { return len(ig.edgeSeq) }

// Directed exposes the underlying gonum graph for algorithms (paths,
// traversal, connectivity). Node payloads are resolved via GonumResource.
func (ig *InfraGraph) Directed() gograph.Directed { return ig.g }

// GonumResource maps a gonum node ID back to its Resource.
func (ig *InfraGraph) GonumResource(gonumID int64) *Resource { return ig.nodes[gonumID] }

type gonumNode struct{ id int64 }

func (n gonumNode) ID() int64 { return n.id }
