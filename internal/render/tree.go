package render

import (
	"fmt"
	"io"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/pydevsg/sudiviz-go/internal/graph"
)

var (
	styleHealthy   = lipgloss.NewStyle().Foreground(lipgloss.Color("10"))
	styleWarn      = lipgloss.NewStyle().Foreground(lipgloss.Color("11"))
	styleUnhealthy = lipgloss.NewStyle().Foreground(lipgloss.Color("9"))
	styleMuted     = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	styleOrphan    = lipgloss.NewStyle().Foreground(lipgloss.Color("9")).Bold(true)
	styleTitle     = lipgloss.NewStyle().Foreground(lipgloss.Color("14")).Bold(true)
	styleBranch    = lipgloss.NewStyle().Foreground(lipgloss.Color("13")).Bold(true)
)

// WriteTree prints the topology as an indented tree (Rich-tree parity).
func WriteTree(w io.Writer, g *graph.InfraGraph) {
	fmt.Fprintln(w, styleTitle.Render("Topology"))
	writeALBTree(w, g)
	writeKindTree(w, g, graph.KindECSCluster, "ECS", graph.RelationContains)
	writeKindTree(w, g, graph.KindEKSCluster, "EKS", graph.RelationContains)
	writeFlatKind(w, g, graph.KindRDS, "RDS")
	writeFlatKind(w, g, graph.KindLambda, "Lambda")
	writeFlatKind(w, g, graph.KindS3, "S3")

	var orphans []*graph.Resource
	for _, n := range g.Resources() {
		if n.Orphan {
			orphans = append(orphans, n)
		}
	}
	if len(orphans) > 0 {
		fmt.Fprintln(w, "  "+styleOrphan.Render("ORPHANS"))
		for _, n := range orphans {
			fmt.Fprintf(w, "    %s\n", styleOrphan.Render(fmt.Sprintf("╌╌ %s: %s (%s)", n.Kind, n.Label, n.ID)))
		}
	}
	region := g.Region
	if region == "" {
		region = "unknown"
	}
	fmt.Fprintf(w, "\n  %s\n", styleMuted.Render("region: "+region))
}

func writeALBTree(w io.Writer, g *graph.InfraGraph) {
	for _, alb := range g.ResourcesByKind(graph.KindALB) {
		fmt.Fprintf(w, "  %s\n", nodeLine(alb))
		for _, e := range g.OutEdges(alb.ID, graph.RelationForwardsTo) {
			tg := g.Resource(e.To)
			if tg == nil {
				continue
			}
			fmt.Fprintf(w, "    %s %s\n", edgeMarker(e), nodeLine(tg))
			for _, reg := range g.InEdges(tg.ID, graph.RelationRegisteredIn) {
				inst := g.Resource(reg.From)
				if inst == nil {
					continue
				}
				fmt.Fprintf(w, "      %s %s\n", edgeMarker(reg), nodeLine(inst))
			}
		}
	}
}

func writeKindTree(w io.Writer, g *graph.InfraGraph, kind graph.Kind, title string, rel graph.Relation) {
	roots := g.ResourcesByKind(kind)
	if len(roots) == 0 {
		return
	}
	fmt.Fprintf(w, "  %s\n", styleBranch.Render(title))
	for _, root := range roots {
		fmt.Fprintf(w, "    %s\n", nodeLine(root))
		for _, e := range g.OutEdges(root.ID, rel) {
			child := g.Resource(e.To)
			if child == nil {
				continue
			}
			fmt.Fprintf(w, "      %s %s\n", edgeMarker(e), nodeLine(child))
		}
	}
}

func writeFlatKind(w io.Writer, g *graph.InfraGraph, kind graph.Kind, title string) {
	nodes := g.ResourcesByKind(kind)
	if len(nodes) == 0 {
		return
	}
	fmt.Fprintf(w, "  %s\n", styleBranch.Render(title))
	for _, n := range nodes {
		fmt.Fprintf(w, "    %s\n", nodeLine(n))
	}
}

func nodeLine(n *graph.Resource) string {
	style := healthStyle(n)
	suffix := ""
	switch n.Kind {
	case graph.KindTargetGroup:
		suffix = fmt.Sprintf(" [%d/%d]", n.AttrInt("healthy_count"), n.AttrInt("total_count"))
	case graph.KindECSService:
		suffix = fmt.Sprintf(" [%d/%d]", n.AttrInt("running_count"), n.AttrInt("desired_count"))
	}
	region := ""
	if n.Region != "" {
		region = styleMuted.Render(" (" + n.Region + ")")
	}
	return style.Render(fmt.Sprintf("%s: %s%s", n.Kind, n.Label, suffix)) + region
}

func healthStyle(n *graph.Resource) lipgloss.Style {
	if n.Orphan {
		return styleOrphan
	}
	switch n.Health {
	case graph.HealthHealthy:
		return styleHealthy
	case graph.HealthUnhealthy:
		return styleUnhealthy
	case graph.HealthInitial, graph.HealthDraining:
		return styleWarn
	default:
		return styleMuted
	}
}

func edgeMarker(e *graph.Edge) string {
	if e.Orphan || e.Style == "dashed" {
		return styleOrphan.Render("╌╌▶")
	}
	return styleMuted.Render("──▶")
}

// WriteDiagnosis prints a severity-sorted findings table.
func WriteDiagnosis(w io.Writer, findings []struct{ Severity, Title, Detail string }, emptyMsg string) {
	if len(findings) == 0 {
		fmt.Fprintln(w, styleHealthy.Render(emptyMsg))
		return
	}
	fmt.Fprintln(w)
	fmt.Fprintln(w, styleTitle.Render("Diagnosis"))
	fmt.Fprintf(w, "  %-10s  %-52s  %s\n", "SEVERITY", "TITLE", "DETAIL")
	fmt.Fprintf(w, "  %s\n", strings.Repeat("─", 100))
	for _, f := range findings {
		sev := colorSeverity(f.Severity).Render(strings.ToUpper(f.Severity))
		fmt.Fprintf(w, "  %-18s  %-52s  %s\n", sev, truncate(f.Title, 52), truncate(f.Detail, 80))
	}
}

func colorSeverity(s string) lipgloss.Style {
	switch s {
	case "critical":
		return styleUnhealthy.Bold(true)
	case "warning":
		return styleWarn
	case "info":
		return lipgloss.NewStyle().Foreground(lipgloss.Color("12"))
	default:
		return lipgloss.NewStyle()
	}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}
