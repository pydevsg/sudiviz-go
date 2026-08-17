// Package static exports the topology graph as PNG/SVG via the graphviz `dot`
// binary (same approach as the Python version).
package static

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/pydevsg/sudiviz-go/internal/graph"
)

var kindShape = map[graph.Kind]string{
	graph.KindALB:           "box3d",
	graph.KindTargetGroup:   "component",
	graph.KindInstance:      "box",
	graph.KindSecurityGroup: "diamond",
	graph.KindVPC:           "rectangle",
	graph.KindECSCluster:    "tab",
	graph.KindECSService:    "note",
	graph.KindEKSCluster:    "hexagon",
	graph.KindEKSNodeGroup:  "parallelogram",
	graph.KindRDS:           "cylinder",
	graph.KindAurora:        "cylinder",
	graph.KindLambda:        "invtriangle",
	graph.KindS3:            "folder",
}

// Export writes a PNG or SVG using the `dot` binary on PATH.
func Export(g *graph.InfraGraph, filename string) (string, error) {
	format := "png"
	ext := strings.ToLower(filepath.Ext(filename))
	switch ext {
	case ".svg":
		format = "svg"
	case ".png", ".dot", "":
		format = "png"
		if ext == "" {
			filename += ".png"
			ext = ".png"
		}
		if ext == ".dot" {
			format = "png"
		}
	default:
		format = strings.TrimPrefix(ext, ".")
	}

	if _, err := exec.LookPath("dot"); err != nil {
		return "", fmt.Errorf("graphviz `dot` is not on PATH — install graphviz to export %s", format)
	}

	dotSrc := BuildDOT(g)
	cmd := exec.Command("dot", "-T"+format, "-o", filename)
	cmd.Stdin = strings.NewReader(dotSrc)
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("dot render: %w", err)
	}
	return filename, nil
}

// BuildDOT returns the Graphviz DOT source for the topology.
func BuildDOT(g *graph.InfraGraph) string {
	var b strings.Builder
	b.WriteString("digraph sudiviz {\n")
	b.WriteString("  rankdir=LR;\n")
	b.WriteString("  bgcolor=white;\n")
	b.WriteString("  node [fontname=Helvetica, style=filled, color=\"#1f2937\", fontcolor=\"#1f2937\"];\n")
	b.WriteString("  edge [fontname=Helvetica, fontsize=10];\n")

	for _, n := range g.Resources() {
		shape := kindShape[n.Kind]
		if shape == "" {
			shape = "ellipse"
		}
		label := fmt.Sprintf("%s\\n[%s]", escape(n.Label), n.Kind)
		switch n.Kind {
		case graph.KindTargetGroup:
			label += fmt.Sprintf("\\n%d/%d healthy", n.AttrInt("healthy_count"), n.AttrInt("total_count"))
		case graph.KindECSService:
			label += fmt.Sprintf("\\n%d/%d running", n.AttrInt("running_count"), n.AttrInt("desired_count"))
		case graph.KindEKSNodeGroup:
			label += fmt.Sprintf("\\n%d nodes", n.AttrInt("desired_size"))
		case graph.KindRDS:
			label += fmt.Sprintf("\\n%s [%s]", escape(n.AttrString("engine")), escape(n.AttrString("status")))
		case graph.KindLambda:
			label += "\\n" + escape(n.AttrString("runtime"))
		}
		fmt.Fprintf(&b, "  %s [label=\"%s\", shape=%s, fillcolor=\"%s\"];\n",
			quote(n.ID), label, shape, fill(n))
	}
	for _, e := range g.Edges() {
		color, style, penwidth := "#374151", "solid", "1"
		if e.Orphan {
			color, style, penwidth = "#dc2626", "dashed", "2"
		}
		fmt.Fprintf(&b, "  %s -> %s [label=\"%s\", color=\"%s\", style=%s, penwidth=%s];\n",
			quote(e.From), quote(e.To), escape(string(e.Relation)), color, style, penwidth)
	}
	b.WriteString("}\n")
	return b.String()
}

func fill(n *graph.Resource) string {
	if n.Orphan {
		return "#fee2e2"
	}
	switch n.Health {
	case graph.HealthHealthy:
		return "#dcfce7"
	case graph.HealthUnhealthy:
		return "#fecaca"
	case graph.HealthInitial:
		return "#fef9c3"
	case graph.HealthDraining:
		return "#fed7aa"
	default:
		return "#e5e7eb"
	}
}

func quote(id string) string {
	return `"` + strings.ReplaceAll(id, `"`, `'`) + `"`
}

func escape(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `'`)
	s = strings.ReplaceAll(s, "\n", `\n`)
	return s
}
