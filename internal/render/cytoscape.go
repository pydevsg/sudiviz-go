// Package render converts an InfraGraph into Cytoscape.js JSON, a terminal
// tree, and a serializable snapshot used by --json output.
package render

import (
	"time"

	"github.com/pydevsg/sudiviz-go/internal/awsurl"
	"github.com/pydevsg/sudiviz-go/internal/discovery"
	"github.com/pydevsg/sudiviz-go/internal/graph"
)

const (
	colorHealthy = "#22c55e"
	colorOrphan  = "#dc2626"
	colorIngress = "#3b82f6"
	colorEgress  = "#8b5cf6"
)

// GraphJSON is the --json graph payload (Python serialize_graph parity).
type GraphJSON struct {
	Meta  map[string]any   `json:"meta"`
	Nodes []map[string]any `json:"nodes"`
	Edges []map[string]any `json:"edges"`
}

// SerializeGraph flattens the topology for CLI --json output.
func SerializeGraph(g *graph.InfraGraph) GraphJSON {
	meta := map[string]any{
		"account_id":    g.AccountID,
		"region":        g.Region,
		"vpc_id":        g.VPCID,
		"discovered_at": g.DiscoveredAt.Format(time.RFC3339),
	}
	nodes := make([]map[string]any, 0, g.NodeCount())
	for _, n := range g.Resources() {
		nodes = append(nodes, map[string]any{
			"id":           n.ID,
			"kind":         n.Kind,
			"label":        n.Label,
			"provider":     n.Provider,
			"region":       n.Region,
			"az":           n.AZ,
			"health":       n.Health,
			"orphan":       n.Orphan,
			"monthly_cost": n.MonthlyCost,
			"tags":         n.Tags,
			"metadata":     n.Attrs,
		})
	}
	edges := make([]map[string]any, 0, g.EdgeCount())
	for _, e := range g.Edges() {
		item := map[string]any{
			"source":   e.From,
			"target":   e.To,
			"relation": e.Relation,
			"style":    e.Style,
			"orphan":   e.Orphan,
		}
		for k, v := range e.Attrs {
			item[k] = v
		}
		edges = append(edges, item)
	}
	return GraphJSON{Meta: meta, Nodes: nodes, Edges: edges}
}

// Cytoscape is the payload consumed by the embedded frontend.
type Cytoscape struct {
	Nodes []CyElement    `json:"nodes"`
	Edges []CyElement    `json:"edges"`
	Meta  map[string]any `json:"meta"`
}

// CyElement is one Cytoscape.js element (node or edge).
type CyElement struct {
	Data    map[string]any `json:"data"`
	Classes string         `json:"classes,omitempty"`
	Style   map[string]any `json:"style,omitempty"`
}

// ExportCytoscape converts the graph to Cytoscape.js elements format.
func ExportCytoscape(g *graph.InfraGraph) Cytoscape {
	region := g.Region
	if region == "" {
		region = "us-east-1"
	}
	out := Cytoscape{
		Nodes: make([]CyElement, 0, g.NodeCount()),
		Edges: make([]CyElement, 0, g.EdgeCount()),
		Meta: map[string]any{
			"account_id":    g.AccountID,
			"region":        region,
			"vpc_id":        g.VPCID,
			"discovered_at": g.DiscoveredAt.Format(time.RFC3339),
		},
	}
	for _, n := range g.Resources() {
		classes := string(n.Kind) + " " + string(n.Health)
		if n.Orphan {
			classes += " orphan"
		}
		costDisplay := ""
		if n.MonthlyCost > 0 {
			costDisplay = discovery.FormatCost(n.MonthlyCost)
		}
		out.Nodes = append(out.Nodes, CyElement{
			Data: map[string]any{
				"id":           n.ID,
				"label":        n.Label,
				"kind":         n.Kind,
				"health":       n.Health,
				"orphan":       n.Orphan,
				"monthly_cost": n.MonthlyCost,
				"cost_display": costDisplay,
				"console_url":  awsurl.Console(string(n.Kind), n.ID, region),
				"pricing_url":  awsurl.Pricing(string(n.Kind), n.Attrs),
				"metrics_url":  awsurl.Metrics(string(n.Kind), n.ID, region),
				"logs_url":     awsurl.Logs(string(n.Kind), n.ID, region),
				"metadata":     n.Attrs,
			},
			Classes: classes,
		})
	}
	for _, e := range g.Edges() {
		classes := ""
		if e.Orphan {
			classes = "orphan"
		}
		rel := string(e.Relation)
		if rel == "allows_ingress" || rel == "allows_egress" {
			if classes != "" {
				classes += " "
			}
			classes += "sg-flow sg-" + e.AttrString("direction")
		}
		line := colorHealthy
		switch {
		case rel == "allows_ingress":
			line = colorIngress
		case rel == "allows_egress":
			line = colorEgress
		case e.Orphan:
			line = colorOrphan
		}
		style := "solid"
		if e.Style != "" {
			style = e.Style
		}
		data := map[string]any{
			"id":       e.From + "__" + e.To,
			"source":   e.From,
			"target":   e.To,
			"relation": rel,
			"orphan":   e.Orphan,
		}
		if rel == "allows_ingress" || rel == "allows_egress" {
			data["protocol"] = e.AttrString("protocol")
			data["from_port"] = e.AttrInt("from_port")
			data["to_port"] = e.AttrInt("to_port")
			data["direction"] = e.AttrString("direction")
		}
		out.Edges = append(out.Edges, CyElement{
			Data:    data,
			Classes: classes,
			Style: map[string]any{
				"line-style":         style,
				"line-color":         line,
				"target-arrow-color": line,
			},
		})
	}
	return out
}
