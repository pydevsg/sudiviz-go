package diagnose

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/pydevsg/sudiviz-go/internal/graph"
)

func TestMarkOrphanedEdges(t *testing.T) {
	g := graph.New()
	g.AddResource(graph.Resource{ID: "tg", Kind: graph.KindTargetGroup, Label: "x", Health: graph.HealthHealthy})
	g.AddResource(graph.Resource{ID: "i", Kind: graph.KindInstance, Label: "y"})
	g.AddEdge(graph.Edge{From: "i", To: "sg", Relation: graph.RelationGuardedBy})
	MarkOrphanedEdges(g)
	assert.True(t, g.Resource("tg").Orphan)
	assert.Equal(t, graph.HealthUnhealthy, g.Resource("tg").Health)
	assert.True(t, g.Resource("i").Orphan)
}

func TestHasCritical(t *testing.T) {
	d := &Diagnosis{Fixes: []Finding{{Severity: SeverityWarning}}}
	assert.False(t, d.HasCritical())
	d.Fixes = append(d.Fixes, Finding{Severity: SeverityCritical})
	assert.True(t, d.HasCritical())
}
