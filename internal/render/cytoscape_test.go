package render

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/pydevsg/sudiviz-go/internal/graph"
)

func TestExportCytoscape(t *testing.T) {
	g := graph.New()
	g.AccountID, g.Region = "111", "us-east-1"
	g.AddResource(graph.Resource{ID: "alb", Kind: graph.KindALB, Label: "public", Health: graph.HealthHealthy, MonthlyCost: 22})
	g.AddResource(graph.Resource{ID: "tg", Kind: graph.KindTargetGroup, Label: "web", Orphan: true})
	g.AddEdge(graph.Edge{From: "alb", To: "tg", Relation: graph.RelationForwardsTo, Orphan: true})
	cy := ExportCytoscape(g)
	require.Len(t, cy.Nodes, 2)
	require.Len(t, cy.Edges, 1)
	assert.Contains(t, cy.Nodes[0].Classes, "alb")
	assert.Contains(t, cy.Edges[0].Classes, "orphan")
	assert.Equal(t, "us-east-1", cy.Meta["region"])
	assert.NotEmpty(t, cy.Nodes[0].Data["console_url"])
}
